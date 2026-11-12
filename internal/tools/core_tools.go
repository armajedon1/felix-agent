package tools

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// ExecTool permite ejecutar comandos shell en el host.
type ExecTool struct {
	DefaultWorkDir string
}

func (t *ExecTool) Name() string { return "exec" }
func (t *ExecTool) Description() string {
	return "Ejecuta un comando en la shell del servidor homelab Debian. Permite inspeccionar procesos, ejecutar scripts, diagnosticos de red y comandos del sistema."
}
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Comando shell a ejecutar.",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Directorio de trabajo opcional.",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Tiempo limite en segundos (por defecto 60).",
			},
		},
		"required": []string{"command"},
	}
}
func (t *ExecTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Command        string `json:"command"`
		WorkDir        string `json:"workdir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	workDir := params.WorkDir
	if workDir == "" {
		workDir = t.DefaultWorkDir
	}
	if workDir == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			workDir = home
		} else {
			workDir = "."
		}
	}

	timeout := 60 * time.Second
	if params.TimeoutSeconds > 0 {
		timeout = time.Duration(params.TimeoutSeconds) * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", params.Command)
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	outStr := stdout.String()
	errStr := stderr.String()

	var result strings.Builder
	if len(outStr) > 0 {
		result.WriteString(outStr)
	}
	if len(errStr) > 0 {
		if result.Len() > 0 {
			result.WriteString("\n--- STDERR ---\n")
		}
		result.WriteString(errStr)
	}

	if err != nil {
		outTruncated := truncateOutput(result.String(), 25000)
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return outTruncated, fmt.Errorf("comando excedio el tiempo limite de %v (procesos hijos detenidos)", timeout)
		}
		return outTruncated, fmt.Errorf("fallo comando (%v): %s", err, errStr)
	}

	res := result.String()
	if strings.TrimSpace(res) == "" {
		return "(comando ejecutado sin salida)", nil
	}
	return truncateOutput(res, 25000), nil
}

func truncateOutput(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	headChars := 10000
	tailChars := 12000
	if headChars+tailChars >= maxChars {
		headChars = (maxChars * 4) / 10
		tailChars = (maxChars * 5) / 10
	}
	omitted := len(s) - (headChars + tailChars)
	return fmt.Sprintf("%s\n\n[... salida truncada: %d caracteres omitidos para proteger contexto ...]\n\n%s", s[:headChars], omitted, s[len(s)-tailChars:])
}

// FileReadTool lee contenido de archivos locales con soporte para rangos de lineas.
type FileReadTool struct{}

func (t *FileReadTool) Name() string { return "file_read" }
func (t *FileReadTool) Description() string {
	return "Lee el contenido de un archivo de texto en el sistema con soporte opcional de inicio de linea (offset) y limite."
}
func (t *FileReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Ruta absoluta o relativa del archivo a leer.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Numero de linea inicial (1-based).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Cantidad maxima de lineas a leer.",
			},
		},
		"required": []string{"path"},
	}
}
func (t *FileReadTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		return "", fmt.Errorf("error leyendo archivo %s: %w", params.Path, err)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	start := 0
	if params.Offset > 1 {
		start = params.Offset - 1
	}
	if start >= totalLines {
		return "", fmt.Errorf("offset %d supera el total de lineas (%d)", params.Offset, totalLines)
	}

	end := totalLines
	if params.Limit > 0 && start+params.Limit < totalLines {
		end = start + params.Limit
	}

	selected := lines[start:end]
	return strings.Join(selected, "\n"), nil
}

// FileWriteTool escribe o sobreescribe archivos en el disco creando carpetas si es necesario.
type FileWriteTool struct{}

func (t *FileWriteTool) Name() string { return "file_write" }
func (t *FileWriteTool) Description() string {
	return "Crea o sobreescribe un archivo en disco con el contenido especificado, creando los directorios padres si no existen."
}
func (t *FileWriteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Ruta del archivo a escribir.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Contenido completo del archivo.",
			},
		},
		"required": []string{"path", "content"},
	}
}
func (t *FileWriteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	dir := filepath.Dir(params.Path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("error creando directorios para %s: %w", params.Path, err)
		}
	}

	if err := os.WriteFile(params.Path, []byte(params.Content), 0644); err != nil {
		return "", fmt.Errorf("error escribiendo archivo %s: %w", params.Path, err)
	}

	return fmt.Sprintf("Archivo guardado exitosamente: %s (%d bytes)", params.Path, len(params.Content)), nil
}

// FileEditTool realiza reemplazos unicos y quirurgicos de texto en archivos.
type FileEditTool struct{}

func (t *FileEditTool) Name() string { return "file_edit" }
func (t *FileEditTool) Description() string {
	return "Reemplaza exactamente una seccion de texto (old_text) por una nueva (new_text) dentro de un archivo existente."
}
func (t *FileEditTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Ruta del archivo a editar.",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "Texto exacto existente que sera reemplazado (debe coincidir de forma unica).",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "Nuevo texto de reemplazo.",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}
func (t *FileEditTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		return "", fmt.Errorf("error leyendo archivo %s: %w", params.Path, err)
	}

	content := string(data)
	count := strings.Count(content, params.OldText)
	if count == 0 {
		return "", fmt.Errorf("el texto 'old_text' no fue encontrado en %s", params.Path)
	}
	if count > 1 {
		return "", fmt.Errorf("el texto 'old_text' aparece %d veces en %s; debe coincidir de forma unica", count, params.Path)
	}

	updated := strings.Replace(content, params.OldText, params.NewText, 1)
	if err := os.WriteFile(params.Path, []byte(updated), 0644); err != nil {
		return "", fmt.Errorf("error guardando edicion en %s: %w", params.Path, err)
	}

	return fmt.Sprintf("Edicion aplicada exitosamente en %s", params.Path), nil
}

// AntigravityTool delega tareas complejas de programacion en Antigravity CLI (agy).
type AntigravityTool struct{}

func (t *AntigravityTool) Name() string { return "antigravity_cli" }
func (t *AntigravityTool) Description() string {
	return "Delega desarrollo, codificacion o refactorizacion en Antigravity CLI (agy) como ejecutor autonomo headless."
}
func (t *AntigravityTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Instruccion detallada de desarrollo a ejecutar por agy.",
			},
			"project_dir": map[string]any{
				"type":        "string",
				"description": "Directorio autocontenido del proyecto donde agy operara.",
			},
			"resume": map[string]any{
				"type":        "boolean",
				"description": "Si es true, reanuda la conversacion anterior (-c / --continue) para mantener memoria del proyecto.",
			},
		},
		"required": []string{"prompt", "project_dir"},
	}
}
func (t *AntigravityTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Prompt     string `json:"prompt"`
		ProjectDir string `json:"project_dir"`
		Resume     bool   `json:"resume"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	cmdArgs := []string{"--dangerously-skip-permissions"}
	if params.Resume {
		cmdArgs = append(cmdArgs, "--continue")
	}
	cmdArgs = append(cmdArgs, "--print", params.Prompt)

	agyBin := "agy"
	if p, err := exec.LookPath("agy"); err == nil {
		agyBin = p
	} else if home, _ := os.UserHomeDir(); home != "" {
		localBin := filepath.Join(home, ".local", "bin", "agy")
		if _, err := os.Stat(localBin); err == nil {
			agyBin = localBin
		}
	}
	cmd := exec.CommandContext(ctx, agyBin, cmdArgs...)
	cmd.Dir = params.ProjectDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := stdout.String()
	if err != nil {
		return res, fmt.Errorf("error ejecutando agy (%w): %s", err, stderr.String())
	}
	return res, nil
}

// PcTaskTool permite encolar y consultar tareas en la notebook via workstation-queue.
type PcTaskTool struct{}

func (t *PcTaskTool) Name() string { return "pc_task" }
func (t *PcTaskTool) Description() string {
	return "Administra la cola asincrona de tareas hacia la notebook Lenovo (CLI pc-task): add, agy, list, show, cancel."
}
func (t *PcTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "agy", "list", "show", "cancel"},
				"description": "Accion a ejecutar en pc-task.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Titulo o descripcion breve de la tarea.",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Comando bash a ejecutar (para action='add').",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Prompt de desarrollo con agy (para action='agy').",
			},
			"project": map[string]any{
				"type":        "string",
				"description": "Nombre o ruta del proyecto en la notebook.",
			},
			"task_id": map[string]any{
				"type":        "string",
				"description": "ID de la tarea para show o cancel.",
			},
		},
		"required": []string{"action"},
	}
}
func (t *PcTaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action  string `json:"action"`
		Title   string `json:"title"`
		Command string `json:"command"`
		Prompt  string `json:"prompt"`
		Project string `json:"project"`
		TaskID  string `json:"task_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	var cmdArgs []string
	switch params.Action {
	case "list":
		cmdArgs = []string{"list"}
	case "show":
		cmdArgs = []string{"show", params.TaskID}
	case "cancel":
		cmdArgs = []string{"cancel", params.TaskID}
	case "add":
		cmdArgs = []string{"add", params.Title, params.Command}
	case "agy":
		cmdArgs = []string{"agy", params.Title, params.Prompt}
		if params.Project != "" {
			cmdArgs = append(cmdArgs, "--project", params.Project)
		}
	default:
		return "", fmt.Errorf("accion invalida: %s", params.Action)
	}

	pcTaskBin := "pc-task"
	if p, err := exec.LookPath("pc-task"); err == nil {
		pcTaskBin = p
	} else if home, _ := os.UserHomeDir(); home != "" {
		localBin := filepath.Join(home, ".local", "bin", "pc-task")
		if _, err := os.Stat(localBin); err == nil {
			pcTaskBin = localBin
		}
	}
	cmd := exec.CommandContext(ctx, pcTaskBin, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("error en pc-task: %w", err)
	}
	return string(out), nil
}

// GcliTool interactua con Google Workspace (Calendar, Gmail, Tasks, Drive).
type GcliTool struct{}

func (t *GcliTool) Name() string { return "gcli" }
func (t *GcliTool) Description() string {
	return "Consulta y gestiona Google Calendar, Gmail, Google Tasks y Google Drive a traves de la CLI gcli con OAuth 2.0."
}
func (t *GcliTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"service": map[string]any{
				"type":        "string",
				"enum":        []string{"calendar", "gmail", "tasks", "drive"},
				"description": "Servicio de Google a consultar.",
			},
			"args": map[string]any{
				"type":        "string",
				"description": "Subcomandos y argumentos (ej: 'list', 'unread', 'search query').",
			},
		},
		"required": []string{"service"},
	}
}
func (t *GcliTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Service string `json:"service"`
		Args    string `json:"args"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	gcliBin := "gcli"
	if p, err := exec.LookPath("gcli"); err == nil {
		gcliBin = p
	} else if home, _ := os.UserHomeDir(); home != "" {
		localBin := filepath.Join(home, ".local", "bin", "gcli")
		if _, err := os.Stat(localBin); err == nil {
			gcliBin = localBin
		}
	}
	fullCmd := fmt.Sprintf("%s %s %s", gcliBin, params.Service, params.Args)
	cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", fullCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			return string(out), fmt.Errorf("comando gcli excedio el tiempo limite de 45s")
		}
		return string(out), fmt.Errorf("error en gcli: %w", err)
	}
	return string(out), nil
}

// HomelabTool administra servicios, PM2, Docker, subdominios y backups.
type HomelabTool struct{}

func (t *HomelabTool) Name() string { return "homelab" }
func (t *HomelabTool) Description() string {
	return "Controla servicios del homelab: status de PM2, logs de PM2, docker ps/stats, subdominios Cloudflare o homelab-monitor."
}
func (t *HomelabTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Comando administrativo a ejecutar (ej: 'pm2 status', 'docker ps', 'cf-subdomain list').",
			},
		},
		"required": []string{"command"},
	}
}
func (t *HomelabTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", params.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			return string(out), fmt.Errorf("comando homelab excedio el tiempo limite de 45s")
		}
		return string(out), fmt.Errorf("error ejecutando accion homelab: %w", err)
	}
	return string(out), nil
}

// WebFetchTool obtiene texto o contenido de paginas web mediante HTTP GET.
type WebFetchTool struct{}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) Description() string {
	return "Obtiene el contenido de una URL web y retorna el texto extraido sin etiquetas pesadas."
}
func (t *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL HTTP o HTTPS a consultar.",
			},
		},
		"required": []string{"url"},
	}
}
func (t *WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	targetURL := strings.TrimSpace(params.URL)
	if targetURL == "" {
		return "", errors.New("la URL no puede estar vacia")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,*/*;q=0.7")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error al consultar URL: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	contentType := resp.Header.Get("Content-Type")
	strBody := string(body)

	// Si es HTML, extraer texto limpio y estructurado omitiendo scripts y estilos
	if strings.Contains(contentType, "text/html") || strings.Contains(strBody, "<html") || strings.Contains(strBody, "<body") {
		strBody = cleanHTML(strBody)
	}

	if len(strBody) > 15000 {
		strBody = strBody[:15000] + "\n\n...(contenido truncado por longitud)..."
	}
	return strBody, nil
}

func cleanHTML(raw string) string {
	// Remover scripts, estilos y bloques no esenciales
	reScript := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	raw = reScript.ReplaceAllString(raw, "")

	reStyle := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	raw = reStyle.ReplaceAllString(raw, "")

	reNav := regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	raw = reNav.ReplaceAllString(raw, "")

	reHeader := regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	raw = reHeader.ReplaceAllString(raw, "")

	reFooter := regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	raw = reFooter.ReplaceAllString(raw, "")

	reAside := regexp.MustCompile(`(?is)<aside[^>]*>.*?</aside>`)
	raw = reAside.ReplaceAllString(raw, "")

	// Encabezados
	reH := regexp.MustCompile(`(?is)<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	raw = reH.ReplaceAllString(raw, "\n\n# $2\n")

	// Parrafos y saltos
	reP := regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	raw = reP.ReplaceAllString(raw, "\n$1\n")

	reBR := regexp.MustCompile(`(?i)<br\s*/?>`)
	raw = reBR.ReplaceAllString(raw, "\n")

	// Items de listas
	reLI := regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
	raw = reLI.ReplaceAllString(raw, "\n- $1")

	// Remover resto de tags HTML
	reTags := regexp.MustCompile(`<[^>]+>`)
	raw = reTags.ReplaceAllString(raw, " ")

	// Decodificar entidades HTML
	raw = html.UnescapeString(raw)

	// Normalizar espacios y saltos de linea
	lines := strings.Split(raw, "\n")
	var cleaned []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return strings.Join(cleaned, "\n\n")
}

// WeatherTool consulta el clima en tiempo real via wttr.in.
type WeatherTool struct{}

func (t *WeatherTool) Name() string { return "weather" }
func (t *WeatherTool) Description() string {
	return "Consulta el pronostico del tiempo y temperatura actual para una ciudad o localidad."
}
func (t *WeatherTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "Nombre de la localidad o ciudad (ej: 'Buenos Aires', 'Florencio Varela').",
			},
		},
		"required": []string{"location"},
	}
}
func (t *WeatherTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	loc := url.QueryEscape(params.Location)
	endpoint := fmt.Sprintf("https://wttr.in/%s?format=3&lang=es", loc)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/7.88.1")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error consultando clima: %w", err)
	}
	defer resp.Body.Close()

	out, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(out)), nil
}

// SqliteQueryTool permite ejecutar consultas SQL (SELECT) en bases de datos locales.
type SqliteQueryTool struct{}

func (t *SqliteQueryTool) Name() string { return "sqlite_query" }
func (t *SqliteQueryTool) Description() string {
	return "Ejecuta una consulta SQL SELECT en una base de datos SQLite local y retorna los resultados formateados en JSON."
}
func (t *SqliteQueryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"db_path": map[string]any{
				"type":        "string",
				"description": "Ruta al archivo .db SQLite.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Consulta SQL a ejecutar (SELECT).",
			},
		},
		"required": []string{"db_path", "query"},
	}
}
func (t *SqliteQueryTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		DBPath string `json:"db_path"`
		Query  string `json:"query"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	trimmed := strings.TrimSpace(strings.ToUpper(params.Query))
	if !strings.HasPrefix(trimmed, "SELECT") && !strings.HasPrefix(trimmed, "PRAGMA") && !strings.HasPrefix(trimmed, "EXPLAIN") {
		return "", errors.New("solo se permiten consultas de lectura (SELECT / PRAGMA)")
	}

	db, err := sql.Open("sqlite", "file:"+params.DBPath+"?mode=ro")
	if err != nil {
		return "", fmt.Errorf("error abriendo DB SQLite: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, params.Query)
	if err != nil {
		return "", fmt.Errorf("error ejecutando consulta SQL: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}

	var results []map[string]any
	for rows.Next() {
		colValues := make([]any, len(cols))
		colPointers := make([]any, len(cols))
		for i := range colValues {
			colPointers[i] = &colValues[i]
		}

		if err := rows.Scan(colPointers...); err != nil {
			return "", err
		}

		rowMap := make(map[string]any)
		for i, colName := range cols {
			val := colValues[i]
			if b, ok := val.([]byte); ok {
				rowMap[colName] = string(b)
			} else {
				rowMap[colName] = val
			}
		}
		results = append(results, rowMap)
		if len(results) >= 100 {
			break
		}
	}

	jsonBytes, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

// RemindersTool consulta y administra recordatorios locales en SQLite.
type RemindersTool struct {
	DBPath string
}

func (t *RemindersTool) Name() string { return "reminders" }
func (t *RemindersTool) Description() string {
	return "Consulta, lista, agrega o elimina los recordatorios y alertas programadas en el sistema local."
}
func (t *RemindersTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "add", "delete"},
				"description": "Accion: 'list' (ver recordatorios), 'add' (agregar nuevo), 'delete' (eliminar por ID).",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Texto del recordatorio (para action='add').",
			},
			"scheduled_at": map[string]any{
				"type":        "string",
				"description": "Fecha y hora en formato 'YYYY-MM-DD HH:MM' o 'HH:MM' (para action='add').",
			},
			"id": map[string]any{
				"type":        "integer",
				"description": "ID del recordatorio a eliminar (para action='delete').",
			},
		},
		"required": []string{"action"},
	}
}
func (t *RemindersTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action      string `json:"action"`
		Message     string `json:"message"`
		ScheduledAt string `json:"scheduled_at"`
		ID          int64  `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	dbPath := t.DBPath
	if dbPath == "" {
		dbPath = filepath.Join("data", "reminders.db")
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return "", fmt.Errorf("error abriendo DB de recordatorios: %w", err)
	}
	defer db.Close()

	switch params.Action {
	case "list":
		rows, err := db.QueryContext(ctx, "SELECT id, chat_jid, target_name, scheduled_at, message, sent FROM reminders ORDER BY scheduled_at ASC LIMIT 50;")
		if err != nil {
			return "", fmt.Errorf("error consultando recordatorios: %w", err)
		}
		defer rows.Close()

		type RemRow struct {
			ID          int64  `json:"id"`
			ChatJID     string `json:"chat_jid"`
			TargetName  string `json:"target_name"`
			ScheduledAt string `json:"scheduled_at"`
			Message     string `json:"message"`
			Sent        int    `json:"sent"`
		}

		var list []RemRow
		for rows.Next() {
			var r RemRow
			var tName sql.NullString
			if err := rows.Scan(&r.ID, &r.ChatJID, &tName, &r.ScheduledAt, &r.Message, &r.Sent); err == nil {
				if tName.Valid {
					r.TargetName = tName.String
				}
				list = append(list, r)
			}
		}

		if len(list) == 0 {
			return "No hay recordatorios guardados en el sistema.", nil
		}

		resBytes, _ := json.MarshalIndent(list, "", "  ")
		return string(resBytes), nil

	case "add":
		if params.Message == "" || params.ScheduledAt == "" {
			return "", errors.New("para action='add' se requieren 'message' y 'scheduled_at'")
		}
		loc, _ := time.LoadLocation("America/Buenos_Aires")
		if loc == nil {
			loc = time.FixedZone("ART", -3*3600)
		}
		now := time.Now().In(loc)
		var targetTime time.Time
		if t, err := time.ParseInLocation("2006-01-02 15:04", params.ScheduledAt, loc); err == nil {
			targetTime = t
		} else if t, err := time.ParseInLocation("15:04", params.ScheduledAt, loc); err == nil {
			targetTime = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc)
			if targetTime.Before(now) {
				targetTime = targetTime.Add(24 * time.Hour)
			}
		} else {
			return "", fmt.Errorf("formato de fecha invalido: %s (usar 'YYYY-MM-DD HH:MM' o 'HH:MM')", params.ScheduledAt)
		}

		res, err := db.ExecContext(ctx, "INSERT INTO reminders (chat_jid, target_name, scheduled_at, message, sent) VALUES (?, ?, ?, ?, 0);", "direct", "Matías", targetTime.Format("2006-01-02 15:04:05"), params.Message)
		if err != nil {
			return "", err
		}
		lastID, _ := res.LastInsertId()
		return fmt.Sprintf("Recordatorio #%d programado exitosamente para el %s: '%s'", lastID, targetTime.Format("2006-01-02 15:04:05"), params.Message), nil

	case "delete":
		if params.ID <= 0 {
			return "", errors.New("para action='delete' se requiere un 'id' valido")
		}
		_, err := db.ExecContext(ctx, "DELETE FROM reminders WHERE id = ?;", params.ID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Recordatorio #%d eliminado.", params.ID), nil

	default:
		return "", fmt.Errorf("accion desconocida: %s", params.Action)
	}
}

// FileLsTool lista directorios con metadatos (archivos, carpetas, enlaces, pesos y fechas).
type FileLsTool struct {
	DefaultWorkDir string
}

func (t *FileLsTool) Name() string { return "file_ls" }
func (t *FileLsTool) Description() string {
	return "Lista el contenido de un directorio en el sistema de archivos (archivos, subdirectorios, enlaces, tamanos legibles y fechas de modificacion)."
}
func (t *FileLsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Ruta del directorio a listar. Si se omite, usa el directorio de trabajo.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Cantidad maxima de entradas a retornar (por defecto 100, maximo 500).",
			},
		},
	}
}
func (t *FileLsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	targetPath := strings.TrimSpace(params.Path)
	if targetPath == "" {
		targetPath = t.DefaultWorkDir
	}
	if targetPath == "" {
		targetPath = "."
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return "", fmt.Errorf("error listando directorio %s: %w", targetPath, err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Directorio: %s (Total: %d entradas)\n\n", targetPath, len(entries)))

	count := 0
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		typeStr := "FILE"
		if entry.IsDir() {
			typeStr = "DIR "
		} else if info.Mode()&os.ModeSymlink != 0 {
			typeStr = "LINK"
		}

		sizeStr := formatFileSize(info.Size())
		modTime := info.ModTime().Format("2006-01-02 15:04")
		sb.WriteString(fmt.Sprintf("[%s] %-10s  %s  %s\n", typeStr, sizeStr, modTime, entry.Name()))

		count++
		if count >= limit {
			if count < len(entries) {
				sb.WriteString(fmt.Sprintf("\n...(y %d entradas mas)...\n", len(entries)-count))
			}
			break
		}
	}

	return strings.TrimSpace(sb.String()), nil
}

func formatFileSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ApplyPatchTool aplica parches con diffs unificados o bloques de edicion en archivos.
type ApplyPatchTool struct {
	DefaultWorkDir string
}

func (t *ApplyPatchTool) Name() string { return "apply_patch" }
func (t *ApplyPatchTool) Description() string {
	return "Aplica un parche en formato unified diff a uno o mas archivos dentro del repositorio o sistema de archivos."
}
func (t *ApplyPatchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patch": map[string]any{
				"type":        "string",
				"description": "Contenido del parche en formato unified diff o patch.",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Directorio base donde aplicar el parche. Si se omite, usa el workspace.",
			},
		},
		"required": []string{"patch"},
	}
}
func (t *ApplyPatchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Patch   string `json:"patch"`
		WorkDir string `json:"workdir"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	patchContent := strings.TrimSpace(params.Patch)
	if patchContent == "" {
		return "", errors.New("el contenido del parche no puede estar vacio")
	}

	dir := strings.TrimSpace(params.WorkDir)
	if dir == "" {
		dir = t.DefaultWorkDir
	}
	if dir == "" {
		dir = "."
	}

	// 1. Intentar con git apply
	cmdGit := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "--recount", "-")
	cmdGit.Dir = dir
	cmdGit.Stdin = strings.NewReader(patchContent + "\n")
	outGit, errGit := cmdGit.CombinedOutput()
	if errGit == nil {
		return fmt.Sprintf("Parche aplicado exitosamente con git apply en %s\n%s", dir, string(outGit)), nil
	}

	// 2. Si falla git apply, intentar con patch -p1 o patch -p0
	cmdPatch := exec.CommandContext(ctx, "patch", "-p1", "--no-backup-if-mismatch")
	cmdPatch.Dir = dir
	cmdPatch.Stdin = strings.NewReader(patchContent + "\n")
	outPatch, errPatch := cmdPatch.CombinedOutput()
	if errPatch == nil {
		return fmt.Sprintf("Parche aplicado exitosamente con patch -p1 en %s\n%s", dir, string(outPatch)), nil
	}

	cmdPatch0 := exec.CommandContext(ctx, "patch", "-p0", "--no-backup-if-mismatch")
	cmdPatch0.Dir = dir
	cmdPatch0.Stdin = strings.NewReader(patchContent + "\n")
	outPatch0, errPatch0 := cmdPatch0.CombinedOutput()
	if errPatch0 == nil {
		return fmt.Sprintf("Parche aplicado exitosamente con patch -p0 en %s\n%s", dir, string(outPatch0)), nil
	}

	return "", fmt.Errorf("error aplicando parche:\n- git apply: %s (%v)\n- patch -p1: %s (%v)\n- patch -p0: %s (%v)",
		strings.TrimSpace(string(outGit)), errGit,
		strings.TrimSpace(string(outPatch)), errPatch,
		strings.TrimSpace(string(outPatch0)), errPatch0)
}
