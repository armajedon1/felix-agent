package channel

import (
	"context"
	"testing"
)

type mockTranscriber struct {
	result string
}

func (m *mockTranscriber) TranscribeAudio(ctx context.Context, audioPath string) (string, error) {
	return m.result, nil
}

type mockChannel struct {
	name      string
	chType    ChannelType
	connected bool
	sentMsgs  []OutgoingMessage
}

func (m *mockChannel) Name() string {
	return m.name
}

func (m *mockChannel) Type() ChannelType {
	return m.chType
}

func (m *mockChannel) IsConnected() bool {
	return m.connected
}

func (m *mockChannel) Start(ctx context.Context, handler MessageHandler) error {
	m.connected = true
	return nil
}

func (m *mockChannel) Stop() error {
	m.connected = false
	return nil
}

func (m *mockChannel) Send(ctx context.Context, msg OutgoingMessage) error {
	m.sentMsgs = append(m.sentMsgs, msg)
	return nil
}

func TestMultimodalPipeline(t *testing.T) {
	pipe := NewMultimodalPipeline(&mockTranscriber{result: "hola felix"})

	msg := IncomingMessage{
		ChannelType: ChannelTelegram,
		Attachments: []Attachment{
			{
				Type: AttachmentAudio,
				Path: "/tmp/test.ogg",
			},
			{
				Type:     AttachmentImage,
				Path:     "/tmp/photo.jpg",
				MimeType: "image/jpeg",
				Size:     2048,
			},
		},
	}

	err := pipe.Process(context.Background(), &msg)
	if err != nil {
		t.Fatalf("error en pipeline: %v", err)
	}

	if msg.Text == "" {
		t.Fatalf("se esperaba texto transcrito en mensaje")
	}
}

func TestManagerUniversalRouting(t *testing.T) {
	mgr := &Manager{
		channels:    make(map[ChannelType]Channel),
		primaryType: ChannelWhatsApp,
	}

	wa := &mockChannel{name: "WhatsApp", chType: ChannelWhatsApp, connected: true}
	tg := &mockChannel{name: "Telegram", chType: ChannelTelegram, connected: true}

	mgr.RegisterChannel(wa)
	mgr.RegisterChannel(tg)

	ctx := context.Background()

	// 1. Enviar a destino WhatsApp
	_ = mgr.SendMessage(ctx, "whatsapp:5491112345678", "Mensaje para WA")
	if len(wa.sentMsgs) != 1 || wa.sentMsgs[0].Text != "Mensaje para WA" {
		t.Errorf("Mensaje no enrutado a WhatsApp: %+v", wa.sentMsgs)
	}

	// 2. Enviar a destino Telegram
	_ = mgr.SendMessage(ctx, "telegram:987654321", "Mensaje para Telegram")
	if len(tg.sentMsgs) != 1 || tg.sentMsgs[0].Text != "Mensaje para Telegram" {
		t.Errorf("Mensaje no enrutado a Telegram: %+v", tg.sentMsgs)
	}

	// 3. Enviar a JID sin prefijo
	_ = mgr.SendMessage(ctx, "123456789@s.whatsapp.net", "Mensaje JID")
	if len(wa.sentMsgs) != 2 {
		t.Errorf("Mensaje JID no enrutado a WhatsApp: %+v", wa.sentMsgs)
	}
}
