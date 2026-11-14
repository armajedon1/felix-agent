package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type slowHangingTool struct{}

func (s *slowHangingTool) Name() string                { return "slow_hanging_tool" }
func (s *slowHangingTool) Description() string         { return "Herramienta que se cuelga intencionalmente" }
func (s *slowHangingTool) Parameters() map[string]any { return map[string]any{} }
func (s *slowHangingTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(2 * time.Second):
		return "completado tarde", nil
	}
}

type oversizedTool struct{}

func (o *oversizedTool) Name() string                { return "oversized_tool" }
func (o *oversizedTool) Description() string         { return "Herramienta que emite volumen excesivo" }
func (o *oversizedTool) Parameters() map[string]any { return map[string]any{} }
func (o *oversizedTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return strings.Repeat("M", 40000), nil
}

func TestRegistry_TimeoutAndTruncationHardening(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&slowHangingTool{})
	reg.Register(&oversizedTool{})

	// 1. Probar timeout con contexto corto
	shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := reg.Execute(shortCtx, "slow_hanging_tool", json.RawMessage("{}"))
	if err == nil {
		t.Fatalf("Se esperaba fallo por timeout/context deadline en slow_hanging_tool")
	}

	// 2. Probar truncamiento de salida masiva
	out, err := reg.Execute(context.Background(), "oversized_tool", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("Execute oversized_tool fallo: %v", err)
	}

	if len(out) >= 40000 {
		t.Errorf("La salida debio ser truncada por debajo de 40000 chars, longitud: %d", len(out))
	}
	if !strings.Contains(out, "salida truncada") {
		t.Errorf("Se esperaba etiqueta de salida truncada en el resultado")
	}
}
