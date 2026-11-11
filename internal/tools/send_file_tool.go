package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type SendFileFunc func(ctx context.Context, targetJID, filePath, caption string) error

// SendFileTool permite al agente enviar archivos, documentos, binarios o imagenes directamente a WhatsApp.
type SendFileTool struct {
	SendFunc   SendFileFunc
	DefaultJID string
}

func (t *SendFileTool) Name() string { return "send_file" }
func (t *SendFileTool) Description() string {
	return "Envia un archivo local (documento, PDF, imagen, script, binario, comprimido zip/tar) directamente al usuario por WhatsApp."
}
func (t *SendFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Ruta absoluta o relativa al archivo que se desea enviar al usuario.",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Mensaje o pie de texto opcional que acompaña al archivo en WhatsApp.",
			},
			"target_jid": map[string]any{
				"type":        "string",
				"description": "JID o numero destino opcional (por defecto el chat actual).",
			},
		},
		"required": []string{"file_path"},
	}
}

func (t *SendFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.SendFunc == nil {
		return "", errors.New("funcion de envio de archivos no configurada en el cliente")
	}

	var params struct {
		FilePath  string `json:"file_path"`
		Caption   string `json:"caption"`
		TargetJID string `json:"target_jid"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.FilePath == "" {
		return "", errors.New("file_path es obligatorio")
	}

	info, err := os.Stat(params.FilePath)
	if err != nil {
		return "", fmt.Errorf("archivo no encontrado en %s: %w", params.FilePath, err)
	}

	target := params.TargetJID
	if target == "" {
		target = t.DefaultJID
	}
	if target == "" {
		if sender, ok := ctx.Value("sender_jid").(string); ok && sender != "" {
			target = sender
		} else {
			return "", fmt.Errorf("no se especifico destinatario ni existe administrador por defecto")
		}
	}

	if err := t.SendFunc(ctx, target, params.FilePath, params.Caption); err != nil {
		return "", fmt.Errorf("error enviando archivo por WhatsApp: %w", err)
	}

	return fmt.Sprintf("Archivo '%s' (%d bytes) enviado exitosamente a WhatsApp.", info.Name(), info.Size()), nil
}
