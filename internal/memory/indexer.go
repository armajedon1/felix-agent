package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SearchResult modela un fragmento de memoria recuperado por relevancia semantica.
type SearchResult struct {
	ID       int64
	FilePath string
	Title    string
	Content  string
	Score    float64
}

// Indexer administra la indexacion y busqueda hibrida (FTS5 + BM25 + Similitud Semantica) de la memoria persistente.
type Indexer struct {
	mu       sync.Mutex
	db       *sql.DB
	baseDir  string
	dbPath   string
	lastSync time.Time
}

// NewIndexer inicializa la base de datos de busqueda semantica local.
func NewIndexer(baseDir, dataDir string) (*Indexer, error) {
	_ = os.MkdirAll(dataDir, 0755)
	dbPath := filepath.Join(dataDir, "memory_index.db")

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("error abriendo memory_index.db: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		file_path TEXT NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		mod_time INTEGER NOT NULL,
		hash TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_chunks_file ON chunks(file_path);

	CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
		content,
		title,
		file_path,
		content='chunks',
		content_rowid='id'
	);

	CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
		INSERT INTO chunks_fts(rowid, content, title, file_path) VALUES (new.id, new.content, new.title, new.file_path);
	END;

	CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
		INSERT INTO chunks_fts(chunks_fts, rowid, content, title, file_path) VALUES('delete', old.id, old.content, old.title, old.file_path);
	END;

	CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
		INSERT INTO chunks_fts(chunks_fts, rowid, content, title, file_path) VALUES('delete', old.id, old.content, old.title, old.file_path);
		INSERT INTO chunks_fts(rowid, content, title, file_path) VALUES (new.id, new.content, new.title, new.file_path);
	END;
	`

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("error inicializando esquema de indice semantico: %w", err)
	}

	idx := &Indexer{
		db:      db,
		baseDir: baseDir,
		dbPath:  dbPath,
	}

	// Sincronizar indice inicial en segundo plano
	go func() {
		count, err := idx.Sync()
		if err != nil {
			log.Printf("[Memory Indexer Error] Sincronizacion inicial: %v", err)
		} else {
			log.Printf("[Memory Indexer] Indice semantico sincronizado: %d fragmentos activos en %s", count, dbPath)
		}
	}()

	return idx, nil
}

// Sync escanea el workspace y actualiza los fragmentos modificados o nuevos.
func (idx *Indexer) Sync() (int, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var filesToIndex []string

	// 1. Archivos raiz del workspace
	rootFiles := []string{"USER.md", "MEMORY.md", "SOUL.md", "PROYECTOS_E_IDEAS.md", "IDENTITY.md"}
	for _, rf := range rootFiles {
		p := filepath.Join(idx.baseDir, rf)
		if _, err := os.Stat(p); err == nil {
			filesToIndex = append(filesToIndex, p)
		}
	}

	// 2. Archivos diarios en memory/
	memDir := filepath.Join(idx.baseDir, "memory")
	if entries, err := os.ReadDir(memDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				filesToIndex = append(filesToIndex, filepath.Join(memDir, e.Name()))
			}
		}
	}

	for _, path := range filesToIndex {
		if err := idx.indexFile(path); err != nil {
			log.Printf("[Memory Indexer Warning] Error indexando %s: %v", path, err)
		}
	}

	// 3. Purgar fragmentos huerfanos de archivos eliminados en disco
	if rows, err := idx.db.Query("SELECT DISTINCT file_path FROM chunks;"); err == nil {
		var orphans []string
		for rows.Next() {
			var fp string
			if err := rows.Scan(&fp); err == nil {
				fullPath := filepath.Join(idx.baseDir, fp)
				if _, err := os.Stat(fullPath); os.IsNotExist(err) {
					orphans = append(orphans, fp)
				}
			}
		}
		rows.Close()

		for _, o := range orphans {
			_, _ = idx.db.Exec("DELETE FROM chunks WHERE file_path = ?;", o)
			log.Printf("[Memory Indexer] Purgados fragmentos huerfanos de archivo eliminado: %s", o)
		}
	}

	idx.lastSync = time.Now()

	var totalChunks int
	_ = idx.db.QueryRow("SELECT COUNT(*) FROM chunks;").Scan(&totalChunks)
	return totalChunks, nil
}

func (idx *Indexer) indexFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	h := sha256.Sum256(contentBytes)
	hashStr := hex.EncodeToString(h[:])
	relPath, _ := filepath.Rel(idx.baseDir, path)
	if relPath == "" {
		relPath = filepath.Base(path)
	}

	// Verificar si el archivo ya esta indexado con el mismo hash
	var existingHash string
	err = idx.db.QueryRow("SELECT hash FROM chunks WHERE file_path = ? LIMIT 1;", relPath).Scan(&existingHash)
	if err == nil && existingHash == hashStr {
		return nil // Sin cambios
	}

	// Eliminar chunks viejos del archivo
	_, _ = idx.db.Exec("DELETE FROM chunks WHERE file_path = ?;", relPath)

	// Fragmentar archivo en secciones semanticas
	chunks := chunkMarkdown(relPath, string(contentBytes))
	tx, err := idx.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO chunks (file_path, title, content, mod_time, hash) VALUES (?, ?, ?, ?, ?);")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, ch := range chunks {
		if strings.TrimSpace(ch.content) == "" {
			continue
		}
		if _, err := stmt.Exec(relPath, ch.title, ch.content, fi.ModTime().Unix(), hashStr); err != nil {
			return err
		}
	}

	return tx.Commit()
}

type rawChunk struct {
	title   string
	content string
}

func chunkMarkdown(filePath, text string) []rawChunk {
	lines := strings.Split(text, "\n")
	var chunks []rawChunk

	currentTitle := filePath
	var currentLines []string

	headerRegex := regexp.MustCompile(`^(#{1,4})\s+(.+)$`)

	flush := func() {
		block := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if len(block) > 20 {
			chunks = append(chunks, rawChunk{
				title:   currentTitle,
				content: block,
			})
		}
		currentLines = nil
	}

	for _, line := range lines {
		if match := headerRegex.FindStringSubmatch(line); match != nil {
			flush()
			currentTitle = match[2]
			currentLines = append(currentLines, line)
		} else {
			currentLines = append(currentLines, line)
			// Si el parrafo supera 1500 caracteres, dividir en sub-chunk para mantener granularidad fina
			if len(strings.Join(currentLines, "\n")) > 1500 {
				flush()
			}
		}
	}
	flush()

	return chunks
}

// Search busca de forma hibrida los fragmentos mas relevantes para una consulta dada.
func (idx *Indexer) Search(queryStr string, limit int) ([]SearchResult, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if limit <= 0 {
		limit = 5
	}

	tokens := tokenize(queryStr)
	if len(tokens) == 0 {
		return nil, nil
	}

	// Construir query para SQLite FTS5 (palabras con operador OR y comodin de prefijo)
	var ftsTerms []string
	for _, t := range tokens {
		if len(t) >= 2 {
			ftsTerms = append(ftsTerms, fmt.Sprintf("\"%s\"*", sanitizeFTS(t)))
		}
	}
	if len(ftsTerms) == 0 {
		return nil, nil
	}

	ftsQuery := strings.Join(ftsTerms, " OR ")

	sqlQuery := `
	SELECT c.id, c.file_path, c.title, c.content, bm25(chunks_fts) as rank
	FROM chunks_fts
	JOIN chunks c ON c.id = chunks_fts.rowid
	WHERE chunks_fts MATCH ?
	ORDER BY rank ASC
	LIMIT ?;
	`

	rows, err := idx.db.Query(sqlQuery, ftsQuery, limit*3)
	if err != nil {
		return nil, fmt.Errorf("error ejecutando consulta FTS5: %w", err)
	}
	defer rows.Close()

	var candidates []SearchResult
	queryVec := termVector(tokens)

	for rows.Next() {
		var r SearchResult
		var bm25Rank float64
		if err := rows.Scan(&r.ID, &r.FilePath, &r.Title, &r.Content, &bm25Rank); err != nil {
			continue
		}

		// Calcular similitud coseno sobre term-frequency entre consulta y fragmento
		chunkTokens := tokenize(r.Title + " " + r.Content)
		chunkVec := termVector(chunkTokens)
		cosine := cosineSimilarity(queryVec, chunkVec)

		// BM25 en SQLite devuelve valores negativos donde menor es mejor (-10.5 > -2.1).
		// Normalizamos la puntuacion hibrida: Score = CosineSimilarity + (BM25Normalizado)
		bm25Norm := 1.0 / (1.0 + math.Abs(bm25Rank))
		r.Score = (cosine * 0.6) + (bm25Norm * 0.4)

		candidates = append(candidates, r)
	}

	// Ordenar por puntuacion hibrida descendente
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	return candidates, nil
}

// GetStats retorna las metricas actuales del indice semantico.
func (idx *Indexer) GetStats() (int, time.Time, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var total int
	err := idx.db.QueryRow("SELECT COUNT(*) FROM chunks;").Scan(&total)
	return total, idx.lastSync, err
}

// Close cierra la conexion con SQLite.
func (idx *Indexer) Close() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.db != nil {
		return idx.db.Close()
	}
	return nil
}

func sanitizeFTS(term string) string {
	reg := regexp.MustCompile(`[^a-zA-Z0-9áéíóúÁÉÍÓÚñÑ]`)
	return reg.ReplaceAllString(term, "")
}

var homelabSynonyms = map[string][]string{
	"notebook":    {"workstation", "laptop", "pc"},
	"laptop":      {"workstation", "notebook", "pc"},
	"workstation": {"notebook", "laptop", "pc"},
	"caida":       {"falla", "incidente", "error", "crash"},
	"error":       {"falla", "incidente", "caida", "crash"},
	"crash":       {"falla", "incidente", "caida", "error"},
	"falla":       {"incidente", "caida", "error", "crash"},
	"incidente":   {"falla", "caida", "error", "crash"},
	"docker":      {"contenedor", "compose", "container"},
	"contenedor":  {"docker", "container"},
	"base":        {"db", "database", "sqlite"},
	"db":          {"base", "database", "sqlite"},
	"database":    {"db", "base", "sqlite"},
	"backup":      {"respaldo", "copia"},
	"respaldo":    {"backup", "copia"},
	"contrasenas": {"claves", "secretos", "vaultwarden"},
	"claves":      {"contrasenas", "secretos", "vaultwarden"},
}

func tokenize(text string) []string {
	reg := regexp.MustCompile(`[a-zA-Z0-9áéíóúÁÉÍÓÚñÑ]+`)
	matches := reg.FindAllString(strings.ToLower(text), -1)

	// Stopwords comunes en espanol
	stopwords := map[string]bool{
		"de": true, "la": true, "el": true, "en": true, "y": true, "a": true, "los": true,
		"que": true, "por": true, "para": true, "con": true, "un": true, "una": true,
		"es": true, "al": true, "lo": true, "como": true, "las": true, "su": true,
		"del": true, "se": true, "o": true, "te": true, "mi": true, "me": true,
	}

	synonymsAdded := make(map[string]bool)
	var res []string
	for _, m := range matches {
		if !stopwords[m] && len(m) > 1 {
			res = append(res, m)
			if syns, ok := homelabSynonyms[m]; ok {
				for _, syn := range syns {
					if !synonymsAdded[syn] {
						synonymsAdded[syn] = true
						res = append(res, syn)
					}
				}
			}
		}
	}
	return res
}

func termVector(tokens []string) map[string]float64 {
	vec := make(map[string]float64)
	for _, t := range tokens {
		vec[t]++
	}
	// Normalizar vector L2
	var sumSq float64
	for _, count := range vec {
		sumSq += count * count
	}
	norm := math.Sqrt(sumSq)
	if norm > 0 {
		for t := range vec {
			vec[t] /= norm
		}
	}
	return vec
}

func cosineSimilarity(v1, v2 map[string]float64) float64 {
	var dotProduct float64
	for term, w1 := range v1 {
		if w2, ok := v2[term]; ok {
			dotProduct += w1 * w2
		}
	}
	return dotProduct
}
