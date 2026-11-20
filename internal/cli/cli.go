package cli

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"felix-go/internal/agent"
	"felix-go/internal/botfactory"
	"felix-go/internal/checkpoint"
	"felix-go/internal/config"
	"felix-go/internal/memory"
	"felix-go/internal/tools"
)

func IsCLICommand(arg string) bool {
	switch strings.ToLower(arg) {
	case "cli", "status", "logs", "conv", "conversations", "sessions", "config", "bots", "bot", "chat", "version", "-v", "--version", "help", "-h", "--help":
		return true
	default:
		return false
	}
}

func PrintBanner() {
	fmt.Println("================================================================================")
	fmt.Println(`       /\_/\      ███████╗███████╗██╗     ██╗██╗  ██╗`)
	fmt.Println(`      ( o.o )     ██╔════╝██╔════╝██║     ██║╚██╗██╔╝`)
	fmt.Println(`       > ^ <      █████╗  █████╗  ██║     ██║ ╚███╔╝ `)
	fmt.Println(`      /|   |\     ██╔══╝  ██╔══╝  ██║     ██║ ██╔██╗ `)
	fmt.Println(`     (_|   |_)    ██║     ███████╗███████╗██║██╔╝ ██╗`)
	fmt.Println(`       "   "      ╚═╝     ╚══════╝╚══════╝╚═╝╚═╝  ╚═╝`)
	fmt.Println(`     [FELIX]      A G E N T  //  S W E  &  D e v O p s`)
	fmt.Println("================================================================================")
	fmt.Println(" Modo: Consola CLI Interactiva (Workstation & Evaluacion Local)")
	fmt.Println(" Version: v2.2.0 | Runtime: Go Nativo (Sin dependencias externas)")
	fmt.Println(" Soberania: Republica Argentina [ARG] | Licencia: MIT Open Source")
	fmt.Println(" Repositorio: https://github.com/m-leiva/felix-agent")
	fmt.Println("================================================================================")
}

func RunCLI(args []string, configPath string) int {
	if len(args) == 0 {
		PrintHelp()
		return 0
	}

	cmd := strings.ToLower(args[0])
	subArgs := args[1:]

	switch cmd {
	case "cli":
		return CmdCLI(subArgs, configPath)
	case "status":
		return CmdStatus(configPath)
	case "logs":
		return CmdLogs(subArgs)
	case "conv", "conversations", "sessions":
		return CmdConv(subArgs, configPath)
	case "config":
		return CmdConfig(subArgs, configPath)
	case "bots", "bot":
		return CmdBots(subArgs, configPath)
	case "chat":
		return CmdChat(subArgs, configPath)
	case "version", "-v", "--version":
		PrintVersion()
		return 0
	case "help", "-h", "--help":
		PrintHelp()
		return 0
	default:
		fmt.Printf("Comando '%s' no reconocido.\n\n", cmd)
		PrintHelp()
		return 1
	}
}

func PrintVersion() {
	PrintBanner()
	fmt.Println("Arquitectura: linux/amd64 (Go 1.22+)")
	fmt.Println("Compilacion estatica, sin runtime de Python o Node.js.")
}

func PrintHelp() {
	PrintBanner()
	fmt.Println("Uso: felix <comando> [argumentos...]")
	fmt.Println()
	fmt.Println("Comandos para PC / Workstation (Evaluacion y Pruebas Locales):")
	fmt.Println("  cli                     Inicia la sesion interactiva con presentacion grafica")
	fmt.Println("  chat [peticion]         Abre consola de chat interactivo o ejecuta un prompt directo")
	fmt.Println("  status                  Muestra el panel de salud, telemetria y estado en vivo")
	fmt.Println("  bots [subcomando]       Motor y fabrica de bots comerciales (list, chat, create)")
	fmt.Println("  config [show|get|set]   Consulta o modifica parametros de inferencia (modelo, api)")
	fmt.Println("  conv [list|show|search] Inspecciona sesiones e historial persistente en SQLite")
	fmt.Println("  logs [-n <lineas>] [-f] Muestra o sigue en tiempo real los logs del demonio")
	fmt.Println("  version                 Muestra banner, version e informacion de compilacion")
	fmt.Println("  help                    Muestra este manual de ayuda")
	fmt.Println()
	fmt.Println("Ejemplos de evaluacion en PC:")
	fmt.Println("  felix cli")
	fmt.Println("  felix chat 'Crea un script en Python que analice el uso de disco'")
	fmt.Println("  felix bots list")
	fmt.Println("  felix bots chat pizzeria-napoli")
	fmt.Println("  felix config show")
	fmt.Println("  felix status")
	fmt.Println("================================================================================")
}

func CmdStatus(configPath string) int {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Error cargando configuracion (%s): %v\n", configPath, err)
		return 1
	}

	healthPort := cfg.HealthPort
	if healthPort == "" {
		healthPort = "8055"
	}

	client := &http.Client{Timeout: 3 * time.Second}
	healthURL := fmt.Sprintf("http://127.0.0.1:%s/healthz", healthPort)
	resp, err := client.Get(healthURL)

	fmt.Println("================================================================================")
	fmt.Println("                       FELIX AGENT - ESTADO EN VIVO                             ")
	fmt.Println("================================================================================")

	if err != nil {
		fmt.Println("Estado del Servicio:     DETENIDO / INACCESIBLE")
		fmt.Printf("Endpoint de Salud:       %s (Sin respuesta: %v)\n", healthURL, err)
	} else {
		defer resp.Body.Close()
		var st struct {
			Status                string `json:"status"`
			UptimeHuman           string `json:"uptime_human"`
			WhatsAppConnected     bool   `json:"whatsapp_connected"`
			TelegramConnected     bool   `json:"telegram_connected"`
			LLMPrimaryHealthy     bool   `json:"llm_primary_healthy"`
			LLMFallbackActive     bool   `json:"llm_fallback_active"`
			TotalPrimaryCalls     int64  `json:"total_primary_calls"`
			TotalFallbackCalls    int64  `json:"total_fallback_calls"`
			TotalPromptTokens     int64  `json:"total_prompt_tokens"`
			TotalCompletionTokens int64  `json:"total_completion_tokens"`
			ActiveSubagents       int    `json:"active_subagents"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&st)

		fmt.Printf("Estado del Servicio:     ONLINE (Puerto HTTP: %s)\n", healthPort)
		fmt.Printf("Tiempo de Operacion:     %s\n", st.UptimeHuman)

		waStatus := "DESCONECTADO"
		if st.WhatsAppConnected {
			waStatus = "CONECTADO"
		}
		tgStatus := "DESCONECTADO / DESACTIVADO"
		if st.TelegramConnected {
			tgStatus = "CONECTADO"
		}
		fmt.Printf("Canal WhatsApp:          %s\n", waStatus)
		fmt.Printf("Canal Telegram:          %s\n", tgStatus)

		fmt.Println()
		fmt.Println("Inferencia LLM:")
		primHealthy := "SALUDABLE"
		if !st.LLMPrimaryHealthy {
			primHealthy = "FALLO / EN COOLDOWN"
		}
		fmt.Printf("  Proveedor Primario:    %s (%s)\n", primHealthy, cfg.APIBase)
		fmt.Printf("  Modelo Configurado:    %s\n", cfg.Model)
		fallbackStatus := "INACTIVO (Normal)"
		if st.LLMFallbackActive {
			fallbackStatus = "ACTIVO (Failover operando)"
		}
		fmt.Printf("  Estado de Fallback:    %s\n", fallbackStatus)
		fmt.Printf("  Llamadas LLM:          Primarias: %d | Fallback: %d\n", st.TotalPrimaryCalls, st.TotalFallbackCalls)
		fmt.Printf("  Tokens Consumidos:     Prompt: %d | Completion: %d (Total: %d)\n",
			st.TotalPromptTokens, st.TotalCompletionTokens, st.TotalPromptTokens+st.TotalCompletionTokens)
		fmt.Printf("  Subagentes Activos:    %d\n", st.ActiveSubagents)
	}

	botsDBPath := filepath.Join(cfg.DataDir, "bots.db")
	if store, err := botfactory.NewStorage(botsDBPath); err == nil {
		defer store.Close()
		bots, _ := store.ListBots()
		fmt.Println()
		fmt.Printf("Bot Factory:             %d bots registrados en %s\n", len(bots), botsDBPath)
		for _, b := range bots {
			stats, _ := store.GetStats(b.ID)
			fmt.Printf("  - [%s] %s (%s) | Pedidos: %d | Turnos: %d | Mensajes: %d\n",
				b.ID, b.Name, b.Type, stats.TotalOrders, stats.TotalAppointments, stats.TotalMessages)
		}
	}

	sessDBPath := filepath.Join(cfg.DataDir, "sessions.db")
	if sdb, err := sql.Open("sqlite", sessDBPath); err == nil {
		defer sdb.Close()
		var count int
		_ = sdb.QueryRow("SELECT COUNT(DISTINCT session_id) FROM session_messages").Scan(&count)
		fmt.Println()
		fmt.Printf("Sesiones Guardadas:      %d conversaciones en %s\n", count, sessDBPath)
	}

	fmt.Println("================================================================================")
	return 0
}

func CmdLogs(args []string) int {
	lines := 50
	follow := false
	logType := "all"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--lines":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
					lines = n
					i++
				}
			}
		case "-f", "--follow":
			follow = true
		case "out":
			logType = "out"
		case "error", "err":
			logType = "error"
		}
	}

	logFiles := []string{
		"/home/matias/.pm2/logs/felix-go-out-9.log",
		"/home/matias/.pm2/logs/felix-go-error-9.log",
	}

	targetPath := ""
	if logType == "out" {
		targetPath = logFiles[0]
	} else if logType == "error" {
		targetPath = logFiles[1]
	} else {
		for _, f := range logFiles {
			if _, err := os.Stat(f); err == nil {
				targetPath = f
				break
			}
		}
	}

	if targetPath == "" {
		matches, _ := filepath.Glob("/home/matias/.pm2/logs/felix-go*.log")
		if len(matches) > 0 {
			targetPath = matches[0]
		}
	}

	if targetPath == "" {
		fmt.Println("No se encontraron archivos de logs activos para felix-go en ~/.pm2/logs/")
		return 1
	}

	fmt.Printf("=== Mostrando ultimas %d lineas de: %s ===\n", lines, targetPath)

	file, err := os.Open(targetPath)
	if err != nil {
		fmt.Printf("Error abriendo log %s: %v\n", targetPath, err)
		return 1
	}
	defer file.Close()

	var collected []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		collected = append(collected, scanner.Text())
	}

	start := 0
	if len(collected) > lines {
		start = len(collected) - lines
	}
	for i := start; i < len(collected); i++ {
		fmt.Println(collected[i])
	}

	if follow {
		fmt.Println("\n--- Siguiendo log en tiempo real (Ctrl+C para salir) ---")
		for {
			time.Sleep(500 * time.Millisecond)
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}
		}
	}

	return 0
}

func CmdConv(args []string, configPath string) int {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Error cargando configuracion: %v\n", err)
		return 1
	}

	sessDBPath := filepath.Join(cfg.DataDir, "sessions.db")
	db, err := sql.Open("sqlite", sessDBPath)
	if err != nil {
		fmt.Printf("Error abriendo base de sesiones (%s): %v\n", sessDBPath, err)
		return 1
	}
	defer db.Close()

	action := "list"
	targetSession := ""
	limit := 30

	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "list":
			action = "list"
		case "show":
			action = "show"
			if len(args) > 1 {
				targetSession = args[1]
			}
		case "search":
			action = "search"
			if len(args) > 1 {
				targetSession = strings.Join(args[1:], " ")
			}
		case "clear", "delete":
			action = "clear"
			if len(args) > 1 {
				targetSession = args[1]
			}
		default:
			action = "show"
			targetSession = args[0]
		}
	}

	for i := 0; i < len(args); i++ {
		if (args[i] == "-n" || args[i] == "--limit") && i+1 < len(args) {
			if l, err := strconv.Atoi(args[i+1]); err == nil && l > 0 {
				limit = l
			}
		}
	}

	switch action {
	case "list":
		query := `SELECT session_id, count(*), max(created_at) 
			FROM session_messages 
			GROUP BY session_id 
			ORDER BY max(created_at) DESC 
			LIMIT 30;`
		rows, err := db.Query(query)
		if err != nil {
			fmt.Printf("Error consultando sesiones: %v\n", err)
			return 1
		}
		defer rows.Close()

		fmt.Println("================================================================================")
		fmt.Printf("%-32s | %-10s | %-20s\n", "ID DE SESION / CONTACTO", "MENSAJES", "ULTIMA ACTIVIDAD")
		fmt.Println("--------------------------------------------------------------------------------")
		count := 0
		for rows.Next() {
			var sessID, lastAt string
			var total int
			_ = rows.Scan(&sessID, &total, &lastAt)
			fmt.Printf("%-32s | %-10d | %-20s\n", sessID, total, lastAt)
			count++
		}
		if count == 0 {
			fmt.Println("No hay conversaciones registradas en la base de datos.")
		}
		fmt.Println("================================================================================")
		fmt.Println("Usa 'felix conv show <session_id>' para leer los mensajes.")

	case "show":
		if targetSession == "" {
			fmt.Println("Debes especificar el ID de sesion a inspeccionar: felix conv show <session_id>")
			return 1
		}
		query := `SELECT id, role, content, created_at 
			FROM session_messages 
			WHERE session_id = ? 
			ORDER BY id DESC LIMIT ?;`
		rows, err := db.Query(query, targetSession, limit)
		if err != nil {
			fmt.Printf("Error consultando historial de '%s': %v\n", targetSession, err)
			return 1
		}
		defer rows.Close()

		type rowData struct {
			id        int64
			role      string
			content   string
			createdAt string
		}
		var descList []rowData
		for rows.Next() {
			var d rowData
			_ = rows.Scan(&d.id, &d.role, &d.content, &d.createdAt)
			descList = append(descList, d)
		}

		fmt.Println("================================================================================")
		fmt.Printf("Historial de conversacion para: %s (Ultimos %d mensajes)\n", targetSession, len(descList))
		fmt.Println("================================================================================")

		if len(descList) == 0 {
			fmt.Println("No hay mensajes registrados para esta sesion.")
		}

		for i := len(descList) - 1; i >= 0; i-- {
			item := descList[i]
			roleLabel := strings.ToUpper(item.role)
			fmt.Printf("[%s] %s:\n%s\n\n", item.createdAt, roleLabel, item.content)
		}
		fmt.Println("================================================================================")

	case "search":
		if targetSession == "" {
			fmt.Println("Debes especificar el termino a buscar: felix conv search <palabra>")
			return 1
		}
		query := `SELECT session_id, role, content, created_at 
			FROM session_messages 
			WHERE content LIKE ? 
			ORDER BY id DESC LIMIT 20;`
		rows, err := db.Query(query, "%"+targetSession+"%")
		if err != nil {
			fmt.Printf("Error buscando mensajes: %v\n", err)
			return 1
		}
		defer rows.Close()

		fmt.Println("================================================================================")
		fmt.Printf("Resultados de busqueda para '%s':\n", targetSession)
		fmt.Println("================================================================================")
		found := 0
		for rows.Next() {
			var sid, role, content, cat string
			_ = rows.Scan(&sid, &role, &content, &cat)
			preview := content
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			fmt.Printf("[%s] Sesion: %s | %s:\n   %s\n\n", cat, sid, strings.ToUpper(role), preview)
			found++
		}
		if found == 0 {
			fmt.Println("No se encontraron coincidencias.")
		}
		fmt.Println("================================================================================")

	case "clear":
		if targetSession == "" {
			fmt.Println("Debes especificar la sesion a limpiar: felix conv clear <session_id>")
			return 1
		}
		_, _ = db.Exec("DELETE FROM session_messages WHERE session_id = ?", targetSession)
		_, _ = db.Exec("DELETE FROM session_summaries WHERE session_id = ?", targetSession)
		fmt.Printf("Historial de la sesion '%s' eliminado correctamente.\n", targetSession)
	}

	return 0
}

func CmdConfig(args []string, configPath string) int {
	_, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Error cargando configuracion (%s): %v\n", configPath, err)
		return 1
	}

	action := "show"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}

	switch action {
	case "show":
		raw, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("Error leyendo %s: %v\n", configPath, err)
			return 1
		}
		var generic map[string]any
		_ = json.Unmarshal(raw, &generic)

		maskKeys := []string{"api_key", "openai_api_key", "groq_api_key", "telegram_bot_token", "fallback_api_key"}
		for _, k := range maskKeys {
			if val, ok := generic[k].(string); ok && len(val) > 6 {
				generic[k] = val[:4] + "****" + val[len(val)-2:]
			}
		}

		out, _ := json.MarshalIndent(generic, "", "  ")
		fmt.Printf("=== Configuracion activa en %s ===\n", configPath)
		fmt.Println(string(out))

	case "path":
		abs, _ := filepath.Abs(configPath)
		fmt.Println(abs)

	case "get":
		if len(args) < 2 {
			fmt.Println("Uso: felix config get <clave>")
			return 1
		}
		key := args[1]
		raw, _ := os.ReadFile(configPath)
		var generic map[string]any
		_ = json.Unmarshal(raw, &generic)
		val, exists := generic[key]
		if !exists {
			fmt.Printf("La clave '%s' no existe en la configuracion.\n", key)
			return 1
		}
		out, _ := json.MarshalIndent(val, "", "  ")
		fmt.Println(string(out))

	case "set":
		if len(args) < 3 {
			fmt.Println("Uso: felix config set <clave> <valor>")
			return 1
		}
		key := args[1]
		valStr := strings.Join(args[2:], " ")

		raw, _ := os.ReadFile(configPath)
		var generic map[string]any
		_ = json.Unmarshal(raw, &generic)

		switch valStr {
		case "true":
			generic[key] = true
		case "false":
			generic[key] = false
		default:
			if n, err := strconv.Atoi(valStr); err == nil {
				generic[key] = n
			} else {
				generic[key] = valStr
			}
		}

		updatedRaw, err := json.MarshalIndent(generic, "", "  ")
		if err != nil {
			fmt.Printf("Error serializando JSON: %v\n", err)
			return 1
		}
		if err := os.WriteFile(configPath, updatedRaw, 0644); err != nil {
			fmt.Printf("Error guardando configuracion: %v\n", err)
			return 1
		}
		fmt.Printf("Configuracion actualizada: '%s' = '%s'. Reinicia felix (pm2 restart felix-go) para aplicar cambios.\n", key, valStr)

	default:
		fmt.Println("Acciones de configuracion disponibles: show, get, set, path")
		return 1
	}

	return 0
}

func CmdBots(args []string, configPath string) int {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Error cargando configuracion: %v\n", err)
		return 1
	}

	botsDBPath := filepath.Join(cfg.DataDir, "bots.db")
	store, err := botfactory.NewStorage(botsDBPath)
	if err != nil {
		fmt.Printf("Error abriendo storage de bots (%s): %v\n", botsDBPath, err)
		return 1
	}
	defer store.Close()

	mgr := botfactory.NewManager(store, nil)

	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}

	switch action {
	case "list":
		bots, err := mgr.ListBots()
		if err != nil {
			fmt.Printf("Error listando bots: %v\n", err)
			return 1
		}
		fmt.Println("================================================================================")
		fmt.Printf("%-24s | %-24s | %-16s | %-8s\n", "ID DEL BOT", "NOMBRE", "TIPO", "ESTADO")
		fmt.Println("--------------------------------------------------------------------------------")
		for _, b := range bots {
			fmt.Printf("%-24s | %-24s | %-16s | %-8s\n", b.ID, b.Name, b.Type, b.Status)
		}
		if len(bots) == 0 {
			fmt.Println("No hay bots creados actualmente.")
		}
		fmt.Println("================================================================================")
		fmt.Println("Usa 'felix bots show <bot_id>' o 'felix bots chat <bot_id>' para interactuar.")

	case "show", "inspect":
		if len(args) < 2 {
			fmt.Println("Uso: felix bots show <bot_id>")
			return 1
		}
		botID := args[1]
		spec, err := mgr.GetBot(botID)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
		stats, _ := store.GetStats(botID)

		fmt.Println("================================================================================")
		fmt.Printf("DETALLE DEL BOT: %s (%s)\n", spec.Name, spec.ID)
		fmt.Println("================================================================================")
		fmt.Printf("Tipo:                    %s\n", spec.Type)
		fmt.Printf("Estado:                  %s\n", spec.Status)
		if len(spec.Nicknames) > 0 {
			fmt.Printf("Apodos / Aliases:        %s\n", strings.Join(spec.Nicknames, ", "))
		}
		fmt.Printf("Descripcion:             %s\n", spec.Description)
		if spec.Personality != "" {
			fmt.Printf("Personalidad:            %s\n", spec.Personality)
		}
		if spec.ReplicationSource != "" {
			fmt.Printf("Replicado a partir de:   %s\n", spec.ReplicationSource)
		}
		fmt.Printf("Grounding Estricto:      %v\n", spec.StrictGrounding)
		fmt.Printf("Items en Catalogo:       %d productos/servicios\n", len(spec.Catalog))
		fmt.Printf("Preguntas Frecuentes:    %d entradas de FAQ\n", len(spec.FAQ))
		fmt.Printf("Metricas en SQLite:      %d pedidos | %d turnos | %d mensajes\n",
			stats.TotalOrders, stats.TotalAppointments, stats.TotalMessages)
		fmt.Println("================================================================================")

	case "create":
		tmpl := "pizzeria"
		id := ""
		name := ""
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--template", "-t":
				if i+1 < len(args) {
					tmpl = args[i+1]
					i++
				}
			case "--id":
				if i+1 < len(args) {
					id = args[i+1]
					i++
				}
			case "--name":
				if i+1 < len(args) {
					name = args[i+1]
					i++
				}
			}
		}
		if id == "" {
			fmt.Println("Uso: felix bots create --template <pizzeria|turnos|llamadas|faq_soporte|mosiah> --id <slug> --name <nombre>")
			return 1
		}
		spec, err := mgr.CreateFromTemplate(tmpl, id, name, nil)
		if err != nil {
			fmt.Printf("Error creando bot: %v\n", err)
			return 1
		}
		fmt.Printf("Bot '%s' (ID: %s, Tipo: %s) creado exitosamente desde plantilla '%s'.\n", spec.Name, spec.ID, spec.Type, tmpl)

	case "replicate":
		if len(args) < 3 {
			fmt.Println("Uso: felix bots replicate <source_id> <new_id> [--name <nombre>]")
			return 1
		}
		sourceID := args[1]
		newID := args[2]
		newName := ""
		for i := 3; i < len(args); i++ {
			if args[i] == "--name" && i+1 < len(args) {
				newName = args[i+1]
				i++
			}
		}
		replica, err := mgr.ReplicateBot(sourceID, newID, newName, nil)
		if err != nil {
			fmt.Printf("Error replicando bot: %v\n", err)
			return 1
		}
		fmt.Printf("Bot '%s' (ID: %s) replicado exitosamente a partir de '%s'.\n", replica.Name, replica.ID, sourceID)

	case "chat":
		if len(args) < 2 {
			fmt.Println("Uso: felix bots chat <bot_id> [mensaje...]")
			return 1
		}
		botID := args[1]
		healthPort := cfg.HealthPort
		if healthPort == "" {
			healthPort = "8055"
		}
		apiEndpoint := fmt.Sprintf("http://127.0.0.1:%s/api/bots/%s/chat", healthPort, botID)

		if len(args) > 2 {
			userMsg := strings.Join(args[2:], " ")
			reply, err := sendBotChatMessage(apiEndpoint, "cli-test", userMsg)
			if err != nil {
				fmt.Printf("Error en chat: %v\n", err)
				return 1
			}
			fmt.Println(reply)
			return 0
		}

		fmt.Printf("=== Sesion de Chat interactivo con Bot '%s' ===\n", botID)
		fmt.Println("Escribe tu mensaje y presiona Enter (o 'salir' para terminar):")
		reader := bufio.NewReader(os.Stdin)
		sessionID := fmt.Sprintf("cli-%d", time.Now().Unix())
		for {
			fmt.Printf("\nbot(%s)> ", botID)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if line == "salir" || line == "exit" || line == "quit" {
				fmt.Println("Finalizando sesion de chat.")
				break
			}
			reply, err := sendBotChatMessage(apiEndpoint, sessionID, line)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Println()
			fmt.Println(reply)
		}

	case "orders":
		if len(args) < 2 {
			fmt.Println("Uso: felix bots orders <bot_id>")
			return 1
		}
		orders, err := store.ListOrders(args[1], 50)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
		fmt.Println("================================================================================")
		fmt.Printf("%-4s | %-16s | %-12s | %-18s | %-12s\n", "ID", "CLIENTE", "TOTAL", "DIRECCION", "PAGO")
		fmt.Println("--------------------------------------------------------------------------------")
		for _, o := range orders {
			fmt.Printf("%-4d | %-16s | $%-11.2f | %-18s | %-12s\n", o.ID, o.CustomerName, o.Total, o.DeliveryAddress, o.PaymentMethod)
		}
		if len(orders) == 0 {
			fmt.Println("No hay pedidos registrados para este bot.")
		}
		fmt.Println("================================================================================")

	case "appointments":
		if len(args) < 2 {
			fmt.Println("Uso: felix bots appointments <bot_id>")
			return 1
		}
		apps, err := store.ListAppointments(args[1], 50)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
		fmt.Println("================================================================================")
		fmt.Printf("%-4s | %-16s | %-20s | %-16s | %-10s\n", "ID", "CLIENTE", "SERVICIO", "FECHA Y HORA", "ESTADO")
		fmt.Println("--------------------------------------------------------------------------------")
		for _, a := range apps {
			fmt.Printf("%-4d | %-16s | %-20s | %-16s | %-10s\n", a.ID, a.CustomerName, a.Service, a.StartTime.Format("2006-01-02 15:04"), a.Status)
		}
		if len(apps) == 0 {
			fmt.Println("No hay turnos agendados para este bot.")
		}
		fmt.Println("================================================================================")

	case "export":
		if len(args) < 3 {
			fmt.Println("Uso: felix bots export <bot_id> <directorio_destino>")
			return 1
		}
		spec, err := mgr.GetBot(args[1])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
		if err := botfactory.ExportStandalone(spec, args[2]); err != nil {
			fmt.Printf("Error exportando bot: %v\n", err)
			return 1
		}
		fmt.Printf("Microservicio Go autonomo para el bot '%s' exportado exitosamente en: %s\n", spec.Name, args[2])

	case "delete":
		if len(args) < 2 {
			fmt.Println("Uso: felix bots delete <bot_id>")
			return 1
		}
		if err := mgr.DeleteBot(args[1]); err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
		fmt.Printf("Bot '%s' eliminado correctamente.\n", args[1])

	default:
		fmt.Println("Acciones disponibles para 'felix bots': list, show, create, replicate, chat, orders, appointments, export, delete")
		return 1
	}

	return 0
}

func sendBotChatMessage(endpoint, sessionID, message string) (string, error) {
	reqBody := map[string]string{
		"session_id": sessionID,
		"message":    message,
	}
	raw, _ := json.Marshal(reqBody)

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("error conectando con el bot (asegurate de que Felix este corriendo): %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var res struct {
		Reply string `json:"reply"`
		Order *struct {
			Total float64 `json:"total"`
		} `json:"order,omitempty"`
		Appointment *struct {
			Service   string    `json:"service"`
			StartTime time.Time `json:"start_time"`
		} `json:"appointment,omitempty"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return string(bodyBytes), nil
	}

	out := res.Reply
	if res.Order != nil {
		out += fmt.Sprintf("\n[PEDIDO REGISTRADO: Total $%.2f]", res.Order.Total)
	}
	if res.Appointment != nil {
		out += fmt.Sprintf("\n[TURNO CONFIRMADO: %s el %s]", res.Appointment.Service, res.Appointment.StartTime.Format("2006-01-02 15:04"))
	}
	return out, nil
}

func CmdCLI(args []string, configPath string) int {
	return CmdChat(args, configPath)
}

func CmdChat(args []string, configPath string) int {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Error cargando configuracion: %v\n", err)
		return 1
	}

	skillsDir := cfg.SkillsDir
	if skillsDir == "" {
		skillsDir = "skills"
	}

	reg := tools.NewRegistry()
	reg.Register(&tools.ExecTool{DefaultWorkDir: cfg.WorkspaceDir})
	reg.Register(&tools.FileReadTool{})
	reg.Register(&tools.FileWriteTool{})
	reg.Register(&tools.FileEditTool{})
	reg.Register(&tools.FileLsTool{DefaultWorkDir: cfg.WorkspaceDir})
	reg.Register(&tools.ApplyPatchTool{DefaultWorkDir: cfg.WorkspaceDir})
	reg.Register(&tools.ProcessTool{})
	reg.Register(&tools.WebSearchTool{})
	reg.Register(&tools.WebFetchTool{})
	reg.Register(&tools.SqliteQueryTool{})
	reg.Register(&tools.SQLiteInspectorTool{})
	reg.Register(&tools.GitTool{DefaultRepoDir: cfg.WorkspaceDir})
	reg.Register(&tools.DockerTool{})
	reg.Register(&tools.TailscaleTool{})
	reg.Register(&tools.WeatherTool{})
	reg.Register(&tools.DocEngineTool{})
	reg.Register(&tools.NetSentinelTool{})
	reg.Register(&tools.SkillsListTool{SkillsDir: skillsDir})
	reg.Register(&tools.SkillsReadTool{SkillsDir: skillsDir})
	reg.Register(&tools.SkillsWriteTool{SkillsDir: skillsDir})

	ckptStore, _ := checkpoint.NewSQLiteStore(filepath.Join(cfg.DataDir, "checkpoints.db"))
	if ckptStore != nil {
		defer ckptStore.Close()
	}

	sessionDBPath := filepath.Join(cfg.DataDir, "sessions.db")
	histMgr, err := agent.NewHistoryManager(sessionDBPath, 40)
	if err != nil {
		fmt.Printf("Error inicializando historial: %v\n", err)
		return 1
	}
	defer histMgr.Close()

	scratchpadMgr, _ := tools.NewScratchpadManager(histMgr.DB())
	if scratchpadMgr != nil {
		reg.Register(&tools.ScratchpadTool{Manager: scratchpadMgr})
	}
	reg.Register(&tools.SessionSearchTool{DB: histMgr.DB()})
	reg.Register(&tools.SessionHistoryTool{DB: histMgr.DB()})
	reg.Register(&tools.GoalManagerTool{DB: histMgr.DB()})

	engine := agent.NewEngine(cfg, reg, histMgr)
	if scratchpadMgr != nil {
		engine.SetScratchpadManager(scratchpadMgr)
	}

	if ckptStore != nil {
		reg.Register(&tools.HarnessDevelopTool{
			DefaultRepoDir:  cfg.WorkspaceDir,
			Completer:       engine,
			CheckpointStore: ckptStore,
		})
	}

	botsDBPath := filepath.Join(cfg.DataDir, "bots.db")
	if botStore, errStore := botfactory.NewStorage(botsDBPath); errStore == nil {
		defer botStore.Close()
		botMgr := botfactory.NewManager(botStore, engine)
		reg.Register(&tools.BotFactoryTool{Manager: botMgr})
	}

	memStore := memory.NewStore(cfg.WorkspaceDir, cfg.DataDir)
	contextMem := memStore.GetContextMemory(memory.MemoryContextOptions{
		IsAdmin: true,
	})

	if len(args) > 0 {
		prompt := strings.Join(args, " ")
		fmt.Printf("=== Felix CLI: Ejecutando peticion ===\n%s\n\n", prompt)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		reply, err := engine.Run(ctx, "cli-session", prompt, contextMem)
		if err != nil {
			fmt.Printf("Error en ejecucion: %v\n", err)
			return 1
		}
		fmt.Println("=== Respuesta de Felix ===")
		fmt.Println(reply)
		return 0
	}

	PrintBanner()
	fmt.Printf("Modelo LLM: %s | Workspace: %s\n", cfg.Model, cfg.WorkspaceDir)
	fmt.Printf("Herramientas activas: %d registradas\n", len(reg.GetOpenAISchemas()))
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("Consola de Evaluacion y Pruebas Locales (PC)")
	fmt.Println("Escribe tu peticion tecnica o consulta en lenguaje natural.")
	fmt.Println("Comandos especiales:")
	fmt.Println("  :tools       Lista todas las herramientas activas en esta sesion")
	fmt.Println("  :model       Muestra el modelo LLM y endpoint configurado")
	fmt.Println("  :clear       Limpia la pantalla de la terminal")
	fmt.Println("  :help        Muestra los atajos de la consola")
	fmt.Println("  :exit        Finaliza la sesion interactiva (o 'salir', 'quit')")
	fmt.Println("--------------------------------------------------------------------------------")

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("felix [cli]> ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == ":exit" || line == "salir" || line == "exit" || line == "quit" {
			fmt.Println("Sesion interactiva finalizada.")
			break
		}
		if line == ":clear" {
			fmt.Print("\033[H\033[2J")
			PrintBanner()
			continue
		}
		if line == ":help" {
			fmt.Println("Comandos de la consola CLI:")
			fmt.Println("  :tools       Lista el catalogo de herramientas disponibles")
			fmt.Println("  :model       Muestra la configuracion del proveedor LLM")
			fmt.Println("  :clear       Limpia la pantalla")
			fmt.Println("  :exit        Sale de la consola interactiva")
			fmt.Println("Cualquier otro texto se envia a Felix para su analisis y ejecucion.")
			continue
		}
		if line == ":model" {
			fmt.Println("Configuracion de Inferencia:")
			fmt.Printf("  Modelo Primario:  %s\n", cfg.Model)
			fmt.Printf("  API Base:         %s\n", cfg.APIBase)
			fmt.Printf("  Proveedor:        %s\n", cfg.Provider)
			fmt.Printf("  Max Iteraciones:  %d\n", cfg.MaxIterations)
			if cfg.FallbackModel != "" {
				fmt.Printf("  Modelo Fallback:  %s\n", cfg.FallbackModel)
			}
			continue
		}
		if line == ":tools" {
			schemas := reg.GetOpenAISchemas()
			fmt.Printf("Herramientas Registradas (%d):\n", len(schemas))
			for i, s := range schemas {
				desc := s.Function.Description
				if len(desc) > 70 {
					desc = desc[:67] + "..."
				}
				fmt.Printf("  %2d) %-22s - %s\n", i+1, s.Function.Name, desc)
			}
			fmt.Println()
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		reply, err := engine.Run(ctx, "cli-session", line, contextMem)
		cancel()

		if err != nil {
			fmt.Printf("Error: %v\n\n", err)
			continue
		}
		fmt.Printf("\n%s\n\n", reply)
	}

	return 0
}
