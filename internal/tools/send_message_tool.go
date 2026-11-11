package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SendMessageTool permite al agente enviar mensajes de texto proactivos a contactos de WhatsApp.
type SendMessageTool struct {
	SendFunc   func(ctx context.Context, targetJID string, text string) error
	DefaultJID string
}

func (t *SendMessageTool) Name() string { return "send_message" }
func (t *SendMessageTool) Description() string {
	return "Envia un mensaje de texto directo o proactivo a un contacto de WhatsApp (util para reportes, alertas, notificaciones programadas o comunicacion directa sin esperar el turno del usuario)."
}

func (t *SendMessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_jid": map[string]any{
				"type":        "string",
				"description": "JID o numero de telefono del destinatario en WhatsApp. Si se omite, se envia al administrador principal o al chat actual.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Texto del mensaje a despachar por WhatsApp.",
			},
		},
		"required": []string{"message"},
	}
}

func (t *SendMessageTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.SendFunc == nil {
		return "Canal de envio de mensajes de WhatsApp no inicializado.", nil
	}

	isAdmin, _ := ctx.Value("is_admin").(bool)
	if !isAdmin {
		return "Acceso denegado: solo el administrador puede despachar mensajes proactivos externos.", nil
	}

	var input struct {
		TargetJID string `json:"target_jid"`
		Message   string `json:"message"`
	}

	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("parametros invalidos: %w", err)
	}

	msg := strings.TrimSpace(input.Message)
	if msg == "" {
		return "El contenido del mensaje no puede estar vacio.", nil
	}

	target := strings.TrimSpace(input.TargetJID)
	if target == "" {
		target = t.DefaultJID
	}
	if target == "" {
		if sender, ok := ctx.Value("sender_jid").(string); ok && sender != "" {
			target = sender
		} else {
			return "No se especifico destinatario ni existe administrador por defecto.", nil
		}
	}

	// Normalizar target si solo pasaron numero
	if !strings.Contains(target, "@") {
		clean := cleanTargetPhone(target)
		target = clean + "@s.whatsapp.net"
	}

	if err := t.SendFunc(ctx, target, msg); err != nil {
		return fmt.Sprintf("Error enviando mensaje WhatsApp a %s: %v", target, err), nil
	}

	return fmt.Sprintf("Mensaje proactivo enviado exitosamente a %s (%d caracteres).", target, len(msg)), nil
}

func cleanTargetPhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
