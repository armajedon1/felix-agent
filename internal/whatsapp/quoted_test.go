package whatsapp

import (
	"strings"
	"testing"

	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestExtractMsgText_QuotedMessage(t *testing.T) {
	// 1. Mensaje de texto simple sin cita
	simpleMsg := &waProto.Message{
		Conversation: proto.String("Hola Felix"),
	}
	res := extractMsgText(simpleMsg)
	if res != "Hola Felix" {
		t.Errorf("Se esperaba 'Hola Felix', obtenido: %q", res)
	}

	// 2. ExtendedTextMessage con cita de conversacion previa
	quotedConversation := "Este es el mensaje previo citado"
	extMsgWithQuote := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String("Que opinas de esto?"),
			ContextInfo: &waProto.ContextInfo{
				QuotedMessage: &waProto.Message{
					Conversation: proto.String(quotedConversation),
				},
			},
		},
	}
	res = extractMsgText(extMsgWithQuote)
	expected := "[En respuesta a: \"Este es el mensaje previo citado\"]\nQue opinas de esto?"
	if res != expected {
		t.Errorf("Cita no coincidente.\nEsperado: %q\nObtenido: %q", expected, res)
	}

	// 3. ExtendedTextMessage citando una imagen con caption
	extMsgQuoteImg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String("Me gusta esa foto"),
			ContextInfo: &waProto.ContextInfo{
				QuotedMessage: &waProto.Message{
					ImageMessage: &waProto.ImageMessage{
						Caption: proto.String("Logo nuevo"),
					},
				},
			},
		},
	}
	res = extractMsgText(extMsgQuoteImg)
	expected = "[En respuesta a: \"[Imagen: Logo nuevo]\"]\nMe gusta esa foto"
	if res != expected {
		t.Errorf("Cita de imagen no coincidente.\nEsperado: %q\nObtenido: %q", expected, res)
	}

	// 4. ExtendedTextMessage citando un audio
	extMsgQuoteAudio := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String("Escuche tu audio"),
			ContextInfo: &waProto.ContextInfo{
				QuotedMessage: &waProto.Message{
					AudioMessage: &waProto.AudioMessage{},
				},
			},
		},
	}
	res = extractMsgText(extMsgQuoteAudio)
	expected = "[En respuesta a: \"[Audio/Nota de voz]\"]\nEscuche tu audio"
	if res != expected {
		t.Errorf("Cita de audio no coincidente.\nEsperado: %q\nObtenido: %q", expected, res)
	}

	// 5. Cita larga truncada a 250 caracteres
	longText := strings.Repeat("A", 300)
	extMsgQuoteLong := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String("Respuesta"),
			ContextInfo: &waProto.ContextInfo{
				QuotedMessage: &waProto.Message{
					Conversation: proto.String(longText),
				},
			},
		},
	}
	res = extractMsgText(extMsgQuoteLong)
	if !strings.Contains(res, "...") || !strings.Contains(res, "[En respuesta a: \"") {
		t.Errorf("Se esperaba truncamiento de cita larga, obtenido: %q", res)
	}
}

func TestSplitTextForWhatsApp(t *testing.T) {
	// 1. Texto menor que el limite: 1 solo chunk
	short := "Hola mundo"
	chunks := splitTextForWhatsApp(short, 100)
	if len(chunks) != 1 || chunks[0] != "Hola mundo" {
		t.Errorf("se esperaba 1 chunk, obtenido: %v", chunks)
	}

	// 2. Texto largo con salto de linea: debe dividir en el salto
	longWithNewline := "Primera linea larga para probar la division\nSegunda linea que continua el texto"
	chunks = splitTextForWhatsApp(longWithNewline, 45)
	if len(chunks) != 2 {
		t.Fatalf("se esperaban 2 chunks, obtenido %d: %v", len(chunks), chunks)
	}
	if chunks[0] != "Primera linea larga para probar la division" {
		t.Errorf("chunk 0 inesperado: %q", chunks[0])
	}
	if chunks[1] != "Segunda linea que continua el texto" {
		t.Errorf("chunk 1 inesperado: %q", chunks[1])
	}
}
