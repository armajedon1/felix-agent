package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// MCPAdaptedTool adapta una definicion de herramienta MCP a la interfaz Tool de Félix-Go.
type MCPAdaptedTool struct {
	client     *Client
	serverName string
	def        MCPToolDefinition
}

// NewMCPAdaptedTool crea un adaptador para una herramienta MCP.
func NewMCPAdaptedTool(client *Client, serverName string, def MCPToolDefinition) *MCPAdaptedTool {
	return &MCPAdaptedTool{
		client:     client,
		serverName: serverName,
		def:        def,
	}
}

func (t *MCPAdaptedTool) Name() string {
	return fmt.Sprintf("mcp_%s_%s", t.serverName, t.def.Name)
}

func (t *MCPAdaptedTool) Description() string {
	desc := t.def.Description
	if desc == "" {
		desc = fmt.Sprintf("Herramienta MCP '%s' provista por el servidor '%s'", t.def.Name, t.serverName)
	}
	return desc
}

func (t *MCPAdaptedTool) Parameters() map[string]any {
	if t.def.InputSchema != nil {
		return t.def.InputSchema
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *MCPAdaptedTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var parsed any
	if len(args) > 0 {
		_ = json.Unmarshal(args, &parsed)
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	return t.client.CallTool(ctx, t.def.Name, parsed)
}
