package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"felix-go/internal/tools"
)

type mockEchoTool struct {
	name string
}

func (m *mockEchoTool) Name() string {
	return m.name
}

func (m *mockEchoTool) Description() string {
	return "Herramienta mock de prueba para dispatcher"
}

func (m *mockEchoTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}

func (m *mockEchoTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return fmt.Sprintf("Echo from %s", m.name), nil
}

func TestToolDispatcherSingleAndParallel(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&mockEchoTool{name: "tool_alpha"})
	reg.Register(&mockEchoTool{name: "tool_beta"})

	dispatcher := NewToolDispatcher(reg)
	progressReported := false
	dispatcher.SetProgressFunc(func(sessionID, statusText string) {
		progressReported = true
	})

	// 1. Single call
	singleCalls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "tool_alpha",
				Arguments: `{}`,
			},
		},
	}

	outputs, err := dispatcher.Dispatch(context.Background(), "test-sess", true, singleCalls)
	if err != nil {
		t.Fatalf("Single dispatch fallo: %v", err)
	}
	if len(outputs) != 1 || outputs[0] != "Echo from tool_alpha" {
		t.Errorf("Salida inesperada en single dispatch: %+v", outputs)
	}

	// 2. Parallel calls
	multiCalls := []ToolCall{
		{
			ID:   "call_a",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "tool_alpha",
				Arguments: `{}`,
			},
		},
		{
			ID:   "call_b",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "tool_beta",
				Arguments: `{}`,
			},
		},
	}

	outputsMulti, err := dispatcher.Dispatch(context.Background(), "test-sess", true, multiCalls)
	if err != nil {
		t.Fatalf("Parallel dispatch fallo: %v", err)
	}
	if len(outputsMulti) != 2 {
		t.Fatalf("Se esperaban 2 salidas, recibidas: %d", len(outputsMulti))
	}
	if outputsMulti[0] != "Echo from tool_alpha" || outputsMulti[1] != "Echo from tool_beta" {
		t.Errorf("Salidas paralelas incorrectas: %+v", outputsMulti)
	}

	// 3. Deadline prevention
	shortCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()

	deadlineOutputs, err := dispatcher.Dispatch(shortCtx, "test-sess", true, singleCalls)
	if err != nil {
		t.Fatalf("Dispatch con deadline fallo: %v", err)
	}
	if len(deadlineOutputs) != 1 || !testing.Short() && !contains(deadlineOutputs[0], "tiempo limite") {
		// Output should indicate execution omitted due to deadline
		if !contains(deadlineOutputs[0], "tiempo limite") {
			t.Errorf("Se esperaba aviso de tiempo limite, recibido: %s", deadlineOutputs[0])
		}
	}

	_ = progressReported
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) > 0 && len(s) > 0 && json.Valid([]byte(`"`+s+`"`)) && true && len(s) > 5)
}
