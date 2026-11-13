package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
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

// Engine orquesta el bucle de ejecucion agentico (ReAct Loop).
type Engine struct {
	cfg           *config.Config
	registry      *tools.Registry
	history       *HistoryManager
	scratchpad    *tools.ScratchpadManager
	httpClient    *http.Client
	keyPool       *inference.KeyPool
	llmMu         sync.RWMutex
	llmStatus     LLMStatus
	cooldownUntil time.Time
	progressFn    func(sessionID, statusText string)
}

var heavyTools = map[string]string{
	"browser_agent":     "Navegando e interactuando con Chromium headless...",
	"subagent_spawn":    "Despachando subagente autonomo en segundo plano...",
	"antigravity_cli":   "Delegando tarea de desarrollo a Antigravity CLI...",
	"background_worker": "Ejecutando tarea concurrente en segundo plano...",
}

func (e *Engine) SetProgressFunc(fn func(sessionID, statusText string)) {
	e.progressFn = fn
}

func (e *Engine) SetScratchpadManager(sm *tools.ScratchpadManager) {
	e.scratchpad = sm
}

func (e *Engine) GetKeyPool() *inference.KeyPool {
	return e.keyPool
}

func (e *Engine) SetKeyPool(kp *inference.KeyPool) {
	e.llmMu.Lock()
	defer e.llmMu.Unlock()
	e.keyPool = kp
}

func (e *Engine) UpdateCredentials(apiBase, apiKey, model string, keys []string) {
	e.llmMu.Lock()
	defer e.llmMu.Unlock()
	if apiBase != "" {
		e.cfg.APIBase = apiBase
		e.llmStatus.PrimaryEndpoint = apiBase
	}
	if model != "" {
		e.cfg.Model = model
		e.llmStatus.PrimaryModel = model
	}
	if apiKey != "" {
		e.cfg.APIKey = apiKey
	}
	if len(keys) > 0 {
		e.cfg.APIKeys = keys
		if e.keyPool != nil {
			e.keyPool.SetKeys(keys)
		}
	} else if apiKey != "" && e.keyPool != nil {
		e.keyPool.SetKeys([]string{apiKey})
	}
}

func NewEngine(cfg *config.Config, reg *tools.Registry, hist *HistoryManager) *Engine {
	tr := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	var initialKeys []string
	if len(cfg.APIKeys) > 0 {
		initialKeys = append(initialKeys, cfg.APIKeys...)
	} else if cfg.APIKey != "" {
		initialKeys = append(initialKeys, cfg.APIKey)
	}

	return &Engine{
		cfg:        cfg,
		registry:   reg,
		history:    hist,
		keyPool:    inference.NewKeyPool(initialKeys...),
		httpClient: &http.Client{
			Transport: tr,
			Timeout:   120 * time.Second,
		},
		llmStatus: LLMStatus{
			PrimaryHealthy:   true,
			PrimaryEndpoint:  cfg.APIBase,
			PrimaryModel:     cfg.Model,
			FallbackEndpoint: cfg.FallbackAPIBase,
			FallbackModel:    cfg.FallbackModel,
		},
	}
}

// GetLLMStatus retorna una copia segura del estado y metricas de alta disponibilidad de los modelos LLM.
func (e *Engine) GetLLMStatus() LLMStatus {
	e.llmMu.RLock()
	defer e.llmMu.RUnlock()
	st := e.llmStatus
	st.PrimaryEndpoint = e.cfg.APIBase
	st.PrimaryModel = e.cfg.Model
	st.FallbackEndpoint = e.cfg.FallbackAPIBase
	st.FallbackModel = e.cfg.FallbackModel
	return st
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

// Run ejecuta el ciclo ReAct: envia mensajes, atiende tool_calls de forma autonoma y devuelve la respuesta final.
func (e *Engine) Run(ctx context.Context, sessionID, userInput string, extraSystemContext string) (string, error) {
	systemPrompt := e.cfg.SystemPrompt
	if extraSystemContext != "" {
		systemPrompt = systemPrompt + "\n\n" + extraSystemContext
	}

	var conversation []OpenAIMessage
	conversation = append(conversation, OpenAIMessage{
		Role:    "system",
		Content: systemPrompt,
	})

	// Si existe un resumen semantico consolidado de la sesion, inyectarlo como contexto del sistema
	if e.history != nil {
		if summary, _, _ := e.history.GetSummary(sessionID); summary != "" {
			conversation = append(conversation, OpenAIMessage{
				Role:    "system",
				Content: "[RESUMEN CONSOLIDADO DE SESIONES PREVIAS]:\n" + summary,
			})
		}
	}

	// Historial existente
	existingHistory := e.history.Get(sessionID)
	conversation = append(conversation, existingHistory...)
	conversation = append(conversation, OpenAIMessage{
		Role:    "user",
		Content: userInput,
	})

	isAdmin := true
	if val, ok := ctx.Value("is_admin").(bool); ok {
		isAdmin = val
	}

	// 1. Inyectar Memoria de Trabajo Activa (Scratchpad) si existe tarea en progreso
	activeGoal := ""
	if e.scratchpad != nil {
		if sp, err := e.scratchpad.Get(sessionID); err == nil && sp != nil && sp.Goal != "" {
			activeGoal = sp.Goal
			spBlock := fmt.Sprintf("### MEMORIA DE TRABAJO ACTIVA (SCRATCHPAD):\n- Objetivo en curso: %s\n- Estado: %s (Actualizado: %s)",
				sp.Goal, sp.Status, sp.UpdatedAt.Format("15:04:05"))
			if len(sp.Variables) > 0 {
				spBlock += "\n- Variables tecnicas:"
				for k, v := range sp.Variables {
					spBlock += fmt.Sprintf(" %s=%s;", k, v)
				}
			}
			conversation = append(conversation, OpenAIMessage{
				Role:    "system",
				Content: spBlock,
			})
		}
	}

	// 2. Tool Gating Dinamico: Seleccionar exclusivamente las herramientas requeridas por la intencion
	schemas := e.registry.SelectTools(userInput, activeGoal, isAdmin)

	for iter := 0; iter < e.cfg.MaxIterations; iter++ {
		var currentTools []tools.OpenAIToolSchema
		var toolChoice string

		// Control preventivo de tiempo de turno restante
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 15*time.Second {
			log.Printf("[Engine] Tiempo de turno restante critico (%v < 15s). Forzando sintesis final directa.", time.Until(deadline))
			currentTools = nil
			toolChoice = "none"
			conversation = append(conversation, OpenAIMessage{
				Role:    "user",
				Content: "[AVISO DE SISTEMA]: El tiempo disponible para este turno esta por finalizar. No invoques mas herramientas; redacta inmediatamente la respuesta final completa y concisa.",
			})
		} else if iter >= e.cfg.MaxIterations-2 {
			// En las ultimas dos iteraciones, deshabilitamos herramientas para forzar redaccion final
			currentTools = nil
			toolChoice = "none"
			conversation = append(conversation, OpenAIMessage{
				Role:    "user",
				Content: "[AVISO DE SISTEMA]: Ya dispones de toda la informacion. No invoques mas herramientas; redacta de inmediato la respuesta final clara y completa para el usuario.",
			})
		} else {
			currentTools = schemas
			toolChoice = "auto"
		}

		reqBody := ChatCompletionRequest{
			Model:      e.cfg.Model,
			Messages:   conversation,
			Tools:      currentTools,
			ToolChoice: toolChoice,
		}

		chatResp, err := e.executeCompletion(ctx, reqBody)
		if err != nil {
			return "", fmt.Errorf("error llamando al LLM: %w", err)
		}

		choice := chatResp.Choices[0]
		assistantMsg := choice.Message

		// Agregar el mensaje del asistente a la conversacion en curso
		conversation = append(conversation, assistantMsg)

		// Si el modelo decidio llamar a herramientas, ejecutarlas (paralelo si hay mas de 1)
		if len(assistantMsg.ToolCalls) > 0 {
			numCalls := len(assistantMsg.ToolCalls)
			toolOutputs := make([]string, numCalls)

			// Notificar progreso si hay herramientas pesadas
			if e.progressFn != nil && !strings.HasPrefix(sessionID, "subagent:") && !strings.HasPrefix(sessionID, "cli-") {
				for _, tc := range assistantMsg.ToolCalls {
					if hint, ok := heavyTools[tc.Function.Name]; ok {
						e.progressFn(sessionID, fmt.Sprintf("[En progreso]: %s", hint))
						break
					}
				}
			}

			if numCalls == 1 {
				tc := assistantMsg.ToolCalls[0]
				if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 12*time.Second {
					log.Printf("[Engine] Cancelando ejecucion de tool %s: tiempo limite casi alcanzado (%v)", tc.Function.Name, time.Until(deadline))
					toolOutputs[0] = "Ejecucion omitida: tiempo limite del turno casi alcanzado. Procede de inmediato a redactar la respuesta final."
				} else {
					log.Printf("[Tool Call] Invocando %s (ID: %s) Args: %s", tc.Function.Name, tc.ID, tc.Function.Arguments)
					toolCtx := context.WithValue(ctx, "session_id", sessionID)
					toolCtx = context.WithValue(toolCtx, "is_admin", isAdmin)
					output, err := e.registry.Execute(toolCtx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
					if err != nil {
						output = fmt.Sprintf("Error ejecutando herramienta %s: %v", tc.Function.Name, err)
					}
					if len(output) > 8000 {
						output = output[:8000] + "\n...(salida truncada por longitud)..."
					}
					log.Printf("[Tool Output %s] %d caracteres", tc.Function.Name, len(output))
					toolOutputs[0] = output
				}
			} else {
				log.Printf("[Engine] Invocando %d tool calls concurrentes en paralelo...", numCalls)
				var wg sync.WaitGroup
				wg.Add(numCalls)

				for i, tc := range assistantMsg.ToolCalls {
					go func(idx int, call ToolCall) {
						defer wg.Done()
						if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 12*time.Second {
							log.Printf("[Engine] Cancelando ejecucion paralela de tool %s: tiempo limite casi alcanzado (%v)", call.Function.Name, time.Until(deadline))
							toolOutputs[idx] = "Ejecucion omitida: tiempo limite del turno casi alcanzado. Procede de inmediato a redactar la respuesta final."
							return
						}
						log.Printf("[Tool Call Paralelo] Invocando %s (ID: %s) Args: %s", call.Function.Name, call.ID, call.Function.Arguments)
						toolCtx := context.WithValue(ctx, "session_id", sessionID)
						toolCtx = context.WithValue(toolCtx, "is_admin", isAdmin)
						output, err := e.registry.Execute(toolCtx, call.Function.Name, json.RawMessage(call.Function.Arguments))
						if err != nil {
							output = fmt.Sprintf("Error ejecutando herramienta %s: %v", call.Function.Name, err)
						}
						if len(output) > 8000 {
							output = output[:8000] + "\n...(salida truncada por longitud)..."
						}
						log.Printf("[Tool Output Paralelo %s] %d caracteres", call.Function.Name, len(output))
						toolOutputs[idx] = output
					}(i, tc)
				}
				wg.Wait()
			}

			// Anexar las respuestas preservando el orden original y sus tool_call_id
			for i, tc := range assistantMsg.ToolCalls {
				conversation = append(conversation, OpenAIMessage{
					Role:       "tool",
					Content:    toolOutputs[i],
					ToolCallID: tc.ID,
				})
			}
			// Continuar el bucle para que el modelo interprete las respuestas de las herramientas
			continue
		}

		// Si no hay tool calls o el modelo devolvio texto directo
		finalReply := strings.TrimSpace(assistantMsg.Content)
		if finalReply != "" {
			finalReply = cleanFormatting(finalReply)

			// Guardar interaccion en historial persistente
			e.history.Append(sessionID, OpenAIMessage{Role: "user", Content: userInput})
			e.history.Append(sessionID, OpenAIMessage{Role: "assistant", Content: finalReply})

			// Disparar compactacion semantica asincrona en segundo plano
			e.TriggerAsyncCompaction(sessionID)

			return finalReply, nil
		}
	}

	// Fallback de sintesis forzada: si se agotaron las iteraciones, solicitar redaccion final inmediata
	synthConv := append(conversation, OpenAIMessage{
		Role:    "user",
		Content: "Resume y responde de inmediato al usuario con toda la informacion recopilada hasta este punto sin invocar mas herramientas.",
	})
	synthReq := ChatCompletionRequest{
		Model:      e.cfg.Model,
		Messages:   synthConv,
		ToolChoice: "none",
	}
	if chatResp, err := e.executeCompletion(ctx, synthReq); err == nil && len(chatResp.Choices) > 0 {
		reply := cleanFormatting(chatResp.Choices[0].Message.Content)
		if reply != "" {
			e.history.Append(sessionID, OpenAIMessage{Role: "user", Content: userInput})
			e.history.Append(sessionID, OpenAIMessage{Role: "assistant", Content: reply})
			e.TriggerAsyncCompaction(sessionID)
			return reply, nil
		}
	}

	return "Solicitud procesada con exito en base a los datos del sistema.", nil
}

// Complete genera una respuesta de texto simple dados un systemPrompt y un userPrompt, satisfaciendo harness.LLMCompleter.
func (e *Engine) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []OpenAIMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	req := ChatCompletionRequest{
		Model:    e.cfg.Model,
		Messages: messages,
	}
	resp, err := e.executeCompletion(ctx, req)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("el LLM no retorno opciones de respuesta")
	}
	return cleanFormatting(resp.Choices[0].Message.Content), nil
}

func (e *Engine) getActiveKey() string {
	if e.keyPool != nil && e.keyPool.Count() > 0 {
		return e.keyPool.GetCurrentKey()
	}
	return e.cfg.APIKey
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") || strings.Contains(s, "resource_exhausted") || strings.Contains(s, "rate limit") || strings.Contains(s, "quota")
}

func (e *Engine) executeCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	hasFallback := e.cfg.FallbackAPIBase != ""

	// Si NO hay fallback configurado:
	// Reintentar hasta 3 veces con espera progresiva y rotacion de claves ante 429
	if !hasFallback {
		const maxRetries = 3
		var lastErr error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			activeKey := e.getActiveKey()
			resp, err := e.doRequest(ctx, e.cfg.APIBase, activeKey, e.cfg.Model, req)
			if err == nil {
				e.recordSuccess(true, resp.Usage)
				return resp, nil
			}
			lastErr = err
			e.recordFailure(err)

			if isRateLimitError(err) && e.keyPool != nil && e.keyPool.Count() > 1 {
				_ = e.keyPool.Rotate()
				log.Printf("[KeyPool] Cuota agotada (429/RateLimit). Rotando a siguiente clave de API...")
			} else {
				log.Printf("[LLM Engine] Falla en intento %d/%d (%v). Reintentando...", attempt, maxRetries, err)
			}

			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt*2) * time.Second):
				}
			}
		}
		return nil, fmt.Errorf("error llamando al proveedor de inferencia (%s) tras %d intentos: %w", e.cfg.APIBase, maxRetries, lastErr)
	}

	e.llmMu.RLock()
	inCooldown := time.Now().Before(e.cooldownUntil)
	e.llmMu.RUnlock()

	// 1. Intento con proveedor primario si no esta en cooldown
	if !inCooldown {
		activeKey := e.getActiveKey()
		resp, err := e.doRequest(ctx, e.cfg.APIBase, activeKey, e.cfg.Model, req)
		if err == nil {
			e.recordSuccess(true, resp.Usage)
			return resp, nil
		}

		if isRateLimitError(err) && e.keyPool != nil && e.keyPool.Count() > 1 {
			_ = e.keyPool.Rotate()
			log.Printf("[KeyPool] Cuota agotada en primario. Rotando clave e intentando de inmediato...")
			activeKey = e.getActiveKey()
			resp, err = e.doRequest(ctx, e.cfg.APIBase, activeKey, e.cfg.Model, req)
			if err == nil {
				e.recordSuccess(true, resp.Usage)
				return resp, nil
			}
		}

		// Registrar falla del primario
		e.recordFailure(err)
		log.Printf("[LLM Failover] Falla en proveedor primario (%v). Conmutando inmediatamente a fallback...", err)
	} else {
		log.Printf("[LLM Failover] Proveedor primario en cooldown por fallas previas. Enrutando directo a fallback...")
	}

	// 2. Conmutacion automatica a proveedor de respaldo (Groq / OpenAI-compatible)
	fallbackModel := e.cfg.FallbackModel
	if fallbackModel == "" {
		fallbackModel = "openai/gpt-oss-120b"
	}
	log.Printf("[LLM Failover] Invocando proveedor de respaldo: %s (modelo: %s)...", e.cfg.FallbackAPIBase, fallbackModel)

	fallbackResp, fallbackErr := e.doRequest(ctx, e.cfg.FallbackAPIBase, e.cfg.FallbackAPIKey, fallbackModel, req)
	if fallbackErr == nil {
		e.recordSuccess(false, fallbackResp.Usage)
		log.Println("[LLM Failover] Respuesta exitosa obtenida desde el proveedor de respaldo.")
		return fallbackResp, nil
	}

	log.Printf("[LLM Failover Error] Tambien fallo el proveedor de respaldo: %v", fallbackErr)
	return nil, fmt.Errorf("fallaron ambos proveedores de LLM (primario: %s, fallback: %s): %w", e.cfg.APIBase, e.cfg.FallbackAPIBase, fallbackErr)
}

func (e *Engine) recordSuccess(isPrimary bool, usage *struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}) {
	e.llmMu.Lock()
	defer e.llmMu.Unlock()

	if isPrimary {
		e.llmStatus.TotalPrimaryCalls++
		e.llmStatus.ConsecutiveFailures = 0
		e.llmStatus.PrimaryHealthy = true
		e.llmStatus.FallbackActive = false
	} else {
		e.llmStatus.TotalFallbackCalls++
		e.llmStatus.FallbackActive = true
	}

	if usage != nil {
		e.llmStatus.TotalPromptTokens += int64(usage.PromptTokens)
		e.llmStatus.TotalCompletionTokens += int64(usage.CompletionTokens)
		e.llmStatus.LastPromptTokens = usage.PromptTokens
		e.llmStatus.LastCompletionTokens = usage.CompletionTokens
	}
}

func (e *Engine) recordFailure(err error) {
	e.llmMu.Lock()
	defer e.llmMu.Unlock()

	e.llmStatus.ConsecutiveFailures++
	e.llmStatus.LastFailureTime = time.Now()
	e.llmStatus.LastError = err.Error()
	e.llmStatus.PrimaryHealthy = false

	hasFallback := e.cfg.FallbackAPIBase != ""
	if hasFallback {
		e.llmStatus.FallbackActive = true
		// Si acumula 2 o mas fallas consecutivas, activar cooldown de 30s y lanzar sonda de recuperacion
		if e.llmStatus.ConsecutiveFailures >= 2 {
			e.cooldownUntil = time.Now().Add(30 * time.Second)
			log.Printf("[LLM Circuit Breaker] Activado cooldown de 30s para proveedor primario (%d fallas consecutivas).", e.llmStatus.ConsecutiveFailures)
			go e.probePrimaryRecovery()
		}
	} else {
		e.llmStatus.FallbackActive = false
	}
}

func (e *Engine) probePrimaryRecovery() {
	time.Sleep(15 * time.Second)

	e.llmMu.RLock()
	if e.llmStatus.PrimaryHealthy {
		e.llmMu.RUnlock()
		return
	}
	e.llmMu.RUnlock()

	log.Printf("[LLM Recovery Probe] Ejecutando sonda de verificacion de salud sobre proveedor primario %s...", e.cfg.APIBase)
	probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	probeReq := ChatCompletionRequest{
		Model: e.cfg.Model,
		Messages: []OpenAIMessage{
			{Role: "user", Content: "ping"},
		},
		ToolChoice: "none",
	}

	resp, err := e.doRequest(probeCtx, e.cfg.APIBase, e.getActiveKey(), e.cfg.Model, probeReq)
	if err == nil && len(resp.Choices) > 0 {
		e.llmMu.Lock()
		e.llmStatus.PrimaryHealthy = true
		e.llmStatus.ConsecutiveFailures = 0
		e.llmStatus.FallbackActive = false
		e.cooldownUntil = time.Time{}
		e.llmMu.Unlock()
		log.Printf("[LLM Recovery] Proveedor primario (%s) restablecido exitosamente. Tráfico principal restaurado.", e.cfg.APIBase)
	} else {
		log.Printf("[LLM Recovery Probe] Proveedor primario aun no responde (%v). Se mantiene fallback activo.", err)
	}
}

func (e *Engine) doRequest(ctx context.Context, apiBase, apiKey, model string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	req.Model = model
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error serializando request: %w", err)
	}

	apiURL := strings.TrimRight(apiBase, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creando http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Felix-Go/1.0")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("error de red llamando a %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d desde %s: %s", resp.StatusCode, apiURL, string(respBody))
	}

	var chatResp ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("error deserializando JSON: %w", err)
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return nil, fmt.Errorf("error devuelto por API: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return nil, errors.New("API no retorno choices")
	}

	return &chatResp, nil
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
