package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type SendVoiceFunc func(ctx context.Context, targetJID, textOrAudioPath string, isText bool) error

// SendVoiceTool permite a Félix-Go sintetizar texto a audio o enviar notas de voz nativas (PTT) por WhatsApp.
type SendVoiceTool struct {
	SendVoiceFunc SendVoiceFunc
	DefaultJID    string
}

func (t *SendVoiceTool) Name() string { return "send_voice" }
func (t *SendVoiceTool) Description() string {
	return "Sintetiza un mensaje de texto en voz humana ultra-natural (Mateo Neural rioplatense) o envia un archivo de audio como una nota de voz nativa (PTT) de WhatsApp con onda de audio para reproducir directamente en el chat."
}

func (t *SendVoiceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "Texto a sintetizar y narrar con voz en espanol rioplatense (Mateo Neural).",
			},
			"audio_path": map[string]any{
				"type":        "string",
				"description": "Ruta a un archivo de audio existente (.mp3, .ogg, .wav, .m4a) que se convertira y enviara como nota de voz nativa.",
			},
			"target_jid": map[string]any{
				"type":        "string",
				"description": "JID destino opcional (por defecto el chat de Matías).",
			},
		},
	}
}

func (t *SendVoiceTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.SendVoiceFunc == nil {
		return "", errors.New("funcion SendVoice no configurada en el cliente")
	}

	var params struct {
		Text       string `json:"text"`
		AudioPath  string `json:"audio_path"`
		TargetJID  string `json:"target_jid"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
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

	if params.Text != "" {
		if err := t.SendVoiceFunc(ctx, target, params.Text, true); err != nil {
			return "", fmt.Errorf("error enviando nota de voz sintetizada: %w", err)
		}
		return fmt.Sprintf("Nota de voz sintetizada y enviada exitosamente a WhatsApp (%d caracteres narrados).", len(params.Text)), nil
	}

	if params.AudioPath != "" {
		if err := t.SendVoiceFunc(ctx, target, params.AudioPath, false); err != nil {
			return "", fmt.Errorf("error enviando archivo de audio como nota de voz: %w", err)
		}
		return fmt.Sprintf("Archivo de audio '%s' enviado como nota de voz nativa a WhatsApp.", params.AudioPath), nil
	}

	return "", errors.New("debes proveer 'text' para sintetizar voz o 'audio_path' con un archivo de audio")
}
