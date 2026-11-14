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
)

// LLMProvider desacopla la inferencia y el transporte HTTP del motor del agente.
type LLMProvider interface {
	Execute(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	Status() LLMStatus
	UpdateCredentials(apiBase, apiKey, model string, keys []string)
	SetKeyPool(kp *inference.KeyPool)
	GetKeyPool() *inference.KeyPool
}

// FailoverProvider implementa LLMProvider con conmutacion automatica primario/fallback,
// rotacion de claves de API ante 429 y recuperacion progresiva con circuito de enfriamiento.
type FailoverProvider struct {
	cfg           *config.Config
	httpClient    *http.Client
	keyPool       *inference.KeyPool
	llmMu         sync.RWMutex
	llmStatus     LLMStatus
	cooldownUntil time.Time
}

// NewFailoverProvider crea una nueva instancia de FailoverProvider.
func NewFailoverProvider(cfg *config.Config, keyPool *inference.KeyPool, client *http.Client) *FailoverProvider {
	if client == nil {
		tr := &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		}
		client = &http.Client{
			Transport: tr,
			Timeout:   120 * time.Second,
		}
	}

	return &FailoverProvider{
		cfg:        cfg,
		httpClient: client,
		keyPool:    keyPool,
		llmStatus: LLMStatus{
			PrimaryHealthy:   true,
			PrimaryEndpoint:  cfg.APIBase,
			PrimaryModel:     cfg.Model,
			FallbackEndpoint: cfg.FallbackAPIBase,
			FallbackModel:    cfg.FallbackModel,
		},
	}
}

func (p *FailoverProvider) GetKeyPool() *inference.KeyPool {
	p.llmMu.RLock()
	defer p.llmMu.RUnlock()
	return p.keyPool
}

func (p *FailoverProvider) SetKeyPool(kp *inference.KeyPool) {
	p.llmMu.Lock()
	defer p.llmMu.Unlock()
	p.keyPool = kp
}

func (p *FailoverProvider) UpdateCredentials(apiBase, apiKey, model string, keys []string) {
	p.llmMu.Lock()
	defer p.llmMu.Unlock()
	if apiBase != "" {
		p.cfg.APIBase = apiBase
		p.llmStatus.PrimaryEndpoint = apiBase
	}
	if model != "" {
		p.cfg.Model = model
		p.llmStatus.PrimaryModel = model
	}
	if apiKey != "" {
		p.cfg.APIKey = apiKey
	}
	if len(keys) > 0 {
		p.cfg.APIKeys = keys
		if p.keyPool != nil {
			p.keyPool.SetKeys(keys)
		}
	} else if apiKey != "" && p.keyPool != nil {
		p.keyPool.SetKeys([]string{apiKey})
	}
}

func (p *FailoverProvider) Status() LLMStatus {
	p.llmMu.RLock()
	defer p.llmMu.RUnlock()
	st := p.llmStatus
	st.PrimaryEndpoint = p.cfg.APIBase
	st.PrimaryModel = p.cfg.Model
	st.FallbackEndpoint = p.cfg.FallbackAPIBase
	st.FallbackModel = p.cfg.FallbackModel
	return st
}

func (p *FailoverProvider) getActiveKey() string {
	if p.keyPool != nil && p.keyPool.Count() > 0 {
		return p.keyPool.GetCurrentKey()
	}
	return p.cfg.APIKey
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") || strings.Contains(s, "resource_exhausted") || strings.Contains(s, "rate limit") || strings.Contains(s, "quota")
}

func (p *FailoverProvider) Execute(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	hasFallback := p.cfg.FallbackAPIBase != ""

	// Si NO hay fallback configurado:
	// Reintentar hasta 3 veces con espera progresiva y rotacion de claves ante 429
	if !hasFallback {
		const maxRetries = 3
		var lastErr error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			activeKey := p.getActiveKey()
			resp, err := p.doRequest(ctx, p.cfg.APIBase, activeKey, p.cfg.Model, req)
			if err == nil {
				p.recordSuccess(true, resp.Usage)
				return resp, nil
			}
			lastErr = err
			p.recordFailure(err)

			if isRateLimitError(err) && p.keyPool != nil && p.keyPool.Count() > 1 {
				_ = p.keyPool.Rotate()
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
		return nil, fmt.Errorf("error llamando al proveedor de inferencia (%s) tras %d intentos: %w", p.cfg.APIBase, maxRetries, lastErr)
	}

	p.llmMu.RLock()
	inCooldown := time.Now().Before(p.cooldownUntil)
	p.llmMu.RUnlock()

	// 1. Intento con proveedor primario si no esta en cooldown
	if !inCooldown {
		activeKey := p.getActiveKey()
		resp, err := p.doRequest(ctx, p.cfg.APIBase, activeKey, p.cfg.Model, req)
		if err == nil {
			p.recordSuccess(true, resp.Usage)
			return resp, nil
		}

		if isRateLimitError(err) && p.keyPool != nil && p.keyPool.Count() > 1 {
			_ = p.keyPool.Rotate()
			log.Printf("[KeyPool] Cuota agotada en primario. Rotando clave e intentando de inmediato...")
			activeKey = p.getActiveKey()
			resp, err = p.doRequest(ctx, p.cfg.APIBase, activeKey, p.cfg.Model, req)
			if err == nil {
				p.recordSuccess(true, resp.Usage)
				return resp, nil
			}
		}

		// Registrar falla del primario
		p.recordFailure(err)
		log.Printf("[LLM Failover] Falla en proveedor primario (%v). Conmutando inmediatamente a fallback...", err)
	} else {
		log.Printf("[LLM Failover] Proveedor primario en cooldown por fallas previas. Enrutando directo a fallback...")
	}

	// 2. Conmutacion automatica a proveedor de respaldo (Groq / OpenAI-compatible)
	fallbackModel := p.cfg.FallbackModel
	if fallbackModel == "" {
		fallbackModel = "openai/gpt-oss-120b"
	}
	log.Printf("[LLM Failover] Invocando proveedor de respaldo: %s (modelo: %s)...", p.cfg.FallbackAPIBase, fallbackModel)

	fallbackResp, fallbackErr := p.doRequest(ctx, p.cfg.FallbackAPIBase, p.cfg.FallbackAPIKey, fallbackModel, req)
	if fallbackErr == nil {
		p.recordSuccess(false, fallbackResp.Usage)
		log.Println("[LLM Failover] Respuesta exitosa obtenida desde el proveedor de respaldo.")
		return fallbackResp, nil
	}

	log.Printf("[LLM Failover Error] Tambien fallo el proveedor de respaldo: %v", fallbackErr)
	return nil, fmt.Errorf("fallaron ambos proveedores de LLM (primario: %s, fallback: %s): %w", p.cfg.APIBase, p.cfg.FallbackAPIBase, fallbackErr)
}

func (p *FailoverProvider) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	compCtx, compCancel := context.WithTimeout(ctx, 90*time.Second)
	defer compCancel()

	messages := []OpenAIMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	req := ChatCompletionRequest{
		Model:    p.cfg.Model,
		Messages: messages,
	}
	resp, err := p.Execute(compCtx, req)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("el LLM no retorno opciones de respuesta")
	}
	return cleanFormatting(resp.Choices[0].Message.Content), nil
}

func (p *FailoverProvider) recordSuccess(isPrimary bool, usage *struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}) {
	p.llmMu.Lock()
	defer p.llmMu.Unlock()

	if isPrimary {
		p.llmStatus.TotalPrimaryCalls++
		p.llmStatus.ConsecutiveFailures = 0
		p.llmStatus.PrimaryHealthy = true
		p.llmStatus.FallbackActive = false
	} else {
		p.llmStatus.TotalFallbackCalls++
		p.llmStatus.FallbackActive = true
	}

	if usage != nil {
		p.llmStatus.TotalPromptTokens += int64(usage.PromptTokens)
		p.llmStatus.TotalCompletionTokens += int64(usage.CompletionTokens)
		p.llmStatus.LastPromptTokens = usage.PromptTokens
		p.llmStatus.LastCompletionTokens = usage.CompletionTokens
	}
}

func (p *FailoverProvider) recordFailure(err error) {
	p.llmMu.Lock()
	defer p.llmMu.Unlock()

	p.llmStatus.ConsecutiveFailures++
	p.llmStatus.LastFailureTime = time.Now()
	p.llmStatus.LastError = err.Error()
	p.llmStatus.PrimaryHealthy = false

	hasFallback := p.cfg.FallbackAPIBase != ""
	if hasFallback {
		p.llmStatus.FallbackActive = true
		if p.llmStatus.ConsecutiveFailures >= 2 {
			p.cooldownUntil = time.Now().Add(30 * time.Second)
			log.Printf("[LLM Circuit Breaker] Activado cooldown de 30s para proveedor primario (%d fallas consecutivas).", p.llmStatus.ConsecutiveFailures)
			go p.probePrimaryRecovery()
		}
	} else {
		p.llmStatus.FallbackActive = false
	}
}

func (p *FailoverProvider) probePrimaryRecovery() {
	time.Sleep(15 * time.Second)

	p.llmMu.RLock()
	if p.llmStatus.PrimaryHealthy {
		p.llmMu.RUnlock()
		return
	}
	p.llmMu.RUnlock()

	log.Printf("[LLM Recovery Probe] Ejecutando sonda de verificacion de salud sobre proveedor primario %s...", p.cfg.APIBase)
	probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	probeReq := ChatCompletionRequest{
		Model: p.cfg.Model,
		Messages: []OpenAIMessage{
			{Role: "user", Content: "ping"},
		},
		ToolChoice: "none",
	}

	resp, err := p.doRequest(probeCtx, p.cfg.APIBase, p.getActiveKey(), p.cfg.Model, probeReq)
	if err == nil && len(resp.Choices) > 0 {
		p.llmMu.Lock()
		p.llmStatus.PrimaryHealthy = true
		p.llmStatus.ConsecutiveFailures = 0
		p.llmStatus.FallbackActive = false
		p.cooldownUntil = time.Time{}
		p.llmMu.Unlock()
		log.Printf("[LLM Recovery] Proveedor primario (%s) restablecido exitosamente. Tráfico principal restaurado.", p.cfg.APIBase)
	} else {
		log.Printf("[LLM Recovery Probe] Proveedor primario aun no responde (%v). Se mantiene fallback activo.", err)
	}
}

func (p *FailoverProvider) doRequest(ctx context.Context, apiBase, apiKey, model string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
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

	resp, err := p.httpClient.Do(httpReq)
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
