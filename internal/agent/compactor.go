package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// CompactSession comprime de forma semantica los mensajes antiguos de una sesion,
// conservando los hechos clave en session_summaries y podando session_messages para mantener la ventana de contexto optima.
func (e *Engine) CompactSession(ctx context.Context, sessionID string, keepRecent int) error {
	if keepRecent <= 0 {
		keepRecent = 12
	}

	totalCount, err := e.history.Count(sessionID)
	if err != nil {
		return fmt.Errorf("error contando mensajes de sesion: %w", err)
	}

	totalChars, _ := e.history.TotalCharacters(sessionID)

	// Compactar si se supera el umbral de mensajes O si el volumen de caracteres es critico (> 24.000 caracteres / ~6.000 tokens)
	shouldCompact := (totalCount > keepRecent+6) || (totalCount > keepRecent && totalChars > 24000)
	if !shouldCompact {
		return nil
	}

	numToCompact := totalCount - keepRecent
	oldMessages, err := e.history.GetOldestMessages(sessionID, numToCompact)
	if err != nil {
		return fmt.Errorf("error obteniendo mensajes antiguos: %w", err)
	}
	if len(oldMessages) == 0 {
		return nil
	}

	existingSummary, _, _ := e.history.GetSummary(sessionID)

	var sb strings.Builder
	for _, m := range oldMessages {
		content := m.Content
		if len(content) > 500 {
			content = content[:500] + "...(truncado)"
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, content))
	}

	prompt := fmt.Sprintf(`Eres el motor de compactacion semantica de contexto para Félix (agente de ingenieria y homelab).
Tu objetivo es consolidar el historial conversacional previo entre Matías y el asistente sin perder ninguna informacion esencial.

Instrucciones estrictas:
1. Extrae y conserva en viñetas concisas:
   - Decisiones tecnicas acordadas (arquitectura, librerias, puertos, rutas de archivos, servicios).
   - Preferencias personales, directivas activas y datos facticos mencionados por Matías.
   - Tareas pendientes, recordatorios o estados de implementacion.
   - Modificaciones realizadas en el servidor, PM2 o Docker.
2. Si ya existe un resumen previo, integralo de forma armonica y sintetica con los nuevos mensajes, actualizando lo que haya cambiado.
3. Formato: lista de viñetas tecnicas sobrias, concisas y directas, sin emojis ni introducciones.

Resumen consolidado previo:
%s

Mensajes a incorporar en el resumen:
%s`, strings.TrimSpace(existingSummary), strings.TrimSpace(sb.String()))

	compReq := ChatCompletionRequest{
		Model: e.cfg.Model,
		Messages: []OpenAIMessage{
			{Role: "user", Content: prompt},
		},
		ToolChoice: "none",
	}

	compCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	resp, err := e.executeCompletion(compCtx, compReq)
	if err != nil {
		return fmt.Errorf("error en llamada LLM de compactacion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return fmt.Errorf("el LLM no retorno opciones de compactacion")
	}

	newSummary := cleanFormatting(resp.Choices[0].Message.Content)
	if newSummary == "" {
		return fmt.Errorf("el resumen generado esta vacio")
	}

	lastCompactID := oldMessages[len(oldMessages)-1].ID
	if err := e.history.SaveSummary(sessionID, newSummary, lastCompactID); err != nil {
		return fmt.Errorf("error guardando nuevo resumen: %w", err)
	}

	if err := e.history.DeleteMessagesUpTo(sessionID, lastCompactID); err != nil {
		return fmt.Errorf("error eliminando mensajes compactados: %w", err)
	}

	log.Printf("[Context Compactor] Sesion %s compactada con exito: %d mensajes consolidados hasta ID %d | Resumen: %d caracteres",
		sessionID, len(oldMessages), lastCompactID, len(newSummary))

	return nil
}

// TriggerAsyncCompaction lanza la compactacion en una goroutine independiente para no demorar la respuesta de WhatsApp.
func (e *Engine) TriggerAsyncCompaction(sessionID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := e.CompactSession(ctx, sessionID, 12); err != nil {
			log.Printf("[Context Compactor Warning] Sesion %s: %v", sessionID, err)
		}
	}()
}
