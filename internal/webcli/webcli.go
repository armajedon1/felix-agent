package webcli

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"felix-go/internal/botfactory"
	"felix-go/internal/config"
)

//go:embed assets/*
var assetsFS embed.FS

type Server struct {
	port       int
	storage    *botfactory.Storage
	cfg        *config.Config
	dataDir    string
	configPath string
	httpServer *http.Server
	listener   net.Listener
	mu         sync.RWMutex
	clients    map[chan botfactory.BotLogEntry]bool
	completer  botfactory.LLMCompleter
	botMgr     *botfactory.Manager
}

type SimpleCompleter struct {
	apiBase string
	apiKey  string
	model   string
	client  *http.Client
}

func NewSimpleCompleter(apiBase, apiKey, model string) *SimpleCompleter {
	if apiBase == "" {
		apiBase = "http://127.0.0.1:8045/v1"
	}
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &SimpleCompleter{
		apiBase: strings.TrimSuffix(apiBase, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 90 * time.Second},
	}
}

func (c *SimpleCompleter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	reqBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.2,
	}
	payload, _ := json.Marshal(reqBody)

	url := c.apiBase + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error conectando a endpoint LLM (%s): %w", url, err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error HTTP %d del LLM: %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", fmt.Errorf("error decodificando respuesta LLM: %w", err)
	}
	if len(res.Choices) == 0 {
		return "", fmt.Errorf("el LLM no retorno opciones de respuesta")
	}

	content := res.Choices[0].Message.Content
	for {
		start := strings.Index(content, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(content, "</think>")
		if end == -1 {
			break
		}
		content = strings.TrimSpace(content[:start] + content[end+len("</think>"):])
	}
	return strings.TrimSpace(content), nil
}

func NewServer(port int, storage *botfactory.Storage, cfg *config.Config, dataDir string) *Server {
	if port <= 0 {
		port = 8056
	}
	completer := NewSimpleCompleter(cfg.APIBase, cfg.APIKey, cfg.Model)
	botMgr := botfactory.NewManager(storage, completer)
	return &Server{
		port:      port,
		storage:   storage,
		cfg:       cfg,
		dataDir:   dataDir,
		clients:   make(map[chan botfactory.BotLogEntry]bool),
		completer: completer,
		botMgr:    botMgr,
	}
}

func (s *Server) SetConfigPath(p string) {
	s.configPath = p
}

func (s *Server) resolveConfigPath() string {
	if s.configPath != "" {
		return s.configPath
	}
	candidates := []string{
		"/home/matias/felix-server/config.json",
		"/home/matias/felix-go/config.json",
		"config.json",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "config.json"
}

func (s *Server) Start() (int, error) {
	mux := http.NewServeMux()

	// Servir archivos estaticos embebidos
	subFS, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return 0, fmt.Errorf("error cargando assets embebidos: %w", err)
	}
	fileServer := http.FileServer(http.FS(subFS))
	mux.Handle("/", fileServer)

	// Servir avatares locales
	avatarsDir := filepath.Join(s.dataDir, "avatars")
	_ = os.MkdirAll(avatarsDir, 0755)
	mux.Handle("/avatars/", http.StripPrefix("/avatars/", http.FileServer(http.Dir(avatarsDir))))

	// Endpoints REST - ABM Bots
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/bots", s.handleBots)
	mux.HandleFunc("/api/v1/bots/", s.handleBotRoute)

	// Endpoints REST - ABM Numeros
	mux.HandleFunc("/api/v1/numbers", s.handleNumbers)
	mux.HandleFunc("/api/v1/numbers/assign", s.handleAssignNumber)
	mux.HandleFunc("/api/v1/numbers/release", s.handleReleaseNumber)

	// Endpoints REST - ABM Base de Datos
	mux.HandleFunc("/api/v1/db/status", s.handleDBStatus)
	mux.HandleFunc("/api/v1/db/vacuum", s.handleDBVacuum)
	mux.HandleFunc("/api/v1/db/backup", s.handleDBBackup)
	mux.HandleFunc("/api/v1/db/backups", s.handleDBBackupsList)
	mux.HandleFunc("/api/v1/db/tables", s.handleDBTables)

	// Endpoints REST - ABM APIs e Inferencia
	mux.HandleFunc("/api/v1/apis", s.handleAPIs)
	mux.HandleFunc("/api/v1/apis/test", s.handleAPIsTest)

	// Endpoints REST - Auth y Logs
	mux.HandleFunc("/api/v1/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/v1/logs/bots", s.handleLogs)
	mux.HandleFunc("/api/v1/logs/stream", s.handleLogStream)
	mux.HandleFunc("/api/v1/command", s.handleCommand)

	// Endpoints REST - Chat Interactivo
	mux.HandleFunc("/api/v1/chat", s.handleChat)
	mux.HandleFunc("/api/v1/chat/history", s.handleChatHistory)
	mux.HandleFunc("/api/v1/chat/clear", s.handleChatClear)

	// Endpoints REST - Actualizacion del Sistema
	mux.HandleFunc("/api/v1/system/update/check", s.handleSystemUpdateCheck)
	mux.HandleFunc("/api/v1/system/update/apply", s.handleSystemUpdateApply)

	// Asignacion de puerto con fallback automatico si 8056 esta ocupado
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", s.port))
	if err != nil {
		listener, err = net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			return 0, fmt.Errorf("no se pudo abrir puerto para Web CLI: %w", err)
		}
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	s.port = actualPort
	s.listener = listener

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[WebCLI] Error de servidor: %v", err)
		}
	}()

	return actualPort, nil
}

func (s *Server) Port() int {
	return s.port
}

func (s *Server) Stop() error {
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

func (s *Server) BroadcastLog(entry botfactory.BotLogEntry) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for ch := range s.clients {
		select {
		case ch <- entry:
		default:
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := map[string]any{
		"agent":       "felix",
		"version":     "v2.2.0",
		"sovereignty": "Republica Argentina [ARG]",
		"port":        s.port,
		"status":      "online",
		"data_dir":    s.dataDir,
		"time":        time.Now().UTC().Format(time.RFC3339),
	}
	_ = json.NewEncoder(w).Encode(status)
}

// handleBots gestiona GET (listar) y POST (crear bot)
func (s *Server) handleBots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.storage == nil {
		http.Error(w, `{"error":"storage no disponible"}`, http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		bots, err := s.storage.ListBots()
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		type BotSummary struct {
			*botfactory.BotSpec
			TotalOrders       int64 `json:"total_orders"`
			TotalAppointments int64 `json:"total_appointments"`
			TotalMessages     int64 `json:"total_messages"`
		}

		result := make([]BotSummary, 0, len(bots))
		for _, b := range bots {
			stats, _ := s.storage.GetStats(b.ID)
			bs := BotSummary{BotSpec: b}
			if stats != nil {
				bs.TotalOrders = stats.TotalOrders
				bs.TotalAppointments = stats.TotalAppointments
				bs.TotalMessages = stats.TotalMessages
			}
			result = append(result, bs)
		}
		_ = json.NewEncoder(w).Encode(result)

	case http.MethodPost:
		var req struct {
			Template        string                    `json:"template"`
			ID              string                    `json:"id"`
			Name            string                    `json:"name"`
			Type            botfactory.BotType        `json:"type"`
			Description     string                    `json:"description"`
			Personality     string                    `json:"personality"`
			SystemPrompt    string                    `json:"system_prompt"`
			PhoneNumber     string                    `json:"phone_number"`
			StrictGrounding bool                      `json:"strict_grounding"`
			Catalog         []botfactory.CatalogItem  `json:"catalog"`
			FAQ             []botfactory.FAQItem      `json:"faq"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "cuerpo JSON invalido"})
			return
		}

		req.ID = strings.TrimSpace(req.ID)
		if req.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "el campo 'id' es obligatorio"})
			return
		}

		var spec *botfactory.BotSpec
		switch strings.ToLower(req.Template) {
		case "pizzeria":
			spec = botfactory.TemplatePizzeria()
		case "appointments", "turnos":
			spec = botfactory.TemplateAppointments()
		case "calls", "llamadas":
			spec = botfactory.TemplateCallScheduler()
		case "faq_support", "soporte":
			spec = botfactory.TemplateFAQSupport()
		case "mosiah":
			spec = botfactory.TemplateMosiah()
		default:
			spec = &botfactory.BotSpec{
				ID:              req.ID,
				Name:            req.Name,
				Type:            req.Type,
				Description:     req.Description,
				Personality:     req.Personality,
				SystemPrompt:    req.SystemPrompt,
				PhoneNumber:     req.PhoneNumber,
				StrictGrounding: req.StrictGrounding,
				Catalog:         req.Catalog,
				FAQ:             req.FAQ,
				Status:          "active",
			}
			if spec.Type == "" {
				spec.Type = botfactory.TypeCustom
			}
		}

		// Aplicar overrides si venia de template
		if req.Template != "" {
			spec.ID = req.ID
			if req.Name != "" {
				spec.Name = req.Name
			}
			if req.Description != "" {
				spec.Description = req.Description
			}
			if req.SystemPrompt != "" {
				spec.SystemPrompt = req.SystemPrompt
			}
			if req.PhoneNumber != "" {
				spec.PhoneNumber = req.PhoneNumber
			}
			if len(req.Catalog) > 0 {
				spec.Catalog = req.Catalog
			}
			if len(req.FAQ) > 0 {
				spec.FAQ = req.FAQ
			}
		}

		if err := s.storage.SaveBot(spec); err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(spec)

	default:
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
	}
}

// handleBotRoute gestiona GET, PUT, DELETE sobre /api/v1/bots/:id y acciones :id/start, :id/stop, :id/avatar, :id/replicate
func (s *Server) handleBotRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.storage == nil {
		http.Error(w, `{"error":"storage no disponible"}`, http.StatusInternalServerError)
		return
	}

	pathTail := strings.TrimPrefix(r.URL.Path, "/api/v1/bots/")
	parts := strings.Split(pathTail, "/")
	botID := parts[0]

	if botID == "" {
		http.Error(w, `{"error":"id de bot vacio"}`, http.StatusBadRequest)
		return
	}

	// Ruta: /api/v1/bots/:id
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			bot, err := s.storage.GetBot(botID)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			stats, _ := s.storage.GetStats(botID)
			response := map[string]any{
				"bot":   bot,
				"stats": stats,
			}
			_ = json.NewEncoder(w).Encode(response)

		case http.MethodPut:
			var updated botfactory.BotSpec
			if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "cuerpo JSON invalido"})
				return
			}
			existing, err := s.storage.GetBot(botID)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bot no encontrado"})
				return
			}

			updated.ID = botID
			updated.CreatedAt = existing.CreatedAt
			if updated.Status == "" {
				updated.Status = existing.Status
			}
			if updated.AvatarPath == "" {
				updated.AvatarPath = existing.AvatarPath
			}

			if err := s.storage.SaveBot(&updated); err != nil {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(updated)

		case http.MethodDelete:
			if err := s.storage.DeleteBot(botID); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "deleted_id": botID})

		default:
			http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	// Acciones: /api/v1/bots/:id/<action>
	action := parts[1]
	switch action {
	case "start":
		if err := s.storage.SetBotActive(botID, true); err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "bot_id": botID, "active": true})

	case "stop":
		if err := s.storage.SetBotActive(botID, false); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "bot_id": botID, "active": false})

	case "replicate":
		var req struct {
			NewID   string `json:"new_id"`
			NewName string `json:"new_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewID == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "se requiere new_id para replicar"})
			return
		}
		source, err := s.storage.GetBot(botID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bot origen no encontrado"})
			return
		}
		clone := *source
		clone.ID = req.NewID
		if req.NewName != "" {
			clone.Name = req.NewName
		} else {
			clone.Name = source.Name + " (Copia)"
		}
		clone.PhoneNumber = ""
		clone.Status = "inactive"
		clone.IsActive = false
		clone.CreatedAt = time.Now().UTC()
		clone.UpdatedAt = time.Now().UTC()
		clone.ReplicationSource = source.ID

		if err := s.storage.SaveBot(&clone); err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(clone)

	case "avatar":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, `{"error":"archivo demasiado grande"}`, http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("avatar")
		if err != nil {
			http.Error(w, `{"error":"archivo de avatar faltante"}`, http.StatusBadRequest)
			return
		}
		defer file.Close()

		ext := filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".png"
		}
		avatarsDir := filepath.Join(s.dataDir, "avatars")
		_ = os.MkdirAll(avatarsDir, 0755)
		destFileName := fmt.Sprintf("%s%s", botID, ext)
		destPath := filepath.Join(avatarsDir, destFileName)

		out, err := os.Create(destPath)
		if err != nil {
			http.Error(w, `{"error":"error guardando imagen en disco"}`, http.StatusInternalServerError)
			return
		}
		defer out.Close()
		if _, err := io.Copy(out, file); err != nil {
			http.Error(w, `{"error":"error escribiendo imagen"}`, http.StatusInternalServerError)
			return
		}

		publicPath := "/avatars/" + destFileName
		_ = s.storage.SetAvatar(botID, publicPath)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "avatar_url": publicPath})

	default:
		http.Error(w, `{"error":"accion no reconocida"}`, http.StatusBadRequest)
	}
}

// handleNumbers gestiona GET (listar lineas)
func (s *Server) handleNumbers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.storage == nil {
		http.Error(w, `{"error":"storage no inicializado"}`, http.StatusInternalServerError)
		return
	}
	numbers, err := s.storage.ListNumbers()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(numbers)
}

// handleAssignNumber vincula numero con validacion de choque
func (s *Server) handleAssignNumber(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BotID       string `json:"bot_id"`
		PhoneNumber string `json:"phone_number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"cuerpo json invalido"}`, http.StatusBadRequest)
		return
	}

	if err := s.storage.AssignNumber(req.BotID, req.PhoneNumber); err != nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "bot_id": req.BotID, "phone_number": req.PhoneNumber})
}

// handleReleaseNumber desvincula linea
func (s *Server) handleReleaseNumber(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BotID       string `json:"bot_id"`
		PhoneNumber string `json:"phone_number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"cuerpo json invalido"}`, http.StatusBadRequest)
		return
	}

	if err := s.storage.ReleaseNumber(req.BotID, req.PhoneNumber); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handleDBStatus inspecciona tamanos e integridad de bases SQLite
func (s *Server) handleDBStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	dbNames := []string{"bots.db", "sessions.db", "users.db", "reminders.db"}

	type DBInfo struct {
		Name       string `json:"name"`
		Exists     bool   `json:"exists"`
		SizeBytes  int64  `json:"size_bytes"`
		SizeKB     string `json:"size_kb"`
		WalSizeKB  string `json:"wal_size_kb"`
		Integrity  string `json:"integrity"`
	}

	result := make([]DBInfo, 0, len(dbNames))
	for _, name := range dbNames {
		p := filepath.Join(s.dataDir, name)
		fi, err := os.Stat(p)
		if err != nil {
			result = append(result, DBInfo{Name: name, Exists: false, SizeKB: "0 KB", WalSizeKB: "-", Integrity: "-"})
			continue
		}
		walFi, walErr := os.Stat(p + "-wal")
		walStr := "0 KB"
		if walErr == nil {
			walStr = fmt.Sprintf("%.2f KB", float64(walFi.Size())/1024)
		}

		integrity := "OK"
		db, dbErr := sql.Open("sqlite", "file:"+p+"?_pragma=busy_timeout(1000)")
		if dbErr == nil {
			var check string
			if err := db.QueryRow("PRAGMA quick_check;").Scan(&check); err != nil || check != "ok" {
				integrity = "WARN"
			}
			db.Close()
		} else {
			integrity = "ERROR"
		}

		result = append(result, DBInfo{
			Name:      name,
			Exists:    true,
			SizeBytes: fi.Size(),
			SizeKB:    fmt.Sprintf("%.2f KB", float64(fi.Size())/1024),
			WalSizeKB: walStr,
			Integrity: integrity,
		})
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handleDBVacuum compacta y optimiza las bases
func (s *Server) handleDBVacuum(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		return
	}

	dbNames := []string{"bots.db", "sessions.db", "users.db", "reminders.db"}
	optimized := []string{}
	for _, name := range dbNames {
		p := filepath.Join(s.dataDir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		db, err := sql.Open("sqlite", p)
		if err != nil {
			continue
		}
		_, _ = db.Exec("VACUUM; PRAGMA optimize;")
		db.Close()
		optimized = append(optimized, name)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "optimized": optimized})
}

// handleDBBackup genera backups timestamped
func (s *Server) handleDBBackup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		return
	}

	destDir := filepath.Join(s.dataDir, "backups")
	_ = os.MkdirAll(destDir, 0755)
	ts := time.Now().UTC().Format("20060102_150405")

	dbNames := []string{"bots.db", "sessions.db", "users.db", "reminders.db"}
	created := []string{}

	for _, name := range dbNames {
		srcPath := filepath.Join(s.dataDir, name)
		if _, err := os.Stat(srcPath); err != nil {
			continue
		}
		baseName := strings.TrimSuffix(name, ".db")
		dstName := fmt.Sprintf("%s_%s.bak", baseName, ts)
		dstPath := filepath.Join(destDir, dstName)

		inF, err := os.Open(srcPath)
		if err != nil {
			continue
		}
		outF, err := os.Create(dstPath)
		if err != nil {
			inF.Close()
			continue
		}
		_, _ = io.Copy(outF, inF)
		inF.Close()
		outF.Close()
		created = append(created, dstName)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "backups_created": created})
}

// handleDBBackupsList lista los backups existentes
func (s *Server) handleDBBackupsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	destDir := filepath.Join(s.dataDir, "backups")
	entries, err := os.ReadDir(destDir)
	if err != nil {
		_ = json.NewEncoder(w).Encode([]any{})
		return
	}

	type BackupItem struct {
		Filename string `json:"filename"`
		SizeKB   string `json:"size_kb"`
		ModTime  string `json:"mod_time"`
	}

	items := []BackupItem{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, BackupItem{
			Filename: e.Name(),
			SizeKB:   fmt.Sprintf("%.2f KB", float64(fi.Size())/1024),
			ModTime:  fi.ModTime().Format(time.RFC3339),
		})
	}
	_ = json.NewEncoder(w).Encode(items)
}

// handleDBTables cuenta registros por tabla
func (s *Server) handleDBTables(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	type TableStats struct {
		Database string `json:"database"`
		Table    string `json:"table"`
		Rows     int64  `json:"rows"`
	}

	result := []TableStats{}
	dbNames := []string{"bots.db", "sessions.db", "users.db", "reminders.db"}

	for _, name := range dbNames {
		p := filepath.Join(s.dataDir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		db, err := sql.Open("sqlite", "file:"+p+"?_pragma=busy_timeout(1000)")
		if err != nil {
			continue
		}
		rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
		if err == nil {
			for rows.Next() {
				var tName string
				if err := rows.Scan(&tName); err == nil {
					var count int64
					_ = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tName)).Scan(&count)
					result = append(result, TableStats{Database: name, Table: tName, Rows: count})
				}
			}
			rows.Close()
		}
		db.Close()
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handleAPIs gestiona consulta y actualizacion de configuracion de inferencia
func (s *Server) handleAPIs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		info := map[string]any{
			"provider":               s.cfg.Provider,
			"model":                  s.cfg.Model,
			"api_base":               s.cfg.APIBase,
			"api_key_set":            s.cfg.APIKey != "",
			"fallback_provider":      "groq",
			"fallback_model":         s.cfg.FallbackModel,
			"fallback_api_base":      s.cfg.FallbackAPIBase,
			"fallback_api_key_set":   s.cfg.FallbackAPIKey != "",
			"groq_api_key_set":       s.cfg.GroqAPIKey != "",
			"openai_api_key_set":     s.cfg.OpenAIAPIKey != "",
			"antigravity_proxy_base": "http://127.0.0.1:8045/v1",
		}
		_ = json.NewEncoder(w).Encode(info)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Provider        string `json:"provider"`
			Model           string `json:"model"`
			APIBase         string `json:"api_base"`
			APIKey          string `json:"api_key"`
			FallbackModel   string `json:"fallback_model"`
			FallbackAPIBase string `json:"fallback_api_base"`
			FallbackAPIKey  string `json:"fallback_api_key"`
			GroqAPIKey      string `json:"groq_api_key"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "cuerpo JSON invalido"})
			return
		}

		if req.Provider != "" {
			s.cfg.Provider = req.Provider
		}
		if req.Model != "" {
			s.cfg.Model = req.Model
		}
		if req.APIBase != "" {
			s.cfg.APIBase = req.APIBase
		}
		if req.APIKey != "" {
			s.cfg.APIKey = req.APIKey
		}
		if req.FallbackModel != "" {
			s.cfg.FallbackModel = req.FallbackModel
		}
		if req.FallbackAPIBase != "" {
			s.cfg.FallbackAPIBase = req.FallbackAPIBase
		}
		if req.FallbackAPIKey != "" {
			s.cfg.FallbackAPIKey = req.FallbackAPIKey
		}
		if req.GroqAPIKey != "" {
			s.cfg.GroqAPIKey = req.GroqAPIKey
		}

		configPath := s.resolveConfigPath()
		if err := s.cfg.Save(configPath); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("error guardando config: %v", err)})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "saved_path": configPath})
		return
	}

	http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
}

// handleAPIsTest prueba la conexion en tiempo real contra un endpoint
func (s *Server) handleAPIsTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Endpoint string `json:"endpoint"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	target := req.Endpoint
	if target == "" {
		target = s.cfg.APIBase
	}
	if target == "" {
		target = "http://127.0.0.1:8045/v1"
	}

	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(target)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         false,
			"target":     target,
			"latency_ms": latency,
			"error":      err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"target":      target,
		"status_code": resp.StatusCode,
		"latency_ms":  latency,
	})
}

// handleAuthStatus reporta el estado de integraciones de credenciales
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	waDBPath := filepath.Join(s.dataDir, "whatsapp.db")
	waDeviceRegistered := false
	var jid string

	if _, err := os.Stat(waDBPath); err == nil {
		db, err := sql.Open("sqlite", "file:"+waDBPath+"?_pragma=busy_timeout(1000)")
		if err == nil {
			var count int
			_ = db.QueryRow("SELECT COUNT(*) FROM whatsmeow_device").Scan(&count)
			waDeviceRegistered = count > 0
			_ = db.QueryRow("SELECT jid FROM whatsmeow_device LIMIT 1").Scan(&jid)
			db.Close()
		}
	}

	status := map[string]any{
		"whatsapp": map[string]any{
			"registered": waDeviceRegistered,
			"jid":        jid,
			"db_path":    waDBPath,
		},
		"groq":   s.cfg.GroqAPIKey != "",
		"openai": s.cfg.OpenAIAPIKey != "",
		"gemini": s.cfg.APIKey != "",
	}
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.storage == nil {
		http.Error(w, `{"error":"storage no disponible"}`, http.StatusInternalServerError)
		return
	}

	botID := r.URL.Query().Get("bot_id")
	level := r.URL.Query().Get("level")
	var logs []botfactory.BotLogEntry
	var err error

	if botID != "" {
		logs, err = s.storage.GetBotLogs(botID, 100, level)
	} else {
		logs, err = s.storage.GetAllBotLogs(100, level)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(logs)
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming no soportado", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	logChan := make(chan botfactory.BotLogEntry, 50)
	s.mu.Lock()
	s.clients[logChan] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, logChan)
		close(logChan)
		s.mu.Unlock()
	}()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case entry, ok := <-logChan:
			if !ok {
				return
			}
			raw, err := json.Marshal(entry)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", string(raw))
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "error en cuerpo de comando", http.StatusBadRequest)
		return
	}

	cmd := strings.TrimSpace(req.Command)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		fmt.Fprintf(w, "Ingrese un comando. Escriba 'help' para ver la lista de comandos disponibles.")
		return
	}

	rootCmd := strings.ToLower(parts[0])
	switch rootCmd {
	case "help":
		fmt.Fprintf(w, "Comandos disponibles en la consola:\n")
		fmt.Fprintf(w, "  status             - Muestra estado y salud general de Felix Server\n")
		fmt.Fprintf(w, "  bots               - Lista todos los bots comerciales y su estado\n")
		fmt.Fprintf(w, "  numbers            - Lista la matriz de asignacion de lineas\n")
		fmt.Fprintf(w, "  db status          - Diagnostico de tamano, WAL e integridad SQLite\n")
		fmt.Fprintf(w, "  db vacuum          - Ejecuta VACUUM en caliente y optimizacion de SQLite\n")
		fmt.Fprintf(w, "  db backup          - Genera respaldo timestamped inmediato\n")
		fmt.Fprintf(w, "  db tables          - Detalle de tablas y conteo de registros\n")
		fmt.Fprintf(w, "  apis               - Muestra modelos configurados y endpoints\n")
		fmt.Fprintf(w, "  auth               - Diagnostico de claves y credenciales del sistema\n")
		fmt.Fprintf(w, "  version            - Version y arquitectura de ejecucion\n")

	case "version":
		fmt.Fprintf(w, "Felix Agent v2.2.0 (Soberania Tecnologica Argentina [ARG])\n")
		fmt.Fprintf(w, "Runtime: %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	case "status":
		bots, _ := s.storage.ListBots()
		onlineBots := 0
		for _, b := range bots {
			if b.Status == "active" || b.IsActive {
				onlineBots++
			}
		}
		fmt.Fprintf(w, "Felix Agent v2.2.0 | Puerto Web CLI: %d | Soberania: Republica Argentina [ARG]\n", s.port)
		fmt.Fprintf(w, "Estado General:          ONLINE (Operativo 100%%)\n")
		fmt.Fprintf(w, "Bots Registrados:        %d (Online: %d)\n", len(bots), onlineBots)
		fmt.Fprintf(w, "Proveedor Inferencia:    %s (%s)\n", s.cfg.Provider, s.cfg.APIBase)
		fmt.Fprintf(w, "Modelo Principal:        %s\n", s.cfg.Model)
		if s.cfg.FallbackModel != "" {
			fmt.Fprintf(w, "Modelo de Fallback:      %s (%s)\n", s.cfg.FallbackModel, s.cfg.FallbackAPIBase)
		}

	case "bots":
		bots, err := s.storage.ListBots()
		if err != nil {
			fmt.Fprintf(w, "Error listando bots: %v\n", err)
			return
		}
		fmt.Fprintf(w, "Total bots registrados: %d\n", len(bots))
		for _, b := range bots {
			state := "OFFLINE"
			if b.Status == "active" || b.IsActive {
				state = "ONLINE"
			}
			phone := b.PhoneNumber
			if phone == "" {
				phone = "-"
			}
			fmt.Fprintf(w, "- [%-18s] %-20s (%-14s) | Linea: %-15s | Estado: %s\n", b.ID, b.Name, b.Type, phone, state)
		}

	case "numbers":
		numbers, err := s.storage.ListNumbers()
		if err != nil {
			fmt.Fprintf(w, "Error listando numeros: %v\n", err)
			return
		}
		fmt.Fprintf(w, "Total lineas asignadas: %d\n", len(numbers))
		for _, n := range numbers {
			act := "STANDBY"
			if n.IsActive {
				act = "ACTIVO"
			}
			fmt.Fprintf(w, "- %-18s -> Bot: %-16s (%-18s) [%s]\n", n.PhoneNumber, n.BotID, n.BotName, act)
		}

	case "db":
		sub := "status"
		if len(parts) > 1 {
			sub = strings.ToLower(parts[1])
		}
		switch sub {
		case "vacuum":
			dbNames := []string{"bots.db", "sessions.db", "users.db", "reminders.db"}
			for _, name := range dbNames {
				p := filepath.Join(s.dataDir, name)
				if _, err := os.Stat(p); err != nil {
					continue
				}
				db, err := sql.Open("sqlite", p)
				if err != nil {
					continue
				}
				_, _ = db.Exec("VACUUM; PRAGMA optimize;")
				db.Close()
				fmt.Fprintf(w, "- %s: optimizado y compactado\n", name)
			}
			fmt.Fprintf(w, "Operacion VACUUM completada exitosamente.\n")

		case "backup":
			destDir := filepath.Join(s.dataDir, "backups")
			_ = os.MkdirAll(destDir, 0755)
			ts := time.Now().UTC().Format("20060102_150405")
			dbNames := []string{"bots.db", "sessions.db", "users.db", "reminders.db"}
			for _, name := range dbNames {
				srcPath := filepath.Join(s.dataDir, name)
				if _, err := os.Stat(srcPath); err != nil {
					continue
				}
				baseName := strings.TrimSuffix(name, ".db")
				dstName := fmt.Sprintf("%s_%s.bak", baseName, ts)
				dstPath := filepath.Join(destDir, dstName)
				src, err := os.Open(srcPath)
				if err != nil {
					continue
				}
				dst, err := os.Create(dstPath)
				if err != nil {
					src.Close()
					continue
				}
				_, _ = io.Copy(dst, src)
				src.Close()
				dst.Close()
				fmt.Fprintf(w, "- %s -> %s\n", name, dstName)
			}
			fmt.Fprintf(w, "Respaldos generados en %s\n", destDir)

		case "tables":
			dbNames := []string{"bots.db", "sessions.db", "users.db", "reminders.db"}
			for _, name := range dbNames {
				p := filepath.Join(s.dataDir, name)
				if _, err := os.Stat(p); err != nil {
					continue
				}
				db, err := sql.Open("sqlite", "file:"+p+"?_pragma=busy_timeout(1000)")
				if err != nil {
					continue
				}
				rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
				if err == nil {
					for rows.Next() {
						var tName string
						if err := rows.Scan(&tName); err == nil {
							var count int64
							_ = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM \"%s\"", tName)).Scan(&count)
							fmt.Fprintf(w, "[%-14s] %-24s: %d filas\n", name, tName, count)
						}
					}
					rows.Close()
				}
				db.Close()
			}

		default:
			dbNames := []string{"bots.db", "sessions.db", "users.db", "reminders.db"}
			fmt.Fprintf(w, "%-20s | %-12s | %-12s | %-10s\n", "BASE DE DATOS", "TAMANO BASE", "TAMANO WAL", "INTEGRIDAD")
			fmt.Fprintf(w, "---------------------------------------------------------------\n")
			for _, name := range dbNames {
				p := filepath.Join(s.dataDir, name)
				fi, err := os.Stat(p)
				if err != nil {
					fmt.Fprintf(w, "%-20s | %-12s | %-12s | %-10s\n", name, "No existe", "-", "-")
					continue
				}
				sizeStr := fmt.Sprintf("%.2f KB", float64(fi.Size())/1024)
				walStr := "0 KB"
				if walFi, err := os.Stat(p + "-wal"); err == nil {
					walStr = fmt.Sprintf("%.2f KB", float64(walFi.Size())/1024)
				}
				integrity := "OK"
				db, err := sql.Open("sqlite", "file:"+p+"?_pragma=busy_timeout(1000)")
				if err == nil {
					var check string
					if err := db.QueryRow("PRAGMA quick_check;").Scan(&check); err != nil || check != "ok" {
						integrity = "WARN"
					}
					db.Close()
				}
				fmt.Fprintf(w, "%-20s | %-12s | %-12s | %-10s\n", name, sizeStr, walStr, integrity)
			}
		}

	case "apis":
		fmt.Fprintf(w, "Configuracion de APIs e Inferencia:\n")
		fmt.Fprintf(w, "  Proveedor Primario:  %s\n", s.cfg.Provider)
		fmt.Fprintf(w, "  Modelo Principal:    %s\n", s.cfg.Model)
		fmt.Fprintf(w, "  Endpoint Base:       %s\n", s.cfg.APIBase)
		if s.cfg.FallbackModel != "" {
			fmt.Fprintf(w, "  Modelo Fallback:     %s\n", s.cfg.FallbackModel)
			fmt.Fprintf(w, "  Endpoint Fallback:   %s\n", s.cfg.FallbackAPIBase)
		}

	case "auth":
		fmt.Fprintf(w, "Estado de Credenciales y Sesiones:\n")
		keyStatus := "NO CONFIGURADA"
		if s.cfg.APIKey != "" {
			keyStatus = fmt.Sprintf("Configurada (%d chars)", len(s.cfg.APIKey))
		}
		fmt.Fprintf(w, "  Inferencia LLM:       %s\n", keyStatus)
		groqStatus := "NO CONFIGURADA"
		if s.cfg.GroqAPIKey != "" {
			groqStatus = "Configurada"
		}
		fmt.Fprintf(w, "  Groq Audio Whisper:   %s\n", groqStatus)
		waDB := filepath.Join(s.dataDir, "whatsapp.db")
		waStatus := "Sin sesion"
		if fi, err := os.Stat(waDB); err == nil && fi.Size() > 0 {
			waStatus = "Sesion activa (" + fmt.Sprintf("%.1f KB", float64(fi.Size())/1024) + ")"
		}
		fmt.Fprintf(w, "  WhatsApp DB:          %s\n", waStatus)

	default:
		fmt.Fprintf(w, "Comando '%s' no reconocido. Escriba 'help' para ver los comandos validos.", cmd)
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BotID     string `json:"bot_id"`
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"JSON invalido"}`, http.StatusBadRequest)
		return
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		http.Error(w, `{"error":"el mensaje no puede estar vacio"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		req.SessionID = "desktop-session"
	}
	if req.BotID == "" {
		req.BotID = "felix"
	}

	start := time.Now()

	if req.BotID == "felix" {
		systemPrompt := s.cfg.SystemPrompt
		if systemPrompt == "" {
			systemPrompt = "Eres Felix, asistente autonomo de ingenieria, DevOps y administracion de sistemas Linux."
		}

		sessionDBPath := filepath.Join(s.dataDir, "sessions.db")
		recentHistory := ""
		if sdb, err := sql.Open("sqlite", "file:"+sessionDBPath+"?_pragma=busy_timeout(1000)"); err == nil {
			rows, qErr := sdb.Query("SELECT role, content FROM session_messages WHERE session_id = ? ORDER BY id DESC LIMIT 6", req.SessionID)
			if qErr == nil {
				type msgPair struct{ role, content string }
				var histList []msgPair
				for rows.Next() {
					var r, c string
					if err := rows.Scan(&r, &c); err == nil {
						histList = append(histList, msgPair{role: r, content: c})
					}
				}
				rows.Close()
				for i := len(histList) - 1; i >= 0; i-- {
					recentHistory += fmt.Sprintf("[%s]: %s\n", strings.ToUpper(histList[i].role), histList[i].content)
				}
			}
			sdb.Close()
		}

		userPrompt := req.Message
		if recentHistory != "" {
			userPrompt = "Historial reciente de la conversacion:\n" + recentHistory + "\nUsuario: " + req.Message
		}

		reply, err := s.completer.Complete(r.Context(), systemPrompt, userPrompt)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		if sdb, err := sql.Open("sqlite", sessionDBPath); err == nil {
			_, _ = sdb.Exec("INSERT INTO session_messages (session_id, role, content, created_at) VALUES (?, 'user', ?, CURRENT_TIMESTAMP)", req.SessionID, req.Message)
			_, _ = sdb.Exec("INSERT INTO session_messages (session_id, role, content, created_at) VALUES (?, 'assistant', ?, CURRENT_TIMESTAMP)", req.SessionID, reply)
			sdb.Close()
		}

		if s.storage != nil {
			_ = s.storage.LogEvent(&botfactory.BotLogEntry{
				BotID:        "felix",
				Level:        "INFO",
				Direction:    "OUT",
				SenderNumber: req.SessionID,
				Message:      reply,
				LatencyMs:    latency,
			})
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"bot_id":     "felix",
			"reply":      reply,
			"latency_ms": latency,
		})
		return
	}

	chatRes, err := s.botMgr.Chat(r.Context(), req.BotID, req.SessionID, req.Message)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	if s.storage != nil {
		_ = s.storage.LogEvent(&botfactory.BotLogEntry{
			BotID:        req.BotID,
			Level:        "INFO",
			Direction:    "OUT",
			SenderNumber: req.SessionID,
			Message:      chatRes.Reply,
			LatencyMs:    latency,
		})
	}

	respMap := map[string]any{
		"bot_id":     req.BotID,
		"reply":      chatRes.Reply,
		"latency_ms": latency,
	}
	if chatRes.Order != nil {
		respMap["order"] = chatRes.Order
	}
	if chatRes.Appointment != nil {
		respMap["appointment"] = chatRes.Appointment
	}
	_ = json.NewEncoder(w).Encode(respMap)
}

func (s *Server) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	botID := r.URL.Query().Get("bot_id")
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = "desktop-session"
	}
	if botID == "" {
		botID = "felix"
	}

	type ChatMsg struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
	}
	msgs := []ChatMsg{}

	if botID == "felix" {
		sessionDBPath := filepath.Join(s.dataDir, "sessions.db")
		if sdb, err := sql.Open("sqlite", "file:"+sessionDBPath+"?_pragma=busy_timeout(1000)"); err == nil {
			defer sdb.Close()
			rows, err := sdb.Query("SELECT role, content, created_at FROM session_messages WHERE session_id = ? ORDER BY id ASC LIMIT 50", sessionID)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var m ChatMsg
					if err := rows.Scan(&m.Role, &m.Content, &m.CreatedAt); err == nil {
						msgs = append(msgs, m)
					}
				}
			}
		}
	} else {
		botsDBPath := filepath.Join(s.dataDir, "bots.db")
		if bdb, err := sql.Open("sqlite", "file:"+botsDBPath+"?_pragma=busy_timeout(1000)"); err == nil {
			defer bdb.Close()
			rows, err := bdb.Query("SELECT role, content, created_at FROM bot_messages WHERE bot_id = ? AND session_id = ? ORDER BY id ASC LIMIT 50", botID, sessionID)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var m ChatMsg
					if err := rows.Scan(&m.Role, &m.Content, &m.CreatedAt); err == nil {
						msgs = append(msgs, m)
					}
				}
			}
		}
	}

	_ = json.NewEncoder(w).Encode(msgs)
}

func (s *Server) handleChatClear(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		BotID     string `json:"bot_id"`
		SessionID string `json:"session_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.SessionID == "" {
		req.SessionID = "desktop-session"
	}

	if req.BotID == "felix" {
		sessionDBPath := filepath.Join(s.dataDir, "sessions.db")
		if sdb, err := sql.Open("sqlite", sessionDBPath); err == nil {
			_, _ = sdb.Exec("DELETE FROM session_messages WHERE session_id = ?", req.SessionID)
			sdb.Close()
		}
	} else {
		botsDBPath := filepath.Join(s.dataDir, "bots.db")
		if bdb, err := sql.Open("sqlite", botsDBPath); err == nil {
			_, _ = bdb.Exec("DELETE FROM bot_messages WHERE bot_id = ? AND session_id = ?", req.BotID, req.SessionID)
			bdb.Close()
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) handleSystemUpdateCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	repoDir := config.FindRepoDir()
	if repoDir == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "repositorio no encontrado"})
		return
	}

	fetchCmd := exec.Command("git", "fetch", "origin", "main")
	fetchCmd.Dir = repoDir
	_ = fetchCmd.Run()

	localCmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	localCmd.Dir = repoDir
	localOut, _ := localCmd.Output()
	localRev := strings.TrimSpace(string(localOut))

	remoteCmd := exec.Command("git", "rev-parse", "--short", "origin/main")
	remoteCmd.Dir = repoDir
	remoteOut, _ := remoteCmd.Output()
	remoteRev := strings.TrimSpace(string(remoteOut))

	logCmd := exec.Command("git", "log", "HEAD..origin/main", "--oneline")
	logCmd.Dir = repoDir
	logOut, _ := logCmd.Output()
	rawLogs := strings.TrimSpace(string(logOut))
	var logLines []string
	if rawLogs != "" {
		logLines = strings.Split(rawLogs, "\n")
	} else {
		logLines = []string{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"up_to_date":     localRev == remoteRev,
		"current_commit": localRev,
		"latest_commit":  remoteRev,
		"commits":        logLines,
	})
}

func (s *Server) handleSystemUpdateApply(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"metodo no permitido"}`, http.StatusMethodNotAllowed)
		return
	}

	repoDir := config.FindRepoDir()
	if repoDir == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "repositorio no encontrado"})
		return
	}

	pullCmd := exec.Command("git", "pull", "origin", "main")
	pullCmd.Dir = repoDir
	if out, err := pullCmd.CombinedOutput(); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprintf("git pull fallo: %v (%s)", err, string(out))})
		return
	}

	binPath := filepath.Join(repoDir, "bin", "felix")
	buildCmd := exec.Command("go", "build", "-o", binPath, "cmd/felix/main.go")
	buildCmd.Dir = repoDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprintf("compilacion fallo: %v (%s)", err, string(out))})
		return
	}

	targetBin := filepath.Join(os.Getenv("HOME"), ".local", "bin", "felix")
	_ = exec.Command("install", "-m", "755", binPath, targetBin).Run()

	revCmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	revCmd.Dir = repoDir
	revOut, _ := revCmd.Output()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"updated_commit": strings.TrimSpace(string(revOut)),
		"message":        "Felix actualizado exitosamente",
	})
}
