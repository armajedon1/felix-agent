package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"felix-go/internal/agent"
	"felix-go/internal/channel"
	tgChan "felix-go/internal/channel/telegram"
	waChan "felix-go/internal/channel/whatsapp"
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

func runSetupWizard(configPath string) {
	fmt.Println()
	fmt.Println("==============================================================")
	fmt.Println("         FELIX - ASISTENTE AUTONOMO OPEN SOURCE               ")
	fmt.Println("            Asistente de Configuracion Inicial                ")
	fmt.Println("==============================================================")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	cfg, _ := config.LoadConfig(configPath)
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	fmt.Println("Seleccione su proveedor de inferencia:")
	fmt.Println("  1) Google Gemini (Recomendado - gemini-2.5-flash)")
	fmt.Println("  2) OpenAI (gpt-4o)")
	fmt.Println("  3) Groq Cloud (llama-3.3-70b-versatile + Whisper)")
	fmt.Println("  4) Ollama Local (http://localhost:11434/v1)")
	fmt.Println("  5) Antigravity Tools / Proxy Local (http://127.0.0.1:8045/v1)")
	fmt.Println("  6) Endpoint Personalizado / Custom")
	fmt.Print("Opcion [1-6, por defecto 1]: ")

	opcion, _ := reader.ReadString('\n')
	opcion = strings.TrimSpace(opcion)
	if opcion == "" {
		opcion = "1"
	}

	switch opcion {
	case "1":
		fmt.Print("\nIngrese su API Key de Google Gemini (de https://aistudio.google.com/): ")
		inputKey, _ := reader.ReadString('\n')
		inputKey = strings.TrimSpace(inputKey)
		_ = cfg.ApplyPreset("gemini", inputKey, "", "")
	case "2":
		fmt.Print("\nIngrese su API Key de OpenAI (sk-...): ")
		inputKey, _ := reader.ReadString('\n')
		inputKey = strings.TrimSpace(inputKey)
		_ = cfg.ApplyPreset("openai", inputKey, "", "")
	case "3":
		fmt.Print("\nIngrese su API Key de Groq (gsk_...): ")
		inputKey, _ := reader.ReadString('\n')
		inputKey = strings.TrimSpace(inputKey)
		_ = cfg.ApplyPreset("groq", inputKey, "", "")
	case "4":
		_ = cfg.ApplyPreset("ollama", "", "", "")
		fmt.Println("\nConfigurado para Ollama en http://localhost:11434/v1 con modelo llama3.")
	case "5":
		_ = cfg.ApplyPreset("antigravity", "", "", "")
		fmt.Println("\nConfigurado para Antigravity Tools en http://127.0.0.1:8045/v1.")
	default:
		fmt.Printf("\n1. Ingrese la direccion del endpoint de inferencia (API Base)\n   [Por defecto: %s]: ", cfg.APIBase)
		inputBase, _ := reader.ReadString('\n')
		inputBase = strings.TrimSpace(inputBase)
		if inputBase != "" {
			cfg.APIBase = inputBase
		}

		fmt.Printf("\n2. Ingrese la clave de API (API Key) para inferencia:\n   (Para servidores locales sin autenticacion puede dejarlo vacio): ")
		inputKey, _ := reader.ReadString('\n')
		inputKey = strings.TrimSpace(inputKey)
		if inputKey != "" {
			cfg.APIKey = inputKey
		}

		fmt.Printf("\n3. Ingrese el modelo LLM a utilizar\n   [Por defecto: %s]: ", cfg.Model)
		inputModel, _ := reader.ReadString('\n')
		inputModel = strings.TrimSpace(inputModel)
		if inputModel != "" {
			cfg.Model = inputModel
		}
	}

	if cfg.GroqAPIKey == "" && opcion != "3" {
		fmt.Printf("\nIngrese la clave de Groq para transcripcion Whisper de notas de voz en WhatsApp\n   (Opcional, presione Enter para omitir): ")
		inputGroq, _ := reader.ReadString('\n')
		inputGroq = strings.TrimSpace(inputGroq)
		if inputGroq != "" {
			cfg.GroqAPIKey = inputGroq
		}
	}

	if err := cfg.Save(configPath); err != nil {
		fmt.Printf("\nError guardando configuracion: %v\n", err)
		return
	}

	fmt.Printf("\nConfiguracion guardada exitosamente en %s\n", configPath)
	fmt.Println("Ahora puede iniciar Felix con: ./bin/felix")
	fmt.Println("==============================================================")
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "setup" || os.Args[1] == "init") {
		runSetupWizard("config.json")
		return
	}

	configPath := flag.String("config", "config.json", "Ruta al archivo config.json")
	cliPrompt := flag.String("cli", "", "Ejecutar una peticion interactiva directa por linea de comandos y mostrar el bucle de herramientas")
	setupFlag := flag.Bool("setup", false, "Iniciar el asistente interactivo de configuracion")
	flag.Parse()

	if *setupFlag {
		runSetupWizard(*configPath)
		return
	}

	log.Println("Iniciando Felix (Agente Autonomo de Ingenieria y DevOps)...")

	// 1. Cargar configuracion
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Error cargando configuracion: %v", err)
	}

	// 2. Inicializar Catalogo de Herramientas (Tool Registry)
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
		waDBPath := filepath.Join(cfg.DataDir, "whatsapp.db")
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
