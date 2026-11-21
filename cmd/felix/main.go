package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"felix-go/internal/agent"
	"felix-go/internal/channel"
	tgChan "felix-go/internal/channel/telegram"
	waChan "felix-go/internal/channel/whatsapp"
	"felix-go/internal/checkpoint"
	"felix-go/internal/config"
	"felix-go/internal/digest"
	"felix-go/internal/fastpath"
	"felix-go/internal/health"
	"felix-go/internal/inference"
	"felix-go/internal/mcp"
	"felix-go/internal/memory"
	"felix-go/internal/scheduler"
	"felix-go/internal/tools"
	"felix-go/internal/users"
	"felix-go/internal/whatsapp"
)

func main() {
	cfgPath := "config.json"
	args := os.Args[1:]
	var subCmd string
	var subArgs []string
	var cliPrompt string
	var setupMode bool

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-c" || a == "--config" {
			if i+1 < len(args) {
				cfgPath = args[i+1]
				i++
			}
		} else if a == "--cli" {
			if i+1 < len(args) {
				cliPrompt = args[i+1]
				i++
			}
		} else if a == "-s" || a == "--setup" {
			setupMode = true
		} else if a == "-h" || a == "--help" || a == "help" {
			PrintHelp()
			return
		} else if subCmd == "" && !strings.HasPrefix(a, "-") {
			subCmd = strings.ToLower(a)
			subArgs = args[i+1:]
			break
		}
	}

	if setupMode || subCmd == "setup" || subCmd == "onboard" || subCmd == "init" {
		runSetupWizard(cfgPath)
		return
	}

	if subCmd == "login" || subCmd == "auth" || subCmd == "status" || subCmd == "doctor" {
		RunAuthManager(cfgPath)
		return
	}

	// Menu interactivo si se ejecuta sin argumentos en una terminal interactiva
	if len(args) == 0 && isTerminal() {
		PrintMOTD()
		fmt.Println("Flujo Principal:")
		fmt.Println("  [1] Iniciar Chat Interactivo en Terminal (felix chat)")
		fmt.Println("  [2] Asistente de Configuracion y Login OAuth (Google / OpenAI / Antigravity)")
		fmt.Println("  [3] Estado y Diagnostico de Conexion (felix status)")
		fmt.Println("  [4] Iniciar Servicio en Segundo Plano (felix daemon)")
		fmt.Println("  [0] Salir")
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Print("Seleccione una opcion [0-4] o escriba un comando (o 'help'): ")
		reader := bufio.NewReader(os.Stdin)
		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1", "chat":
			subCmd = "chat"
		case "2", "setup", "onboard":
			runSetupWizard(cfgPath)
			return
		case "3", "status", "auth", "login":
			RunAuthManager(cfgPath)
			return
		case "4", "daemon", "server":
			subCmd = "daemon"
		case "0", "exit", "salir":
			return
		case "help", "-h", "--help":
			PrintHelp()
			return
		default:
			if opt != "" {
				cliPrompt = opt
			} else {
				subCmd = "chat"
			}
		}
	}

	if subCmd != "chat" && cliPrompt == "" {
		PrintMOTD()
		log.Println("Iniciando Felix en segundo plano (Agente Autonomo de Ingenieria y DevOps)...")
	}

	// 1. Cargar configuracion
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Error cargando configuracion: %v", err)
	}

	// 2. Inicializar Catalogo de Herramientas (Tool Registry)
	ckptStore, err := checkpoint.NewSQLiteStore(filepath.Join(cfg.DataDir, "checkpoints.db"))
	if err != nil {
		log.Printf("[warn] no se pudo inicializar checkpoint store: %v", err)
	} else {
		defer ckptStore.Close()
	}

	skillsDir := cfg.SkillsDir
	reg := tools.NewRegistry()
	reg.Register(&tools.ExecTool{DefaultWorkDir: cfg.WorkspaceDir})
	reg.Register(&tools.FileReadTool{})
	reg.Register(&tools.FileWriteTool{})
	reg.Register(&tools.FileEditTool{})
	reg.Register(&tools.FileLsTool{DefaultWorkDir: cfg.WorkspaceDir})
	reg.Register(&tools.ApplyPatchTool{DefaultWorkDir: cfg.WorkspaceDir})
	reg.Register(&tools.ProcessTool{})
	reg.Register(&tools.AntigravityTool{})
	reg.Register(&tools.PcTaskTool{})
	reg.Register(&tools.GcliTool{})
	reg.Register(&tools.HomelabTool{})
	reg.Register(&tools.WebFetchTool{})
	reg.Register(&tools.WebSearchTool{})
	reg.Register(&tools.WeatherTool{})
	reg.Register(&tools.SqliteQueryTool{})
	reg.Register(&tools.SkillsListTool{SkillsDir: skillsDir})
	reg.Register(&tools.SkillsReadTool{SkillsDir: skillsDir})
	reg.Register(&tools.SkillsWriteTool{SkillsDir: skillsDir})
	reg.Register(&tools.DocEngineTool{})
	reg.Register(&tools.NetSentinelTool{})
	reg.Register(&tools.DockerTool{})
	reg.Register(&tools.VisionInspectTool{APIBase: cfg.APIBase, APIKey: cfg.APIKey, Model: cfg.Model})
	reg.Register(&tools.BrowserTool{DownloadsDir: filepath.Join(cfg.DataDir, "downloads")})
	reg.Register(&tools.WorkstationTool{Host: cfg.WorkstationHost})
	reg.Register(tools.NewMinecraftTool())
	reg.Register(&tools.GitTool{DefaultRepoDir: cfg.WorkspaceDir})
	reg.Register(&tools.TailscaleTool{})
	reg.Register(&tools.SQLiteInspectorTool{})

	// 3. Inicializar Administrador de Memoria y registrar herramientas
	memStore := memory.NewStore(cfg.WorkspaceDir, cfg.DataDir)
	contextMem := memStore.GetContextMemory(memory.MemoryContextOptions{
		IsAdmin: true,
		Query:   cliPrompt,
	})
	reg.Register(&tools.MemoryManagerTool{Store: memStore})
	if memStore.GetIndexer() != nil {
		reg.Register(&tools.MemorySearchTool{Indexer: memStore.GetIndexer()})
	}

	// 4. Inicializar Administrador de Historial Persistente en SQLite
	sessionDBPath := filepath.Join(cfg.DataDir, "sessions.db")
	histMgr, err := agent.NewHistoryManager(sessionDBPath, 40)
	if err != nil {
		log.Fatalf("Error inicializando almacenamiento persistente de sesiones SQLite: %v", err)
	}
	defer histMgr.Close()
	log.Printf("Gestor de sesiones persistentes en SQLite inicializado en %s", sessionDBPath)

	// Inicializar Memoria de Trabajo Persistente (Scratchpad)
	scratchpadMgr, err := tools.NewScratchpadManager(histMgr.DB())
	if err != nil {
		log.Fatalf("Error inicializando administrador de scratchpad SQLite: %v", err)
	}
	reg.Register(&tools.ScratchpadTool{Manager: scratchpadMgr})

	// Registrar busqueda y recuperacion en historial de sesiones
	reg.Register(&tools.SessionSearchTool{DB: histMgr.DB()})
	reg.Register(&tools.SessionHistoryTool{DB: histMgr.DB()})

	// Registrar gestor de metas y objetivos persistentes
	reg.Register(&tools.GoalManagerTool{DB: histMgr.DB()})

	// 5. Inicializar Administrador de Usuarios, Roles y Confidencialidad
	usersDBPath := filepath.Join(cfg.DataDir, "users.db")
	userMgr, err := users.NewManager(usersDBPath, cfg.AllowedUsers)
	if err != nil {
		log.Fatalf("Error inicializando administrador de usuarios SQLite: %v", err)
	}
	defer userMgr.Close()
	log.Printf("Gestor de usuarios y confidencialidad inicializado en %s", usersDBPath)

	// Registrar herramienta de gestion de usuarios y directivas de veto
	reg.Register(&tools.UsersManagerTool{UserMgr: userMgr})

	// Inicializar Motor Agentico (ReAct Loop) con persistencia en disco
	engine := agent.NewEngine(cfg, reg, histMgr)
	engine.SetScratchpadManager(scratchpadMgr)

	// Registrar arnes nativo de desarrollo conectado al motor de inferencia LLM
	reg.Register(&tools.HarnessDevelopTool{
		DefaultRepoDir:  cfg.WorkspaceDir,
		Completer:       engine,
		CheckpointStore: ckptStore,
	})

	// Inicializar Monitor de Telemetria Asincrono y Digest Precomputado
	digestWatcher := digest.NewWatcher(2 * time.Minute)
	digestCtx, digestCancel := context.WithCancel(context.Background())
	defer digestCancel()
	digestWatcher.StartRunner(digestCtx)
	fastRouter := fastpath.NewRouter(digestWatcher)

	// 6. Inicializar Planificador de Recordatorios y Tareas
	schedDBPath := filepath.Join(cfg.DataDir, "reminders.db")
	sched, err := scheduler.NewScheduler(schedDBPath)
	if err != nil {
		log.Printf("[Aviso] No se pudo inicializar scheduler: %v", err)
	} else {
		sched.SetDataDir(cfg.DataDir)
	}

	// Registrar herramienta de gestion de recordatorios
	reg.Register(&tools.RemindersTool{DBPath: schedDBPath})

	// Registrar herramienta de auto-curacion y versionado de infraestructura
	infraTool, err := tools.NewInfraTool(sessionDBPath)
	if err != nil {
		log.Printf("[Aviso] No se pudo inicializar InfraTool: %v", err)
	} else {
		reg.Register(infraTool)
	}

	defaultJID := cfg.OwnerJID

	// Registrar herramienta de generacion de imagenes (DALL-E 3)
	reg.Register(&tools.ImageGenerateTool{
		OpenAIKey:  cfg.OpenAIAPIKey,
		DataDir:    cfg.DataDir,
		DefaultJID: defaultJID,
	})

	// Inicializar Servidores MCP si estan configurados
	if len(cfg.MCPServers) > 0 {
		mcpMgr := mcp.NewManager()
		defer mcpMgr.Close()
		mcpConfigs := make(map[string]mcp.ServerConfig)
		for k, v := range cfg.MCPServers {
			mcpConfigs[k] = mcp.ServerConfig{
				Command: v.Command,
				Args:    v.Args,
				Env:     v.Env,
			}
		}
		_ = mcpMgr.RegisterServers(context.Background(), mcpConfigs, reg)
	}

	log.Printf("Catalogo de herramientas inicializado: %d herramientas registradas", len(reg.GetOpenAISchemas()))

	// Modo Chat Interactivo o Prompt Directo
	if subCmd == "chat" && len(subArgs) == 0 && cliPrompt == "" {
		runInteractiveChat(engine, contextMem, sched)
		return
	}

	if len(subArgs) > 0 && cliPrompt == "" {
		cliPrompt = strings.Join(subArgs, " ")
	}

	if cliPrompt != "" {
		fmt.Printf("\n=== EJECUTANDO PROMPT EN MODO CLI ===\nPrompt: %s\n\n", cliPrompt)
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()

		reply, err := engine.Run(ctx, "cli-session", cliPrompt, contextMem)
		if err != nil {
			log.Fatalf("Error en ejecucion agentica: %v", err)
		}

		if sched != nil {
			reply = sched.ParseAndSchedule(reply, "cli-chat", "Admin")
		}

		fmt.Println("\n=== RESPUESTA FINAL DEL AGENTE ===")
		fmt.Println(reply)
		fmt.Println("==================================")
		return
	}

	// Inicializar Pipeline Multimodal compartido (Whisper STT, vision, documentos)
	multimodalPipe := channel.NewMultimodalPipeline(nil)

	// Inicializar Gestor Omnicanal
	chanManager := channel.NewManager(cfg, engine, sched, userMgr, fastRouter, memStore, multimodalPipe)

	var waClient *whatsapp.Client
	var cmdClient *whatsapp.Client

	// 1. Canal WhatsApp (Condicional: si esta habilitado en configuracion)
	if cfg.WhatsAppEnabled {
		log.Println("Inicializando canal de comunicacion WhatsApp...")
		waDBPath := resolveWhatsAppDBPath(cfg.DataDir)
		var err error
		waClient, err = whatsapp.NewClient(cfg, engine, sched, memStore, userMgr, digestWatcher, scratchpadMgr, waDBPath)
		if err != nil {
			log.Fatalf("Error inicializando cliente WhatsApp: %v", err)
		}
		cmdClient = waClient

		if waClient.GetWhisperClient() != nil {
			multimodalPipe.SetTranscriber(waClient.GetWhisperClient())
		}

		chanManager.SetSlashHandler(waClient.HandleSlashCommand)
		chanManager.SetMenuHandler(waClient.GetOnboardingMenu)

		waChannel := waChan.NewWhatsAppChannel(waClient)
		chanManager.RegisterChannel(waChannel)
	}

	// 2. Canal Telegram (Condicional: si esta habilitado o token configurado)
	if cfg.TelegramEnabled || cfg.TelegramBotToken != "" {
		log.Println("Inicializando canal de comunicacion Telegram...")
		downloadsDir := filepath.Join(cfg.DataDir, "downloads")
		tgChannel := tgChan.NewTelegramChannel(cfg.TelegramBotToken, downloadsDir, cfg.TelegramAllowed)
		chanManager.RegisterChannel(tgChannel)

		if cmdClient == nil {
			cmdClient = whatsapp.NewStandaloneCommandHandler(cfg, engine, sched, userMgr, digestWatcher, scratchpadMgr)
			chanManager.SetSlashHandler(cmdClient.HandleSlashCommand)
			chanManager.SetMenuHandler(cmdClient.GetOnboardingMenu)
		}

		if (cfg.GroqAPIKey != "" || cfg.OpenAIAPIKey != "") && (waClient == nil || waClient.GetWhisperClient() == nil) {
			wClient := whatsapp.NewWhisperClient(cfg.GroqAPIKey, cfg.OpenAIAPIKey)
			multimodalPipe.SetTranscriber(wClient)
		}
	}

	// Feedback de progreso hacia el canal correspondiente
	engine.SetProgressFunc(func(sessionID, statusText string) {
		_ = chanManager.SendMessage(context.Background(), sessionID, statusText)
	})

	// Registrar herramientas nativas omnicanal (despachan a WhatsApp o Telegram segun el destinatario)
	reg.Register(&tools.SendMessageTool{
		SendFunc:   chanManager.SendMessage,
		DefaultJID: defaultJID,
	})
	reg.Register(&tools.SendFileTool{
		SendFunc:   chanManager.SendFile,
		DefaultJID: defaultJID,
	})
	reg.Register(&tools.SendVoiceTool{
		SendVoiceFunc: func(ctx context.Context, targetJID, textOrAudioPath string, isText bool) error {
			if isText {
				return chanManager.SendMessage(ctx, targetJID, textOrAudioPath)
			}
			return chanManager.SendUniversal(ctx, targetJID, channel.OutgoingMessage{
				VoicePath: textOrAudioPath,
			})
		},
		DefaultJID: defaultJID,
	})
	reg.Register(&tools.ImageGenerateTool{
		OpenAIKey:    cfg.OpenAIAPIKey,
		DataDir:      cfg.DataDir,
		SendFileFunc: chanManager.SendFile,
		DefaultJID:   defaultJID,
	})
	reg.Register(&tools.BackgroundWorkerTool{
		SendFunc: func(chatJID, message string) error {
			return chanManager.SendMessage(context.Background(), chatJID, message)
		},
		DefaultJID: defaultJID,
	})

	// Inicializar Administrador de Subagentes Concurrentes
	subagentMgr, err := agent.NewSubagentManager(histMgr.DB(), engine, func(chatJID, message string) error {
		return chanManager.SendMessage(context.Background(), chatJID, message)
	}, defaultJID)
	if err != nil {
		log.Fatalf("Error inicializando SubagentManager: %v", err)
	}
	if waClient != nil {
		waClient.SetSubagentManager(subagentMgr)
	}
	reg.Register(&tools.SubagentSpawnTool{
		Runner: subagentMgr,
	})

	// Despacho autonomo de recordatorios
	if sched != nil {
		schedCtx, schedCancel := context.WithCancel(context.Background())
		defer schedCancel()
		sched.StartRunner(schedCtx, func(chatJID, message string) error {
			return chanManager.SendMessage(context.Background(), chatJID, message)
		}, defaultJID)
	}

	// Servidor HTTP interno de salud, metricas, gateway de inferencia y webhooks
	healthServer := health.NewServer("0.0.0.0:"+cfg.HealthPort, engine, waClient, memStore, subagentMgr)
	healthServer.SetChannelManager(chanManager)
	gw := inference.NewGateway(cfg.APIBase, cfg.Model, engine.GetKeyPool())
	healthServer.SetGateway(gw)
	healthServer.Start()
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		_ = healthServer.Stop(stopCtx)
	}()

	// Iniciar todos los canales activos
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	if err := chanManager.StartAll(appCtx); err != nil {
		log.Fatalf("Error iniciando canales: %v", err)
	}

	// Manejo de senales para apagado ordenado
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	log.Printf("Felix listo y operando en modo demonio. Directorio de datos: %s", filepath.Clean(cfg.DataDir))
	sig := <-sigChan
	log.Printf("Senal recibida (%v). Finalizando limpiamente...", sig)

	chanManager.StopAll()
	log.Println("Felix detenido ordenadamente.")
}

func resolveWhatsAppDBPath(dataDir string) string {
	waDBPath := filepath.Join(dataDir, "whatsapp.db")
	legacyDBPath := filepath.Join(dataDir, "felix.db")

	if hasRegisteredDevice(waDBPath) {
		return waDBPath
	}
	if hasRegisteredDevice(legacyDBPath) {
		log.Printf("[whatsapp] sesion activa detectada en base historica: %s", legacyDBPath)
		return legacyDBPath
	}
	return waDBPath
}

func hasRegisteredDevice(dbPath string) bool {
	if _, err := os.Stat(dbPath); err != nil {
		return false
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(2000)")
	if err != nil {
		return false
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM whatsmeow_device").Scan(&count)
	return err == nil && count > 0
}
