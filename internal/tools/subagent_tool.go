package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// SubagentRunner define la interfaz desacoplada para despachar subagentes concurrentes.
type SubagentRunner interface {
	Spawn(title, objective string, timeoutMinutes int) (string, error)
	SpawnWithRole(title, objective, role string, timeoutMinutes int) (string, error)
}

// SubagentSpawnTool permite al agente principal delegar tareas de investigacion o resolucion compleja en un subagente autonomo en segundo plano.
type SubagentSpawnTool struct {
	Runner SubagentRunner
}

func (t *SubagentSpawnTool) Name() string { return "subagent_spawn" }
func (t *SubagentSpawnTool) Description() string {
	return "Despacha un subagente autonomo en segundo plano con arquetipo de especializacion ('researcher', 'coder', 'sysadmin', 'auditor', 'general'). Opera de forma independiente y genera un informe entregable estructurado."
}

func (t *SubagentSpawnTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Titulo corto y descriptivo de la mision del subagente.",
			},
			"objective": map[string]any{
				"type":        "string",
				"description": "Instrucciones detalladas y objetivo que el subagente debe investigar, diagnosticar o ejecutar de forma autonoma.",
			},
			"role": map[string]any{
				"type":        "string",
				"enum":        []string{"general", "researcher", "coder", "sysadmin", "auditor"},
				"description": "Arquetipo de especializacion: 'researcher' (investigacion y lectura), 'coder' (desarrollo y tests), 'sysadmin' (servidores y redes), 'auditor' (seguridad y consistencia), 'general' (estandar).",
			},
			"timeout_minutes": map[string]any{
				"type":        "integer",
				"description": "Tiempo maximo en minutos permitido para la tarea (por defecto 10 minutos).",
			},
		},
		"required": []string{"title", "objective"},
	}
}

func (t *SubagentSpawnTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.Runner == nil {
		return "El administrador de subagentes no esta configurado.", nil
	}

	var input struct {
		Title          string `json:"title"`
		Objective      string `json:"objective"`
		Role           string `json:"role"`
		TimeoutMinutes int    `json:"timeout_minutes"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	if input.Role == "" {
		input.Role = "general"
	}

	taskID, err := t.Runner.SpawnWithRole(input.Title, input.Objective, input.Role, input.TimeoutMinutes)
	if err != nil {
		return fmt.Sprintf("Error iniciando subagente: %v", err), nil
	}

	return fmt.Sprintf("Subagente autonomo despachado con exito (ID: `%s`, Rol: `%s`). Mision: '%s'. Ejecutando en segundo plano; te notificare por WhatsApp con el informe completo cuando termine.", taskID, input.Role, input.Title), nil
}
