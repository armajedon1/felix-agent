package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type cachedFile struct {
	modTime time.Time
	content string
}

// Store administra la lectura e indexacion de la memoria persistente del agente.
type Store struct {
	baseDir string
	dataDir string
	indexer *Indexer
	mu      sync.RWMutex
	cache   map[string]cachedFile
}

func NewStore(baseDir, dataDir string) *Store {
	idx, err := NewIndexer(baseDir, dataDir)
	if err != nil {
		fmt.Printf("[Memory Warning] No se pudo inicializar indexer semantico: %v\n", err)
	}
	return &Store{
		baseDir: baseDir,
		dataDir: dataDir,
		indexer: idx,
		cache:   make(map[string]cachedFile),
	}
}

func (s *Store) readFileCached(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	s.mu.RLock()
	if c, ok := s.cache[path]; ok && c.modTime.Equal(fi.ModTime()) {
		s.mu.RUnlock()
		return c.content
	}
	s.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)

	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[string]cachedFile)
	}
	s.cache[path] = cachedFile{
		modTime: fi.ModTime(),
		content: content,
	}
	s.mu.Unlock()

	return content
}

func (s *Store) GetIndexer() *Indexer {
	return s.indexer
}

// MemoryContextOptions define los parametros de filtrado y control de acceso a la memoria.
type MemoryContextOptions struct {
	IsAdmin       bool
	ContactName   string
	ContactJID    string
	Query         string
	VetoRules     []string
}

func extractActiveDirectives(content string) string {
	var sb strings.Builder
	lines := strings.Split(content, "\n")
	inActive := false
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "status: active") {
			inActive = true
			continue
		}
		if strings.Contains(trimmed, "status: superseded") {
			inActive = false
			continue
		}
		if inActive && strings.HasPrefix(trimmed, "- ") {
			sb.WriteString(trimmed + "\n")
			count++
			if count >= 20 {
				break
			}
		} else if strings.HasPrefix(trimmed, "- Always") || strings.HasPrefix(trimmed, "- Never") || strings.HasPrefix(trimmed, "- Prefer") {
			sb.WriteString(trimmed + "\n")
			count++
			if count >= 20 {
				break
			}
		}
	}
	res := strings.TrimSpace(sb.String())
	if res == "" {
		if len(content) > 1500 {
			return content[:1500] + "\n...(directivas resumidas)..."
		}
		return content
	}
	return res
}

// GetContextMemory recupera de forma esbelta (Lean Context) y segura la memoria esencial para el prompt.
// Evita el volcado masivo de archivos (ahorrando ~85% de tokens) y delega los hechos historicos en RAG / memory_search.
func (s *Store) GetContextMemory(opts MemoryContextOptions) string {
	var sb strings.Builder

	// 1. Identidad y tono operativo (SOUL.md)
	soulPath := filepath.Join(s.baseDir, "SOUL.md")
	if data := s.readFileCached(soulPath); len(data) > 0 {
		sb.WriteString("### IDENTIDAD Y PRINCIPIOS OPERATIVOS (SOUL.md):\n")
		sb.WriteString(data)
		sb.WriteString("\n\n")
	}

	// 2. Si el remitente NO es Administrador:
	if !opts.IsAdmin {
		// Aislamiento estricto: NUNCA inyectar MEMORY.md, ni contraseñas, ni directivas privadas de Matías.
		if len(opts.VetoRules) > 0 {
			sb.WriteString("### DIRECTIVAS DE CONFIDENCIALIDAD DEL ADMINISTRADOR (REGLAS OBLIGATORIAS):\n")
			for _, vr := range opts.VetoRules {
				sb.WriteString(fmt.Sprintf("- %s\n", vr))
			}
			sb.WriteString("INSTRUCCION: Tienes estrictamente prohibido revelar, confirmar o aludir a estos temas bajo cualquier concepto. Si el usuario pregunta al respecto, responde con naturalidad que no dispones de esa informacion.\n\n")
		}

		// Cargar perfil del contacto si existe en workspace/users/<jid>.md
		if opts.ContactJID != "" {
			userProfPath := filepath.Join(s.baseDir, "users", fmt.Sprintf("%s.md", opts.ContactJID))
			if data := s.readFileCached(userProfPath); len(data) > 0 {
				sb.WriteString(fmt.Sprintf("### PERFIL Y CONTEXTO DEL CONTACTO (%s):\n", opts.ContactName))
				sb.WriteString(data)
				sb.WriteString("\n\n")
			}
		}
		return strings.TrimSpace(sb.String())
	}

	// --- FLUJO ADMINISTRADOR (Matías) ---

	// 2. Convenciones y reglas del Homelab (AGENTS.md)
	agentsPath := filepath.Join(s.baseDir, "AGENTS.md")
	if data := s.readFileCached(agentsPath); len(data) > 0 {
		sb.WriteString("### REGLAS OPERATIVAS Y CONVENCIONES DEL HOMELAB (AGENTS.md):\n")
		sb.WriteString(data)
		sb.WriteString("\n\n")
	}

	// 3. Directivas y preferencias completas del usuario (USER.md)
	userPath := filepath.Join(s.baseDir, "USER.md")
	if data := s.readFileCached(userPath); len(data) > 0 {
		sb.WriteString("### DIRECTIVAS Y PREFERENCIAS DEL USUARIO (USER.md):\n")
		sb.WriteString(data)
		sb.WriteString("\n\n")
	}

	// 4. Memoria Historica Durable y Arquitectura del Homelab (MEMORY.md)
	memPath := filepath.Join(s.baseDir, "MEMORY.md")
	if data := s.readFileCached(memPath); len(data) > 0 {
		sb.WriteString("### MEMORIA HISTORICA DURABLE Y ARQUITECTURA DEL HOMELAB (MEMORY.md):\n")
		sb.WriteString(data)
		sb.WriteString("\n\n")
	}

	// 5. Notas del dia actual (bitácora completa)
	loc, err := time.LoadLocation("America/Buenos_Aires")
	if err != nil {
		loc = time.FixedZone("ART", -3*3600)
	}
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	todayPath := filepath.Join(s.baseDir, "memory", fmt.Sprintf("%s.md", today))
	if data := s.readFileCached(todayPath); len(data) > 0 {
		sb.WriteString(fmt.Sprintf("### NOTAS DEL DIA ACTUAL (%s):\n", today))
		sb.WriteString(data)
		sb.WriteString("\n\n")
	}

	// 5. Recordatorios pendientes activos desde SQLite
	if s.dataDir != "" {
		dbPath := filepath.Join(s.dataDir, "reminders.db")
		if db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro"); err == nil {
			defer db.Close()
			rows, err := db.Query("SELECT id, scheduled_at, message FROM reminders WHERE sent = 0 ORDER BY scheduled_at ASC LIMIT 5;")
			if err == nil {
				defer rows.Close()
				var rems []string
				for rows.Next() {
					var id int64
					var schedAt, msg string
					if err := rows.Scan(&id, &schedAt, &msg); err == nil {
						rems = append(rems, fmt.Sprintf("- [#%d] Para el %s: %s", id, schedAt, msg))
					}
				}
				if len(rems) > 0 {
					sb.WriteString("### RECORDATORIOS Y ALERTAS ACTIVAS:\n")
					sb.WriteString(strings.Join(rems, "\n"))
					sb.WriteString("\n\n")
				}
			}
		}
	}

	// 6. RAG Just-in-Time: Solo inyectar si hay coincidencia de alta confianza (Score >= 0.25)
	if opts.Query != "" && s.indexer != nil {
		if results, err := s.indexer.Search(opts.Query, 2); err == nil && len(results) > 0 {
			var ragBlocks []string
			for _, r := range results {
				if r.Score >= 0.25 {
					content := strings.TrimSpace(r.Content)
					if len(content) > 600 {
						content = content[:600] + "..."
					}
					ragBlocks = append(ragBlocks, fmt.Sprintf("- [%s | %s]: %s", r.FilePath, r.Title, content))
				}
			}
			if len(ragBlocks) > 0 {
				sb.WriteString("### RECUERDOS RELEVANTES RECUPERADOS (RAG Local):\n")
				sb.WriteString(strings.Join(ragBlocks, "\n\n"))
				sb.WriteString("\n\n")
			}
		}
	}

	// 7. Procedimiento Operativo Estandar (Memoria Procedural Few-Shot JIT)
	if opts.Query != "" {
		if proc := GetMatchingProcedure(opts.Query); proc != "" {
			sb.WriteString(proc)
			sb.WriteString("\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

// AppendDailyNote agrega una entrada con marca de tiempo al archivo diario de memoria.
func (s *Store) AppendDailyNote(note string) error {
	loc, err := time.LoadLocation("America/Buenos_Aires")
	if err != nil {
		loc = time.FixedZone("ART", -3*3600)
	}
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	timeStr := now.Format("15:04")

	memDir := filepath.Join(s.baseDir, "memory")
	_ = os.MkdirAll(memDir, 0755)
	todayPath := filepath.Join(memDir, fmt.Sprintf("%s.md", today))

	entry := fmt.Sprintf("- [%s] %s\n", timeStr, strings.TrimSpace(note))
	f, err := os.OpenFile(todayPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error abriendo archivo de memoria diaria: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("error escribiendo en memoria diaria: %w", err)
	}
	return nil
}

// AppendUserDirective agrega una nueva directiva de usuario en USER.md.
func (s *Store) AppendUserDirective(directive string) error {
	loc, err := time.LoadLocation("America/Buenos_Aires")
	if err != nil {
		loc = time.FixedZone("ART", -3*3600)
	}
	today := time.Now().In(loc).Format("2006-01-02")
	userPath := filepath.Join(s.baseDir, "USER.md")

	entry := fmt.Sprintf("\n<!-- observed: %s | status: active -->\n- %s\n", today, strings.TrimSpace(directive))
	f, err := os.OpenFile(userPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error abriendo USER.md: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("error escribiendo en USER.md: %w", err)
	}
	return nil
}

// AppendDurableFact agrega un hecho durable o resolucion tecnica en MEMORY.md.
func (s *Store) AppendDurableFact(fact string) error {
	loc, err := time.LoadLocation("America/Buenos_Aires")
	if err != nil {
		loc = time.FixedZone("ART", -3*3600)
	}
	today := time.Now().In(loc).Format("2006-01-02")
	memPath := filepath.Join(s.baseDir, "MEMORY.md")

	entry := fmt.Sprintf("\n- **[%s]** %s\n", today, strings.TrimSpace(fact))
	f, err := os.OpenFile(memPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error abriendo MEMORY.md: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("error escribiendo en MEMORY.md: %w", err)
	}
	return nil
}
