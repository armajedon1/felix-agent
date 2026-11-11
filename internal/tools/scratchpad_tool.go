package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ScratchpadTool permite al agente consultar, actualizar o limpiar el objetivo de trabajo activo.
type ScratchpadTool struct {
	Manager *ScratchpadManager
}

func (t *ScratchpadTool) Name() string { return "scratchpad" }
func (t *ScratchpadTool) Description() string {
	return "Gestiona la memoria de trabajo activa (Scratchpad). Permite registrar o actualizar el objetivo en curso, variables tecnicas (puertos, rutas, servicios) o marcar la tarea como completada."
}

func (t *ScratchpadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"get", "set", "clear"},
				"description": "Accion: 'get' para ver la tarea activa, 'set' para actualizar objetivo y variables, 'clear' para marcarla completada.",
			},
			"goal": map[string]any{
				"type":        "string",
				"description": "Descripcion concisa del objetivo tecnico en curso.",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Estado actual de avance (ej. 'compilando', 'verificando puerto 5000', 'esperando confirmacion').",
			},
			"variables": map[string]any{
				"type":        "object",
				"description": "Variables clave del flujo en formato clave-valor (ej: {\"port\": \"8080\", \"service\": \"sitio_web\"}).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ScratchpadTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.Manager == nil {
		return "El administrador de scratchpad no esta inicializado.", nil
	}

	sessionID, _ := ctx.Value("session_id").(string)
	if sessionID == "" {
		sessionID = "default"
	}

	var input struct {
		Action    string            `json:"action"`
		Goal      string            `json:"goal"`
		Status    string            `json:"status"`
		Variables map[string]string `json:"variables"`
	}

	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	switch input.Action {
	case "get":
		item, err := t.Manager.Get(sessionID)
		if err != nil {
			return fmt.Sprintf("Error consultando scratchpad: %v", err), nil
		}
		if item == nil || item.Goal == "" {
			return "No hay ninguna tarea activa en el scratchpad de esta sesion.", nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Tarea Activa: %s\nEstado: %s (Actualizado: %s)\n",
			item.Goal, item.Status, item.UpdatedAt.Format("15:04:05")))
		if len(item.Variables) > 0 {
			sb.WriteString("Variables:\n")
			for k, v := range item.Variables {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
			}
		}
		return strings.TrimSpace(sb.String()), nil

	case "set":
		if input.Goal == "" {
			return "Debe especificar un objetivo ('goal').", nil
		}
		status := input.Status
		if status == "" {
			status = "en_progreso"
		}
		if err := t.Manager.Set(sessionID, input.Goal, status, input.Variables); err != nil {
			return fmt.Sprintf("Error actualizando scratchpad: %v", err), nil
		}
		return fmt.Sprintf("Scratchpad actualizado exitosamente: Objetivo: '%s' | Estado: '%s'", input.Goal, status), nil

	case "clear":
		if err := t.Manager.Clear(sessionID); err != nil {
			return fmt.Sprintf("Error limpiando scratchpad: %v", err), nil
		}
		return "Scratchpad limpiado exitosamente. Tarea marcada como completada.", nil

	default:
		return fmt.Sprintf("Accion '%s' no reconocida. Use: get, set, clear.", input.Action), nil
	}
}
