package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"felix-go/internal/agent"
	"felix-go/internal/inference"
	"felix-go/internal/memory"
	"felix-go/internal/whatsapp"
)

// Server expone endpoints HTTP internos para verificacion de salud y metricas del homelab.
type Server struct {
	addr        string
	server      *http.Server
	engine      *agent.Engine
	waClient    *whatsapp.Client
	memStore    *memory.Store
	subagentMgr *agent.SubagentManager
	gateway     *inference.Gateway
	startTime   time.Time
}

// StatusResponse modela la respuesta JSON de /healthz.
type StatusResponse struct {
	Status                string    `json:"status"`
	UptimeSeconds         int64     `json:"uptime_seconds"`
	UptimeHuman           string    `json:"uptime_human"`
	WhatsAppConnected     bool      `json:"whatsapp_connected"`
	LLMPrimaryHealthy     bool      `json:"llm_primary_healthy"`
	LLMFallbackActive     bool      `json:"llm_fallback_active"`
	TotalPrimaryCalls     int64     `json:"total_primary_calls"`
	TotalFallbackCalls    int64     `json:"total_fallback_calls"`
	TotalPromptTokens     int64     `json:"total_prompt_tokens"`
	TotalCompletionTokens int64     `json:"total_completion_tokens"`
	ActiveSubagents       int       `json:"active_subagents"`
	Timestamp             time.Time `json:"timestamp"`
}

// NewServer crea una nueva instancia del servidor de salud interno.
func NewServer(addr string, eng *agent.Engine, wa *whatsapp.Client, mem *memory.Store, sub *agent.SubagentManager) *Server {
	if addr == "" {
		addr = "127.0.0.1:8055"
	}

	s := &Server{
		addr:        addr,
		engine:      eng,
		waClient:    wa,
		memStore:    mem,
		subagentMgr: sub,
		startTime:   time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/api/notify", s.handleNotify)
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if s.gateway != nil {
			s.gateway.HandleChatCompletions(w, r)
		} else {
			http.Error(w, "Inference Gateway no inicializado", http.StatusServiceUnavailable)
		}
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if s.gateway != nil {
			s.gateway.HandleModels(w, r)
		} else {
			http.Error(w, "Inference Gateway no inicializado", http.StatusServiceUnavailable)
		}
	})

	s.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s
}

// SetGateway enlaza la puerta de enlace de inferencia integrada.
func (s *Server) SetGateway(gw *inference.Gateway) {
	s.gateway = gw
}

// Start inicia el servidor en segundo plano e inicia el latido push opcional si esta configurado.
func (s *Server) Start() {
	go func() {
		log.Printf("[Health Server] Escuchando verificaciones de salud y metricas en http://%s", s.addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Health Server Warning] Error en servidor HTTP: %v", err)
		}
	}()

	pushURL := os.Getenv("UPTIME_KUMA_PUSH_URL")
	if pushURL != "" {
		go s.runPushHeartbeat(pushURL, 60*time.Second)
	}
}

func (s *Server) runPushHeartbeat(pushURL string, interval time.Duration) {
	log.Printf("[Uptime Kuma Push] Latido periodico configurado cada %v hacia %s", interval, pushURL)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	client := &http.Client{Timeout: 5 * time.Second}

	for range ticker.C {
		resp, err := client.Get(pushURL)
		if err != nil {
			log.Printf("[Uptime Kuma Push Warning] Fallo enviando latido: %v", err)
			continue
		}
		_ = resp.Body.Close()
	}
}

// Stop detiene el servidor ordenadamente.
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(s.startTime)
	llmStatus := s.engine.GetLLMStatus()
	waConn := false
	if s.waClient != nil {
		waConn = s.waClient.IsConnected()
	}

	activeSubs := 0
	if s.subagentMgr != nil {
		activeSubs = s.subagentMgr.GetActiveCount()
	}

	status := "healthy"
	if !llmStatus.PrimaryHealthy {
		status = "degraded_llm"
	}

	resp := StatusResponse{
		Status:                status,
		UptimeSeconds:         int64(uptime.Seconds()),
		UptimeHuman:           uptime.Round(time.Second).String(),
		WhatsAppConnected:     waConn,
		LLMPrimaryHealthy:     llmStatus.PrimaryHealthy,
		LLMFallbackActive:     llmStatus.FallbackActive,
		TotalPrimaryCalls:     llmStatus.TotalPrimaryCalls,
		TotalFallbackCalls:    llmStatus.TotalFallbackCalls,
		TotalPromptTokens:     llmStatus.TotalPromptTokens,
		TotalCompletionTokens: llmStatus.TotalCompletionTokens,
		ActiveSubagents:       activeSubs,
		Timestamp:             time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(s.startTime).Seconds()
	llmStatus := s.engine.GetLLMStatus()

	waConn := 0
	if s.waClient != nil && s.waClient.IsConnected() {
		waConn = 1
	}

	primaryHealthy := 0
	if llmStatus.PrimaryHealthy {
		primaryHealthy = 1
	}

	fallbackActive := 0
	if llmStatus.FallbackActive {
		fallbackActive = 1
	}

	activeSubs := 0
	if s.subagentMgr != nil {
		activeSubs = s.subagentMgr.GetActiveCount()
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP felix_uptime_seconds Tiempo total de operacion continua en segundos\n")
	fmt.Fprintf(w, "# TYPE felix_uptime_seconds gauge\n")
	fmt.Fprintf(w, "felix_uptime_seconds %.1f\n", uptime)

	fmt.Fprintf(w, "# HELP felix_whatsapp_connected Estado de conexion a WhatsApp (1=conectado, 0=desconectado)\n")
	fmt.Fprintf(w, "# TYPE felix_whatsapp_connected gauge\n")
	fmt.Fprintf(w, "felix_whatsapp_connected %d\n", waConn)

	fmt.Fprintf(w, "# HELP felix_llm_primary_healthy Estado del proveedor primario de LLM\n")
	fmt.Fprintf(w, "# TYPE felix_llm_primary_healthy gauge\n")
	fmt.Fprintf(w, "felix_llm_primary_healthy %d\n", primaryHealthy)

	fmt.Fprintf(w, "# HELP felix_llm_fallback_active Si el failover a respaldo esta activo\n")
	fmt.Fprintf(w, "# TYPE felix_llm_fallback_active gauge\n")
	fmt.Fprintf(w, "felix_llm_fallback_active %d\n", fallbackActive)

	fmt.Fprintf(w, "# HELP felix_llm_primary_calls_total Total de llamadas exitosas al proveedor primario\n")
	fmt.Fprintf(w, "# TYPE felix_llm_primary_calls_total counter\n")
	fmt.Fprintf(w, "felix_llm_primary_calls_total %d\n", llmStatus.TotalPrimaryCalls)

	fmt.Fprintf(w, "# HELP felix_llm_fallback_calls_total Total de llamadas al proveedor de respaldo\n")
	fmt.Fprintf(w, "# TYPE felix_llm_fallback_calls_total counter\n")
	fmt.Fprintf(w, "felix_llm_fallback_calls_total %d\n", llmStatus.TotalFallbackCalls)

	fmt.Fprintf(w, "# HELP felix_llm_prompt_tokens_total Total acumulado de tokens de entrada (prompt)\n")
	fmt.Fprintf(w, "# TYPE felix_llm_prompt_tokens_total counter\n")
	fmt.Fprintf(w, "felix_llm_prompt_tokens_total %d\n", llmStatus.TotalPromptTokens)

	fmt.Fprintf(w, "# HELP felix_llm_completion_tokens_total Total acumulado de tokens de salida (completion)\n")
	fmt.Fprintf(w, "# TYPE felix_llm_completion_tokens_total counter\n")
	fmt.Fprintf(w, "felix_llm_completion_tokens_total %d\n", llmStatus.TotalCompletionTokens)

	fmt.Fprintf(w, "# HELP felix_subagents_active Cantidad de subagentes ejecutandose concurrentemente\n")
	fmt.Fprintf(w, "# TYPE felix_subagents_active gauge\n")
	fmt.Fprintf(w, "felix_subagents_active %d\n", activeSubs)
}

type NotifyRequest struct {
	Target  string `json:"target,omitempty"`  // "admin", "default" o un JID explicito
	Message string `json:"message"`           // Texto a enviar por WhatsApp
	Source  string `json:"source,omitempty"`   // Identificador de origen (ej: "pc-task", "uptime-kuma")
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	clientIP := r.RemoteAddr
	if idx := strings.LastIndex(clientIP, ":"); idx != -1 {
		clientIP = clientIP[:idx]
	}
	clientIP = strings.Trim(clientIP, "[]")

	isAuthorized := (clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "192.0.2.1" || strings.HasPrefix(clientIP, "100.") || clientIP == "")
	if !isAuthorized {
		http.Error(w, "Acceso no autorizado (solo red local o Tailscale)", http.StatusForbidden)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Metodo no permitido (usar POST)", http.StatusMethodNotAllowed)
		return
	}

	var req NotifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("JSON invalido: %v", err), http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "Campo 'message' obligatorio", http.StatusBadRequest)
		return
	}

	if s.waClient == nil {
		http.Error(w, "Cliente WhatsApp no disponible", http.StatusServiceUnavailable)
		return
	}

	targetJID := req.Target
	if targetJID == "" || targetJID == "admin" || targetJID == "default" {
		targetJID = s.waClient.GetOwnerJID()
	}
	if targetJID == "" {
		http.Error(w, "No hay administrador o destinatario configurado", http.StatusBadRequest)
		return
	}

	msgText := req.Message
	if req.Source != "" {
		msgText = fmt.Sprintf("[%s]: %s", req.Source, req.Message)
	}

	if err := s.waClient.SendMessage(r.Context(), targetJID, msgText); err != nil {
		http.Error(w, fmt.Sprintf("Error enviando mensaje WhatsApp: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"delivered": true,
		"target":    targetJID,
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
	})
}
