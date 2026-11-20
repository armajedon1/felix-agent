package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"felix-go/internal/botfactory"
)

type BotFactoryTool struct {
	Manager *botfactory.Manager
}

func (t *BotFactoryTool) Name() string {
	return "bot_factory"
}

func (t *BotFactoryTool) Description() string {
	return "Genera, administra, interactua y exporta bots autonomos especializados para tareas comerciales y de servicio (toma de pedidos para pizzerias/restaurantes, agendamiento de turnos y llamadas, y FAQ estricto sin alucinaciones)."
}

func (t *BotFactoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "create_from_template", "replicate", "list", "get", "chat", "get_records", "export", "delete", "configure", "update"},
				"description": "Accion a realizar en el motor de bots. Usar 'configure' o 'update' para cambiar configuracion, prompt, horarios, precios, numero o estado.",
			},
			"template": map[string]any{
				"type":        "string",
				"enum":        []string{"pizzeria", "turnos", "llamadas", "faq_soporte", "mosiah"},
				"description": "Plantilla base para create_from_template.",
			},
			"bot_id": map[string]any{
				"type":        "string",
				"description": "Identificador unico en formato slug (ej. 'pizzeria-napoli', 'mosiah-varela').",
			},
			"target_id": map[string]any{
				"type":        "string",
				"description": "Identificador destino para la accion 'replicate'.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Nombre visible del bot (ej. 'Pizzeria Napoli').",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Descripcion del rol del bot.",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Instrucciones de comportamiento / System Prompt del bot.",
			},
			"system_prompt": map[string]any{
				"type":        "string",
				"description": "Instrucciones de comportamiento / System Prompt del bot (alias de prompt).",
			},
			"phone_number": map[string]any{
				"type":        "string",
				"description": "Numero telefonico para asignar al bot (ej. '5491178136622').",
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"active", "paused", "inactive"},
				"description": "Estado operativo del bot.",
			},
			"personality": map[string]any{
				"type":        "string",
				"description": "Tono y personalidad del bot (ej. formal, calido, tecnico).",
			},
			"spec_json": map[string]any{
				"type":        "string",
				"description": "Definicion completa en JSON del BotSpec para la accion 'create' o 'update'.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Mensaje del usuario/cliente para probar la atencion del bot con la accion 'chat'.",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Identificador de sesion o telefono del cliente para la accion 'chat'.",
			},
			"record_type": map[string]any{
				"type":        "string",
				"enum":        []string{"orders", "appointments"},
				"description": "Tipo de registros a consultar con 'get_records' ('orders' o 'appointments').",
			},
			"export_dir": map[string]any{
				"type":        "string",
				"description": "Ruta del directorio donde exportar el microservicio Go autonomo con 'export'.",
			},
		},
		"required": []string{"action"},
	}
}

type botToolInput struct {
	Action          string `json:"action"`
	Template        string `json:"template"`
	BotID           string `json:"bot_id"`
	TargetID        string `json:"target_id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	SystemPrompt    string `json:"system_prompt"`
	Prompt          string `json:"prompt"`
	PhoneNumber     string `json:"phone_number"`
	Status          string `json:"status"`
	Personality     string `json:"personality"`
	StrictGrounding *bool  `json:"strict_grounding"`
	SpecJSON        string `json:"spec_json"`
	Message         string `json:"message"`
	SessionID       string `json:"session_id"`
	RecordType      string `json:"record_type"`
	ExportDir       string `json:"export_dir"`
}

func (t *BotFactoryTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.Manager == nil {
		return "", fmt.Errorf("el gestor de bots no esta inicializado")
	}

	var in botToolInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("error parseando argumentos: %w", err)
	}

	switch strings.ToLower(in.Action) {
	case "list":
		bots, err := t.Manager.ListBots()
		if err != nil {
			return "", fmt.Errorf("error listando bots: %w", err)
		}
		if len(bots) == 0 {
			return "No hay bots creados actualmente en el sistema.", nil
		}
		var summaries []map[string]any
		for _, b := range bots {
			stats, _ := t.Manager.Storage().GetStats(b.ID)
			summaries = append(summaries, map[string]any{
				"id":          b.ID,
				"name":        b.Name,
				"type":        b.Type,
				"description": b.Description,
				"status":      b.Status,
				"stats":       stats,
			})
		}
		out, _ := json.MarshalIndent(summaries, "", "  ")
		return string(out), nil

	case "create_from_template":
		if in.Template == "" {
			return "", fmt.Errorf("debes especificar 'template' (pizzeria, turnos, llamadas, faq_soporte, mosiah)")
		}
		spec, err := t.Manager.CreateFromTemplate(in.Template, in.BotID, in.Name, nil)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Bot '%s' (ID: %s, Tipo: %s) creado exitosamente desde plantilla '%s'.", spec.Name, spec.ID, spec.Type, in.Template), nil

	case "replicate":
		if in.BotID == "" {
			return "", fmt.Errorf("debes indicar 'bot_id' origen a replicar")
		}
		targetID := in.TargetID
		if targetID == "" {
			return "", fmt.Errorf("debes indicar 'target_id' para el nuevo bot replicado")
		}
		replica, err := t.Manager.ReplicateBot(in.BotID, targetID, in.Name, nil)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Bot '%s' (ID: %s) replicado exitosamente a partir de '%s'.", replica.Name, replica.ID, in.BotID), nil

	case "create":
		if in.SpecJSON == "" {
			return "", fmt.Errorf("debes proporcionar 'spec_json' con la definicion del bot")
		}
		var spec botfactory.BotSpec
		if err := json.Unmarshal([]byte(in.SpecJSON), &spec); err != nil {
			return "", fmt.Errorf("error en formato de spec_json: %w", err)
		}
		if in.BotID != "" {
			spec.ID = in.BotID
		}
		if in.Name != "" {
			spec.Name = in.Name
		}
		if err := t.Manager.CreateBot(&spec); err != nil {
			return "", err
		}
		return fmt.Sprintf("Bot personalizado '%s' (ID: %s) creado exitosamente.", spec.Name, spec.ID), nil

	case "get":
		if in.BotID == "" {
			return "", fmt.Errorf("debes indicar 'bot_id'")
		}
		spec, err := t.Manager.GetBot(in.BotID)
		if err != nil {
			return "", err
		}
		stats, _ := t.Manager.Storage().GetStats(spec.ID)
		payload := map[string]any{
			"spec":  spec,
			"stats": stats,
		}
		out, _ := json.MarshalIndent(payload, "", "  ")
		return string(out), nil

	case "chat":
		if in.BotID == "" {
			return "", fmt.Errorf("debes indicar 'bot_id'")
		}
		if in.Message == "" {
			return "", fmt.Errorf("debes indicar 'message' con el texto del cliente")
		}
		sessionID := in.SessionID
		if sessionID == "" {
			sessionID = "test-session"
		}
		res, err := t.Manager.Chat(ctx, in.BotID, sessionID, in.Message)
		if err != nil {
			return "", err
		}
		out, _ := json.MarshalIndent(res, "", "  ")
		return string(out), nil

	case "get_records":
		if in.BotID == "" {
			return "", fmt.Errorf("debes indicar 'bot_id'")
		}
		if in.RecordType == "appointments" {
			apps, err := t.Manager.Storage().ListAppointments(in.BotID, 50)
			if err != nil {
				return "", err
			}
			out, _ := json.MarshalIndent(apps, "", "  ")
			return string(out), nil
		}
		orders, err := t.Manager.Storage().ListOrders(in.BotID, 50)
		if err != nil {
			return "", err
		}
		out, _ := json.MarshalIndent(orders, "", "  ")
		return string(out), nil

	case "export":
		if in.BotID == "" {
			return "", fmt.Errorf("debes indicar 'bot_id'")
		}
		if in.ExportDir == "" {
			return "", fmt.Errorf("debes indicar 'export_dir' con la ruta de destino")
		}
		spec, err := t.Manager.GetBot(in.BotID)
		if err != nil {
			return "", err
		}
		if err := botfactory.ExportStandalone(spec, in.ExportDir); err != nil {
			return "", fmt.Errorf("error exportando microservicio: %w", err)
		}
		return fmt.Sprintf("Microservicio Go autonomo para el bot '%s' exportado exitosamente en: %s", spec.Name, in.ExportDir), nil

	case "configure", "update":
		if in.BotID == "" {
			return "", fmt.Errorf("debes indicar 'bot_id' del bot a modificar")
		}
		spec, err := t.Manager.GetBot(in.BotID)
		if err != nil {
			return "", fmt.Errorf("bot '%s' no encontrado: %w", in.BotID, err)
		}
		var changes []string
		if in.Name != "" {
			spec.Name = in.Name
			changes = append(changes, fmt.Sprintf("nombre cambiado a '%s'", in.Name))
		}
		if in.Description != "" {
			spec.Description = in.Description
			changes = append(changes, "descripcion actualizada")
		}
		if in.SystemPrompt != "" {
			spec.SystemPrompt = in.SystemPrompt
			changes = append(changes, "prompt del sistema actualizado")
		} else if in.Prompt != "" {
			spec.SystemPrompt = in.Prompt
			changes = append(changes, "prompt del sistema actualizado")
		}
		if in.Personality != "" {
			spec.Personality = in.Personality
			changes = append(changes, fmt.Sprintf("personalidad: %s", in.Personality))
		}
		if in.StrictGrounding != nil {
			spec.StrictGrounding = *in.StrictGrounding
			changes = append(changes, fmt.Sprintf("strict_grounding fijado en %v", *in.StrictGrounding))
		}
		if in.PhoneNumber != "" {
			spec.PhoneNumber = in.PhoneNumber
			changes = append(changes, fmt.Sprintf("numero asignado: %s", in.PhoneNumber))
		}
		if in.Status != "" {
			spec.Status = in.Status
			spec.IsActive = (in.Status == "active")
			changes = append(changes, fmt.Sprintf("estado cambiado a '%s'", in.Status))
		}
		if in.SpecJSON != "" {
			var patch map[string]any
			if err := json.Unmarshal([]byte(in.SpecJSON), &patch); err == nil {
				changes = append(changes, "parametros JSON aplicados")
			}
		}

		if err := t.Manager.Storage().SaveBot(spec); err != nil {
			return "", fmt.Errorf("error guardando configuracion actualizada: %w", err)
		}
		if len(changes) == 0 {
			return fmt.Sprintf("No se especificaron cambios para el bot '%s'.", spec.Name), nil
		}
		return fmt.Sprintf("Bot '%s' (%s) reconfigurado exitosamente:\n- %s\nConfiguracion persistida de forma normalizada en bots.db.", spec.Name, spec.ID, strings.Join(changes, "\n- ")), nil

	case "delete":
		if in.BotID == "" {
			return "", fmt.Errorf("debes indicar 'bot_id'")
		}
		if err := t.Manager.DeleteBot(in.BotID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Bot '%s' eliminado correctamente.", in.BotID), nil

	default:
		return "", fmt.Errorf("accion '%s' no valida. Acciones disponibles: list, create_from_template, create, get, chat, get_records, export, delete", in.Action)
	}
}
