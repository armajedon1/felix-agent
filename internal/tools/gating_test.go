package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type mockTool struct {
	name string
}

func (m *mockTool) Name() string                 { return m.name }
func (m *mockTool) Description() string          { return "Mock " + m.name }
func (m *mockTool) Parameters() map[string]any   { return map[string]any{"type": "object"} }
func (m *mockTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "ok", nil
}

func TestToolGating_SelectTools(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "memory_search"})
	reg.Register(&mockTool{name: "session_search"})
	reg.Register(&mockTool{name: "exec"})
	reg.Register(&mockTool{name: "docker"})
	reg.Register(&mockTool{name: "file_read"})
	reg.Register(&mockTool{name: "web_search"})
	reg.Register(&mockTool{name: "weather"})

	// 1. Para el administrador: SIEMPRE dispone de todas las herramientas en cualquier mensaje
	toolsAdmin := reg.SelectTools("hola", "", true)
	if len(toolsAdmin) != 7 {
		t.Errorf("Se esperaba que el admin tuviera todas las herramientas (7), obtenidas: %d", len(toolsAdmin))
	}

	// 2. Para no-administradores: filtrado estricto por categorias seguras
	toolsNonAdminConv := reg.SelectTools("hola que tal?", "", false)
	for _, schema := range toolsNonAdminConv {
		if schema.Function.Name == "docker" || schema.Function.Name == "exec" || schema.Function.Name == "file_read" {
			t.Errorf("No-admin no deberia tener acceso a '%s' en consulta basica", schema.Function.Name)
		}
	}

	// 3. No-admin con consulta de clima (CatWeb)
	toolsNonAdminWeather := reg.SelectTools("va a llover hoy en buenos aires?", "", false)
	hasWeather := false
	for _, schema := range toolsNonAdminWeather {
		if schema.Function.Name == "weather" {
			hasWeather = true
			break
		}
	}
	if !hasWeather {
		t.Error("Se esperaba que 'weather' estuviera seleccionado para no-admin en consulta de clima")
	}
}
