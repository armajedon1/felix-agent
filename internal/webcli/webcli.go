package webcli

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	httpServer *http.Server
	listener   net.Listener
	mu         sync.RWMutex
	clients    map[chan botfactory.BotLogEntry]bool
}

func NewServer(port int, storage *botfactory.Storage, cfg *config.Config, dataDir string) *Server {
	if port <= 0 {
		port = 8056
	}
	return &Server{
		port:    port,
		storage: storage,
		cfg:     cfg,
		dataDir: dataDir,
		clients: make(map[chan botfactory.BotLogEntry]bool),
	}
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

	// Endpoints REST
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/bots", s.handleBots)
	mux.HandleFunc("/api/v1/bots/", s.handleBotActions)
	mux.HandleFunc("/api/v1/numbers", s.handleNumbers)
	mux.HandleFunc("/api/v1/numbers/assign", s.handleAssignNumber)
	mux.HandleFunc("/api/v1/numbers/release", s.handleReleaseNumber)
	mux.HandleFunc("/api/v1/apis", s.handleAPIs)
	mux.HandleFunc("/api/v1/logs/bots", s.handleLogs)
	mux.HandleFunc("/api/v1/logs/stream", s.handleLogStream)
	mux.HandleFunc("/api/v1/command", s.handleCommand)

	// Asignacion de puerto con fallback automatico si 8056 esta ocupado
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", s.port))
	if err != nil {
		// Intentar en cualquier puerto libre disponible
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
		"time":        time.Now().UTC().Format(time.RFC3339),
	}
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleBots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.storage == nil {
		http.Error(w, `{"error":"storage no disponible"}`, http.StatusInternalServerError)
		return
	}

	bots, err := s.storage.ListBots()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(bots)
}

func (s *Server) handleBotActions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/bots/"), "/")
	if len(parts) < 2 {
		http.Error(w, `{"error":"ruta invalida"}`, http.StatusBadRequest)
		return
	}
	botID := parts[0]
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

func (s *Server) handleAPIs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	apis := []map[string]string{
		{
			"provider": "Google Gemini",
			"model":    s.cfg.Model,
			"endpoint": s.cfg.APIBase,
			"status":   "ACTIVO",
		},
		{
			"provider": "Antigravity Tools Proxy",
			"model":    "gemini-3.8-flash-high",
			"endpoint": "http://127.0.0.1:8045/v1",
			"status":   "LOCAL",
		},
		{
			"provider": "OpenAI Compatible",
			"model":    "gpt-4o",
			"endpoint": "https://api.openai.com/v1",
			"status":   "DISPONIBLE",
		},
	}
	_ = json.NewEncoder(w).Encode(apis)
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
		logs, err = s.storage.GetBotLogs(botID, 50, level)
	} else {
		logs, err = s.storage.GetAllBotLogs(50, level)
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

	logChan := make(chan botfactory.BotLogEntry, 20)
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
	switch strings.ToLower(cmd) {
	case "status":
		fmt.Fprintf(w, "Felix Agent v2.2.0 | Puerto: %d | Soberania: Republica Argentina [ARG]\nEstado: Operativo 100%%", s.port)
	case "bots":
		bots, err := s.storage.ListBots()
		if err != nil {
			fmt.Fprintf(w, "Error listando bots: %v", err)
			return
		}
		fmt.Fprintf(w, "Total bots registrados: %d\n", len(bots))
		for _, b := range bots {
			fmt.Fprintf(w, "- [%s] %s (%s) - Numero: %s - Estado: %s\n", b.ID, b.Name, b.Type, b.PhoneNumber, b.Status)
		}
	case "numbers":
		numbers, err := s.storage.ListNumbers()
		if err != nil {
			fmt.Fprintf(w, "Error: %v", err)
			return
		}
		for _, n := range numbers {
			act := "INACTIVO"
			if n.IsActive {
				act = "ACTIVO"
			}
			fmt.Fprintf(w, "- %s -> Bot: %s (%s) [%s]\n", n.PhoneNumber, n.BotName, n.BotID, act)
		}
	default:
		fmt.Fprintf(w, "Comando '%s' ejecutado en entorno Felix. Subcomandos validos: status, bots, numbers.", cmd)
	}
}
