package main

import (
	"context"
	"database/sql"
	"flag"
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
	"felix-go/internal/botfactory"
	"felix-go/internal/channel"
	tgChan "felix-go/internal/channel/telegram"
	waChan "felix-go/internal/channel/whatsapp"
	"felix-go/internal/checkpoint"
	"felix-go/internal/cli"
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
	"felix-go/internal/webcli"
	"felix-go/internal/whatsapp"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "setup" || os.Args[1] == "init" || os.Args[1] == "onboard") {
		os.Exit(cli.RunOnboardingWizard("config.json"))
		return
	}

	// Detectar si se invoco modo servidor daemon 24/7 explicitamente
	isServerMode := false
	serverArgs := []string{}
	for i := 1; i < len(os.Args); i++ {
		arg := strings.ToLower(os.Args[i])
		if arg == "server" || arg == "daemon" || arg == "--server" || arg == "-s" {
			isServerMode = true
		} else {
			serverArgs = append(serverArgs, os.Args[i])
		}
	}

	// Si NO es modo servidor explicito, la ejecucion va directamente al CLI interactivo / Desktop / Web
	if !isServerMode {
		cfgPath := ""
		cleanArgs := []string{}
		for i := 1; i < len(os.Args); i++ {
			if (os.Args[i] == "--config" || os.Args[i] == "-c") && i+1 < len(os.Args) {
				cfgPath = os.Args[i+1]
				i++
			} else {
				cleanArgs = append(cleanArgs, os.Args[i])
			}
		}

		if cfgPath == "" {
			for _, candidate := range []string{
				"/home/matias/felix-server/config.json",
				"/home/matias/felix-go/config.json",
				"config.json",
				filepath.Join(os.Getenv("HOME"), ".config/felix/config.json"),
			} {
				if _, sErr := os.Stat(candidate); sErr == nil {
					cfgPath = candidate
					break
				}
			}
		}

		os.Exit(cli.RunCLI(cleanArgs, cfgPath))
	}

	// Reconfigurar argumentos para el modo servidor daemon
	os.Args = append([]string{os.Args[0]}, serverArgs...)
	configPath := flag.String("config", "config.json", "Ruta al archivo config.json")
	cliPrompt := flag.String("cli", "", "Ejecutar una peticion interactiva directa por linea de comandos y mostrar el bucle de herramientas")
	setupFlag := flag.Bool("setup", false, "Iniciar el asistente interactivo de configuracion")
	flag.Parse()

	if *setupFlag {
		os.Exit(cli.RunOnboardingWizard(*configPath))
		return
	}

	log.Println("Iniciando Felix Server en modo daemon 24/7...")

	// 1. Cargar configuracion
	cfg, err := config.LoadConfig(*configPath)
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
		Query:   *cliPrompt,
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

	// Inicializar Motor Nativo de Bots Comerciales y Especializados (Bot Factory)
	botsDBPath := filepath.Join(cfg.DataDir, "bots.db")
	botStore, err := botfactory.NewStorage(botsDBPath)
	var botMgr *botfactory.Manager
	if err != nil {
		log.Printf("[Aviso] No se pudo inicializar storage de botfactory: %v", err)
	} else {
		defer botStore.Close()
		botMgr = botfactory.NewManager(botStore, engine)
		reg.Register(&tools.BotFactoryTool{Manager: botMgr})
		log.Printf("Motor de Bot Factory inicializado en %s", botsDBPath)
	}

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

	// MODO CLI: Permite probar el bucle de herramientas y razonamiento sin conexion a WhatsApp
	if *cliPrompt != "" {
		fmt.Printf("\n=== EJECUTANDO PROMPT EN MODO CLI ===\nPrompt: %s\n\n", *cliPrompt)
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()

		reply, err := engine.Run(ctx, "cli-session", *cliPrompt, contextMem)
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
	if botMgr != nil {
		healthServer.SetBotManager(botMgr)
	}
	healthServer.SetChannelManager(chanManager)
	gw := inference.NewGateway(cfg.APIBase, cfg.Model, engine.GetKeyPool())
	healthServer.SetGateway(gw)
	healthServer.Start()
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		_ = healthServer.Stop(stopCtx)
	}()

	var webServer *webcli.Server
	if botStore != nil {
		webServer = webcli.NewServer(8056, botStore, cfg, cfg.DataDir)
		if waClient != nil {
			webServer.SetWhatsAppClient(waClient)
		}
		if webPort, err := webServer.Start(); err == nil {
			log.Printf("Consola Web CLI embebida iniciada en http://0.0.0.0:%d", webPort)
		} else {
			log.Printf("[aviso] no se pudo iniciar Web CLI embebido: %v", err)
		}
	}
	defer func() {
		if webServer != nil {
			_ = webServer.Stop()
		}
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
