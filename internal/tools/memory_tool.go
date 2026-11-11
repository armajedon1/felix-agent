package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"felix-go/internal/memory"
)

// MemoryManagerTool permite al agente gestionar y actualizar su memoria persistente.
type MemoryManagerTool struct {
	Store *memory.Store
}

func (t *MemoryManagerTool) Name() string { return "memory_manager" }
func (t *MemoryManagerTool) Description() string {
	return "Administra la memoria persistente de Félix: registrar notas diarias en memory/YYYY-MM-DD.md, registrar directivas activas de Matías en USER.md o asentar decisiones y hechos tecnicos en MEMORY.md."
}

func (t *MemoryManagerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"append_daily", "save_user_directive", "save_durable_fact"},
				"description": "Accion a realizar en la memoria persistente.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Texto, directiva o hecho durable a registrar.",
			},
		},
		"required": []string{"action", "content"},
	}
}

func (t *MemoryManagerTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.Store == nil {
		return "", errors.New("almacenamiento de memoria no configurado")
	}

	var params struct {
		Action  string `json:"action"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Content == "" {
		return "", errors.New("content es requerido")
	}

	switch params.Action {
	case "append_daily":
		if err := t.Store.AppendDailyNote(params.Content); err != nil {
			return "", fmt.Errorf("error agregando nota diaria: %w", err)
		}
		return "Nota agregada exitosamente a la bitacora diaria.", nil
	case "save_user_directive":
		if err := t.Store.AppendUserDirective(params.Content); err != nil {
			return "", fmt.Errorf("error guardando directiva en USER.md: %w", err)
		}
		return "Directiva registrada exitosamente en USER.md.", nil
	case "save_durable_fact":
		if err := t.Store.AppendDurableFact(params.Content); err != nil {
			return "", fmt.Errorf("error guardando hecho en MEMORY.md: %w", err)
		}
		return "Hecho durable registrado exitosamente en MEMORY.md.", nil
	default:
		return "", fmt.Errorf("accion de memoria desconocida: %s", params.Action)
	}
}
