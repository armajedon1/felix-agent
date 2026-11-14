package agent

import (
	"context"
	"path/filepath"
	"testing"

	"felix-go/internal/config"
	"felix-go/internal/inference"
	"felix-go/internal/tools"
)

type mockLLMProvider struct {
	responses []*ChatCompletionResponse
	calls     int
}

func (m *mockLLMProvider) Execute(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if m.calls < len(m.responses) {
		resp := m.responses[m.calls]
		m.calls++
		return resp, nil
	}
	m.calls++
	return &ChatCompletionResponse{
		Choices: []struct {
			Index        int           `json:"index"`
			Message      OpenAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: "Respuesta final por defecto del mock",
				},
				FinishReason: "stop",
			},
		},
	}, nil
}

func (m *mockLLMProvider) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return "Mock completion", nil
}

func (m *mockLLMProvider) Status() LLMStatus {
	return LLMStatus{PrimaryHealthy: true}
}

func (m *mockLLMProvider) UpdateCredentials(apiBase, apiKey, model string, keys []string) {}
func (m *mockLLMProvider) SetKeyPool(kp *inference.KeyPool)                                {}
func (m *mockLLMProvider) GetKeyPool() *inference.KeyPool                                 { return nil }

func TestReActOrchestratorDirect(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_orch_hist.db")
	histMgr, err := NewHistoryManager(dbPath, 30)
	if err != nil {
		t.Fatalf("Error creando history manager: %v", err)
	}
	defer histMgr.Close()

	reg := tools.NewRegistry()
	reg.Register(&mockEchoTool{name: "status_checker"})

	cfg := &config.Config{
		Model:         "mock-model",
		MaxIterations: 5,
		SystemPrompt:  "Eres un agente de prueba.",
	}

	assembler := NewContextAssembler(cfg, histMgr, nil)
	dispatcher := NewToolDispatcher(reg)

	// Simular turno donde el modelo primero llama a la herramienta y luego responde
	firstResp := &ChatCompletionResponse{}
	firstResp.Choices = append(firstResp.Choices, struct {
		Index        int           `json:"index"`
		Message      OpenAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	}{
		Index: 0,
		Message: OpenAIMessage{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{
					ID:   "call_status",
					Type: "function",
					Function: ToolCallFunction{
						Name:      "status_checker",
						Arguments: `{}`,
					},
				},
			},
		},
		FinishReason: "tool_calls",
	})

	secondResp := &ChatCompletionResponse{}
	secondResp.Choices = append(secondResp.Choices, struct {
		Index        int           `json:"index"`
		Message      OpenAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	}{
		Index: 0,
		Message: OpenAIMessage{
			Role:    "assistant",
			Content: "El sistema se encuentra 100% operativo.",
		},
		FinishReason: "stop",
	})

	mockProv := &mockLLMProvider{
		responses: []*ChatCompletionResponse{firstResp, secondResp},
	}

	orchestrator := NewReActOrchestrator(
		cfg,
		reg,
		histMgr,
		mockProv,
		assembler,
		dispatcher,
		nil,
	)

	reply, err := orchestrator.Run(context.Background(), "session-orch-1", "¿cual es el estado?", "")
	if err != nil {
		t.Fatalf("Orchestrator.Run fallo: %v", err)
	}

	expected := "El sistema se encuentra 100% operativo."
	if reply != expected {
		t.Errorf("Respuesta esperada '%s', recibida '%s'", expected, reply)
	}

	if mockProv.calls != 2 {
		t.Errorf("Se esperaban 2 llamadas al LLM (tool_call + respuesta final), llamadas: %d", mockProv.calls)
	}

	// Verificar persistencia en historial
	hist := histMgr.Get("session-orch-1")
	if len(hist) != 2 {
		t.Fatalf("Se esperaban 2 mensajes en historial, encontrados: %d", len(hist))
	}
	if hist[0].Role != "user" || hist[1].Role != "assistant" {
		t.Errorf("Roles en historial incorrectos: %+v", hist)
	}
}
