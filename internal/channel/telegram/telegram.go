package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"felix-go/internal/channel"
)

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
}

type PhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int64  `json:"file_size"`
}

type Voice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type Audio struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size"`
}

type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size"`
}

type Message struct {
	MessageID int         `json:"message_id"`
	From      *User       `json:"from,omitempty"`
	Chat      Chat        `json:"chat"`
	Date      int64       `json:"date"`
	Text      string      `json:"text,omitempty"`
	Caption   string      `json:"caption,omitempty"`
	Voice     *Voice      `json:"voice,omitempty"`
	Audio     *Audio      `json:"audio,omitempty"`
	Photo     []PhotoSize `json:"photo,omitempty"`
	Document  *Document   `json:"document,omitempty"`
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

type GetUpdatesResponse struct {
	OK          bool     `json:"ok"`
	Result      []Update `json:"result"`
	Description string   `json:"description,omitempty"`
}

type GetFileResponse struct {
	OK          bool `json:"ok"`
	Result      struct {
		FileID   string `json:"file_id"`
		FilePath string `json:"file_path"`
		FileSize int64  `json:"file_size"`
	} `json:"result"`
	Description string `json:"description,omitempty"`
}

type GetMeResponse struct {
	OK          bool `json:"ok"`
	Result      struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		Username  string `json:"username"`
	} `json:"result"`
	Description string `json:"description,omitempty"`
}

// TelegramChannel implementa channel.Channel usando la API oficial de Telegram Bot (long-polling HTTP).
type TelegramChannel struct {
	token        string
	apiBase      string
	downloadsDir string
	allowedUsers []string
	httpClient   *http.Client
	pollClient   *http.Client
	handler      channel.MessageHandler
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	connected    int32
	botUsername  string
	botID        int64
}

// NewTelegramChannel inicializa un conector ligero a Telegram Bot API.
func NewTelegramChannel(token string, downloadsDir string, allowedUsers []string) *TelegramChannel {
	cleanToken := strings.TrimSpace(token)
	if downloadsDir == "" {
		downloadsDir = "./data/downloads"
	}

	return &TelegramChannel{
		token:        cleanToken,
		apiBase:      "https://api.telegram.org",
		downloadsDir: downloadsDir,
		allowedUsers: allowedUsers,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		pollClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (t *TelegramChannel) Name() string {
	return "Telegram"
}

func (t *TelegramChannel) Type() channel.ChannelType {
	return channel.ChannelTelegram
}

func (t *TelegramChannel) IsConnected() bool {
	return atomic.LoadInt32(&t.connected) == 1
}

// Start valida el bot con getMe e inicia el bucle de recepcion long-polling.
func (t *TelegramChannel) Start(ctx context.Context, handler channel.MessageHandler) error {
	if t.token == "" {
		return fmt.Errorf("token de Telegram no configurado")
	}

	t.handler = handler
	_ = os.MkdirAll(t.downloadsDir, 0755)

	// Validar bot token
	meURL := fmt.Sprintf("%s/bot%s/getMe", t.apiBase, t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meURL, nil)
	if err != nil {
		return fmt.Errorf("error creando request getMe: %w", err)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error conectando con Telegram API: %w", err)
	}
	defer resp.Body.Close()

	var meResp GetMeResponse
	if err := json.NewDecoder(resp.Body).Decode(&meResp); err != nil || !meResp.OK {
		return fmt.Errorf("token de Telegram invalido o respuesta no exitosa: %s", meResp.Description)
	}

	t.botID = meResp.Result.ID
	t.botUsername = meResp.Result.Username
	atomic.StoreInt32(&t.connected, 1)

	log.Printf("[Telegram Channel] Conectado exitosamente como @%s (ID: %d)", t.botUsername, t.botID)

	pollCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	t.wg.Add(1)
	go t.pollUpdates(pollCtx)

	return nil
}

// Stop finaliza el bucle de sondeo de Telegram.
func (t *TelegramChannel) Stop() error {
	atomic.StoreInt32(&t.connected, 0)
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
	log.Println("[Telegram Channel] Desconectado limpiamente.")
	return nil
}

func (t *TelegramChannel) pollUpdates(ctx context.Context) {
	defer t.wg.Done()
	var offset int64 = 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pollURL := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=30", t.apiBase, t.token, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		resp, err := t.pollClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[Telegram Warning] Error en long-polling: %v. Reintentando...", err)
			time.Sleep(3 * time.Second)
			continue
		}

		var updateResp GetUpdatesResponse
		decErr := json.NewDecoder(resp.Body).Decode(&updateResp)
		_ = resp.Body.Close()

		if decErr != nil || !updateResp.OK {
			time.Sleep(2 * time.Second)
			continue
		}

		for _, upd := range updateResp.Result {
			if upd.UpdateID >= offset {
				offset = upd.UpdateID + 1
			}

			if upd.Message != nil {
				go t.handleIncomingMessage(upd.Message)
			}
		}
	}
}

func (t *TelegramChannel) isAllowed(senderID, username string) bool {
	if len(t.allowedUsers) == 0 {
		return true
	}
	cleanID := strings.TrimSpace(senderID)
	cleanUser := strings.ToLower(strings.TrimPrefix(username, "@"))

	for _, a := range t.allowedUsers {
		aClean := strings.TrimSpace(a)
		if aClean == cleanID || strings.ToLower(strings.TrimPrefix(aClean, "@")) == cleanUser {
			return true
		}
	}
	return false
}

func (t *TelegramChannel) handleIncomingMessage(m *Message) {
	if m.From == nil {
		return
	}

	senderID := strconv.FormatInt(m.From.ID, 10)
	chatID := strconv.FormatInt(m.Chat.ID, 10)
	senderName := strings.TrimSpace(m.From.FirstName + " " + m.From.LastName)
	if senderName == "" {
		senderName = m.From.Username
	}
	if senderName == "" {
		senderName = "Usuario Telegram " + senderID
	}

	if !t.isAllowed(senderID, m.From.Username) {
		log.Printf("[Telegram Ignorado] Mensaje de usuario no autorizado: %s (@%s)", senderID, m.From.Username)
		return
	}

	text := strings.TrimSpace(m.Text)
	if text == "" && m.Caption != "" {
		text = strings.TrimSpace(m.Caption)
	}

	var attachments []channel.Attachment
	isVoice := false

	// Descargar nota de voz
	if m.Voice != nil {
		isVoice = true
		fileName := fmt.Sprintf("voice_tg_%d.ogg", time.Now().UnixNano())
		targetPath := filepath.Join(t.downloadsDir, fileName)
		if err := t.downloadFile(m.Voice.FileID, targetPath); err == nil {
			attachments = append(attachments, channel.Attachment{
				Type:     channel.AttachmentAudio,
				Path:     targetPath,
				MimeType: m.Voice.MimeType,
				FileName: fileName,
				Size:     m.Voice.FileSize,
			})
		}
	} else if m.Audio != nil {
		isVoice = true
		fileName := m.Audio.FileName
		if fileName == "" {
			fileName = fmt.Sprintf("audio_tg_%d.mp3", time.Now().UnixNano())
		}
		targetPath := filepath.Join(t.downloadsDir, fileName)
		if err := t.downloadFile(m.Audio.FileID, targetPath); err == nil {
			attachments = append(attachments, channel.Attachment{
				Type:     channel.AttachmentAudio,
				Path:     targetPath,
				MimeType: m.Audio.MimeType,
				FileName: fileName,
				Size:     m.Audio.FileSize,
			})
		}
	}

	// Descargar foto
	if len(m.Photo) > 0 {
		bestPhoto := m.Photo[len(m.Photo)-1]
		fileName := fmt.Sprintf("photo_tg_%d.jpg", time.Now().UnixNano())
		targetPath := filepath.Join(t.downloadsDir, fileName)
		if err := t.downloadFile(bestPhoto.FileID, targetPath); err == nil {
			attachments = append(attachments, channel.Attachment{
				Type:     channel.AttachmentImage,
				Path:     targetPath,
				MimeType: "image/jpeg",
				FileName: fileName,
				Size:     bestPhoto.FileSize,
			})
		}
	}

	// Descargar documento
	if m.Document != nil {
		fileName := m.Document.FileName
		if fileName == "" {
			fileName = fmt.Sprintf("doc_tg_%d.dat", time.Now().UnixNano())
		}
		targetPath := filepath.Join(t.downloadsDir, fileName)
		if err := t.downloadFile(m.Document.FileID, targetPath); err == nil {
			attachments = append(attachments, channel.Attachment{
				Type:     channel.AttachmentDocument,
				Path:     targetPath,
				MimeType: m.Document.MimeType,
				FileName: fileName,
				Size:     m.Document.FileSize,
			})
		}
	}

	if text == "" && len(attachments) == 0 {
		return
	}

	incoming := channel.IncomingMessage{
		ID:          strconv.Itoa(m.MessageID),
		ChannelType: channel.ChannelTelegram,
		SenderID:    senderID,
		SenderName:  senderName,
		ChatID:      chatID,
		Text:        text,
		Attachments: attachments,
		Timestamp:   m.Date,
		IsVoice:     isVoice,
		Metadata: map[string]string{
			"username":   m.From.Username,
			"chat_type":  m.Chat.Type,
			"first_name": m.From.FirstName,
			"last_name":  m.From.LastName,
		},
	}

	if t.handler != nil {
		if err := t.handler(context.Background(), incoming); err != nil {
			log.Printf("[Telegram Error] Fallo al procesar mensaje de %s: %v", chatID, err)
		}
	}
}

func (t *TelegramChannel) downloadFile(fileID, targetPath string) error {
	getFileURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", t.apiBase, t.token, fileID)
	resp, err := t.httpClient.Get(getFileURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var fileResp GetFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil || !fileResp.OK {
		return fmt.Errorf("error obteniendo ruta de archivo Telegram: %s", fileResp.Description)
	}

	downloadURL := fmt.Sprintf("%s/file/bot%s/%s", t.apiBase, t.token, fileResp.Result.FilePath)
	dlResp, err := t.httpClient.Get(downloadURL)
	if err != nil {
		return err
	}
	defer dlResp.Body.Close()

	out, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, dlResp.Body)
	return err
}

// Send envia mensajes de texto, notas de voz, fotos o documentos hacia Telegram.
func (t *TelegramChannel) Send(ctx context.Context, msg channel.OutgoingMessage) error {
	targetChat := strings.TrimPrefix(msg.TargetChatID, "telegram:")
	if targetChat == "" {
		return fmt.Errorf("target chat ID vacio")
	}

	// 1. Enviar nota de voz si se especifico
	if msg.VoicePath != "" || len(msg.VoiceBytes) > 0 {
		return t.sendVoice(ctx, targetChat, msg)
	}

	// 2. Enviar imagen si se especifico
	if msg.ImagePath != "" {
		return t.sendPhoto(ctx, targetChat, msg)
	}

	// 3. Enviar documento si se especifico
	if msg.DocumentPath != "" {
		return t.sendDocument(ctx, targetChat, msg)
	}

	// 4. Enviar texto estandar
	return t.sendTextMessage(ctx, targetChat, msg.Text, msg.ReplyToID)
}

func (t *TelegramChannel) sendTextMessage(ctx context.Context, chatID, text, replyToID string) error {
	if text == "" {
		return nil
	}

	sendURL := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBase, t.token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	if replyToID != "" {
		if replyID, err := strconv.Atoi(replyToID); err == nil {
			payload["reply_to_message_id"] = replyID
		}
	}

	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Si Telegram rechaza el formato Markdown, reintentar en texto plano
	if resp.StatusCode == http.StatusBadRequest {
		delete(payload, "parse_mode")
		fallbackBytes, _ := json.Marshal(payload)
		reqPlain, _ := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(fallbackBytes))
		reqPlain.Header.Set("Content-Type", "application/json")
		respPlain, errPlain := t.httpClient.Do(reqPlain)
		if errPlain == nil {
			_ = respPlain.Body.Close()
			return nil
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error HTTP %d de Telegram: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (t *TelegramChannel) sendVoice(ctx context.Context, chatID string, msg channel.OutgoingMessage) error {
	sendURL := fmt.Sprintf("%s/bot%s/sendVoice", t.apiBase, t.token)

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("chat_id", chatID)
	if msg.Text != "" {
		_ = w.WriteField("caption", msg.Text)
	}

	part, err := w.CreateFormFile("voice", "voice.ogg")
	if err != nil {
		return err
	}

	if len(msg.VoiceBytes) > 0 {
		_, _ = part.Write(msg.VoiceBytes)
	} else if msg.VoicePath != "" {
		fileBytes, err := os.ReadFile(msg.VoicePath)
		if err != nil {
			return fmt.Errorf("error leyendo archivo de voz: %w", err)
		}
		_, _ = part.Write(fileBytes)
	}
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error enviando nota de voz a Telegram (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func (t *TelegramChannel) sendPhoto(ctx context.Context, chatID string, msg channel.OutgoingMessage) error {
	sendURL := fmt.Sprintf("%s/bot%s/sendPhoto", t.apiBase, t.token)

	fileBytes, err := os.ReadFile(msg.ImagePath)
	if err != nil {
		return fmt.Errorf("error leyendo imagen para Telegram: %w", err)
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("chat_id", chatID)
	if msg.Text != "" {
		_ = w.WriteField("caption", msg.Text)
	}

	part, err := w.CreateFormFile("photo", filepath.Base(msg.ImagePath))
	if err != nil {
		return err
	}
	_, _ = part.Write(fileBytes)
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error enviando foto a Telegram (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func (t *TelegramChannel) sendDocument(ctx context.Context, chatID string, msg channel.OutgoingMessage) error {
	sendURL := fmt.Sprintf("%s/bot%s/sendDocument", t.apiBase, t.token)

	fileBytes, err := os.ReadFile(msg.DocumentPath)
	if err != nil {
		return fmt.Errorf("error leyendo documento para Telegram: %w", err)
	}

	fileName := msg.FileName
	if fileName == "" {
		fileName = filepath.Base(msg.DocumentPath)
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("chat_id", chatID)
	if msg.Text != "" {
		_ = w.WriteField("caption", msg.Text)
	}

	part, err := w.CreateFormFile("document", fileName)
	if err != nil {
		return err
	}
	_, _ = part.Write(fileBytes)
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error enviando documento a Telegram (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
