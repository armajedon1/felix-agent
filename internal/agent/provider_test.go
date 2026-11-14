package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"felix-go/internal/config"
	"felix-go/internal/inference"
)

func TestFailoverProviderDirect(t *testing.T) {
	primaryCalls := 0
	primaryMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error": {"message": "502 Bad Gateway"}}`))
	}))
	defer primaryMock.Close()

	fallbackCalls := 0
	fallbackMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		resp := ChatCompletionResponse{
			ID: "chatcmpl-provider-test",
		}
		resp.Choices = append(resp.Choices, struct {
			Index        int           `json:"index"`
			Message      OpenAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			Index: 0,
			Message: OpenAIMessage{
				Role:    "assistant",
				Content: "Respuesta desde FallbackProvider",
			},
			FinishReason: "stop",
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer fallbackMock.Close()

	cfg := &config.Config{
		APIBase:         primaryMock.URL,
		APIKey:          "key-1",
		Model:           "primary-model",
		FallbackAPIBase: fallbackMock.URL,
		FallbackAPIKey:  "key-fallback",
		FallbackModel:   "fallback-model",
	}

	keyPool := inference.NewKeyPool("key-1")
	provider := NewFailoverProvider(cfg, keyPool, nil)

	req := ChatCompletionRequest{
		Model: cfg.Model,
		Messages: []OpenAIMessage{
			{Role: "user", Content: "ping"},
		},
	}

	resp, err := provider.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute fallo inesperadamente: %v", err)
	}

	if primaryCalls != 1 || fallbackCalls != 1 {
		t.Errorf("Llamadas incorrectas: primario=%d, fallback=%d", primaryCalls, fallbackCalls)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "Respuesta desde FallbackProvider" {
		t.Errorf("Respuesta inesperada: %+v", resp)
	}

	st := provider.Status()
	if st.PrimaryHealthy {
		t.Errorf("PrimaryHealthy deberia ser false")
	}
	if !st.FallbackActive {
		t.Errorf("FallbackActive deberia ser true")
	}
}
