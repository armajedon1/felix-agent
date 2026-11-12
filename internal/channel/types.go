package channel

import (
	"context"
)

type ChannelType string

const (
	ChannelWhatsApp ChannelType = "whatsapp"
	ChannelTelegram ChannelType = "telegram"
	ChannelCLI      ChannelType = "cli"
)

type AttachmentType string

const (
	AttachmentAudio    AttachmentType = "audio"
	AttachmentImage    AttachmentType = "image"
	AttachmentDocument AttachmentType = "document"
)

type Attachment struct {
	Type     AttachmentType `json:"type"`
	Path     string         `json:"path"`
	MimeType string         `json:"mime_type"`
	FileName string         `json:"file_name"`
	Size     int64          `json:"size"`
}

type IncomingMessage struct {
	ID          string            `json:"id"`
	ChannelType ChannelType       `json:"channel_type"`
	SenderID    string            `json:"sender_id"`
	SenderName  string            `json:"sender_name"`
	ChatID      string            `json:"chat_id"`
	Text        string            `json:"text"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Timestamp   int64             `json:"timestamp"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	IsVoice     bool              `json:"is_voice"`
}

type OutgoingMessage struct {
	TargetChatID string            `json:"target_chat_id"`
	Text         string            `json:"text"`
	VoicePath    string            `json:"voice_path,omitempty"`
	VoiceBytes   []byte            `json:"voice_bytes,omitempty"`
	ImagePath    string            `json:"image_path,omitempty"`
	DocumentPath string            `json:"document_path,omitempty"`
	FileName     string            `json:"file_name,omitempty"`
	ReplyToID    string            `json:"reply_to_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type MessageHandler func(ctx context.Context, msg IncomingMessage) error

type Channel interface {
	Name() string
	Type() ChannelType
	Start(ctx context.Context, handler MessageHandler) error
	Stop() error
	Send(ctx context.Context, msg OutgoingMessage) error
	IsConnected() bool
}
