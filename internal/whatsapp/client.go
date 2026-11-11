package whatsapp

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/mdp/qrterminal/v3"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"

	"felix-go/internal/agent"
	"felix-go/internal/config"
	"felix-go/internal/digest"
	"felix-go/internal/fastpath"
	"felix-go/internal/memory"
	"felix-go/internal/scheduler"
	"felix-go/internal/tools"
	"felix-go/internal/users"
)

// Client envuelve whatsmeow y gestiona los eventos de mensajeria de WhatsApp.
type Client struct {
	cfg           *config.Config
	client        *whatsmeow.Client
	engine        *agent.Engine
	scheduler     *scheduler.Scheduler
	memStore      *memory.Store
	userMgr       *users.Manager
	whisper       *WhisperClient
	subagentMgr   *agent.SubagentManager
	digestWatcher *digest.Watcher
	fastRouter    *fastpath.Router
	scratchpad    *tools.ScratchpadManager
	debouncers    sync.Map
	voiceModeMap  sync.Map
}

func (c *Client) SetSubagentManager(sm *agent.SubagentManager) {
	c.subagentMgr = sm
}

func (c *Client) SetVoiceMode(chatJID string, enabled bool) {
	c.voiceModeMap.Store(chatJID, enabled)
}

func (c *Client) IsVoiceMode(chatJID string) bool {
	val, ok := c.voiceModeMap.Load(chatJID)
	if !ok {
		return false
	}
	return val.(bool)
}

type chatDebouncer struct {
	mu           sync.Mutex
	timer        *time.Timer
	messages     []string
	lastChat     types.JID
	senderName   string
	senderJID    string
	chatJID      string
	client       *Client
	running      bool
	isVoiceInput bool
}

func cleanPhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isAllowed(sender, senderAlt string, allowedList []string) bool {
	if len(allowedList) == 0 {
		return true
	}
	cleanS := cleanPhone(sender)
	cleanAlt := cleanPhone(senderAlt)
	for _, a := range allowedList {
		cleanA := cleanPhone(a)
		if cleanA == cleanS || a == sender {
			return true
		}
		if cleanAlt != "" && (cleanA == cleanAlt || a == senderAlt) {
			return true
		}
	}
	return false
}

// NewClient inicializa el cliente whatsmeow con almacenamiento SQLite en dbPath.
func NewClient(cfg *config.Config, eng *agent.Engine, sched *scheduler.Scheduler, mem *memory.Store, userMgr *users.Manager, dw *digest.Watcher, sm *tools.ScratchpadManager, dbPath string) (*Client, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	clientLog := waLog.Stdout("Félix-Go", "INFO", true)
	container, err := sqlstore.New(context.Background(), "sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)", clientLog)
	if err != nil {
		return nil, fmt.Errorf("error inicializando almacenamiento whatsmeow: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error obteniendo dispositivo whatsmeow: %w", err)
	}

	cli := whatsmeow.NewClient(deviceStore, clientLog)
	cli.EnableAutoReconnect = true

	c := &Client{
		cfg:           cfg,
		client:        cli,
		engine:        eng,
		scheduler:     sched,
		memStore:      mem,
		userMgr:       userMgr,
		whisper:       NewWhisperClient(cfg.GroqAPIKey, cfg.OpenAIAPIKey),
		digestWatcher: dw,
		fastRouter:    fastpath.NewRouter(dw),
		scratchpad:    sm,
	}

	cli.AddEventHandler(c.handleEvent)
	return c, nil
}

// Start conecta el cliente a WhatsApp o genera el codigo QR en terminal si es una sesion nueva.
func (c *Client) Start(ctx context.Context) error {
	if c.client.Store.ID == nil {
		log.Println("Dispositivo WhatsApp no vinculado. Preparando vinculacion...")
		qrChan, _ := c.client.GetQRChannel(ctx)

		if err := c.client.Connect(); err != nil {
			return fmt.Errorf("error al conectar para vinculacion: %w", err)
		}

		if c.cfg.PhoneNumber != "" {
			cleanNum := cleanPhone(c.cfg.PhoneNumber)
			code, err := c.client.PairPhone(ctx, cleanNum, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
			if err == nil {
				pairFilePath := filepath.Join(c.cfg.DataDir, "pairing_code.txt")
				_ = os.WriteFile(pairFilePath, []byte(code), 0644)
				fmt.Println("==================================================")
				fmt.Printf("CODIGO DE VINCULACION TELEFONICO: %s\n", code)
				fmt.Println("==================================================")
			}
		}

		fmt.Println()
		fmt.Println("==============================================================")
		fmt.Println("       ESCANEA EL SIGUIENTE CODIGO QR CON TU WHATSAPP:")
		fmt.Println("  (WhatsApp -> Dispositivos vinculados -> Vincular un dispositivo)")
		fmt.Println("==============================================================")
		fmt.Println()

		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("\nEsperando escaneo de codigo QR...")
			} else if evt.Event == "success" {
				fmt.Println("\n==============================================================")
				fmt.Println(" Dispositivo WhatsApp vinculado con exito!")
				fmt.Println("==============================================================")
				break
			} else if evt.Event == "timeout" {
				fmt.Println("\nTiempo de espera del codigo QR agotado. Reinicie la aplicacion.")
				break
			} else if evt.Event == "error" {
				return fmt.Errorf("error de sesion QR: %v", evt.Error)
			}
		}
	} else {
		log.Printf("Dispositivo ya vinculado (%s). Conectando...", c.client.Store.ID.String())
		if err := c.client.Connect(); err != nil {
			return fmt.Errorf("error al conectar cliente WhatsApp: %w", err)
		}
	}
	return nil
}

// GetOwnerJID retorna el JID del propietario principal o del primer administrador.
func (c *Client) GetOwnerJID() string {
	if c.cfg.OwnerJID != "" {
		return c.cfg.OwnerJID
	}
	if c.userMgr != nil {
		if a := c.userMgr.GetPrimaryAdmin(); a != "" {
			return a
		}
	}
	return ""
}

// splitTextForWhatsApp divide textos extensos respetando saltos de linea o palabras para evitar cortes abruptos.
func splitTextForWhatsApp(text string, maxChunkSize int) []string {
	if len(text) <= maxChunkSize {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxChunkSize {
			chunks = append(chunks, remaining)
			break
		}

		// Buscar salto de linea conveniente antes del limite
		splitIdx := strings.LastIndex(remaining[:maxChunkSize], "\n")
		if splitIdx == -1 || splitIdx < maxChunkSize/2 {
			splitIdx = strings.LastIndex(remaining[:maxChunkSize], " ")
		}
		if splitIdx == -1 || splitIdx < maxChunkSize/2 {
			splitIdx = maxChunkSize
		}

		chunk := strings.TrimSpace(remaining[:splitIdx])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		remaining = strings.TrimSpace(remaining[splitIdx:])
	}
	return chunks
}

// SendMessage envia un mensaje de texto simple al JID especificado, dividiendo en chunks si es extenso.
func (c *Client) SendMessage(ctx context.Context, targetJID string, text string) error {
	jid, err := types.ParseJID(targetJID)
	if err != nil {
		return err
	}

	chunks := splitTextForWhatsApp(text, 3500)
	for i, chunk := range chunks {
		msg := &waProto.Message{
			Conversation: proto.String(chunk),
		}
		if _, err := c.client.SendMessage(ctx, jid, msg); err != nil {
			return err
		}
		if i < len(chunks)-1 {
			time.Sleep(150 * time.Millisecond)
		}
	}
	return nil
}

// SendFile envia un archivo local (documento o imagen) al JID especificado.
func (c *Client) SendFile(ctx context.Context, targetJID string, filePath string, caption string) error {
	jid, err := types.ParseJID(targetJID)
	if err != nil {
		return fmt.Errorf("JID invalido: %w", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("error leyendo archivo: %w", err)
	}

	fileName := filepath.Base(filePath)
	ext := strings.ToLower(filepath.Ext(filePath))

	mimeType := "application/octet-stream"
	isImage := false
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
		isImage = true
	case ".png":
		mimeType = "image/png"
		isImage = true
	case ".webp":
		mimeType = "image/webp"
		isImage = true
	case ".pdf":
		mimeType = "application/pdf"
	case ".docx":
		mimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".odt":
		mimeType = "application/vnd.oasis.opendocument.text"
	case ".zip":
		mimeType = "application/zip"
	case ".tar", ".gz":
		mimeType = "application/gzip"
	case ".txt":
		mimeType = "text/plain"
	}

	if isImage {
		resp, err := c.client.Upload(ctx, data, whatsmeow.MediaImage)
		if err != nil {
			return fmt.Errorf("error subiendo imagen a WhatsApp: %w", err)
		}
		imgMsg := &waProto.ImageMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           &resp.URL,
			DirectPath:    &resp.DirectPath,
			MediaKey:      resp.MediaKey,
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    &resp.FileLength,
		}
		_, err = c.client.SendMessage(ctx, jid, &waProto.Message{ImageMessage: imgMsg})
		return err
	}

	resp, err := c.client.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("error subiendo documento a WhatsApp: %w", err)
	}
	docMsg := &waProto.DocumentMessage{
		Title:         proto.String(fileName),
		FileName:      proto.String(fileName),
		Mimetype:      proto.String(mimeType),
		Caption:       proto.String(caption),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    &resp.FileLength,
	}
	_, err = c.client.SendMessage(ctx, jid, &waProto.Message{DocumentMessage: docMsg})
	return err
}

// SendVoice sintetiza texto a voz (usando edge-tts con voz Mateo) o convierte un archivo existente a OGG Opus PTT y lo envia como nota de voz nativa a WhatsApp.
func (c *Client) SendVoice(ctx context.Context, targetJID string, textOrAudioPath string, isText bool) error {
	jid, err := types.ParseJID(targetJID)
	if err != nil {
		return fmt.Errorf("JID invalido: %w", err)
	}

	tmpOgg := fmt.Sprintf("/tmp/voice_%d.ogg", time.Now().UnixNano())
	defer os.Remove(tmpOgg)

	if isText {
		tmpMp3 := fmt.Sprintf("/tmp/tts_%d.mp3", time.Now().UnixNano())
		defer os.Remove(tmpMp3)

		ttsBin := "edge-tts"
		if p, err := exec.LookPath("edge-tts"); err == nil {
			ttsBin = p
		} else if home, _ := os.UserHomeDir(); home != "" {
			localBin := filepath.Join(home, ".local", "bin", "edge-tts")
			if _, err := os.Stat(localBin); err == nil {
				ttsBin = localBin
			}
		}
		cmdTTS := exec.CommandContext(ctx, ttsBin, "--voice", "es-UY-MateoNeural", "--text", textOrAudioPath, "--write-media", tmpMp3)
		if out, err := cmdTTS.CombinedOutput(); err != nil {
			return fmt.Errorf("error generando audio TTS: %v (%s)", err, string(out))
		}

		cmdFFmpeg := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", tmpMp3, "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", tmpOgg)
		if out, err := cmdFFmpeg.CombinedOutput(); err != nil {
			return fmt.Errorf("error convirtiendo audio con ffmpeg: %v (%s)", err, string(out))
		}
	} else {
		cmdFFmpeg := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", textOrAudioPath, "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", tmpOgg)
		if out, err := cmdFFmpeg.CombinedOutput(); err != nil {
			return fmt.Errorf("error convirtiendo archivo a opus: %v (%s)", err, string(out))
		}
	}

	oggBytes, err := os.ReadFile(tmpOgg)
	if err != nil {
		return fmt.Errorf("error leyendo ogg generado: %w", err)
	}

	resp, err := c.client.Upload(ctx, oggBytes, whatsmeow.MediaAudio)
	if err != nil {
		return fmt.Errorf("error subiendo audio a WhatsApp: %w", err)
	}

	audioMsg := &waProto.AudioMessage{
		Mimetype:      proto.String("audio/ogg; codecs=opus"),
		PTT:           proto.Bool(true),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    &resp.FileLength,
	}

	_, err = c.client.SendMessage(ctx, jid, &waProto.Message{AudioMessage: audioMsg})
	return err
}

// Disconnect desconecta el cliente de WhatsApp.
func (c *Client) Disconnect() {
	if c.client != nil {
		c.client.Disconnect()
	}
}

// IsConnected retorna true si el cliente WhatsMeow mantiene conexion activa con WhatsApp.
func (c *Client) IsConnected() bool {
	if c.client == nil {
		return false
	}
	return c.client.IsConnected()
}

func (c *Client) handleEvent(rawEvt any) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		go c.handleMessage(evt)
	case *events.PairSuccess:
		log.Printf("[WhatsApp Vinculado Exitosamente] Dispositivo emparejado con JID: %s (LID: %s)", evt.ID.String(), evt.LID.String())
		_ = os.Remove(filepath.Join(c.cfg.DataDir, "pairing_code.txt"))
	case *events.PairError:
		log.Printf("[Error Vinculacion WhatsApp] Fallo al vincular dispositivo: %v", evt.Error)
	case *events.Connected:
		log.Println("Conexion establecida exitosamente con WhatsApp.")
	case *events.Disconnected:
		log.Println("Desconectado de WhatsApp. Intentando reconectar automaticamente...")
	case *events.LoggedOut:
		log.Printf("[WhatsApp Alerta] Sesion revocada por WhatsApp: %v. Purgando credenciales locales para permitir re-emparejamiento...", evt.Reason)
		c.client.Disconnect()
		if c.client.Store != nil && c.client.Store.ID != nil {
			_ = c.client.Store.Delete(context.Background())
		}
		log.Println("[WhatsApp Recuperacion] Credenciales revocadas eliminadas. Estado limpio para nuevo emparejamiento.")
	}
}

func (c *Client) handleMessage(evt *events.Message) {
	if evt.Info.IsFromMe || evt.Info.Chat.Server == "broadcast" || evt.Info.IsNewsletterStatus {
		return
	}

	sender := evt.Info.Sender.User
	if sender == "" {
		sender = evt.Info.Chat.User
	}
	senderAlt := evt.Info.SenderAlt.User

	senderName := sender
	if evt.Info.PushName != "" {
		senderName = evt.Info.PushName
	}
	chatJID := evt.Info.Chat.String()

	// Validacion de remitente
	if evt.Info.IsGroup {
		// En grupos, solo operar si el grupo esta explicitamente permitido
		if len(c.cfg.AllowedGroups) == 0 {
			return
		}
		allowed := false
		for _, g := range c.cfg.AllowedGroups {
			if g == chatJID || strings.HasPrefix(chatJID, g) {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	} else {
		// Chat directo: verificar rol con el gestor de usuarios
		var role users.Role
		if c.userMgr != nil {
			role = c.userMgr.GetRole(sender)
			if role == users.RoleBlocked && senderAlt != "" {
				role = c.userMgr.GetRole(senderAlt)
			}
		} else {
			if isAllowed(sender, senderAlt, c.cfg.AllowedUsers) {
				role = users.RoleAdmin
			} else {
				role = users.RoleBlocked
			}
		}

		if role == users.RoleBlocked {
			log.Printf("[Acceso Denegado / Bloqueado] Remitente no autorizado en chat directo: %s (%s, alt: %s) (ignorado)", senderName, sender, senderAlt)
			return
		}
	}

	// Extraer texto
	text := extractMsgText(evt.Message)
	if text == "" && evt.RawMessage != nil {
		if unwrapped := evt.UnwrapRaw(); unwrapped != nil {
			text = extractMsgText(unwrapped.Message)
		}
	}
	text = strings.TrimSpace(text)

	// Extraer audio
	audioMsg := extractAudio(evt.Message)
	if audioMsg == nil && evt.RawMessage != nil {
		if unwrapped := evt.UnwrapRaw(); unwrapped != nil {
			audioMsg = extractAudio(unwrapped.Message)
		}
	}

	if audioMsg != nil {
		fileLen := audioMsg.GetFileLength()
		const maxAudioBytes = 25 * 1024 * 1024
		if fileLen > maxAudioBytes {
			log.Printf("[Audio Recibido Warning] Audio de %s excede el limite de 25MB (%d bytes)", sender, fileLen)
			_ = c.SendMessage(context.Background(), chatJID, "El archivo de audio recibido supera los 25 MB (limite maximo de transcripcion). Por favor, envia una nota de voz mas breve.")
			return
		}

		log.Printf("[Audio Recibido] Descargando nota de voz de %s (%s)...", senderName, sender)
		audioBytes, err := c.client.Download(context.Background(), audioMsg)
		if err != nil {
			log.Printf("[Error Descarga Audio] %v", err)
		} else if len(audioBytes) > maxAudioBytes {
			log.Printf("[Audio Descargado Warning] Tamano descargado %d bytes supera limite de 25MB", len(audioBytes))
			_ = c.SendMessage(context.Background(), chatJID, "El archivo de audio recibido supera los 25 MB. Por favor, envia una nota de voz mas breve.")
			return
		} else {
			transCtx, transCancel := context.WithTimeout(context.Background(), 60*time.Second)
			transcript, err := c.whisper.Transcribe(transCtx, audioBytes)
			transCancel()
			if err != nil {
				log.Printf("[Error Whisper Audio] %v", err)
			} else {
				log.Printf("[Audio Transcrito Exitosamente] De %s (%s): %s", senderName, sender, transcript)
				text = transcript
				if quoted := extractQuotedSnippet(audioMsg.GetContextInfo()); quoted != "" {
					text = fmt.Sprintf("[En respuesta a: \"%s\"]\n%s", quoted, transcript)
				}
			}
		}
	}

	// Extraer documento adjunto (PDF, ODF, Word, texto, etc.)
	docMsg := extractDocument(evt.Message)
	if docMsg == nil && evt.RawMessage != nil {
		if unwrapped := evt.UnwrapRaw(); unwrapped != nil {
			docMsg = extractDocument(unwrapped.Message)
		}
	}

	if docMsg != nil {
		fileName := docMsg.GetFileName()
		if fileName == "" {
			fileName = fmt.Sprintf("doc_%d.dat", time.Now().Unix())
		}
		fileName = filepath.Base(fileName)
		downloadsDir := filepath.Join(c.cfg.DataDir, "downloads")
		_ = os.MkdirAll(downloadsDir, 0755)
		targetPath := filepath.Join(downloadsDir, fileName)

		log.Printf("[Documento Recibido] Descargando %s (%s) de %s...", fileName, docMsg.GetMimetype(), senderName)
		docBytes, err := c.client.Download(context.Background(), docMsg)
		if err != nil {
			log.Printf("[Error Descarga Documento] %v", err)
		} else {
			if err := os.WriteFile(targetPath, docBytes, 0644); err != nil {
				log.Printf("[Error Guardando Documento] %v", err)
			} else {
				log.Printf("[Documento Guardado] Ruta: %s | Tamano: %d bytes", targetPath, len(docBytes))
				docNotice := fmt.Sprintf("[Archivo recibido en el servidor: %s (Nombre original: %s, Tamano: %d bytes, Tipo: %s). El archivo ya se encuentra en disco listo para operar. Tienes autonomia total para manipularlo: si el usuario te pide instalarlo (ej. sudo dpkg -i para .deb, pip/npm para paquetes), compilarlo (go build, make, gcc), ejecutarlo (bash, python), descomprimirlo (tar, unzip), transferirlo a otra maquina (scp o rsync), leerlo (doc_engine, file_read) o modificarlo, hazlo directamente con las herramientas del sistema sin trabas ni confirmaciones intermedias.]", targetPath, fileName, len(docBytes), docMsg.GetMimetype())
				if text != "" {
					text = docNotice + "\nMensaje acompanante del usuario: " + text
				} else {
					text = docNotice + "\nEl usuario envio este archivo para que lo proceses, instales, ejecutes o analices segun su necesidad."
				}
			}
		}
	}

	// Extraer imagen adjunta (JPG, PNG, WebP)
	imgMsg := extractImage(evt.Message)
	if imgMsg == nil && evt.RawMessage != nil {
		if unwrapped := evt.UnwrapRaw(); unwrapped != nil {
			imgMsg = extractImage(unwrapped.Message)
		}
	}

	if imgMsg != nil {
		ext := ".jpg"
		if imgMsg.GetMimetype() == "image/png" {
			ext = ".png"
		} else if imgMsg.GetMimetype() == "image/webp" {
			ext = ".webp"
		}
		fileName := fmt.Sprintf("img_%d%s", time.Now().Unix(), ext)
		downloadsDir := filepath.Join(c.cfg.DataDir, "downloads")
		_ = os.MkdirAll(downloadsDir, 0755)
		targetPath := filepath.Join(downloadsDir, fileName)

		log.Printf("[Imagen Recibida] Descargando imagen %s (%s) de %s...", fileName, imgMsg.GetMimetype(), senderName)
		imgBytes, err := c.client.Download(context.Background(), imgMsg)
		if err != nil {
			log.Printf("[Error Descarga Imagen] %v", err)
		} else {
			if err := os.WriteFile(targetPath, imgBytes, 0644); err != nil {
				log.Printf("[Error Guardando Imagen] %v", err)
			} else {
				log.Printf("[Imagen Guardada] Ruta: %s | Tamano: %d bytes", targetPath, len(imgBytes))
				imgNotice := fmt.Sprintf("[Imagen adjunta recibida en WhatsApp: %s (Tamano: %d bytes). Utiliza la herramienta vision_inspect con image_path=\"%s\" para transcribir su texto o analizar su contenido visual.]", targetPath, len(imgBytes), targetPath)
				if text != "" {
					text = imgNotice + "\nMensaje acompanante del usuario: " + text
				} else {
					text = imgNotice + "\nEl usuario envio esta imagen para su transcripcion o analisis visual."
				}
			}
		}
	}

	if text == "" {
		return
	}

	isVoice := (audioMsg != nil)
	log.Printf("[Mensaje Entrante %s] De: %s (%s) | Texto: %s (EsVoz: %v)", chatJID, senderName, sender, text, isVoice)
	c.enqueueMessage(chatJID, evt.Info.Chat, senderName, sender, text, isVoice)
}

func (c *Client) enqueueMessage(chatJID string, chat types.JID, senderName, senderJID, text string, isVoice bool) {
	val, _ := c.debouncers.LoadOrStore(chatJID, &chatDebouncer{
		client:  c,
		chatJID: chatJID,
	})
	deb := val.(*chatDebouncer)

	deb.mu.Lock()
	defer deb.mu.Unlock()

	deb.messages = append(deb.messages, text)
	deb.lastChat = chat
	deb.senderName = senderName
	deb.senderJID = senderJID
	if isVoice {
		deb.isVoiceInput = true
	}

	// Notificar estado de escribiendo en el chat
	_ = c.client.SendChatPresence(context.Background(), chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)

	if deb.running {
		log.Printf("[Debounce %s] Mensaje encolado mientras el agente esta respondiendo.", chatJID)
		return
	}

	if deb.timer != nil {
		deb.timer.Stop()
	}

	// Ventana ultra-rapida de debounce (450ms) para recortar latencia fija sin fragmentar mensajes
	deb.timer = time.AfterFunc(450*time.Millisecond, func() {
		deb.processQueue()
	})
}

func (d *chatDebouncer) processQueue() {
	d.mu.Lock()
	if len(d.messages) == 0 || d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	combinedText := strings.Join(d.messages, "\n")
	d.messages = nil
	chat := d.lastChat
	senderName := d.senderName
	senderJID := d.senderJID
	chatJID := d.chatJID
	c := d.client
	isVoice := d.isVoiceInput
	d.isVoiceInput = false
	d.mu.Unlock()

	defer func() {
		_ = c.client.SendChatPresence(context.Background(), chat, types.ChatPresencePaused, types.ChatPresenceMediaText)
		d.mu.Lock()
		d.running = false
		hasMore := len(d.messages) > 0
		d.mu.Unlock()
		if hasMore {
			time.AfterFunc(150*time.Millisecond, func() {
				d.processQueue()
			})
		}
	}()

	log.Printf("[Procesando Turno Unificado %s] De: %s (%s) | Longitud: %d caracteres", chatJID, senderName, senderJID, len(combinedText))

	// Onboarding: Si aun no hay ningun admin registrado, el primer remitente se convierte en Administrador Principal
	if c.userMgr != nil && !c.userMgr.IsAnyAdminConfigured() {
		if promoted, _ := c.userMgr.EnsureFirstAdmin(senderJID, senderName); promoted {
			_ = c.cfg.SetOwner(senderJID, "config.json")
			log.Printf("[Onboarding] Primer usuario %s (%s) registrado como Administrador Principal", senderName, senderJID)
		}
	}

	isAdmin := true
	var vetoRules []string
	if c.userMgr != nil {
		role := c.userMgr.GetRole(senderJID)
		isAdmin = (role == users.RoleAdmin)
		if !isAdmin {
			vetoRules = c.userMgr.GetVetoRulesForUser(senderJID, senderName)
		}
	}

	// Comandos directos de administracion (Cero Latencia, sin llamada a LLM)
	if handled, cmdReply := c.HandleSlashCommand(chatJID, senderJID, combinedText, isAdmin); handled {
		respMsg := &waProto.Message{
			Conversation: proto.String(cmdReply),
		}
		_, err := c.client.SendMessage(context.Background(), chat, respMsg)
		if err != nil {
			log.Printf("[Error Envio WhatsApp] Fallo al responder comando directo a %s: %v", chatJID, err)
		} else {
			log.Printf("[Comando Directo Atendido] Chat: %s | Comando ejecutado con exito", chatJID)
		}
		return
	}

	// Verificar si la inferencia requiere configuracion inicial
	needsConfig := c.cfg.APIBase == "" || (c.cfg.APIKey == "" && !strings.Contains(c.cfg.APIBase, "127.0.0.1") && !strings.Contains(c.cfg.APIBase, "localhost"))
	if needsConfig && isAdmin {
		onboardingMsg := "*CONFIGURACION INICIAL REQUERIDA*\n\n" +
			"Para habilitar el razonamiento y respuestas inteligentes de Felix, configure su proveedor de IA enviando:\n\n" +
			"*1. Endpoint de Inferencia (API Base)*\n" +
			"   `/config apibase <url>`\n" +
			"   Ejemplos:\n" +
			"   - OpenAI: `/config apibase https://api.openai.com/v1`\n" +
			"   - Gemini: `/config apibase https://generativelanguage.googleapis.com/v1beta`\n" +
			"   - Groq: `/config apibase https://api.groq.com/openai/v1`\n" +
			"   - Proxy Local: `/config apibase http://127.0.0.1:8045/v1`\n" +
			"   - Ollama: `/config apibase http://localhost:11434/v1`\n\n" +
			"*2. Clave de API*\n" +
			"   `/config apikey <tu_api_key>`\n\n" +
			"*3. Modelo LLM*\n" +
			"   `/config model <nombre_modelo>`\n" +
			"   Ejemplos: `gemini-2.5-flash`, `gpt-4o`, `llama-3.3-70b-versatile`\n\n" +
			"*4. Ver estado*\n" +
			"   `/config status`"
		_ = c.SendMessage(context.Background(), chatJID, onboardingMsg)
		return
	}

	// Fast-Path Heuristico (Respuestas instantaneas en < 5ms sin costo de LLM)
	if c.fastRouter != nil {
		if handled, fastReply := c.fastRouter.Route(combinedText, isAdmin); handled {
			respMsg := &waProto.Message{
				Conversation: proto.String(fastReply),
			}
			_, err := c.client.SendMessage(context.Background(), chat, respMsg)
			if err != nil {
				log.Printf("[Error Envio WhatsApp] Fallo al responder Fast-Path a %s: %v", chatJID, err)
			} else {
				log.Printf("[Fast-Path Atendido] Chat: %s | Respuesta instantanea enviada (0 tokens)", chatJID)
			}
			return
		}
	}

	// Iniciar latido continuo de presencia (escribiendo...) mientras se procesa la solicitud
	presenceDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-presenceDone:
				return
			case <-ticker.C:
				_ = c.client.SendChatPresence(context.Background(), chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)
			}
		}
	}()

	runCtx, runCancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer runCancel()

	contextMem := ""
	if c.memStore != nil {
		contextMem = c.memStore.GetContextMemory(memory.MemoryContextOptions{
			IsAdmin:     isAdmin,
			ContactName: senderName,
			ContactJID:  senderJID,
			Query:       combinedText,
			VetoRules:   vetoRules,
		})
	}

	runCtx = context.WithValue(runCtx, "is_admin", isAdmin)
	runCtx = context.WithValue(runCtx, "session_id", chatJID)
	runCtx = context.WithValue(runCtx, "sender_jid", senderJID)
	runCtx = context.WithValue(runCtx, "sender_name", senderName)

	reply, err := c.engine.Run(runCtx, chatJID, combinedText, contextMem)
	close(presenceDone)
	if err != nil {
		log.Printf("[Error Engine] Fallo procesando mensaje de %s: %v", chatJID, err)
		reply = fmt.Sprintf("Error procesando solicitud: %v", err)
	}

	// Procesar recordatorios si el modelo emitio directivas tecnicas
	if c.scheduler != nil {
		reply = c.scheduler.ParseAndSchedule(reply, chatJID, senderName)
	}

	// Enviar respuesta por WhatsApp (Voz nativa o texto paginado)
	if isVoice || c.IsVoiceMode(chatJID) {
		log.Printf("[Modo Manos Libres] Sintetizando respuesta de voz nativa para %s...", chatJID)
		if err := c.SendVoice(context.Background(), chatJID, reply, true); err != nil {
			log.Printf("[Aviso Voz] Fallo sintesis TTS (%v), enviando en texto", err)
			_ = c.SendMessage(context.Background(), chatJID, reply)
		} else {
			log.Printf("[Respuesta de Voz Enviada] A: %s (%s)", senderName, chatJID)
		}
	} else {
		if err := c.SendMessage(context.Background(), chatJID, reply); err != nil {
			log.Printf("[Error Envio WhatsApp] Fallo al responder a %s: %v", chatJID, err)
			return
		}
		log.Printf("[Respuesta Unificada Enviada] A: %s (%s) | Longitud: %d caracteres", senderName, chatJID, len(reply))
	}
}

func extractQuotedSnippet(ctxInfo *waProto.ContextInfo) string {
	if ctxInfo == nil || ctxInfo.QuotedMessage == nil {
		return ""
	}
	q := ctxInfo.QuotedMessage
	var quoted string
	switch {
	case q.Conversation != nil && *q.Conversation != "":
		quoted = *q.Conversation
	case q.ExtendedTextMessage != nil && q.ExtendedTextMessage.Text != nil && *q.ExtendedTextMessage.Text != "":
		quoted = *q.ExtendedTextMessage.Text
	case q.ImageMessage != nil:
		if q.ImageMessage.Caption != nil && *q.ImageMessage.Caption != "" {
			quoted = "[Imagen: " + *q.ImageMessage.Caption + "]"
		} else {
			quoted = "[Imagen]"
		}
	case q.VideoMessage != nil:
		if q.VideoMessage.Caption != nil && *q.VideoMessage.Caption != "" {
			quoted = "[Video: " + *q.VideoMessage.Caption + "]"
		} else {
			quoted = "[Video]"
		}
	case q.AudioMessage != nil:
		quoted = "[Audio/Nota de voz]"
	case q.DocumentMessage != nil:
		name := "Documento"
		if q.DocumentMessage.FileName != nil && *q.DocumentMessage.FileName != "" {
			name = *q.DocumentMessage.FileName
		}
		quoted = "[" + name + "]"
	case q.StickerMessage != nil:
		quoted = "[Sticker]"
	}

	quoted = strings.TrimSpace(quoted)
	if quoted == "" {
		return ""
	}
	// Truncar para prevenir desborde de tokens en citas muy largas
	r := []rune(quoted)
	if len(r) > 250 {
		quoted = string(r[:250]) + "..."
	}
	return quoted
}

func extractMsgText(msg *waProto.Message) string {
	if msg == nil {
		return ""
	}

	var rawText string
	var ctxInfo *waProto.ContextInfo

	switch {
	case msg.Conversation != nil && msg.GetConversation() != "":
		rawText = msg.GetConversation()
	case msg.ExtendedTextMessage != nil:
		rawText = msg.ExtendedTextMessage.GetText()
		ctxInfo = msg.ExtendedTextMessage.GetContextInfo()
	case msg.ImageMessage != nil:
		rawText = msg.ImageMessage.GetCaption()
		ctxInfo = msg.ImageMessage.GetContextInfo()
	case msg.VideoMessage != nil:
		rawText = msg.VideoMessage.GetCaption()
		ctxInfo = msg.VideoMessage.GetContextInfo()
	case msg.DocumentMessage != nil:
		rawText = msg.DocumentMessage.GetCaption()
		ctxInfo = msg.DocumentMessage.GetContextInfo()
	case msg.EphemeralMessage != nil && msg.EphemeralMessage.GetMessage() != nil:
		return extractMsgText(msg.EphemeralMessage.GetMessage())
	case msg.ViewOnceMessage != nil && msg.ViewOnceMessage.GetMessage() != nil:
		return extractMsgText(msg.ViewOnceMessage.GetMessage())
	case msg.ViewOnceMessageV2 != nil && msg.ViewOnceMessageV2.GetMessage() != nil:
		return extractMsgText(msg.ViewOnceMessageV2.GetMessage())
	}

	rawText = strings.TrimSpace(rawText)
	quoted := extractQuotedSnippet(ctxInfo)

	if quoted != "" && rawText != "" {
		return fmt.Sprintf("[En respuesta a: \"%s\"]\n%s", quoted, rawText)
	}
	if rawText != "" {
		return rawText
	}
	if quoted != "" {
		return fmt.Sprintf("[En respuesta a: \"%s\"]", quoted)
	}
	return ""
}

func extractAudio(msg *waProto.Message) *waProto.AudioMessage {
	if msg == nil {
		return nil
	}
	if audio := msg.GetAudioMessage(); audio != nil {
		return audio
	}
	if eph := msg.GetEphemeralMessage(); eph != nil && eph.GetMessage() != nil {
		return extractAudio(eph.GetMessage())
	}
	if view := msg.GetViewOnceMessage(); view != nil && view.GetMessage() != nil {
		return extractAudio(view.GetMessage())
	}
	return nil
}

func extractDocument(msg *waProto.Message) *waProto.DocumentMessage {
	if msg == nil {
		return nil
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		return doc
	}
	if eph := msg.GetEphemeralMessage(); eph != nil && eph.GetMessage() != nil {
		return extractDocument(eph.GetMessage())
	}
	if view := msg.GetViewOnceMessage(); view != nil && view.GetMessage() != nil {
		return extractDocument(view.GetMessage())
	}
	return nil
}

func extractImage(msg *waProto.Message) *waProto.ImageMessage {
	if msg == nil {
		return nil
	}
	if img := msg.GetImageMessage(); img != nil {
		return img
	}
	if eph := msg.GetEphemeralMessage(); eph != nil && eph.GetMessage() != nil {
		return extractImage(eph.GetMessage())
	}
	if view := msg.GetViewOnceMessage(); view != nil && view.GetMessage() != nil {
		return extractImage(view.GetMessage())
	}
	return nil
}
