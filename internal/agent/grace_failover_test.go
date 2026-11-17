package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"felix-go/internal/config"
	"felix-go/internal/tools"
)

func TestFastFailoverOnPrimaryHang(t *testing.T) {
	// 1. Mock Server Primario que se cuelga (simula upstream colgado)
	primaryMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"tarde"}}]}`))
	}))
	defer primaryMock.Close()

	// 2. Mock Server de Fallback ultrarrapido (Groq)
	fallbackCalls := 0
	fallbackMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		resp := ChatCompletionResponse{
			ID: "chatcmpl-fallback-fast",
		}
		resp.Choices = append(resp.Choices, struct {
			Index        int           `json:"index"`
			Message      OpenAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			Index: 0,
			Message: OpenAIMessage{
				Role:    "assistant",
				Content: "Respuesta rapida desde fallback",
			},
			FinishReason: "stop",
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer fallbackMock.Close()

	cfg := &config.Config{
		APIBase:         primaryMock.URL,
		APIKey:          "key-prim",
		Model:           "gemini-2.5-flash",
		FallbackAPIBase: fallbackMock.URL,
		FallbackAPIKey:  "key-fb",
		FallbackModel:   "openai/gpt-oss-120b",
		MaxIterations:   5,
	}

	reg := tools.NewRegistry()
	engine := NewEngine(cfg, reg, nil)

	// Context padre con timeout corto (500ms)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := ChatCompletionRequest{
		Model: cfg.Model,
		Messages: []OpenAIMessage{
			{Role: "user", Content: "ping"},
		},
	}

	resp, err := engine.executeCompletion(ctx, req)
	if err != nil {
		t.Fatalf("executeCompletion fallo: %v", err)
	}

	if fallbackCalls != 1 {
		t.Errorf("Se esperaba 1 llamada al fallback tras timeout primario, obtenidas: %d", fallbackCalls)
	}

	if resp.Choices[0].Message.Content != "Respuesta rapida desde fallback" {
		t.Errorf("Contenido inesperado: %s", resp.Choices[0].Message.Content)
	}
}

func TestSummarizeExecutedTools(t *testing.T) {
	messages := []OpenAIMessage{
		{Role: "user", Content: "ejecuta git status"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1"}}},
		{Role: "tool", Content: "On branch main\nnothing to commit, working tree clean", ToolCallID: "call_1"},
	}

	summary := summarizeExecutedTools(messages)
	if !strings.Contains(summary, "On branch main") {
		t.Fatalf("summarizeExecutedTools no rescato la salida de la tool: %s", summary)
	}
}
