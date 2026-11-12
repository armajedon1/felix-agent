package channel

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// Transcriber define la interfaz para transcribir audios de cualquier canal.
type Transcriber interface {
	TranscribeAudio(ctx context.Context, audioPath string) (string, error)
}

// MultimodalPipeline enriquece el texto del mensaje entrante procesando audios, imagenes y documentos de forma agnostica al canal.
type MultimodalPipeline struct {
	transcriber Transcriber
}

// NewMultimodalPipeline inicializa el pipeline multimodal compartido.
func NewMultimodalPipeline(t Transcriber) *MultimodalPipeline {
	return &MultimodalPipeline{
		transcriber: t,
	}
}

// SetTranscriber actualiza el transcriptor de audio (Whisper).
func (p *MultimodalPipeline) SetTranscriber(t Transcriber) {
	p.transcriber = t
}

// Process enriquece el mensaje entrante con transcripciones de audio y referencias a medios guardados en disco.
func (p *MultimodalPipeline) Process(ctx context.Context, msg *IncomingMessage) error {
	var notices []string

	for _, att := range msg.Attachments {
		switch att.Type {
		case AttachmentAudio:
			if p.transcriber != nil && att.Path != "" {
				log.Printf("[Multimodal %s] Transcribiendo audio: %s...", msg.ChannelType, att.Path)
				transcript, err := p.transcriber.TranscribeAudio(ctx, att.Path)
				if err != nil {
					log.Printf("[Multimodal Warning] Error transcribiendo audio (%s): %v", att.Path, err)
					notices = append(notices, fmt.Sprintf("[Nota de voz recibida: %s. No se pudo transcribir automaticamente: %v]", att.Path, err))
				} else if transcript != "" {
					log.Printf("[Multimodal Exito] Audio transcrito: \"%s\"", transcript)
					notices = append(notices, fmt.Sprintf("[Nota de voz transcrita: \"%s\"]", transcript))
					if msg.Text == "" {
						msg.Text = transcript
					}
				}
			}

		case AttachmentImage:
			if att.Path != "" {
				notice := fmt.Sprintf("[Imagen adjunta recibida: %s (Tamano: %d bytes, Tipo: %s). Utiliza la herramienta vision_inspect con image_path=\"%s\" para transcribir su texto o analizar su contenido visual.]", att.Path, att.Size, att.MimeType, att.Path)
				notices = append(notices, notice)
			}

		case AttachmentDocument:
			if att.Path != "" {
				notice := fmt.Sprintf("[Archivo recibido en el servidor: %s (Nombre original: %s, Tamano: %d bytes, Tipo: %s). El archivo ya se encuentra en disco listo para operar con las herramientas del sistema.]", att.Path, att.FileName, att.Size, att.MimeType)
				notices = append(notices, notice)
			}
		}
	}

	if len(notices) > 0 {
		header := strings.Join(notices, "\n")
		if msg.Text != "" && !strings.Contains(header, msg.Text) {
			msg.Text = header + "\nMensaje acompanante del usuario: " + msg.Text
		} else if msg.Text == "" {
			msg.Text = header
		}
	}

	return nil
}
