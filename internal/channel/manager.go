package channel

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"felix-go/internal/agent"
	"felix-go/internal/config"
	"felix-go/internal/fastpath"
	"felix-go/internal/memory"
	"felix-go/internal/scheduler"
	"felix-go/internal/users"
)

type SlashCommandHandler func(chatID, senderID, text string, isAdmin bool) (bool, string)
type MenuHandler func(senderName string) string

// Manager coordina los canales de transporte activos (WhatsApp, Telegram) y centraliza el enrutamiento.
type Manager struct {
	mu           sync.RWMutex
	channels     map[ChannelType]Channel
	cfg          *config.Config
	engine       *agent.Engine
	scheduler    *scheduler.Scheduler
	userMgr      *users.Manager
	fastRouter   *fastpath.Router
	memStore     *memory.Store
	multimodal   *MultimodalPipeline
	slashHandler SlashCommandHandler
	menuHandler  MenuHandler
	primaryType  ChannelType
}

// NewManager crea un nuevo administrador omnicanal.
func NewManager(cfg *config.Config, eng *agent.Engine, sched *scheduler.Scheduler, um *users.Manager, fr *fastpath.Router, mem *memory.Store, mm *MultimodalPipeline) *Manager {
	return &Manager{
		channels:    make(map[ChannelType]Channel),
		cfg:         cfg,
		engine:      eng,
		scheduler:   sched,
		userMgr:     um,
		fastRouter:  fr,
		memStore:    mem,
		multimodal:  mm,
		primaryType: ChannelWhatsApp,
	}
}

func (m *Manager) SetSlashHandler(sh SlashCommandHandler) {
	m.slashHandler = sh
}

func (m *Manager) SetMenuHandler(mh MenuHandler) {
	m.menuHandler = mh
}

// RegisterChannel anade un canal a la coleccion del manager.
func (m *Manager) RegisterChannel(ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[ch.Type()] = ch
	if m.primaryType == "" {
		m.primaryType = ch.Type()
	}
	log.Printf("[Omnichannel Manager] Canal registrado: %s (%s)", ch.Name(), ch.Type())
}

// GetChannel retorna un canal especifico si esta registrado.
func (m *Manager) GetChannel(t ChannelType) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, ok := m.channels[t]
	return ch, ok
}

// StartAll inicia todos los canales registrados en segundo plano pasando el despachador unificado.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.channels) == 0 {
		log.Println("[Omnichannel Warning] No hay canales de comunicacion registrados. Felix operara en modo headless / API.")
		return nil
	}

	for chType, ch := range m.channels {
		cType := chType
		channelRef := ch
		go func() {
			log.Printf("[Omnichannel] Iniciando canal %s...", channelRef.Name())
			if err := channelRef.Start(ctx, m.HandleIncoming); err != nil {
				log.Printf("[Omnichannel Error] Fallo al iniciar canal %s (%s): %v", channelRef.Name(), cType, err)
			}
		}()
	}
	return nil
}

// StopAll desconecta limpiamente todos los canales.
func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, ch := range m.channels {
		_ = ch.Stop()
	}
	log.Println("[Omnichannel Manager] Todos los canales detenidos.")
}

// HandleIncoming procesa un mensaje proveniente de cualquier canal bajo un flujo comun unificado.
func (m *Manager) HandleIncoming(ctx context.Context, msg IncomingMessage) error {
	log.Printf("[Omnichannel Inbound %s] De: %s (%s) | Longitud: %d", msg.ChannelType, msg.SenderName, msg.SenderID, len(msg.Text))

	// 1. Procesamiento multimodal estandarizado (Whisper para audio, vision para fotos, doc_engine para archivos)
	if m.multimodal != nil {
		_ = m.multimodal.Process(ctx, &msg)
	}

	// 2. Onboarding: Si aun no hay ningun admin registrado, el primer remitente se convierte en Administrador Principal
	justPromoted := false
	if m.userMgr != nil && !m.userMgr.IsAnyAdminConfigured() {
		if promoted, _ := m.userMgr.EnsureFirstAdmin(msg.SenderID, msg.SenderName); promoted {
			_ = m.cfg.SetOwner(msg.SenderID, "config.json")
			log.Printf("[Omnichannel Onboarding] Primer usuario %s (%s via %s) registrado como Administrador Principal", msg.SenderName, msg.SenderID, msg.ChannelType)
			justPromoted = true
		}
	}

	isAdmin := true
	if m.userMgr != nil {
		role := m.userMgr.GetRole(msg.SenderID)
		isAdmin = (role == users.RoleAdmin)
	}

	// 3. Comandos directos de administracion (Cero Latencia, sin llamada a LLM)
	if m.slashHandler != nil && strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		if handled, reply := m.slashHandler(msg.ChatID, msg.SenderID, msg.Text, isAdmin); handled {
			return m.SendText(ctx, msg.ChannelType, msg.ChatID, reply)
		}
	}

	// 4. Si acaba de ser promovido como administrador, enviarle el menu de bienvenida interactivo
	if justPromoted && m.menuHandler != nil {
		welcomeMsg := m.menuHandler(msg.SenderName)
		return m.SendText(ctx, msg.ChannelType, msg.ChatID, welcomeMsg)
	}

	// 5. Verificar si la inferencia requiere configuracion inicial
	needsConfig := m.cfg.APIBase == "" || (m.cfg.APIKey == "" && !strings.Contains(m.cfg.APIBase, "127.0.0.1") && !strings.Contains(m.cfg.APIBase, "localhost"))
	if needsConfig && isAdmin && m.menuHandler != nil {
		return m.SendText(ctx, msg.ChannelType, msg.ChatID, m.menuHandler(msg.SenderName))
	}

	// 6. Fast-Path Heuristico (Respuestas instantaneas en < 5ms sin costo de LLM)
	if m.fastRouter != nil {
		if handled, fastReply := m.fastRouter.Route(msg.Text, isAdmin); handled {
			return m.SendText(ctx, msg.ChannelType, msg.ChatID, fastReply)
		}
	}

	// 7. Bucle ReAct y Razonamiento Agentico
	sessionID := fmt.Sprintf("%s:%s", msg.ChannelType, msg.ChatID)

	var contextMem string
	if m.memStore != nil {
		contextMem = m.memStore.GetContextMemory(memory.MemoryContextOptions{
			IsAdmin: isAdmin,
			Query:   msg.Text,
		})
	}

	runCtx, runCancel := context.WithTimeout(ctx, 180*time.Second)
	defer runCancel()

	reply, err := m.engine.Run(runCtx, sessionID, msg.Text, contextMem)
	if err != nil {
		errMsg := fmt.Sprintf("Error procesando solicitud: %v", err)
		return m.SendText(ctx, msg.ChannelType, msg.ChatID, errMsg)
	}

	// 8. Parser de recordatorios automaticos
	if m.scheduler != nil {
		reply = m.scheduler.ParseAndSchedule(reply, sessionID, msg.SenderName)
	}

	return m.SendText(ctx, msg.ChannelType, msg.ChatID, reply)
}

// SendText envia un mensaje de texto formateado hacia el canal y chat indicados.
func (m *Manager) SendText(ctx context.Context, chType ChannelType, chatID, text string) error {
	m.mu.RLock()
	ch, ok := m.channels[chType]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("canal %s no registrado o no disponible", chType)
	}

	return ch.Send(ctx, OutgoingMessage{
		TargetChatID: chatID,
		Text:         text,
	})
}

// SendUniversal enruta un mensaje saliente resolviendo automaticamente el canal a partir del prefijo o formato del destinatario.
func (m *Manager) SendUniversal(ctx context.Context, target string, msg OutgoingMessage) error {
	chType, cleanTarget := m.ResolveTargetChannel(target)
	msg.TargetChatID = cleanTarget

	m.mu.RLock()
	ch, ok := m.channels[chType]
	m.mu.RUnlock()

	if !ok {
		// Fallback: usar el canal primario disponible
		m.mu.RLock()
		ch, ok = m.channels[m.primaryType]
		m.mu.RUnlock()
		if !ok {
			return fmt.Errorf("ningun canal disponible para despachar a %s", target)
		}
	}

	return ch.Send(ctx, msg)
}

// ResolveTargetChannel detecta el canal correspondiente segun el prefijo (ej: "telegram:123" o "whatsapp:54911...")
func (m *Manager) ResolveTargetChannel(target string) (ChannelType, string) {
	t := strings.TrimSpace(target)
	if strings.HasPrefix(t, "telegram:") {
		return ChannelTelegram, strings.TrimPrefix(t, "telegram:")
	}
	if strings.HasPrefix(t, "whatsapp:") {
		return ChannelWhatsApp, strings.TrimPrefix(t, "whatsapp:")
	}
	if strings.Contains(t, "@s.whatsapp.net") || strings.Contains(t, "@lid") || strings.Contains(t, "@g.us") {
		return ChannelWhatsApp, t
	}

	// Si es solo numerico y no parece un JID completo de WhatsApp, comprobar si Telegram esta activo
	m.mu.RLock()
	_, tgActive := m.channels[ChannelTelegram]
	m.mu.RUnlock()

	if tgActive && !strings.Contains(t, "@") {
		return ChannelTelegram, t
	}

	return m.primaryType, t
}

// SendMessage envia texto resolviendo automaticamente el canal.
func (m *Manager) SendMessage(ctx context.Context, target, text string) error {
	return m.SendUniversal(ctx, target, OutgoingMessage{
		Text: text,
	})
}

// SendFile envia un archivo o imagen resolviendo automaticamente el canal.
func (m *Manager) SendFile(ctx context.Context, target, filePath, caption string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	isImg := ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp"

	msg := OutgoingMessage{
		Text: caption,
	}
	if isImg {
		msg.ImagePath = filePath
	} else {
		msg.DocumentPath = filePath
		msg.FileName = filepath.Base(filePath)
	}

	return m.SendUniversal(ctx, target, msg)
}

// SendVoice envia una nota de voz resolviendo automaticamente el canal.
func (m *Manager) SendVoice(ctx context.Context, target string, voiceData []byte) error {
	return m.SendUniversal(ctx, target, OutgoingMessage{
		VoiceBytes: voiceData,
	})
}
