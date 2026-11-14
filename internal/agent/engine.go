package agent

import (
	"context"
	"net/http"
	"strings"
	"time"

	"felix-go/internal/config"
	"felix-go/internal/inference"
	"felix-go/internal/tools"
)

// OpenAIMessage representa un mensaje en el protocolo de chat completions.
type OpenAIMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall representa una invocacion a una herramienta devuelta por el modelo.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionRequest es el cuerpo de la peticion a /chat/completions.
type ChatCompletionRequest struct {
	Model      string                   `json:"model"`
	Messages   []OpenAIMessage          `json:"messages"`
	Tools      []tools.OpenAIToolSchema `json:"tools,omitempty"`
	ToolChoice string                   `json:"tool_choice,omitempty"`
}

// ChatCompletionResponse modela la respuesta del endpoint.
type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      OpenAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// LLMStatus representa las metricas y el estado en vivo de la alta disponibilidad de modelos.
type LLMStatus struct {
	PrimaryHealthy        bool      `json:"primary_healthy"`
	ConsecutiveFailures   int       `json:"consecutive_failures"`
	LastFailureTime       time.Time `json:"last_failure_time"`
	LastError             string    `json:"last_error"`
	TotalPrimaryCalls     int64     `json:"total_primary_calls"`
	TotalFallbackCalls    int64     `json:"total_fallback_calls"`
	FallbackActive        bool      `json:"fallback_active"`
	PrimaryEndpoint       string    `json:"primary_endpoint"`
	PrimaryModel          string    `json:"primary_model"`
	FallbackEndpoint      string    `json:"fallback_endpoint"`
	FallbackModel         string    `json:"fallback_model"`
	TotalPromptTokens     int64     `json:"total_prompt_tokens"`
	TotalCompletionTokens int64     `json:"total_completion_tokens"`
	LastPromptTokens      int       `json:"last_prompt_tokens"`
	LastCompletionTokens  int       `json:"last_completion_tokens"`
}

var heavyTools = map[string]string{
	"browser_agent":     "Navegando e interactuando con Chromium headless...",
	"subagent_spawn":    "Despachando subagente autonomo en segundo plano...",
	"antigravity_cli":   "Delegando tarea de desarrollo a Antigravity CLI...",
	"background_worker": "Ejecutando tarea concurrente en segundo plano...",
	"harness_develop":   "Ejecutando arnes de desarrollo: analisis AST, parches y verificacion...",
}

// Engine orquesta el bucle agentico (ReAct) conectando subsistemas compartimentados.
type Engine struct {
	cfg          *config.Config
	registry     *tools.Registry
	history      *HistoryManager
	scratchpad   *tools.ScratchpadManager
	provider     LLMProvider
	assembler    *DefaultContextAssembler
	dispatcher   *DefaultToolDispatcher
	orchestrator Orchestrator
}

// NewEngine inicializa el Engine configurando sus dependencias modulares.
func NewEngine(cfg *config.Config, reg *tools.Registry, hist *HistoryManager) *Engine {
	tr := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	httpClient := &http.Client{
		Transport: tr,
		Timeout:   120 * time.Second,
	}

	var initialKeys []string
	if len(cfg.APIKeys) > 0 {
		initialKeys = append(initialKeys, cfg.APIKeys...)
	} else if cfg.APIKey != "" {
		initialKeys = append(initialKeys, cfg.APIKey)
	}
	keyPool := inference.NewKeyPool(initialKeys...)

	provider := NewFailoverProvider(cfg, keyPool, httpClient)
	assembler := NewContextAssembler(cfg, hist, nil)
	dispatcher := NewToolDispatcher(reg)

	eng := &Engine{
		cfg:        cfg,
		registry:   reg,
		history:    hist,
		provider:   provider,
		assembler:  assembler,
		dispatcher: dispatcher,
	}

	eng.orchestrator = NewReActOrchestrator(
		cfg,
		reg,
		hist,
		provider,
		assembler,
		dispatcher,
		func(sessionID string) {
			eng.TriggerAsyncCompaction(sessionID)
		},
	)

	return eng
}

// SetProgressFunc asigna el callback para reportar estado intermedio al canal.
func (e *Engine) SetProgressFunc(fn func(sessionID, statusText string)) {
	if e.dispatcher != nil {
		e.dispatcher.SetProgressFunc(fn)
	}
}

// SetScratchpadManager asigna el gestor de memoria de trabajo al motor y al ensamblador de contexto.
func (e *Engine) SetScratchpadManager(sm *tools.ScratchpadManager) {
	e.scratchpad = sm
	if e.assembler != nil {
		e.assembler.SetScratchpadManager(sm)
	}
}

// GetKeyPool retorna el pool de claves de API activo.
func (e *Engine) GetKeyPool() *inference.KeyPool {
	return e.provider.GetKeyPool()
}

// SetKeyPool actualiza el pool de claves del proveedor de inferencia.
func (e *Engine) SetKeyPool(kp *inference.KeyPool) {
	e.provider.SetKeyPool(kp)
}

// UpdateCredentials actualiza dinamicamente los parametros de conexion al LLM.
func (e *Engine) UpdateCredentials(apiBase, apiKey, model string, keys []string) {
	e.provider.UpdateCredentials(apiBase, apiKey, model, keys)
}

// GetLLMStatus retorna el estado y las metricas de alta disponibilidad de inferencia.
func (e *Engine) GetLLMStatus() LLMStatus {
	return e.provider.Status()
}

// ClearHistory limpia el historial persistente de la sesion indicada.
func (e *Engine) ClearHistory(sessionID string) error {
	if e.history != nil {
		return e.history.Clear(sessionID)
	}
	return nil
}

// GetHistoryCount retorna la cantidad de mensajes guardados para la sesion.
func (e *Engine) GetHistoryCount(sessionID string) (int, error) {
	if e.history != nil {
		return e.history.Count(sessionID)
	}
	return 0, nil
}

// GetSessionSummary retorna el resumen consolidado de la sesion si existe.
func (e *Engine) GetSessionSummary(sessionID string) (string, error) {
	if e.history != nil {
		s, _, err := e.history.GetSummary(sessionID)
		return s, err
	}
	return "", nil
}

// Run ejecuta el ciclo ReAct delegando en el orquestador desacoplado.
func (e *Engine) Run(ctx context.Context, sessionID, userInput string, extraSystemContext string) (string, error) {
	return e.orchestrator.Run(ctx, sessionID, userInput, extraSystemContext)
}

// Complete genera una respuesta de texto simple dados un systemPrompt y un userPrompt, satisfaciendo harness.LLMCompleter.
func (e *Engine) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return e.provider.Complete(ctx, systemPrompt, userPrompt)
}

// executeCompletion ejecuta una llamada de completacion delegando en el proveedor (preservado para tests unitarios).
func (e *Engine) executeCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	return e.provider.Execute(ctx, req)
}

func cleanFormatting(text string) string {
	for {
		start := strings.Index(text, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(text, "</think>")
		if end == -1 {
			break
		}
		text = strings.TrimSpace(text[:start] + text[end+len("</think>"):])
	}
	text = strings.ReplaceAll(text, "#", "")
	text = strings.ReplaceAll(text, "**", "*")
	return strings.TrimSpace(text)
}
