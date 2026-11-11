package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"felix-go/internal/config"
	"felix-go/internal/tools"
)

func TestLLMFailoverAutomatic(t *testing.T) {
	// 1. Mock Server Primario que simula caida 502 Bad Gateway
	primaryCalls := 0
	primaryMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error": {"message": "502 Bad Gateway from upstream model proxy"}}`))
	}))
	defer primaryMock.Close()

	// 2. Mock Server de Fallback (Groq) que responde exitosamente
	fallbackCalls := 0
	fallbackMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		resp := ChatCompletionResponse{
			ID: "chatcmpl-test-fallback",
		}
		resp.Choices = append(resp.Choices, struct {
			Index        int           `json:"index"`
			Message      OpenAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			Index: 0,
			Message: OpenAIMessage{
				Role:    "assistant",
				Content: "Respuesta exitosa generada por el proveedor de respaldo Groq",
			},
			FinishReason: "stop",
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer fallbackMock.Close()

	// 3. Inicializar Engine configurado con primario caido y fallback operativo
	cfg := &config.Config{
		APIBase:         primaryMock.URL,
		APIKey:          "test-primary-key",
		Model:           "gemini-3.8-flash",
		FallbackAPIBase: fallbackMock.URL,
		FallbackAPIKey:  "test-fallback-key",
		FallbackModel:   "openai/gpt-oss-120b",
		MaxIterations:   5,
	}

	reg := tools.NewRegistry()
	engine := NewEngine(cfg, reg, nil)

	// 4. Ejecutar peticion
	req := ChatCompletionRequest{
		Model: cfg.Model,
		Messages: []OpenAIMessage{
			{Role: "user", Content: "Hola, que modelo esta activo?"},
		},
	}

	resp, err := engine.executeCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("executeCompletion fallo inesperadamente: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatalf("Respuesta no contiene choices")
	}

	content := resp.Choices[0].Message.Content
	expected := "Respuesta exitosa generada por el proveedor de respaldo Groq"
	if content != expected {
		t.Errorf("Contenido recibido '%s', esperado '%s'", content, expected)
	}

	// 5. Verificar metricas y contadores de failover
	if primaryCalls != 1 {
		t.Errorf("Se esperaba 1 llamada al primario, se registraron: %d", primaryCalls)
	}
	if fallbackCalls != 1 {
		t.Errorf("Se esperaba 1 llamada al fallback, se registraron: %d", fallbackCalls)
	}

	st := engine.GetLLMStatus()
	if st.PrimaryHealthy {
		t.Errorf("PrimaryHealthy deberia ser false tras la falla del primario")
	}
	if st.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures esperado 1, obtenido: %d", st.ConsecutiveFailures)
	}
	if st.TotalFallbackCalls != 1 {
		t.Errorf("TotalFallbackCalls esperado 1, obtenido: %d", st.TotalFallbackCalls)
	}
	if !st.FallbackActive {
		t.Errorf("FallbackActive deberia ser true")
	}
}

func TestLLMAntigravityExclusiveRetry(t *testing.T) {
	// Mock Server Primario que falla la primera vez y responde exitosamente en el segundo intento
	attempts := 0
	primaryMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error": {"message": "502 Bad Gateway"}}`))
			return
		}

		resp := ChatCompletionResponse{
			ID: "chatcmpl-test-primary-retry",
		}
		resp.Choices = append(resp.Choices, struct {
			Index        int           `json:"index"`
			Message      OpenAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			Index: 0,
			Message: OpenAIMessage{
				Role:    "assistant",
				Content: "Respuesta exitosa de Antigravity Tools tras recuperacion",
			},
			FinishReason: "stop",
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer primaryMock.Close()

	// Sin fallback configurado
	cfg := &config.Config{
		APIBase:       primaryMock.URL,
		APIKey:        "***",
		Model:         "gemini-3.8-flash",
		MaxIterations: 5,
	}

	reg := tools.NewRegistry()
	engine := NewEngine(cfg, reg, nil)

	req := ChatCompletionRequest{
		Model: cfg.Model,
		Messages: []OpenAIMessage{
			{Role: "user", Content: "Test reintento"},
		},
	}

	resp, err := engine.executeCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("executeCompletion fallo inesperadamente: %v", err)
	}

	if attempts != 2 {
		t.Errorf("Se esperaban 2 intentos en el primario, registrados: %d", attempts)
	}

	content := resp.Choices[0].Message.Content
	expected := "Respuesta exitosa de Antigravity Tools tras recuperacion"
	if content != expected {
		t.Errorf("Contenido recibido '%s', esperado '%s'", content, expected)
	}

	st := engine.GetLLMStatus()
	if !st.PrimaryHealthy {
		t.Errorf("PrimaryHealthy deberia ser true tras el reintento exitoso")
	}
	if st.FallbackActive {
		t.Errorf("FallbackActive deberia ser false")
	}
}
