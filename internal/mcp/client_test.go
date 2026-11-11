package mcp

import (
	"encoding/json"
	"testing"
)

func TestMCPAdaptedTool(t *testing.T) {
	def := MCPToolDefinition{
		Name:        "get_weather",
		Description: "Obtiene el clima actual",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{"type": "string"},
			},
		},
	}

	adapted := NewMCPAdaptedTool(nil, "clima_server", def)

	if adapted.Name() != "mcp_clima_server_get_weather" {
		t.Errorf("nombre inesperado: %s", adapted.Name())
	}
	if adapted.Description() != "Obtiene el clima actual" {
		t.Errorf("descripcion inesperada: %s", adapted.Description())
	}
	params := adapted.Parameters()
	if params["type"] != "object" {
		t.Errorf("schema de parametros invalido: %v", params)
	}
}

func TestJSONRPCSerialization(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  map[string]any{"client": "Felix"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("error serializando JSON-RPC: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("error deserializando JSON: %v", err)
	}

	if parsed["jsonrpc"] != "2.0" || parsed["method"] != "initialize" {
		t.Errorf("datos JSON-RPC incorrectos: %v", parsed)
	}
}
