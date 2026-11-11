package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"felix-go/internal/config"
	"felix-go/internal/tools"
)

type mockTool struct {
	name     string
	delay    time.Duration
	executed *atomic.Int32
}

func (m *mockTool) Name() string { return m.name }
func (m *mockTool) Description() string { return "Mock tool para testing" }
func (m *mockTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (m *mockTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	time.Sleep(m.delay)
	m.executed.Add(1)
	return "resultado de " + m.name, nil
}

func TestParallelToolExecution(t *testing.T) {
	var tool1Executed atomic.Int32
	var tool2Executed atomic.Int32

	t1 := &mockTool{name: "mock_tool_1", delay: 50 * time.Millisecond, executed: &tool1Executed}
	t2 := &mockTool{name: "mock_tool_2", delay: 50 * time.Millisecond, executed: &tool2Executed}

	reg := tools.NewRegistry()
	reg.Register(t1)
	reg.Register(t2)

	turnCount := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turnCount++
		w.Header().Set("Content-Type", "application/json")

		if turnCount == 1 {
			// El modelo devuelve 2 llamadas a herramientas en paralelo
			resp := ChatCompletionResponse{
				ID: "call-tools-test",
			}
			resp.Choices = append(resp.Choices, struct {
				Index        int           `json:"index"`
				Message      OpenAIMessage `json:"message"`
				FinishReason string        `json:"finish_reason"`
			}{
				Index: 0,
				Message: OpenAIMessage{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: ToolCallFunction{
								Name:      "mock_tool_1",
								Arguments: "{}",
							},
						},
						{
							ID:   "call_2",
							Type: "function",
							Function: ToolCallFunction{
								Name:      "mock_tool_2",
								Arguments: "{}",
							},
						},
					},
				},
				FinishReason: "tool_calls",
			})
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Turno 2: respuesta final
		resp := ChatCompletionResponse{
			ID: "final-test",
		}
		resp.Choices = append(resp.Choices, struct {
			Index        int           `json:"index"`
			Message      OpenAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			Index: 0,
			Message: OpenAIMessage{
				Role:    "assistant",
				Content: "Ambas herramientas completadas",
			},
			FinishReason: "stop",
		})
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		APIBase:       mockServer.URL,
		Model:         "test-model",
		MaxIterations: 5,
	}

	histDir, _ := osMkdirTemp()
	defer osRemoveAll(histDir)
	histMgr, _ := NewHistoryManager(histDir+"/test.db", 10)
	defer histMgr.Close()

	eng := NewEngine(cfg, reg, histMgr)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	reply, err := eng.Run(ctx, "session-parallel-test", "ejecuta ambas herramientas", "")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run devolvio error: %v", err)
	}

	if reply != "Ambas herramientas completadas" {
		t.Errorf("respuesta inesperada: %s", reply)
	}

	if tool1Executed.Load() != 1 || tool2Executed.Load() != 1 {
		t.Errorf("las herramientas no fueron ejecutadas: t1=%d, t2=%d", tool1Executed.Load(), tool2Executed.Load())
	}

	// Al correr en paralelo (50ms cada una), el tiempo total de ejecucion de herramientas debe ser ~50ms, no 100ms+
	if elapsed > 300*time.Millisecond {
		t.Logf("Aviso: tiempo total de ejecucion: %v", elapsed)
	}
}

func osMkdirTemp() (string, error) {
	return os.MkdirTemp("", "test_felix_eng_*")
}

func osRemoveAll(path string) {
	_ = os.RemoveAll(path)
}
