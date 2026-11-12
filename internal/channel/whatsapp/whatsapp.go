package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"felix-go/internal/channel"
	"felix-go/internal/whatsapp"
)

// WhatsAppChannel adapta el cliente whatsmeow a la interfaz channel.Channel.
type WhatsAppChannel struct {
	client *whatsapp.Client
}

// NewWhatsAppChannel envuelve una instancia activa de whatsapp.Client.
func NewWhatsAppChannel(client *whatsapp.Client) *WhatsAppChannel {
	return &WhatsAppChannel{
		client: client,
	}
}

func (w *WhatsAppChannel) Name() string {
	return "WhatsApp"
}

func (w *WhatsAppChannel) Type() channel.ChannelType {
	return channel.ChannelWhatsApp
}

func (w *WhatsAppChannel) IsConnected() bool {
	if w.client == nil {
		return false
	}
	return w.client.IsConnected()
}

func (w *WhatsAppChannel) Start(ctx context.Context, handler channel.MessageHandler) error {
	if w.client == nil {
		return fmt.Errorf("cliente de WhatsApp no inicializado")
	}
	return w.client.Start(ctx)
}

func (w *WhatsAppChannel) Stop() error {
	if w.client != nil {
		w.client.Disconnect()
	}
	return nil
}

func (w *WhatsAppChannel) Send(ctx context.Context, msg channel.OutgoingMessage) error {
	if w.client == nil {
		return fmt.Errorf("cliente de WhatsApp no inicializado")
	}
	targetChat := strings.TrimPrefix(msg.TargetChatID, "whatsapp:")

	if msg.VoicePath != "" {
		return w.client.SendVoice(ctx, targetChat, msg.VoicePath, false)
	}

	if len(msg.VoiceBytes) > 0 {
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("voice_%d.ogg", time.Now().UnixNano()))
		if err := os.WriteFile(tmpFile, msg.VoiceBytes, 0644); err == nil {
			defer os.Remove(tmpFile)
			return w.client.SendVoice(ctx, targetChat, tmpFile, false)
		}
	}

	if msg.ImagePath != "" {
		return w.client.SendFile(ctx, targetChat, msg.ImagePath, msg.Text)
	}

	if msg.DocumentPath != "" {
		return w.client.SendFile(ctx, targetChat, msg.DocumentPath, msg.Text)
	}

	return w.client.SendMessage(ctx, targetChat, msg.Text)
}

func (w *WhatsAppChannel) GetClient() *whatsapp.Client {
	return w.client
}
