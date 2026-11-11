package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"felix-go/internal/healer"
	"felix-go/internal/minecraft"
	"felix-go/internal/tools"
	"felix-go/internal/users"
)

// HandleSlashCommand intercepta y ejecuta comandos directos de administracion en milisegundos.
// Retorna (handled bool, responseText string).
func (c *Client) HandleSlashCommand(chatJID, senderJID, rawText string, isAdmin bool) (bool, string) {
	text := strings.TrimSpace(rawText)
	if !strings.HasPrefix(text, "/") {
		return false, ""
	}

	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])

	// Comandos universales permitidos a todos los roles
	if cmd == "/ayuda" || cmd == "/help" {
		if !isAdmin {
			return true, "*COMANDOS DISPONIBLES*\n- */help*: Esta lista de comandos basicos."
		}
		helpMsg := "*COMANDOS DIRECTOS DE ADMINISTRACION (Cero Latencia)*\n" +
			"- */status*: Diagnostico en tiempo real de hardware, red y servicios.\n" +
			"- */heal [servicio]*: Diagnostico y auto-curacion bajo demanda (PM2 y Docker).\n" +
			"- */snapshot*: Registra un snapshot instantaneo del estado de la infraestructura.\n" +
			"- */diff*: Compara el estado en vivo contra el ultimo snapshot y muestra diferencias.\n" +
			"- */mc* o */mc status*: Estado general del servidor Minecraft, PM2 y jugadores.\n" +
			"- */mc list*: Lista de jugadores online en Minecraft.\n" +
			"- */mc wl* o */mc whitelist*: Gestion de whitelist (list, add <user>, remove <user>).\n" +
			"- */mc say <msg>*: Difusion en el chat de Minecraft.\n" +
			"- */mc kick <user>*: Expulsar jugador de Minecraft.\n" +
			"- */pc* o */pc status*: Telemetria en vivo de la workstation remota.\n" +
			"- */pc clip <texto>* o */pc copy <texto>*: Copiar texto al portapapeles remoto.\n" +
			"- */services* o */servicios*: Estado en vivo de servicios monitoreados.\n" +
			"- */config* o */setup*: Menu interactivo de configuracion del sistema y de IA.\n" +
			"- */pm2*: Estado detallado de procesos en ejecucion.\n" +
			"- */tasks* o */queue*: Estado de la cola de desarrollo de la workstation.\n" +
			"- */task* o */scratchpad*: Memoria de trabajo activa (ver estado o /task clear).\n" +
			"- */user*: Gestion de contactos (list, add <jid> <nombre>, block <jid>).\n" +
			"- */veto*: Reglas de confidencialidad (list, add <destinatario|*> <regla>, del <id>).\n" +
			"- */voz* o */voice [on|off]*: Control del modo de respuestas en audio manos libres.\n" +
			"- */llm* o */failover*: Estado de alta disponibilidad y conmutacion de modelos LLM.\n" +
			"- */sub <id>* o */subs*: Lista y lectura de reportes tecnicos de subagentes.\n" +
			"- */cancelsub <id>*: Cancelar un subagente activo.\n" +
			"- */git [repo] [cmd]*: Estado, diff o log rapido de repositorios Git.\n" +
			"- */tailscale* o */net*: Topologia y estado de nodos en red Tailscale.\n" +
			"- */backup*: Respaldo atomico inmediato de todas las bases SQLite en data/backups/.\n" +
			"- */logs [rotate]*: Inspecciona los logs de PM2 mas pesados o fuerza su rotacion.\n" +
			"- */clima*: Pronostico del tiempo actual en Buenos Aires.\n" +
			"- */clean* o */purgar*: Limpieza inmediata de descargas antiguas y rotacion de logs.\n" +
			"- */sesion* o */historial*: Metricas de la sesion persistente en SQLite.\n" +
			"- */clear* o */reiniciar*: Limpiar la memoria de sesion en SQLite.\n" +
			"- */index* o */rag*: Metricas del indice semantico local y fragmentos indexados.\n" +
			"- */reindex*: Forzar reindexacion completa de los archivos de memoria.\n" +
			"- */help*: Esta lista de comandos."
		return true, helpMsg
	}

	// Seguridad: Cualquier otro comando administrativo requiere privilegios de Administrador
	if !isAdmin {
		return true, "*Acceso denegado:* Este comando administrativo esta reservado exclusivamente para el Administrador."
	}

	switch cmd {

	case "/backup", "/respaldo":
		if c.scheduler == nil {
			return true, "Error: Scheduler no inicializado."
		}
		count, err := c.scheduler.BackupDatabases(7)
		if err != nil {
			return true, fmt.Sprintf("Error ejecutando respaldo atomico: %v", err)
		}
		backupsDir := filepath.Join(c.cfg.DataDir, "backups")
		entries, _ := os.ReadDir(backupsDir)
		var filesList []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".db") {
				info, err := e.Info()
				if err == nil {
					filesList = append(filesList, fmt.Sprintf("- `%s` (%.2f MB)", e.Name(), float64(info.Size())/(1024*1024)))
				}
			}
		}
		listStr := strings.Join(filesList, "\n")
		return true, fmt.Sprintf("*RESPALDO ATOMICO SQLITE EXITOSO*\n- Bases respaldadas hoy: %d\n- Directorio: `%s`\n\n*Respaldos disponibles en disco:*\n%s", count, backupsDir, listStr)

	case "/logs":
		rotate := len(parts) > 1 && (parts[1] == "rotate" || parts[1] == "rotar" || parts[1] == "limpiar")
		if rotate {
			if c.scheduler == nil {
				return true, "Error: Scheduler no inicializado."
			}
			rotated, err := c.scheduler.RotateLargeLogs(50 * 1024 * 1024)
			if err != nil {
				return true, fmt.Sprintf("Error rotando logs: %v", err)
			}
			return true, fmt.Sprintf("*Rotacion completada:* %d archivo(s) de logs truncados a las ultimas 1.000 lineas.", rotated)
		}

		pm2LogsDir := filepath.Join(os.Getenv("HOME"), ".pm2", "logs")
		entries, err := os.ReadDir(pm2LogsDir)
		if err != nil {
			return true, fmt.Sprintf("Error leyendo logs de PM2 (%s): %v", pm2LogsDir, err)
		}

		type logFile struct {
			name string
			size int64
		}
		var files []logFile
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
				info, err := e.Info()
				if err == nil {
					files = append(files, logFile{name: e.Name(), size: info.Size()})
				}
			}
		}
		sort.Slice(files, func(i, j int) bool {
			return files[i].size > files[j].size
		})

		var b strings.Builder
		b.WriteString(fmt.Sprintf("*LOGS DE PM2 EN DISCO* (`%s`)\n", pm2LogsDir))
		maxShow := 8
		if len(files) < maxShow {
			maxShow = len(files)
		}
		for i := 0; i < maxShow; i++ {
			mb := float64(files[i].size) / (1024 * 1024)
			b.WriteString(fmt.Sprintf("- `%s`: *%.2f MB*\n", files[i].name, mb))
		}
		b.WriteString("\nUsa `/logs rotate` para forzar la rotacion de archivos mayores a 50MB.")
		return true, b.String()

	case "/clima":
		httpClient := http.Client{Timeout: 4 * time.Second}
		resp, err := httpClient.Get("https://wttr.in/Buenos_Aires?format=3")
		if err != nil {
			return true, "Error temporal consultando el pronostico del clima."
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return true, fmt.Sprintf("*PRONOSTICO METEOROLOGICO*\n%s", strings.TrimSpace(string(data)))

	case "/clean", "/purgar", "/prune":
		if c.scheduler == nil {
			return true, "Error: Scheduler no inicializado."
		}
		deleted, err := c.scheduler.CleanOldDownloads(1)
		if err != nil {
			return true, fmt.Sprintf("Error purgando descargas: %v", err)
		}
		rotated, errRot := c.scheduler.RotateLargeLogs(50 * 1024 * 1024)
		if errRot != nil {
			return true, fmt.Sprintf("Error rotando logs: %v", errRot)
		}
		return true, fmt.Sprintf("*Mantenimiento y limpieza completados:*\n- Archivos temporales y descargas purgados: %d\n- Logs pesados de PM2 truncados (>50MB): %d", deleted, rotated)

	case "/heal", "/curar":
		target := ""
		if len(parts) > 1 {
			target = parts[1]
		}
		eng := healer.NewEngine()
		healCtx, healCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer healCancel()
		rep, err := eng.Heal(healCtx, target)
		if err != nil {
			return true, fmt.Sprintf("Error ejecutando auto-curacion: %v", err)
		}
		return true, rep.FormatMarkdown()

	case "/snapshot":
		dbFile := filepath.Join(c.cfg.DataDir, "sessions.db")
		sm, err := healer.NewSnapshotManager(dbFile)
		if err != nil {
			return true, fmt.Sprintf("Error inicializando gestor de snapshots: %v", err)
		}
		defer sm.Close()

		snapCtx, snapCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer snapCancel()

		snap, err := sm.TakeSnapshot(snapCtx)
		if err != nil {
			return true, fmt.Sprintf("Error capturando snapshot: %v", err)
		}

		info := fmt.Sprintf("*SNAPSHOT DE INFRAESTRUCTURA REGISTRADO*\n"+
			"- ID: #%d | Fecha: %s\n"+
			"- Puertos TCP en escucha: %d\n"+
			"- Servicios PM2 activos: %d\n"+
			"- Contenedores Docker: %d\n"+
			"- Memoria RAM: %d MB usados / %d MB libres (Total: %d MB)\n"+
			"- Almacenamiento SSD libre: %s (Total: %s)\n"+
			"- Persistencia: guardado exitosamente en sessions.db",
			snap.ID, snap.Timestamp.Format("02/01/2006 15:04:05"),
			len(snap.TCPPorts), len(snap.PM2Services), len(snap.DockerContainers),
			snap.Metrics.RAMUsedMB, snap.Metrics.RAMFreeMB, snap.Metrics.RAMTotalMB,
			snap.Metrics.DiskFree, snap.Metrics.DiskTotal)
		return true, info

	case "/diff":
		dbFile := filepath.Join(c.cfg.DataDir, "sessions.db")
		sm, err := healer.NewSnapshotManager(dbFile)
		if err != nil {
			return true, fmt.Sprintf("Error inicializando gestor de snapshots: %v", err)
		}
		defer sm.Close()

		diffCtx, diffCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer diffCancel()

		latest, err := sm.GetLatestSnapshot(diffCtx)
		if err != nil {
			return true, fmt.Sprintf("Error consultando ultimo snapshot: %v", err)
		}

		if latest == nil {
			newSnap, err := sm.TakeSnapshot(diffCtx)
			if err != nil {
				return true, fmt.Sprintf("Error generando snapshot inicial: %v", err)
			}
			return true, fmt.Sprintf("*Primer snapshot registrado (#%d).* Ejecuta `/diff` mas tarde para observar variaciones.", newSnap.ID)
		}

		current := &healer.Snapshot{
			ID:               latest.ID + 1,
			Timestamp:        time.Now(),
			TCPPorts:         healer.CollectTCPPorts(diffCtx),
			PM2Services:      healer.CollectPM2Services(diffCtx),
			DockerContainers: healer.CollectDockerContainers(diffCtx),
			Metrics:          healer.CollectSystemMetrics(diffCtx),
		}

		return true, healer.CompareSnapshots(latest, current)

	case "/mc":
		return true, handleMinecraftCommand(parts)

	case "/pc":
		return true, c.handleWorkstationCommand(parts, text)

	case "/services", "/servicios":
		return true, c.getMonitoredServicesStatus()

	case "/config", "/setup":
		return true, c.handleConfigCommand(parts)

	case "/voz", "/voice":
		action := ""
		if len(parts) > 1 {
			action = strings.ToLower(parts[1])
		}
		switch action {
		case "on", "activar", "si", "1":
			c.SetVoiceMode(chatJID, true)
			return true, "*Modo Manos Libres Activado*\nFélix respondera con notas de voz nativas en audio rioplatense (Mateo Neural) a todos tus mensajes en este chat. Usa `/voz off` para volver a texto."
		case "off", "desactivar", "no", "0":
			c.SetVoiceMode(chatJID, false)
			return true, "*Modo Manos Libres Desactivado*\nFélix respondera en texto plano por defecto (salvo cuando le envies una nota de voz directa)."
		default:
			active := c.IsVoiceMode(chatJID)
			state := "DESACTIVADO (solo responde en audio si le envias una nota de voz)"
			if active {
				state = "ACTIVADO (responde siempre en audio)"
			}
			return true, fmt.Sprintf("*Estado Modo Voz:* %s\n- */voz on*: Activar respuestas de voz fijas.\n- */voz off*: Desactivar.", state)
		}

	case "/llm", "/failover":
		st := c.engine.GetLLMStatus()
		priStatus := "ONLINE"
		if !st.PrimaryHealthy {
			priStatus = "DEGRADADO / REINTENTANDO"
		}
		lastFail := "Ninguna"
		if !st.LastFailureTime.IsZero() {
			lastFail = fmt.Sprintf("%s (%s)", st.LastFailureTime.Format("02/01 15:04:05"), st.LastError)
		}

		if st.FallbackEndpoint == "" {
			info := fmt.Sprintf("*ESTADO DE INFERENCIA LLM*\n"+
				"- *Proveedor Exclusivo:* Antigravity Tools (%s)\n"+
				"  - Endpoint: %s\n"+
				"  - Estado: *%s*\n"+
				"  - Llamadas exitosas: %d\n"+
				"  - Fallas consecutivas: %d\n"+
				"- *Consumo de Tokens:* %d prompt | %d completion (Ultimo: %d in / %d out)\n"+
				"- *Ultima falla:* %s\n"+
				"- *Politica de Fallover:* Desactivada. La inferencia depende exclusivamente del proxy local Antigravity Tools (Groq reservado unicamente para transcripcion Whisper).\n"+
				"- *Reintentos:* Hasta 3 reintentos automaticos con espera progresiva sin conmutar de modelo.",
				st.PrimaryModel, st.PrimaryEndpoint, priStatus, st.TotalPrimaryCalls, st.ConsecutiveFailures,
				st.TotalPromptTokens, st.TotalCompletionTokens, st.LastPromptTokens, st.LastCompletionTokens, lastFail)
			return true, info
		}

		fbStatus := "STANDBY"
		if st.FallbackActive {
			fbStatus = "ACTIVO (Enrutando trafico)"
		}

		info := fmt.Sprintf("*ALTA DISPONIBILIDAD LLM (FAILOVER AUTOMATICO)*\n"+
			"- *Proveedor Primario:* %s\n"+
			"  - Endpoint: %s\n"+
			"  - Estado: *%s*\n"+
			"  - Llamadas exitosas: %d\n"+
			"  - Fallas consecutivas: %d\n"+
			"- *Proveedor de Respaldo:* Groq (%s)\n"+
			"  - Endpoint: %s\n"+
			"  - Estado: *%s*\n"+
			"  - Llamadas procesadas: %d\n"+
			"- *Consumo de Tokens:* %d prompt | %d completion (Ultimo: %d in / %d out)\n"+
			"- *Ultima falla:* %s\n"+
			"- *Mecanismo:* Circuit breaker con cooldown de 30s, sonda automatica de recuperacion y failover <1s ante HTTP 429/5xx o timeouts.",
			st.PrimaryModel, st.PrimaryEndpoint, priStatus, st.TotalPrimaryCalls, st.ConsecutiveFailures,
			st.FallbackModel, st.FallbackEndpoint, fbStatus, st.TotalFallbackCalls,
			st.TotalPromptTokens, st.TotalCompletionTokens, st.LastPromptTokens, st.LastCompletionTokens, lastFail)
		return true, info

	case "/clear", "/reiniciar":
		_ = c.engine.ClearHistory(chatJID)
		return true, "*Memoria de sesion reiniciada.* El historial persistente en SQLite fue limpiado exitosamente."

	case "/sesion", "/historial":
		count, _ := c.engine.GetHistoryCount(chatJID)
		summary, _ := c.engine.GetSessionSummary(chatJID)
		st := c.engine.GetLLMStatus()
		dbFile := filepath.Join(c.cfg.DataDir, "sessions.db")

		summaryStatus := "Sin resumen previo (mensajes en buffer reciente)"
		if summary != "" {
			summaryStatus = fmt.Sprintf("Activo (%d caracteres):\n%s", len(summary), summary)
		}

		info := fmt.Sprintf("*ESTADO DE SESION PERSISTENTE EN SQLITE*\n"+
			"- Chat JID: %s\n"+
			"- Mensajes activos en ventana: %d\n"+
			"- Tokens globales: %d prompt | %d completion (Ultimo: %d in / %d out)\n"+
			"- Base de datos: %s\n"+
			"- Compactacion semantica: %s\n"+
			"- Modo: SQLite WAL (indestructible ante reinicios de PM2 o caidas)",
			chatJID, count, st.TotalPromptTokens, st.TotalCompletionTokens, st.LastPromptTokens, st.LastCompletionTokens, dbFile, summaryStatus)
		return true, info

	case "/status", "/estado":
		return true, getSystemStatus()

	case "/pm2":
		return true, getPM2Status()

	case "/tasks", "/queue":
		return true, getQueueStatus()

	case "/task", "/scratchpad":
		if c.scratchpad == nil {
			return true, "*Memoria de trabajo (Scratchpad) no inicializada.*"
		}
		if len(parts) > 1 && (parts[1] == "clear" || parts[1] == "limpiar") {
			_ = c.scratchpad.Clear(chatJID)
			return true, "*Scratchpad de la sesion limpiado exitosamente.*"
		}
		item, err := c.scratchpad.Get(chatJID)
		if err != nil {
			return true, fmt.Sprintf("Error consultando scratchpad: %v", err)
		}
		if item == nil || item.Goal == "" {
			return true, "*MEMORIA DE TRABAJO (SCRATCHPAD)*\nNo hay ninguna tarea activa en esta sesion."
		}
		var sb strings.Builder
		sb.WriteString("*MEMORIA DE TRABAJO ACTIVA (SCRATCHPAD)*\n")
		sb.WriteString(fmt.Sprintf("- *Objetivo:* %s\n- *Estado:* %s\n- *Actualizado:* %s\n",
			item.Goal, item.Status, item.UpdatedAt.Format("15:04:05")))
		if len(item.Variables) > 0 {
			sb.WriteString("- *Variables:*\n")
			for k, v := range item.Variables {
				sb.WriteString(fmt.Sprintf("  * `%s`: `%s`\n", k, v))
			}
		}
		return true, strings.TrimSpace(sb.String())

	case "/index", "/rag":
		if c.memStore == nil || c.memStore.GetIndexer() == nil {
			return true, "*Indice RAG no disponible.*"
		}
		total, lastSync, err := c.memStore.GetIndexer().GetStats()
		if err != nil {
			return true, fmt.Sprintf("Error consultando indice: %v", err)
		}
		syncStr := "Nunca"
		if !lastSync.IsZero() {
			syncStr = lastSync.Format("02/01/2006 15:04:05")
		}
		info := fmt.Sprintf("*ESTADO DEL INDICE SEMANTICO LOCAL (RAG)*\n"+
			"- Total de fragmentos indexados: %d\n"+
			"- Base de datos: %s\n"+
			"- Ultima sincronizacion: %s\n"+
			"- Motor: FTS5 + BM25 + Similitud Coseno Vectorial\n"+
			"- Archivos indexados: USER.md, MEMORY.md, SOUL.md, PROYECTOS_E_IDEAS.md y bitacoras de memory/",
			total, filepath.Join(c.cfg.DataDir, "memory_index.db"), syncStr)
		return true, info

	case "/reindex":
		if c.memStore == nil || c.memStore.GetIndexer() == nil {
			return true, "*Indice RAG no disponible.*"
		}
		count, err := c.memStore.GetIndexer().Sync()
		if err != nil {
			return true, fmt.Sprintf("Error reindexando memoria: %v", err)
		}
		return true, fmt.Sprintf("*Reindexacion completada exitosamente.*\n- Total de fragmentos indexados: %d en %s", count, filepath.Join(c.cfg.DataDir, "memory_index.db"))

	case "/sub", "/subs", "/subagents":
		if c.subagentMgr == nil {
			return true, "*Administrador de subagentes no inicializado.*"
		}
		if len(parts) > 1 {
			targetID := parts[1]
			if targetID == "report" || targetID == "informe" {
				if len(parts) > 2 {
					targetID = parts[2]
				} else {
					return true, "Uso: `/subs report <task_id>` o `/sub <task_id>`"
				}
			}
			rep, err := c.subagentMgr.GetTaskReport(targetID)
			if err != nil {
				return true, fmt.Sprintf("Error obteniendo reporte de %s: %v", targetID, err)
			}
			return true, rep
		}

		tasks, err := c.subagentMgr.List(10)
		if err != nil {
			return true, fmt.Sprintf("Error consultando subagentes: %v", err)
		}
		if len(tasks) == 0 {
			return true, "*SUBAGENTES AUTONOMOS*\nNo hay tareas registradas en disco."
		}
		var sb strings.Builder
		sb.WriteString("*SUBAGENTES AUTONOMOS CONCURRENTES (Goroutines + SQLite)*\n")
		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("- [%s] *%s* (ID: `%s` | Rol: `%s`)\n  Objetivo: %s\n  Duracion: %s\n",
				strings.ToUpper(t.Status), t.Title, t.ID, t.Role, t.Objective, t.Duration))
		}
		sb.WriteString("\nUsa `/sub <id>` para ver el reporte tecnico completo.")
		return true, strings.TrimSpace(sb.String())

	case "/git":
		action := "status"
		repo := c.cfg.WorkspaceDir
		if len(parts) > 1 {
			p1 := parts[1]
			if p1 == "status" || p1 == "diff" || p1 == "log" || p1 == "branch" || p1 == "show" {
				action = p1
			} else {
				repo = p1
				if len(parts) > 2 {
					action = parts[2]
				}
			}
		}
		tool := &tools.GitTool{DefaultRepoDir: repo}
		rawArgs, _ := json.Marshal(map[string]any{
			"action":    action,
			"repo_path": repo,
		})
		out, err := tool.Execute(context.Background(), rawArgs)
		if err != nil {
			return true, fmt.Sprintf("Error en git %s: %v\n%s", action, err, out)
		}
		return true, out

	case "/tailscale", "/net":
		tool := &tools.TailscaleTool{}
		rawArgs, _ := json.Marshal(map[string]any{
			"action": "status",
		})
		out, err := tool.Execute(context.Background(), rawArgs)
		if err != nil {
			return true, fmt.Sprintf("Error consultando Tailscale: %v\n%s", err, out)
		}
		return true, out

	case "/user", "/usuarios":
		if c.userMgr == nil {
			return true, "*Administrador de usuarios no disponible.*"
		}
		if len(parts) < 2 || parts[1] == "list" {
			list, err := c.userMgr.ListUsers()
			if err != nil {
				return true, fmt.Sprintf("Error listando usuarios: %v", err)
			}
			if len(list) == 0 {
				return true, "*SISTEMA DE USUARIOS*\nNo hay usuarios ni contactos registrados."
			}
			var sb strings.Builder
			sb.WriteString("*SISTEMA DE USUARIOS Y ROLES (ACL)*\n")
			for _, u := range list {
				sb.WriteString(fmt.Sprintf("- [%s] *%s* (`%s`)\n", strings.ToUpper(string(u.Role)), u.Name, u.JID))
			}
			return true, strings.TrimSpace(sb.String())
		}
		subCmd := strings.ToLower(parts[1])
		switch subCmd {
		case "add":
			if len(parts) < 3 {
				return true, "Uso: `/user add <jid_o_telefono> [nombre]`"
			}
			targetJID := parts[2]
			name := targetJID
			if len(parts) > 3 {
				name = strings.Join(parts[3:], " ")
			}
			if err := c.userMgr.AddOrUpdateUser(targetJID, name, users.RoleContact); err != nil {
				return true, fmt.Sprintf("Error agregando contacto: %v", err)
			}
			return true, fmt.Sprintf("*Contacto registrado exitosamente.*\n- Nombre: *%s*\n- JID: `%s`\n- Rol: *CONTACT*", name, targetJID)

		case "block":
			if len(parts) < 3 {
				return true, "Uso: `/user block <jid_o_telefono>`"
			}
			targetJID := parts[2]
			if err := c.userMgr.AddOrUpdateUser(targetJID, targetJID, users.RoleBlocked); err != nil {
				return true, fmt.Sprintf("Error bloqueando usuario: %v", err)
			}
			return true, fmt.Sprintf("*Usuario `%s` bloqueado exitosamente.*", targetJID)

		default:
			return true, "Subcomandos disponibles para `/user`: `list`, `add <jid> <nombre>`, `block <jid>`."
		}

	case "/veto", "/confidencialidad":
		if c.userMgr == nil {
			return true, "*Administrador de usuarios y confidencialidad no disponible.*"
		}
		if len(parts) < 2 || parts[1] == "list" {
			rules, err := c.userMgr.ListVetoRules()
			if err != nil {
				return true, fmt.Sprintf("Error listando reglas: %v", err)
			}
			if len(rules) == 0 {
				return true, "*REGLAS DE CONFIDENCIALIDAD Y VETO*\nNo hay reglas activas en el sistema."
			}
			var sb strings.Builder
			sb.WriteString("*DIRECTIVAS DE VETO Y CONFIDENCIALIDAD ACTIVAS*\n")
			for _, r := range rules {
				sb.WriteString(fmt.Sprintf("- [#%d] Para *%s*: %s\n", r.ID, r.TargetContact, r.Rule))
			}
			return true, strings.TrimSpace(sb.String())
		}
		subCmd := strings.ToLower(parts[1])
		switch subCmd {
		case "add":
			if len(parts) < 4 {
				return true, "Uso: `/veto add <contacto|*> <regla de confidencialidad>`\nEj: `/veto add pablo no contar sobre proyectos del servidor`"
			}
			target := parts[2]
			rule := strings.Join(parts[3:], " ")
			if err := c.userMgr.AddVetoRule(target, rule); err != nil {
				return true, fmt.Sprintf("Error registrando regla: %v", err)
			}
			return true, fmt.Sprintf("*Regla de confidencialidad registrada.*\n- Para: *%s*\n- Directiva: \"%s\"", target, rule)

		case "del", "remove", "rm":
			if len(parts) < 3 {
				return true, "Uso: `/veto del <id>`"
			}
			var id int64
			fmt.Sscanf(parts[2], "%d", &id)
			if id <= 0 {
				return true, "ID de regla invalido."
			}
			if err := c.userMgr.DeleteVetoRule(id); err != nil {
				return true, fmt.Sprintf("Error eliminando regla: %v", err)
			}
			return true, fmt.Sprintf("*Regla de confidencialidad #%d eliminada.*", id)

		default:
			return true, "Subcomandos disponibles para `/veto`: `list`, `add <contacto|*> <regla>`, `del <id>`."
		}

	case "/cancelsub":
		if len(parts) < 2 {
			return true, "Uso: `/cancelsub <id>` (ej: `/cancelsub sub_a1b2c3d4`)"
		}
		if c.subagentMgr == nil {
			return true, "*Administrador de subagentes no inicializado.*"
		}
		targetID := parts[1]
		if err := c.subagentMgr.Cancel(targetID); err != nil {
			return true, fmt.Sprintf("Error cancelando subagente %s: %v", targetID, err)
		}
		return true, fmt.Sprintf("*Subagente %s cancelado exitosamente.*", targetID)
	}

	return false, ""
}

func handleMinecraftCommand(parts []string) string {
	sub := ""
	if len(parts) > 1 {
		sub = strings.ToLower(parts[1])
	}

	switch sub {
	case "", "status":
		return getMinecraftStatus()

	case "list":
		client := minecraft.DefaultClient()
		out, err := client.ListPlayers()
		if err != nil {
			return fmt.Sprintf("*MINECRAFT (RCON Error)*\n%v", err)
		}
		return fmt.Sprintf("*JUGADORES ONLINE (MINECRAFT)*\n%s", out)

	case "whitelist", "wl":
		client := minecraft.DefaultClient()
		wlSub := ""
		if len(parts) > 2 {
			wlSub = strings.ToLower(parts[2])
		}
		switch wlSub {
		case "add":
			if len(parts) < 4 {
				return "Uso: `/mc whitelist add <usuario>`"
			}
			user := parts[3]
			out, err := client.WhitelistAdd(user)
			if err != nil {
				return fmt.Sprintf("Error agregando a whitelist: %v", err)
			}
			return fmt.Sprintf("*WHITELIST MINECRAFT*\n%s", out)
		case "remove", "del", "rm":
			if len(parts) < 4 {
				return "Uso: `/mc whitelist remove <usuario>`"
			}
			user := parts[3]
			out, err := client.WhitelistRemove(user)
			if err != nil {
				return fmt.Sprintf("Error quitando de whitelist: %v", err)
			}
			return fmt.Sprintf("*WHITELIST MINECRAFT*\n%s", out)
		case "reload":
			out, err := client.WhitelistReload()
			if err != nil {
				return fmt.Sprintf("Error recargando whitelist: %v", err)
			}
			return fmt.Sprintf("*WHITELIST MINECRAFT*\n%s", out)
		default:
			out, err := client.WhitelistList()
			if err != nil {
				return fmt.Sprintf("Error consultando whitelist: %v", err)
			}
			return fmt.Sprintf("*WHITELIST MINECRAFT*\n%s", out)
		}

	case "say":
		if len(parts) < 3 {
			return "Uso: `/mc say <mensaje>`"
		}
		msg := strings.Join(parts[2:], " ")
		if strings.TrimSpace(msg) == "" {
			return "Uso: `/mc say <mensaje>`"
		}
		client := minecraft.DefaultClient()
		out, err := client.Say(msg)
		if err != nil {
			return fmt.Sprintf("Error transmitiendo mensaje: %v", err)
		}
		if out == "" {
			out = fmt.Sprintf("Mensaje transmitido al servidor: %s", msg)
		}
		return fmt.Sprintf("*DIFUSION MINECRAFT*\n%s", out)

	case "kick":
		if len(parts) < 3 {
			return "Uso: `/mc kick <usuario>`"
		}
		user := parts[2]
		client := minecraft.DefaultClient()
		out, err := client.Kick(user)
		if err != nil {
			return fmt.Sprintf("Error expulsando jugador: %v", err)
		}
		return fmt.Sprintf("*EXPULSION MINECRAFT*\n%s", out)

	default:
		rawCmd := strings.Join(parts[1:], " ")
		client := minecraft.DefaultClient()
		out, err := client.Execute(rawCmd)
		if err != nil {
			return fmt.Sprintf("Error ejecutando comando RCON: %v", err)
		}
		return fmt.Sprintf("*COMANDO MINECRAFT*\n%s", out)
	}
}

func getMinecraftStatus() string {
	pm2Status := "DESCONOCIDO"
	pm2PID := 0
	pm2MemMB := 0.0
	pm2Restarts := 0
	pm2UpStr := "N/D"

	if out, err := exec.Command("pm2", "jlist").Output(); err == nil {
		type pm2Monit struct {
			Memory float64 `json:"memory"`
			CPU    float64 `json:"cpu"`
		}
		type pm2Env struct {
			Status      string `json:"status"`
			Uptime      int64  `json:"pm_uptime"`
			RestartTime int    `json:"restart_time"`
		}
		type pm2Item struct {
			Name   string   `json:"name"`
			PID    int      `json:"pid"`
			Monit  pm2Monit `json:"monit"`
			PM2Env pm2Env   `json:"pm2_env"`
		}
		var items []pm2Item
		if err := json.Unmarshal(out, &items); err == nil {
			for _, it := range items {
				if it.Name == "minecraft" {
					pm2Status = strings.ToUpper(it.PM2Env.Status)
					pm2PID = it.PID
					pm2MemMB = it.Monit.Memory / (1024 * 1024)
					pm2Restarts = it.PM2Env.RestartTime
					if it.PM2Env.Uptime > 0 {
						diff := time.Since(time.UnixMilli(it.PM2Env.Uptime))
						if diff.Hours() >= 24 {
							pm2UpStr = fmt.Sprintf("%.0fd", diff.Hours()/24)
						} else if diff.Hours() >= 1 {
							pm2UpStr = fmt.Sprintf("%.0fh", diff.Hours())
						} else {
							pm2UpStr = fmt.Sprintf("%.0fm", diff.Minutes())
						}
					}
					break
				}
			}
		}
	}

	client := minecraft.DefaultClient()
	rconStatus := "Online"
	playersOut, err := client.ListPlayers()
	if err != nil {
		rconStatus = "Desconectado / En reposo (lazymc)"
		playersOut = fmt.Sprintf("No se pudo conectar a RCON: %v", err)
	}

	return fmt.Sprintf("*ESTADO DEL SERVIDOR MINECRAFT*\n"+
		"- Proceso PM2: *%s* | PID: %d | RAM: %.1f MB | Up: %s | ↺ %d\n"+
		"- Enlace RCON: *%s*\n"+
		"- Jugadores online: %s",
		pm2Status, pm2PID, pm2MemMB, pm2UpStr, pm2Restarts, rconStatus, playersOut)
}

func (c *Client) handleWorkstationCommand(parts []string, rawText string) string {
	if c.cfg.WorkstationHost == "" {
		return "*Workstation remota no configurada.*\nPuedes configurarla con `/config workstation <usuario@ip>`."
	}

	sub := ""
	if len(parts) > 1 {
		sub = strings.ToLower(parts[1])
	}

	switch sub {
	case "", "status", "estado":
		return c.getWorkstationStatus()

	case "clip", "copy", "portapapeles":
		var textToCopy string
		idx := strings.Index(strings.ToLower(rawText), sub)
		if idx != -1 {
			textToCopy = strings.TrimSpace(rawText[idx+len(sub):])
		}
		if textToCopy == "" {
			return "Uso: `/pc clip <texto>` o `/pc copy <texto>` para copiar al portapapeles remoto."
		}

		escapedText := strings.ReplaceAll(textToCopy, "'", "'\\''")
		clipCmd := fmt.Sprintf("printf '%%s' '%s' | (wl-copy 2>/dev/null || xclip -selection clipboard 2>/dev/null)", escapedText)

		pcTaskBin := "pc-task"
		if p, err := exec.LookPath("pc-task"); err == nil {
			pcTaskBin = p
		}
		out, err := exec.Command(pcTaskBin, "add", "Copiar al portapapeles", clipCmd).CombinedOutput()
		if err != nil {
			return fmt.Sprintf("Error encolando tarea en workstation-queue: %v (%s)", err, string(out))
		}

		resp := strings.TrimSpace(string(out))
		return fmt.Sprintf("*PORTAPAPELES WORKSTATION*\n"+
			"Texto encolado exitosamente para copiar al portapapeles remoto.\n"+
			"- Longitud: %d caracteres\n"+
			"- Tarea workstation-queue: %s", len(textToCopy), resp)

	default:
		return "Comando de /pc no reconocido. Opciones validas:\n- `/pc` o `/pc status`: Telemetria de la workstation.\n- `/pc clip <texto>`: Copiar texto al portapapeles."
	}
}

func (c *Client) getWorkstationStatus() string {
	if c.cfg.WorkstationHost == "" {
		return "*Workstation remota no configurada.*\nPuedes configurarla con `/config workstation <usuario@ip>`."
	}
	wsHost := c.cfg.WorkstationHost

	sshStatus := "DESCONECTADO / SUSPENDIDO"
	sshDetail := "El equipo remoto no responde a verificacion SSH."
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	sshCmd := exec.CommandContext(ctx, "ssh", "-o", "ConnectTimeout=2", "-o", "BatchMode=yes", wsHost,
		"uptime; upower -i $(upower -e | grep BAT | head -n1) 2>/dev/null | grep -E 'state|percentage|time to empty'")
	if out, err := sshCmd.CombinedOutput(); err == nil && len(out) > 0 {
		sshStatus = "ONLINE (SSH Conectado)"
		sshDetail = strings.TrimSpace(string(out))
	}

	queueStr := "No hay tareas pendientes en la cola."
	pcTaskBin := "pc-task"
	if p, err := exec.LookPath("pc-task"); err == nil {
		pcTaskBin = p
	}
	if out, err := exec.Command(pcTaskBin, "list").CombinedOutput(); err == nil {
		lines := strings.Split(string(out), "\n")
		var pending []string
		for _, l := range lines {
			if strings.Contains(l, "pending") || strings.Contains(l, "running") {
				pending = append(pending, strings.TrimSpace(l))
			}
		}
		if len(pending) > 0 {
			queueStr = fmt.Sprintf("%d tarea(s) activa(s):\n  - %s", len(pending), strings.Join(pending, "\n  - "))
		}
	}

	return fmt.Sprintf("*TELEMETRIA WORKSTATION REMOTA*\n"+
		"- Host configurado: `%s`\n"+
		"- Enlace SSH: *%s*\n"+
		"  %s\n"+
		"- Tareas activas en cola:\n  %s",
		wsHost, sshStatus, strings.ReplaceAll(sshDetail, "\n", "\n  "), queueStr)
}

func (c *Client) getMonitoredServicesStatus() string {
	if len(c.cfg.MonitoredServices) == 0 {
		return "*SERVICIOS MONITOREADOS*\nNo hay servicios configurados.\nPuedes agregar servicios con `/config service add <nombre>`."
	}

	var sb strings.Builder
	sb.WriteString("*ESTADO DE SERVICIOS MONITOREADOS*\n")
	for _, svc := range c.cfg.MonitoredServices {
		status := "OFFLINE / INACTIVO"
		if out, err := exec.Command("pm2", "describe", svc).Output(); err == nil && len(out) > 0 {
			if strings.Contains(string(out), "online") {
				status = "ONLINE (PM2)"
			} else {
				status = "DETENIDO (PM2)"
			}
		} else if outSys, errSys := exec.Command("systemctl", "is-active", svc).Output(); errSys == nil {
			status = strings.ToUpper(strings.TrimSpace(string(outSys))) + " (Systemd)"
		} else if outDoc, errDoc := exec.Command("docker", "inspect", "-f", "{{.State.Status}}", svc).Output(); errDoc == nil {
			status = strings.ToUpper(strings.TrimSpace(string(outDoc))) + " (Docker)"
		}
		sb.WriteString(fmt.Sprintf("- *%s*: %s\n", svc, status))
	}
	return strings.TrimSpace(sb.String())
}

func (c *Client) handleConfigCommand(parts []string) string {
	subcmd := ""
	if len(parts) > 1 {
		subcmd = strings.ToLower(parts[1])
	}

	switch subcmd {
	case "apibase", "endpoint", "url":
		if len(parts) < 3 {
			return "*Uso:* `/config apibase <url>`\nEjemplos:\n- `/config apibase http://127.0.0.1:8045/v1`\n- `/config apibase https://api.openai.com/v1`\n- `/config apibase https://generativelanguage.googleapis.com/v1beta`\n- `/config apibase https://api.groq.com/openai/v1`\n- `/config apibase http://localhost:11434/v1`"
		}
		newBase := strings.TrimSpace(parts[2])
		c.cfg.APIBase = newBase
		_ = c.cfg.Save("config.json")
		return fmt.Sprintf("*API Base actualizada con exito:*\n`%s`\n(Persistido en config.json)", newBase)

	case "apikey", "key":
		if len(parts) < 3 {
			return "*Uso:* `/config apikey <tu-clave>`"
		}
		newKey := strings.TrimSpace(parts[2])
		c.cfg.APIKey = newKey
		_ = c.cfg.Save("config.json")
		masked := newKey
		if len(masked) > 8 {
			masked = masked[:4] + "..." + masked[len(masked)-4:]
		}
		return fmt.Sprintf("*Clave de API guardada con exito:*\n`%s`\n(Inferencia activada y persistida en config.json)", masked)

	case "model", "modelo":
		if len(parts) < 3 {
			return "*Uso:* `/config model <nombre-modelo>`\nEjemplos:\n- `/config model gemini-2.5-flash`\n- `/config model gpt-4o`\n- `/config model llama-3.3-70b-versatile`"
		}
		newModel := strings.TrimSpace(parts[2])
		c.cfg.Model = newModel
		_ = c.cfg.Save("config.json")
		return fmt.Sprintf("*Modelo LLM actualizado con exito:*\n`%s`", newModel)

	case "groq":
		if len(parts) < 3 {
			return "*Uso:* `/config groq <clave-groq>`"
		}
		newGroq := strings.TrimSpace(parts[2])
		c.cfg.GroqAPIKey = newGroq
		_ = c.cfg.Save("config.json")
		c.whisper = NewWhisperClient(newGroq, c.cfg.OpenAIAPIKey)
		return "*Clave de Groq guardada con exito.*\nTranscripcion Whisper de notas de voz actualizada."

	case "openai":
		if len(parts) < 3 {
			return "*Uso:* `/config openai <clave-openai>`"
		}
		newOpenAI := strings.TrimSpace(parts[2])
		c.cfg.OpenAIAPIKey = newOpenAI
		_ = c.cfg.Save("config.json")
		c.whisper = NewWhisperClient(c.cfg.GroqAPIKey, newOpenAI)
		return "*Clave de OpenAI guardada con exito.*"

	case "workstation":
		if len(parts) < 3 {
			return "*Uso:* `/config workstation <usuario@ip>`\nEj: `/config workstation usuario@192.168.1.50`"
		}
		newWS := strings.TrimSpace(parts[2])
		c.cfg.WorkstationHost = newWS
		_ = c.cfg.Save("config.json")
		return fmt.Sprintf("*Host de Workstation configurado:* `%s`", newWS)

	case "service":
		if len(parts) < 4 {
			return "*Uso:* `/config service add <nombre>` o `/config service remove <nombre>`"
		}
		action := strings.ToLower(parts[2])
		svcName := strings.TrimSpace(parts[3])
		if action == "add" {
			c.cfg.MonitoredServices = append(c.cfg.MonitoredServices, svcName)
			_ = c.cfg.Save("config.json")
			return fmt.Sprintf("*Servicio agregado a monitoreo:* `%s`", svcName)
		} else if action == "remove" || action == "del" {
			var filtered []string
			for _, s := range c.cfg.MonitoredServices {
				if s != svcName {
					filtered = append(filtered, s)
				}
			}
			c.cfg.MonitoredServices = filtered
			_ = c.cfg.Save("config.json")
			return fmt.Sprintf("*Servicio eliminado de monitoreo:* `%s`", svcName)
		}
		return "Accion invalida. Usa 'add' o 'remove'."

	case "admin":
		if len(parts) < 4 || strings.ToLower(parts[2]) != "add" {
			return "*Uso:* `/config admin add <telefono>`\nEj: `/config admin add 5491122334455`"
		}
		newAdminPhone := strings.TrimSpace(parts[3])
		c.cfg.AllowedUsers = append(c.cfg.AllowedUsers, newAdminPhone)
		_ = c.cfg.Save("config.json")
		if c.userMgr != nil {
			_ = c.userMgr.AddOrUpdateUser(newAdminPhone, "Administrador Adicional", users.RoleAdmin)
		}
		return fmt.Sprintf("*Nuevo administrador registrado:* `%s`", newAdminPhone)

	case "status", "":
		keyStatus := "NO CONFIGURADA"
		if c.cfg.APIKey != "" {
			if len(c.cfg.APIKey) > 8 {
				keyStatus = c.cfg.APIKey[:4] + "..." + c.cfg.APIKey[len(c.cfg.APIKey)-4:]
			} else {
				keyStatus = "CONFIGURADA"
			}
		}
		groqStatus := "No configurada (usando fallback si disponible)"
		if c.cfg.GroqAPIKey != "" {
			groqStatus = "CONFIGURADA (Groq Whisper activo)"
		}
		owner := c.cfg.OwnerJID
		if owner == "" {
			owner = "Auto-asignado al primer contacto"
		}
		svcs := "Ninguno (agrega con /config service add <nombre>)"
		if len(c.cfg.MonitoredServices) > 0 {
			svcs = strings.Join(c.cfg.MonitoredServices, ", ")
		}

		return fmt.Sprintf("*ESTADO DE CONFIGURACION DE FELIX*\n\n"+
			"- *Owner / Admin JID:* `%s`\n"+
			"- *API Base (Inferencia):* `%s`\n"+
			"- *API Key:* `%s`\n"+
			"- *Modelo LLM:* `%s`\n"+
			"- *Groq Whisper:* %s\n"+
			"- *Puerto Health / API:* `%s`\n"+
			"- *Workspace:* `%s`\n"+
			"- *Servicios Monitoreados:* %s\n\n"+
			"*Comandos de configuracion:*\n"+
			"- `/config apibase <url>`: Cambiar endpoint de inferencia.\n"+
			"- `/config apikey <clave>`: Establecer API Key.\n"+
			"- `/config model <modelo>`: Cambiar modelo de lenguaje.\n"+
			"- `/config groq <clave>`: Configurar Whisper en Groq.\n"+
			"- `/config service add|del <nombre>`: Gestionar servicios monitoreados.\n"+
			"- `/config admin add <telefono>`: Agregar un administrador.\n"+
			"- `/config voice [on|off]`: Modo respuestas de voz.",
			owner, c.cfg.APIBase, keyStatus, c.cfg.Model, groqStatus, c.cfg.HealthPort, c.cfg.WorkspaceDir, svcs)
	}

	return "Subcomando de /config no reconocido. Ejecuta `/config status` para ver las opciones."
}

func getSystemStatus() string {
	// Temp CPU
	tempStr := "N/D"
	if b, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		t := strings.TrimSpace(string(b))
		var m int
		if _, err := fmt.Sscanf(t, "%d", &m); err == nil {
			tempStr = fmt.Sprintf("%.1f °C", float64(m)/1000.0)
		}
	}

	// RAM
	ramStr := "N/D"
	if out, err := exec.Command("free", "-m").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "Mem:") {
				f := strings.Fields(line)
				if len(f) >= 7 {
					ramStr = fmt.Sprintf("%s MB libres / %s MB total (%s MB usados)", f[6], f[1], f[2])
				}
				break
			}
		}
	}

	// Disco SSD
	ssdStr := "N/D"
	if out, err := exec.Command("df", "-h", "/mnt/ssd").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "/dev/") || strings.Contains(line, "/mnt/ssd") {
				f := strings.Fields(line)
				if len(f) >= 5 {
					ssdStr = fmt.Sprintf("%s libres (%s en uso)", f[3], f[4])
					break
				}
			}
		}
	}

	// Uptime
	uptimeStr := "N/D"
	if out, err := exec.Command("uptime", "-p").Output(); err == nil {
		uptimeStr = strings.TrimSpace(string(out))
	}

	// Tailscale
	tsStr := "Offline"
	if out, err := exec.Command("tailscale", "status").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		tsStr = fmt.Sprintf("Activo (%d nodos conectados)", len(lines))
	}

	res := fmt.Sprintf("*ESTADO GENERAL DEL HOMELAB*\n"+
		"- CPU Temp: %s\n"+
		"- Memoria RAM: %s\n"+
		"- Almacenamiento SSD: %s\n"+
		"- Uptime: %s\n"+
		"- Red Tailscale: %s",
		tempStr, ramStr, ssdStr, uptimeStr, tsStr)
	return res
}

func getPM2Status() string {
	out, err := exec.Command("pm2", "jlist").Output()
	if err != nil {
		return fmt.Sprintf("Error consultando PM2: %v", err)
	}

	type pm2Monit struct {
		Memory float64 `json:"memory"`
		CPU    float64 `json:"cpu"`
	}
	type pm2Env struct {
		Status      string `json:"status"`
		Uptime      int64  `json:"pm_uptime"`
		RestartTime int    `json:"restart_time"`
	}
	type pm2Item struct {
		Name   string   `json:"name"`
		PID    int      `json:"pid"`
		Monit  pm2Monit `json:"monit"`
		PM2Env pm2Env   `json:"pm2_env"`
	}

	var items []pm2Item
	if err := json.Unmarshal(out, &items); err != nil {
		return fmt.Sprintf("Error deserializando lista de PM2: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("*PROCESOS ACTIVOS EN PM2*\n")
	for _, it := range items {
		memMB := it.Monit.Memory / (1024 * 1024)
		upStr := ""
		if it.PM2Env.Uptime > 0 {
			diff := time.Since(time.UnixMilli(it.PM2Env.Uptime))
			if diff.Hours() >= 24 {
				upStr = fmt.Sprintf("%.0fd", diff.Hours()/24)
			} else if diff.Hours() >= 1 {
				upStr = fmt.Sprintf("%.0fh", diff.Hours())
			} else {
				upStr = fmt.Sprintf("%.0fm", diff.Minutes())
			}
		}
		sb.WriteString(fmt.Sprintf("- *%s*: %s | PID: %d | RAM: %.1f MB | ↺ %d | Up: %s\n",
			it.Name, strings.ToUpper(it.PM2Env.Status), it.PID, memMB, it.PM2Env.RestartTime, upStr))
	}

	return strings.TrimRight(sb.String(), "\n")
}

func getQueueStatus() string {
	pcTaskBin := "pc-task"
	if p, err := exec.LookPath("pc-task"); err == nil {
		pcTaskBin = p
	}
	out, err := exec.Command(pcTaskBin, "list").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error consultando cola de tareas: %v (%s)", err, string(out))
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "*COLA DE WORKSTATION*\nNo hay tareas pendientes en la cola."
	}
	return fmt.Sprintf("*COLA DE WORKSTATION*\n%s", s)
}
