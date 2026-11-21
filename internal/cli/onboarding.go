package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"felix-go/internal/config"
)

// CmdOnboard ejecuta el asistente guiado e interactivo de configuracion inicial.
func CmdOnboard(args []string, configPath string) int {
	return RunOnboardingWizard(configPath)
}

// RunOnboardingWizard guia paso a paso en la configuracion sin tocar archivos JSON.
func RunOnboardingWizard(configPath string) int {
	reader := bufio.NewReader(os.Stdin)

	PrintBanner()
	fmt.Println("================================================================================")
	fmt.Println("             ASISTENTE DE CONFIGURACION RAPIDA (ONBOARDING WIZARD)              ")
	fmt.Println("================================================================================")
	fmt.Println(" Configure su proveedor de inferencia, credenciales y canales en 1 minuto.")
	fmt.Println(" No es necesario editar manualmente ningun archivo de configuracion ni JSON.")
	fmt.Println("================================================================================")
	fmt.Println()

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// PASO 1: SELECCION DE PROVEEDOR LLM
	fmt.Println("PASO 1: SELECCION DEL PROVEEDOR DE INTELIGENCIA ARTIFICIAL")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("  [1] Antigravity Tools Proxy Local (http://127.0.0.1:8045/v1) [Recomendado]")
	fmt.Println("  [2] OpenAI Oficial (api.openai.com - GPT-4o / GPT-4o-mini)")
	fmt.Println("  [3] Google Gemini (generativelanguage.googleapis.com - Gemini 2.5 / 3.5)")
	fmt.Println("  [4] Anthropic Claude (api.anthropic.com - Claude 3.5 Sonnet)")
	fmt.Println("  [5] Groq Cloud LPU (api.groq.com - Llama 3.3 70B Ultra-rapido)")
	fmt.Println("  [6] Ollama Local (http://localhost:11434/v1 - Modelos locales)")
	fmt.Println("  [7] Endpoint Personalizado / Proxy OpenAI-compatible")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Print("Seleccione una opcion [1-7] (Enter para Antigravity Tools): ")

	provOpt, _ := reader.ReadString('\n')
	provOpt = strings.TrimSpace(provOpt)
	if provOpt == "" {
		provOpt = "1"
	}

	var selectedBase string
	var selectedKey string
	var selectedModel string
	var providerName string

	switch provOpt {
	case "1":
		providerName = "antigravity"
		selectedBase = "http://127.0.0.1:8045/v1"
		selectedModel = "gemini-3.5-flash-lite"
		selectedKey = "sk-antigravity"

		fmt.Println("Comprobando disponibilidad de Antigravity Tools Proxy...")
		probe := func(url string) bool {
			c := &http.Client{Timeout: 1 * time.Second}
			resp, err := c.Get(url)
			if err == nil {
				_ = resp.Body.Close()
				return resp.StatusCode < 500
			}
			return false
		}

		localOnline := probe("http://127.0.0.1:8045/v1/models")
		remoteOnline := probe("http://100.96.230.125:8045/v1/models")

		if localOnline {
			fmt.Printf("[OK] Proxy local detectado y activo en %s\n", selectedBase)
		} else if remoteOnline {
			fmt.Println("[INFO] Proxy del Servidor Homelab detectado en http://100.96.230.125:8045/v1")
			fmt.Print("Desea conectarse al servidor Homelab? [S/n]: ")
			ans, _ := reader.ReadString('\n')
			ans = strings.ToLower(strings.TrimSpace(ans))
			if ans == "" || ans == "s" || ans == "si" || ans == "y" {
				selectedBase = "http://100.96.230.125:8045/v1"
				fmt.Printf("[OK] Configurado para usar servidor Homelab: %s\n", selectedBase)
			}
		} else {
			fmt.Println("[AVISO] El proxy de Antigravity Tools no responde en 127.0.0.1:8045.")
			fmt.Println("--------------------------------------------------------------------------------")
			fmt.Println("Link de administracion y login de cuentas: http://127.0.0.1:8045")
			fmt.Println("Opciones disponibles:")
			fmt.Println("  [1] Iniciar proxy local en segundo plano (antigravity-tools --headless)")
			fmt.Println("  [2] Usar API Key gratuita de Google AI Studio (https://aistudio.google.com/app/apikey)")
			fmt.Println("  [3] Continuar con 127.0.0.1:8045 (abrire la app por separado)")
			fmt.Print("Seleccione una opcion [1-3] (Enter para [1]): ")
			act, _ := reader.ReadString('\n')
			act = strings.TrimSpace(act)
			if act == "" || act == "1" {
				if _, err := exec.LookPath("antigravity-tools"); err == nil {
					fmt.Println("Iniciando antigravity-tools --headless en segundo plano...")
					bgCmd := exec.Command("antigravity-tools", "--headless")
					_ = bgCmd.Start()
					time.Sleep(1500 * time.Millisecond)
					if probe("http://127.0.0.1:8045/v1/models") {
						fmt.Println("[OK] Proxy local iniciado y respondiendo.")
					}
				} else {
					fmt.Println("[AVISO] Binario antigravity-tools no encontrado en PATH.")
				}
			} else if act == "2" {
				providerName = "gemini"
				selectedBase = "https://generativelanguage.googleapis.com/v1beta/openai/"
				selectedModel = "gemini-2.5-flash"
				fmt.Println("Obten tu API Key gratis en: https://aistudio.google.com/app/apikey")
				fmt.Print("Ingrese su Google AI Studio API Key (AIzaSy...): ")
				kIn, _ := reader.ReadString('\n')
				selectedKey = strings.TrimSpace(kIn)
			}
		}

	case "2":
		providerName = "openai"
		selectedBase = "https://api.openai.com/v1"
		selectedModel = "gpt-4o"
		fmt.Println("Link para obtener o gestionar claves: https://platform.openai.com/api-keys")
		fmt.Print("Ingrese su OpenAI API Key (sk-...): ")
		keyInput, _ := reader.ReadString('\n')
		selectedKey = strings.TrimSpace(keyInput)

	case "3":
		providerName = "gemini"
		selectedBase = "https://generativelanguage.googleapis.com/v1beta/openai/"
		selectedModel = "gemini-2.5-flash"
		fmt.Println("Link para obtener clave de API gratuita: https://aistudio.google.com/app/apikey")
		fmt.Print("Ingrese su Google AI Studio API Key (AIzaSy...): ")
		keyInput, _ := reader.ReadString('\n')
		selectedKey = strings.TrimSpace(keyInput)

	case "4":
		providerName = "anthropic"
		selectedBase = "https://api.anthropic.com/v1"
		selectedModel = "claude-3-5-sonnet-20241022"
		fmt.Println("Link para obtener clave de API: https://console.anthropic.com/settings/keys")
		fmt.Print("Ingrese su Anthropic API Key (sk-ant-...): ")
		keyInput, _ := reader.ReadString('\n')
		selectedKey = strings.TrimSpace(keyInput)

	case "5":
		providerName = "groq"
		selectedBase = "https://api.groq.com/openai/v1"
		selectedModel = "llama-3.3-70b-versatile"
		fmt.Println("Link para obtener clave gratuita: https://console.groq.com/keys")
		fmt.Print("Ingrese su Groq API Key (gsk_...): ")
		keyInput, _ := reader.ReadString('\n')
		selectedKey = strings.TrimSpace(keyInput)

	case "6":
		providerName = "ollama"
		fmt.Print("URL de Ollama (Enter para http://localhost:11434/v1): ")
		urlInput, _ := reader.ReadString('\n')
		urlInput = strings.TrimSpace(urlInput)
		if urlInput == "" {
			urlInput = "http://localhost:11434/v1"
		}
		selectedBase = urlInput
		fmt.Print("Modelo en Ollama (Enter para llama3): ")
		mInput, _ := reader.ReadString('\n')
		mInput = strings.TrimSpace(mInput)
		if mInput == "" {
			mInput = "llama3"
		}
		selectedModel = mInput
		selectedKey = "ollama"

	case "7":
		providerName = "custom"
		fmt.Print("URL Base de la API (ej. https://api.openai-proxy.com/v1): ")
		urlInput, _ := reader.ReadString('\n')
		selectedBase = strings.TrimSpace(urlInput)
		fmt.Print("API Key / Token: ")
		keyInput, _ := reader.ReadString('\n')
		selectedKey = strings.TrimSpace(keyInput)
		fmt.Print("Nombre del modelo (ej. deepseek-chat, gpt-4o): ")
		mInput, _ := reader.ReadString('\n')
		selectedModel = strings.TrimSpace(mInput)

	default:
		fmt.Println("Opcion invalida, utilizando Antigravity Tools por defecto.")
		providerName = "antigravity"
		selectedBase = "http://127.0.0.1:8045/v1"
		selectedModel = "gemini-3.5-flash-lite"
		selectedKey = "sk-antigravity"
	}

	// Personalizacion opcional de modelo
	if provOpt != "6" && provOpt != "7" {
		fmt.Printf("Modelo sugerido [%s]. Presione Enter para confirmar o ingrese otro: ", selectedModel)
		customM, _ := reader.ReadString('\n')
		customM = strings.TrimSpace(customM)
		if customM != "" {
			selectedModel = customM
		}
	}

	fmt.Println()

	// PASO 2: VERIFICACION DE INFERENCIA EN VIVO
	fmt.Println("PASO 2: VERIFICACION DE INFERENCIA EN VIVO")
	fmt.Println("--------------------------------------------------------------------------------")

	for {
		fmt.Printf("Comprobando conexion con %s (%s) en %s ...\n", providerName, selectedModel, selectedBase)
		latency, errTest := VerifyInference(selectedBase, selectedKey, selectedModel)
		if errTest == nil {
			fmt.Printf("[OK] Conexion de inferencia verificada exitosamente (Latencia: %d ms)\n", latency)
			break
		}

		fmt.Println()
		fmt.Printf("[AVISO] No se pudo verificar la conexion: %v\n", errTest)
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Println("Opciones de resolucion:")
		fmt.Println("  [1] Reintentar verificacion")
		fmt.Println("  [2] Conectar al servidor Homelab (http://100.96.230.125:8045/v1)")
		fmt.Println("  [3] Iniciar proxy local (antigravity-tools --headless)")
		fmt.Println("  [4] Ingresar API Key de Google AI Studio (https://aistudio.google.com/app/apikey)")
		fmt.Println("  [5] Ingresar otra API Key (OpenAI, Groq o Custom)")
		fmt.Println("  [6] Guardar configuracion de todos modos y continuar")
		fmt.Println("  [0] Cancelar instalacion")
		fmt.Print("Seleccione una opcion [1-6/0]: ")
		recOpt, _ := reader.ReadString('\n')
		recOpt = strings.TrimSpace(recOpt)

		if recOpt == "1" {
			continue
		} else if recOpt == "2" {
			providerName = "antigravity"
			selectedBase = "http://100.96.230.125:8045/v1"
			selectedModel = "gemini-3.5-flash-lite"
			selectedKey = "sk-antigravity"
			continue
		} else if recOpt == "3" {
			if _, err := exec.LookPath("antigravity-tools"); err == nil {
				fmt.Println("Iniciando servicio antigravity-tools --headless en segundo plano...")
				bgCmd := exec.Command("antigravity-tools", "--headless")
				_ = bgCmd.Start()
				time.Sleep(1500 * time.Millisecond)
			} else {
				fmt.Println("[AVISO] Binario antigravity-tools no instalado.")
			}
			continue
		} else if recOpt == "4" {
			providerName = "gemini"
			selectedBase = "https://generativelanguage.googleapis.com/v1beta/openai/"
			selectedModel = "gemini-2.5-flash"
			fmt.Println("Enlace para clave gratuita: https://aistudio.google.com/app/apikey")
			fmt.Print("Ingrese su Google AI Studio API Key (AIzaSy...): ")
			kIn, _ := reader.ReadString('\n')
			selectedKey = strings.TrimSpace(kIn)
			continue
		} else if recOpt == "5" {
			fmt.Print("Ingrese URL Base: ")
			uIn, _ := reader.ReadString('\n')
			selectedBase = strings.TrimSpace(uIn)
			fmt.Print("Ingrese API Key: ")
			kIn, _ := reader.ReadString('\n')
			selectedKey = strings.TrimSpace(kIn)
			fmt.Print("Ingrese Modelo: ")
			mIn, _ := reader.ReadString('\n')
			selectedModel = strings.TrimSpace(mIn)
			continue
		} else if recOpt == "6" {
			fmt.Println("Guardando configuracion sin confirmacion de inferencia.")
			break
		} else {
			fmt.Println("Operacion cancelada. No se modifico la configuracion.")
			return 1
		}
	}

	fmt.Println()

	// PASO 3: CANALES DE COMUNICACION
	fmt.Println("PASO 3: CANALES DE COMUNICACION")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("  [1] Solo Terminal y Consola Web (Recomendado para administracion de servidor)")
	fmt.Println("  [2] Habilitar WhatsApp (solicitara escanear codigo QR al iniciar)")
	fmt.Println("  [3] Habilitar Telegram (solicitara Token de BotFather)")
	fmt.Println("  [4] Habilitar ambos canales (WhatsApp y Telegram)")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Print("Seleccione una opcion [1-4] (Enter para Terminal y Web): ")

	chanOpt, _ := reader.ReadString('\n')
	chanOpt = strings.TrimSpace(chanOpt)
	if chanOpt == "" {
		chanOpt = "1"
	}

	var waEnabled bool
	var tgEnabled bool
	var tgToken string

	switch chanOpt {
	case "1":
		waEnabled = false
		tgEnabled = false
	case "2":
		waEnabled = true
		tgEnabled = false
	case "3":
		waEnabled = false
		tgEnabled = true
		fmt.Print("Ingrese su Telegram Bot Token: ")
		tok, _ := reader.ReadString('\n')
		tgToken = strings.TrimSpace(tok)
	case "4":
		waEnabled = true
		tgEnabled = true
		fmt.Print("Ingrese su Telegram Bot Token: ")
		tok, _ := reader.ReadString('\n')
		tgToken = strings.TrimSpace(tok)
	default:
		waEnabled = false
		tgEnabled = false
	}

	// PASO 4: GUARDADO AUTOMATICO
	fmt.Println()
	fmt.Println("PASO 4: GUARDADO DE CONFIGURACION")
	fmt.Println("--------------------------------------------------------------------------------")

	cfg.Provider = providerName
	cfg.APIBase = selectedBase
	cfg.APIKey = selectedKey
	cfg.Model = selectedModel
	cfg.WhatsAppEnabled = waEnabled
	cfg.TelegramEnabled = tgEnabled
	if tgToken != "" {
		cfg.TelegramBotToken = tgToken
	}

	if err := cfg.Save(configPath); err != nil {
		fmt.Printf("Error guardando configuracion: %v\n", err)
		return 1
	}

	absPath, _ := filepath.Abs(configPath)
	fmt.Printf("[OK] Configuracion guardada exitosamente en:\n     %s\n", absPath)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("Asistente completado. Felix esta listo para operar.")
	fmt.Println()
	fmt.Print("Desea iniciar una sesion de chat interactivo con Felix ahora? [S/n]: ")

	startChat, _ := reader.ReadString('\n')
	startChat = strings.ToLower(strings.TrimSpace(startChat))
	if startChat == "" || startChat == "s" || startChat == "si" || startChat == "y" || startChat == "yes" {
		return CmdChat(nil, configPath)
	}

	fmt.Println("Para iniciar Felix en cualquier momento ejecuta:")
	fmt.Println("  felix          -> Menu interactivo principal")
	fmt.Println("  felix chat     -> Chat en consola con Felix o bots")
	fmt.Println("  felix web      -> Consola web ligera embebida (puerto 8056)")
	return 0
}

// VerifyInference prueba la conectividad y latencia con el endpoint LLM.
func VerifyInference(apiBase, apiKey, model string) (int64, error) {
	if apiBase == "" {
		return 0, fmt.Errorf("api_base no configurada")
	}

	endpoint := strings.TrimRight(apiBase, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		if strings.HasSuffix(endpoint, "/v1") {
			endpoint = endpoint + "/chat/completions"
		} else if strings.HasSuffix(endpoint, "/openai") {
			endpoint = endpoint + "/chat/completions"
		} else {
			endpoint = endpoint + "/chat/completions"
		}
	}

	reqBody := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 5,
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("fallo de conexion HTTP: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()

	if resp.StatusCode >= 400 {
		return latency, fmt.Errorf("el servidor respondio con codigo HTTP %d", resp.StatusCode)
	}

	return latency, nil
}

// RunConfigMenu despliega el menu interactivo de administracion de configuracion.
func RunConfigMenu(configPath string) int {
	reader := bufio.NewReader(os.Stdin)

	for {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			fmt.Printf("Error cargando configuracion: %v\n", err)
			return 1
		}

		fmt.Println("================================================================================")
		fmt.Println("                 ADMINISTRACION DE CONFIGURACION DE FELIX                       ")
		fmt.Println("================================================================================")
		fmt.Printf("  Archivo:   %s\n", configPath)
		fmt.Printf("  Proveedor: %s | Modelo: %s\n", cfg.Provider, cfg.Model)
		fmt.Printf("  Endpoint:  %s\n", cfg.APIBase)
		fmt.Printf("  Canales:   WhatsApp: %v | Telegram: %v\n", cfg.WhatsAppEnabled, cfg.TelegramEnabled)
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Println("  [1] Ejecutar Asistente de Configuracion Completa (Onboarding)")
		fmt.Println("  [2] Cambiar Proveedor y Modelo LLM")
		fmt.Println("  [3] Diagnostico de Conexion de Inferencia (Doctor)")
		fmt.Println("  [4] Configurar Canales de Mensajeria (WhatsApp / Telegram)")
		fmt.Println("  [5] Ver Configuracion Completa (Valores enmascarados)")
		fmt.Println("  [0] Volver al Menu Principal")
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Print("Seleccione una opcion [0-5]: ")

		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "0", "volver", "exit", "salir":
			return 0

		case "1":
			return RunOnboardingWizard(configPath)

		case "2":
			fmt.Println("\nSeleccione el nuevo proveedor:")
			fmt.Println("  [1] Antigravity Tools Proxy Local (http://127.0.0.1:8045/v1)")
			fmt.Println("  [2] OpenAI Oficial (api.openai.com)")
			fmt.Println("  [3] Google Gemini (generativelanguage.googleapis.com)")
			fmt.Println("  [4] Anthropic Claude (api.anthropic.com)")
			fmt.Println("  [5] Groq Cloud LPU (api.groq.com)")
			fmt.Println("  [6] Ollama Local (http://localhost:11434/v1)")
			fmt.Println("  [7] Endpoint Personalizado")
			fmt.Print("Opcion [1-7]: ")
			pOpt, _ := reader.ReadString('\n')
			pOpt = strings.TrimSpace(pOpt)

			switch pOpt {
			case "1":
				_ = cfg.SetInferenceCredentials("http://127.0.0.1:8045/v1", "sk-antigravity", "gemini-3.5-flash-lite", configPath)
				cfg.Provider = "antigravity"
				_ = cfg.Save(configPath)
				fmt.Println("[OK] Proveedor configurado como Antigravity Tools.")
			case "2":
				fmt.Print("OpenAI API Key: ")
				k, _ := reader.ReadString('\n')
				_ = cfg.SetInferenceCredentials("https://api.openai.com/v1", strings.TrimSpace(k), "gpt-4o", configPath)
				cfg.Provider = "openai"
				_ = cfg.Save(configPath)
				fmt.Println("[OK] Proveedor configurado como OpenAI.")
			case "3":
				fmt.Print("Gemini API Key: ")
				k, _ := reader.ReadString('\n')
				_ = cfg.SetInferenceCredentials("https://generativelanguage.googleapis.com/v1beta/openai/", strings.TrimSpace(k), "gemini-2.5-flash", configPath)
				cfg.Provider = "gemini"
				_ = cfg.Save(configPath)
				fmt.Println("[OK] Proveedor configurado como Gemini.")
			case "4":
				fmt.Print("Anthropic API Key: ")
				k, _ := reader.ReadString('\n')
				_ = cfg.SetInferenceCredentials("https://api.anthropic.com/v1", strings.TrimSpace(k), "claude-3-5-sonnet-20241022", configPath)
				cfg.Provider = "anthropic"
				_ = cfg.Save(configPath)
				fmt.Println("[OK] Proveedor configurado como Anthropic.")
			case "5":
				fmt.Print("Groq API Key: ")
				k, _ := reader.ReadString('\n')
				_ = cfg.SetInferenceCredentials("https://api.groq.com/openai/v1", strings.TrimSpace(k), "llama-3.3-70b-versatile", configPath)
				cfg.Provider = "groq"
				_ = cfg.Save(configPath)
				fmt.Println("[OK] Proveedor configurado como Groq.")
			case "6":
				_ = cfg.SetInferenceCredentials("http://localhost:11434/v1", "ollama", "llama3", configPath)
				cfg.Provider = "ollama"
				_ = cfg.Save(configPath)
				fmt.Println("[OK] Proveedor configurado como Ollama Local.")
			case "7":
				fmt.Print("URL Base: ")
				u, _ := reader.ReadString('\n')
				fmt.Print("API Key: ")
				k, _ := reader.ReadString('\n')
				fmt.Print("Modelo: ")
				m, _ := reader.ReadString('\n')
				_ = cfg.SetInferenceCredentials(strings.TrimSpace(u), strings.TrimSpace(k), strings.TrimSpace(m), configPath)
				cfg.Provider = "custom"
				_ = cfg.Save(configPath)
				fmt.Println("[OK] Proveedor personalizado configurado.")
			}

		case "3":
			fmt.Printf("\nEjecutando prueba de conexion con %s (%s) ...\n", cfg.Provider, cfg.Model)
			lat, err := VerifyInference(cfg.APIBase, cfg.APIKey, cfg.Model)
			if err != nil {
				fmt.Printf("[FALLO] Error de conexion: %v\n", err)
			} else {
				fmt.Printf("[OK] Conexion exitosa. Latencia: %d ms\n", lat)
			}

		case "4":
			fmt.Println("\nConfiguracion de Canales:")
			fmt.Printf("1) Alternar WhatsApp (Actual: %v)\n", cfg.WhatsAppEnabled)
			fmt.Printf("2) Alternar Telegram (Actual: %v)\n", cfg.TelegramEnabled)
			fmt.Print("Seleccione opcion [1-2]: ")
			cOpt, _ := reader.ReadString('\n')
			cOpt = strings.TrimSpace(cOpt)
			if cOpt == "1" {
				cfg.WhatsAppEnabled = !cfg.WhatsAppEnabled
				_ = cfg.Save(configPath)
				fmt.Printf("[OK] WhatsApp ahora esta: %v\n", cfg.WhatsAppEnabled)
			} else if cOpt == "2" {
				cfg.TelegramEnabled = !cfg.TelegramEnabled
				if cfg.TelegramEnabled && cfg.TelegramBotToken == "" {
					fmt.Print("Ingrese Telegram Bot Token: ")
					tok, _ := reader.ReadString('\n')
					cfg.TelegramBotToken = strings.TrimSpace(tok)
				}
				_ = cfg.Save(configPath)
				fmt.Printf("[OK] Telegram ahora esta: %v\n", cfg.TelegramEnabled)
			}

		case "5":
			fmt.Println()
			CmdConfig([]string{"show"}, configPath)
		}
		fmt.Println()
	}
}
