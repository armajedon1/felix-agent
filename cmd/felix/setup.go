package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"felix-go/internal/config"
)

func runSetupWizard(configPath string) {
	PrintMOTD()
	fmt.Println("ASISTENTE DE CONFIGURACION Y PROVEEDORES LLM")
	fmt.Println("Soporta inicio de sesion con cuentas de Google y OpenAI sin necesidad de API keys.")
	fmt.Println("================================================================================")

	reader := bufio.NewReader(os.Stdin)

	cfg, _ := config.LoadConfig(configPath)
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	fmt.Println("Seleccione su modo de acceso o proveedor:")
	fmt.Println("  [1] Antigravity Tools (Recomendado: Login OAuth con cuentas de Google o OpenAI sin API Keys)")
	fmt.Println("  [2] Google Gemini (OAuth via Antigravity o API Key de Google AI Studio)")
	fmt.Println("  [3] OpenAI / ChatGPT (OAuth via Antigravity o API Key de OpenAI Platform)")
	fmt.Println("  [4] Groq Cloud (Llama 3.3 70B ultra-rapido con API Key gratuita)")
	fmt.Println("  [5] Ollama Local (http://localhost:11434/v1)")
	fmt.Println("  [6] Endpoint Personalizado / Custom (OpenAI-compatible)")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Print("Seleccione una opcion [1-6, por defecto 1]: ")

	opcion, _ := reader.ReadString('\n')
	opcion = strings.TrimSpace(opcion)
	if opcion == "" {
		opcion = "1"
	}

	selectedProvider := "antigravity"
	selectedBase := "http://127.0.0.1:8045/v1"
	selectedModel := "gemini-2.5-flash"
	selectedKey := "sk-antigravity"

	switch opcion {
	case "1":
		selectedProvider = "antigravity"
		selectedBase = "http://127.0.0.1:8045/v1"
		selectedModel = "gemini-2.5-flash"
		selectedKey = "sk-antigravity"

		fmt.Println()
		fmt.Println("================================================================================")
		fmt.Println("INICIO DE SESION OAUTH - CUENTAS GOOGLE Y OPENAI (ANTIGRAVITY TOOLS)")
		fmt.Println("================================================================================")
		fmt.Println("Antigravity Tools te permite usar modelos de Google y OpenAI iniciando sesion")
		fmt.Println("directamente con tu cuenta, sin necesidad de generar ni pagar API Keys.")
		fmt.Println()
		fmt.Println("  >>> Link de Login y Administracion: http://127.0.0.1:8045 <<<")
		fmt.Println("  (En red Homelab / Tailscale: http://100.96.230.125:8045)")
		fmt.Println()
		fmt.Println("Opciones:")
		fmt.Println("  [1] Abrir link de login en el navegador ahora")
		fmt.Println("  [2] Conectar al proxy del servidor Homelab (http://100.96.230.125:8045/v1)")
		fmt.Println("  [3] Iniciar proxy local en segundo plano (antigravity-tools --headless)")
		fmt.Println("  [4] Continuar directamente")
		fmt.Print("Seleccione una opcion [1-4, Enter para [1]]: ")
		subOpt, _ := reader.ReadString('\n')
		subOpt = strings.TrimSpace(subOpt)
		if subOpt == "" || subOpt == "1" {
			_ = OpenBrowser("http://127.0.0.1:8045")
			fmt.Println("Enlace abierto en el navegador: http://127.0.0.1:8045")
		} else if subOpt == "2" {
			selectedBase = "http://100.96.230.125:8045/v1"
			fmt.Println("Configurado endpoint Homelab: http://100.96.230.125:8045/v1")
		} else if subOpt == "3" {
			if _, err := exec.LookPath("antigravity-tools"); err == nil {
				fmt.Println("Iniciando antigravity-tools --headless...")
				_ = exec.Command("antigravity-tools", "--headless").Start()
				time.Sleep(1500 * time.Millisecond)
			} else {
				fmt.Println("[AVISO] Binario antigravity-tools no encontrado en PATH del sistema.")
			}
		}

	case "2":
		fmt.Println()
		fmt.Println("Metodo de acceso para Google Gemini:")
		fmt.Println("  [1] Login OAuth con cuenta de Google (via Antigravity Tools - Sin API Key)")
		fmt.Println("      Link de login: http://127.0.0.1:8045")
		fmt.Println("  [2] Ingresar API Key de Google AI Studio (https://aistudio.google.com/app/apikey)")
		fmt.Print("Seleccione metodo [1-2, Enter para [1]]: ")
		gOpt, _ := reader.ReadString('\n')
		gOpt = strings.TrimSpace(gOpt)

		if gOpt == "" || gOpt == "1" {
			selectedProvider = "antigravity"
			selectedBase = "http://127.0.0.1:8045/v1"
			selectedModel = "gemini-2.5-flash"
			selectedKey = "sk-antigravity"
			fmt.Println()
			fmt.Println("Inicia sesion con tu cuenta de Google en:")
			fmt.Println("  >>> http://127.0.0.1:8045 <<<")
			fmt.Print("Deseas abrir el link de login en tu navegador ahora? [S/n]: ")
			openAns, _ := reader.ReadString('\n')
			openAns = strings.ToLower(strings.TrimSpace(openAns))
			if openAns == "" || openAns == "s" || openAns == "si" || openAns == "y" {
				_ = OpenBrowser("http://127.0.0.1:8045")
			}
		} else {
			selectedProvider = "gemini"
			selectedBase = "https://generativelanguage.googleapis.com/v1beta/openai/"
			selectedModel = "gemini-2.5-flash"
			fmt.Println("Obten tu API Key gratis en: https://aistudio.google.com/app/apikey")
			fmt.Print("Ingrese su Google AI Studio API Key (AIzaSy...): ")
			kIn, _ := reader.ReadString('\n')
			selectedKey = strings.TrimSpace(kIn)
		}

	case "3":
		fmt.Println()
		fmt.Println("Metodo de acceso para OpenAI:")
		fmt.Println("  [1] Login OAuth con cuenta de OpenAI / ChatGPT (via Antigravity Tools - Sin API Key)")
		fmt.Println("      Link de login: http://127.0.0.1:8045")
		fmt.Println("  [2] Ingresar API Key de OpenAI Platform (https://platform.openai.com/api-keys)")
		fmt.Print("Seleccione metodo [1-2, Enter para [1]]: ")
		oOpt, _ := reader.ReadString('\n')
		oOpt = strings.TrimSpace(oOpt)

		if oOpt == "" || oOpt == "1" {
			selectedProvider = "antigravity"
			selectedBase = "http://127.0.0.1:8045/v1"
			selectedModel = "gpt-4o"
			selectedKey = "sk-antigravity"
			fmt.Println()
			fmt.Println("Inicia sesion con tu cuenta de OpenAI en:")
			fmt.Println("  >>> http://127.0.0.1:8045 <<<")
			fmt.Print("Deseas abrir el link de login en tu navegador ahora? [S/n]: ")
			openAns, _ := reader.ReadString('\n')
			openAns = strings.ToLower(strings.TrimSpace(openAns))
			if openAns == "" || openAns == "s" || openAns == "si" || openAns == "y" {
				_ = OpenBrowser("http://127.0.0.1:8045")
			}
		} else {
			selectedProvider = "openai"
			selectedBase = "https://api.openai.com/v1"
			selectedModel = "gpt-4o"
			fmt.Println("Gestiona tu clave en: https://platform.openai.com/api-keys")
			fmt.Print("Ingrese su OpenAI API Key (sk-...): ")
			kIn, _ := reader.ReadString('\n')
			selectedKey = strings.TrimSpace(kIn)
		}

	case "4":
		selectedProvider = "groq"
		selectedBase = "https://api.groq.com/openai/v1"
		selectedModel = "llama-3.3-70b-versatile"
		fmt.Println("Obten tu clave gratuita en: https://console.groq.com/keys")
		fmt.Print("Ingrese su Groq API Key (gsk_...): ")
		kIn, _ := reader.ReadString('\n')
		selectedKey = strings.TrimSpace(kIn)

	case "5":
		selectedProvider = "ollama"
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

	default:
		selectedProvider = "custom"
		fmt.Printf("Endpoint de inferencia API Base [actual: %s]: ", cfg.APIBase)
		urlInput, _ := reader.ReadString('\n')
		urlInput = strings.TrimSpace(urlInput)
		if urlInput != "" {
			selectedBase = urlInput
		}
		fmt.Print("Clave de API / Token (dejar vacio si no requiere): ")
		kIn, _ := reader.ReadString('\n')
		selectedKey = strings.TrimSpace(kIn)
		fmt.Printf("Nombre del modelo [actual: %s]: ", cfg.Model)
		mIn, _ := reader.ReadString('\n')
		mIn = strings.TrimSpace(mIn)
		if mIn != "" {
			selectedModel = mIn
		}
	}

	// Comprobacion en vivo
	fmt.Println()
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("VERIFICACION DE INFERENCIA EN VIVO")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Comprobando conexion con %s (%s) en %s ...\n", selectedProvider, selectedModel, selectedBase)
	latency, errTest := VerifyInference(selectedBase, selectedKey, selectedModel)
	if errTest == nil {
		fmt.Printf("[OK] Conexion verificada exitosamente (Latencia: %d ms)\n", latency)
	} else {
		fmt.Printf("[AVISO] No se pudo verificar la conexion: %v\n", errTest)
		if selectedProvider == "antigravity" {
			fmt.Println("Recuerda iniciar sesion con tu cuenta en http://127.0.0.1:8045")
		}
	}

	// Aplicar preset y guardar
	_ = cfg.ApplyPreset(selectedProvider, selectedKey, selectedModel, "")
	cfg.APIBase = selectedBase

	if err := cfg.Save(configPath); err != nil {
		fmt.Printf("\nError guardando configuracion: %v\n", err)
		return
	}

	fmt.Println("================================================================================")
	fmt.Printf("[OK] Configuracion guardada exitosamente en %s\n", configPath)
	fmt.Println("Para chatear o interactuar ejecuta:")
	fmt.Println("  felix chat                    -> Chat interactivo en terminal")
	fmt.Println("  felix --cli \"tu consulta\"     -> Ejecutar prompt directo")
	fmt.Println("  felix                         -> Menu principal interactivo")
	fmt.Println("================================================================================")
}
