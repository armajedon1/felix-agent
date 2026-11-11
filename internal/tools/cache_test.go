package tools

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

type countingWeatherTool struct {
	calls *atomic.Int32
}

func (c *countingWeatherTool) Name() string        { return "weather" }
func (c *countingWeatherTool) Description() string { return "Clima de prueba" }
func (c *countingWeatherTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (c *countingWeatherTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	count := c.calls.Add(1)
	return "Pronostico " + string(args) + " llamada " + string(rune('0'+count)), nil
}

func TestToolCache(t *testing.T) {
	reg := NewRegistry()
	var calls atomic.Int32
	wt := &countingWeatherTool{calls: &calls}
	reg.Register(wt)

	ctx := context.Background()
	args := json.RawMessage(`{"city":"Buenos Aires"}`)

	// 1. Primera ejecucion: debe invocar a la herramienta
	res1, err := reg.Execute(ctx, "weather", args)
	if err != nil {
		t.Fatalf("error en ejecucion 1: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("se esperaba 1 llamada real, obtenido %d", calls.Load())
	}

	// 2. Segunda ejecucion identica: debe ser servida desde cache sin incrementar calls
	res2, err := reg.Execute(ctx, "weather", args)
	if err != nil {
		t.Fatalf("error en ejecucion 2: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("la segunda llamada debio ser servida desde cache, pero calls=%d", calls.Load())
	}
	if res1 != res2 {
		t.Errorf("resultado cacheado difiere: %q vs %q", res1, res2)
	}

	// 3. Limpiar cache: tercera ejecucion debe invocar a la herramienta nuevamente
	reg.cache.Clear()
	_, _ = reg.Execute(ctx, "weather", args)
	if calls.Load() != 2 {
		t.Errorf("tras limpiar cache debio llamar nuevamente, calls=%d", calls.Load())
	}
}
