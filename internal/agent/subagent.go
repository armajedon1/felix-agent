package agent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SubagentTask modela la ejecucion de un subagente autonomo en segundo plano.
type SubagentTask struct {
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	Objective  string             `json:"objective"`
	Role       string             `json:"role"` // "general", "researcher", "coder", "sysadmin", "auditor"
	Status     string             `json:"status"` // "pending", "running", "completed", "failed", "cancelled"
	StartedAt  time.Time          `json:"started_at"`
	EndedAt    time.Time          `json:"ended_at"`
	Duration   string             `json:"duration"`
	Result     string             `json:"result"`
	Error      string             `json:"error,omitempty"`
	ReportPath string             `json:"report_path,omitempty"`
	cancel     context.CancelFunc `json:"-"`
}

// SubagentManager administra la concurrencia, ciclo de vida, persistencia y entregables de subagentes.
type SubagentManager struct {
	mu         sync.Mutex
	db         *sql.DB
	engine     *Engine
	tasks      map[string]*SubagentTask
	sendFunc   func(chatJID, message string) error
	targetID   string
	reportsDir string
	semaphore  chan struct{}
}

// NewSubagentManager inicializa el administrador de subagentes concurrentes con limite de paralelismo.
func NewSubagentManager(db *sql.DB, engine *Engine, sendFunc func(chatJID, message string) error, targetJID string) (*SubagentManager, error) {
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS subagent_tasks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		objective TEXT NOT NULL,
		role TEXT DEFAULT 'general',
		status TEXT NOT NULL,
		started_at DATETIME,
		ended_at DATETIME,
		duration TEXT,
		result TEXT,
		error TEXT,
		report_path TEXT DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_subagent_tasks_status ON subagent_tasks(status);
	`
	if _, err := db.Exec(createTableQuery); err != nil {
		return nil, fmt.Errorf("error creando tabla subagent_tasks: %w", err)
	}

	// Migraciones idempotentes de columnas si la tabla ya existia
	_, _ = db.Exec("ALTER TABLE subagent_tasks ADD COLUMN role TEXT DEFAULT 'general';")
	_, _ = db.Exec("ALTER TABLE subagent_tasks ADD COLUMN report_path TEXT DEFAULT '';")

	// Reconciliar subagentes interrumpidos por caidas o reinicios previos del servicio
	reconcileQuery := `UPDATE subagent_tasks SET status = 'failed', error = 'Interrumpido por reinicio del servicio', ended_at = CURRENT_TIMESTAMP WHERE status IN ('running', 'pending');`
	if res, err := db.Exec(reconcileQuery); err == nil {
		if aff, _ := res.RowsAffected(); aff > 0 {
			log.Printf("[Subagent Manager] Reconciliadas %d tareas huerfanas de sesiones previas", aff)
		}
	}

	sm := &SubagentManager{
		db:         db,
		engine:     engine,
		tasks:      make(map[string]*SubagentTask),
		sendFunc:   sendFunc,
		targetID:   targetJID,
		reportsDir: "./workspace/reports",
		semaphore:  make(chan struct{}, 3), // Maximo 3 subagentes concurrentes en paralelo
	}

	_ = os.MkdirAll(sm.reportsDir, 0755)

	return sm, nil
}

// SetReportsDir configura la carpeta de destino para los reportes completos en Markdown.
func (sm *SubagentManager) SetReportsDir(dir string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.reportsDir = dir
	_ = os.MkdirAll(dir, 0755)
}

// GetActiveCount retorna la cantidad de subagentes ejecutandose activamente.
func (sm *SubagentManager) GetActiveCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	count := 0
	for _, t := range sm.tasks {
		if t.Status == "running" || t.Status == "pending" {
			count++
		}
	}
	return count
}

// Spawn inicia un subagente autonomo con rol general.
func (sm *SubagentManager) Spawn(title, objective string, timeoutMinutes int) (string, error) {
	return sm.SpawnWithRole(title, objective, "general", timeoutMinutes)
}

// SpawnWithRole inicia un subagente autonomo con arquetipo de especializacion y timeout.
func (sm *SubagentManager) SpawnWithRole(title, objective, role string, timeoutMinutes int) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if timeoutMinutes <= 0 {
		timeoutMinutes = 10
	}
	if role == "" {
		role = "general"
	}

	rawID := make([]byte, 4)
	_, _ = rand.Read(rawID)
	taskID := fmt.Sprintf("sub_%s", hex.EncodeToString(rawID))

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMinutes)*time.Minute)

	task := &SubagentTask{
		ID:        taskID,
		Title:     title,
		Objective: objective,
		Role:      role,
		Status:    "pending",
		StartedAt: time.Now(),
		cancel:    cancel,
	}

	sm.tasks[taskID] = task

	// Persistir estado inicial en SQLite
	query := `INSERT INTO subagent_tasks (id, title, objective, role, status, started_at) VALUES (?, ?, ?, ?, ?, ?);`
	_, _ = sm.db.Exec(query, task.ID, task.Title, task.Objective, task.Role, task.Status, task.StartedAt)

	// Ejecutar subagente en goroutine concurrente controlada por semaforo
	go sm.runSubagentWorker(ctx, task)

	log.Printf("[Subagent Manager] Encolado subagente '%s' [Rol: %s] (ID: %s, Timeout: %d min)", title, role, taskID, timeoutMinutes)
	return taskID, nil
}

func (sm *SubagentManager) runSubagentWorker(ctx context.Context, task *SubagentTask) {
	if sm.engine == nil {
		sm.mu.Lock()
		task.EndedAt = time.Now()
		task.Duration = "0s"
		task.Status = "completed"
		task.Result = "Ejecucion completada (modo mock/test sin motor agentico)"
		_, _ = sm.db.Exec("UPDATE subagent_tasks SET status = ?, ended_at = ?, duration = ?, result = ? WHERE id = ?;",
			task.Status, task.EndedAt, task.Duration, task.Result, task.ID)
		sm.mu.Unlock()
		return
	}

	// Adquirir turno en el semaforo de concurrencia
	select {
	case sm.semaphore <- struct{}{}:
		defer func() { <-sm.semaphore }()
	case <-ctx.Done():
		sm.mu.Lock()
		task.EndedAt = time.Now()
		task.Status = "cancelled"
		task.Error = "Tarea cancelada o excedio tiempo en cola de espera"
		_, _ = sm.db.Exec("UPDATE subagent_tasks SET status = ?, ended_at = ?, error = ? WHERE id = ?;", task.Status, task.EndedAt, task.Error, task.ID)
		sm.mu.Unlock()
		return
	}

	sm.mu.Lock()
	task.Status = "running"
	task.StartedAt = time.Now()
	_, _ = sm.db.Exec("UPDATE subagent_tasks SET status = 'running', started_at = ? WHERE id = ?;", task.StartedAt, task.ID)
	sm.mu.Unlock()

	startTime := time.Now()
	subagentPrompt := getRoleSystemPrompt(task.Role, task.Title, task.Objective)

	// Ejecutar ciclo ReAct en una sesion aislada
	sessionID := "subagent:" + task.ID
	result, err := sm.engine.Run(ctx, sessionID, task.Objective, subagentPrompt)

	duration := time.Since(startTime).Round(time.Second)

	sm.mu.Lock()
	task.EndedAt = time.Now()
	task.Duration = duration.String()

	if err != nil {
		if ctx.Err() == context.Canceled {
			task.Status = "cancelled"
			task.Error = "Tarea cancelada por el usuario"
		} else {
			task.Status = "failed"
			task.Error = err.Error()
		}
	} else {
		task.Status = "completed"
		task.Result = result
	}

	// Persistir reporte entregable en disco
	reportPath := sm.saveReportToDisk(task)
	task.ReportPath = reportPath

	// Actualizar registro en SQLite
	updateQuery := `
	UPDATE subagent_tasks 
	SET status = ?, ended_at = ?, duration = ?, result = ?, error = ?, report_path = ?
	WHERE id = ?;
	`
	_, _ = sm.db.Exec(updateQuery, task.Status, task.EndedAt, task.Duration, task.Result, task.Error, task.ReportPath, task.ID)
	sm.mu.Unlock()

	log.Printf("[Subagent Manager] Subagente %s finalizado [Rol: %s, Estado: %s, Duracion: %s]. Entregable: %s",
		task.ID, task.Role, task.Status, task.Duration, reportPath)

	// Despachar reporte final proactivo a WhatsApp
	if sm.sendFunc != nil {
		report := sm.formatReport(task)
		if sendErr := sm.sendFunc(sm.targetID, report); sendErr != nil {
			log.Printf("[Subagent Error] Fallo despachando reporte a WhatsApp (%s): %v", sm.targetID, sendErr)
		} else {
			log.Printf("[Subagent Notificado] Reporte de %s enviado a WhatsApp exitosamente", task.ID)
		}
	}
}

func getRoleSystemPrompt(role, title, objective string) string {
	baseRules := "Prohibicion estricta de emojis. Respuestas sobrias, directas y tecnicamente fundamentadas."
	switch strings.ToLower(role) {
	case "researcher":
		return fmt.Sprintf(`Eres un subagente investigador autonomo de Félix.
Mision: %s
Objetivo: %s

Directivas de Investigador:
1. Tu rol es recopilacion rigurosa, contraste de fuentes, lectura documental y analisis de informacion.
2. Utiliza herramientas de consulta: web_search, web_fetch, file_read, doc_engine y memory_search.
3. No ejecutes acciones destructivas ni modifiques archivos de produccion.
4. Genera un informe tecnico estructurado con hechos comprobados, datos y referencias concretas. %s`, title, objective, baseRules)

	case "coder":
		return fmt.Sprintf(`Eres un subagente de desarrollo y programacion de Félix.
Mision: %s
Objetivo: %s

Directivas de Desarrollador:
1. Resuelve la tarea de programacion siguiendo estrictamente principios KISS, YAGNI, tipado estricto y cero sobreingenieria.
2. Utiliza file_read, file_write, file_edit, exec y herramientas de codigo.
3. Verifica tus cambios compilando o ejecutando tests antes de concluir.
4. Redacta un reporte final con los archivos modificados, diffs y verificacion de pruebas. %s`, title, objective, baseRules)

	case "sysadmin", "devops":
		return fmt.Sprintf(`Eres un subagente de infraestructura y DevOps de Félix.
Mision: %s
Objetivo: %s

Directivas de Sysadmin:
1. Tu rol es diagnosticar, monitorear y gestionar servicios, redes y contenedores en el homelab.
2. Utiliza herramientas de infraestructura (docker, homelab, net_sentinel, workstation_link, exec).
3. Asegura la estabilidad operativa de los servicios y tuneles.
4. Entrega un informe consolidado con estado de servicios, recursos medidos y acciones ejecutadas. %s`, title, objective, baseRules)

	case "auditor":
		return fmt.Sprintf(`Eres un subagente auditor de seguridad y salud de Félix.
Mision: %s
Objetivo: %s

Directivas de Auditor:
1. Inspecciona minuciosamente logs, configuraciones, secretos y consistencia de datos sin introducir cambios.
2. Verifica que no existan credenciales expuestas, errores no atendidos o discrepancias de estado.
3. Entrega un informe estructurado de hallazgos clasificados por severidad. %s`, title, objective, baseRules)

	default: // general
		return fmt.Sprintf(`Eres un subagente autonomo de Félix. Tu mision es resolver la siguiente tarea tecnica de forma independiente:
Mision: %s
Objetivo: %s

Instrucciones:
1. Utiliza las herramientas pertinentes de forma autonoma para investigar, diagnosticar o ejecutar los pasos necesarios.
2. No requieres interaccion con el usuario durante tu ejecucion; investiga y resuelve.
3. Al concluir, redacta un informe final estructurado con tus conclusiones, datos verificados y resolucion. %s`, title, objective, baseRules)
	}
}

func (sm *SubagentManager) saveReportToDisk(task *SubagentTask) string {
	_ = os.MkdirAll(sm.reportsDir, 0755)

	slug := cleanSlug(task.Title)
	fileName := fmt.Sprintf("%s_%s.md", task.ID, slug)
	filePath := filepath.Join(sm.reportsDir, fileName)

	var sb strings.Builder
	sb.WriteString("# Reporte Tecnico de Subagente\n\n")
	sb.WriteString(fmt.Sprintf("- **ID de Tarea:** `%s`\n", task.ID))
	sb.WriteString(fmt.Sprintf("- **Titulo:** %s\n", task.Title))
	sb.WriteString(fmt.Sprintf("- **Rol:** %s\n", strings.ToUpper(task.Role)))
	sb.WriteString(fmt.Sprintf("- **Estado:** %s\n", strings.ToUpper(task.Status)))
	sb.WriteString(fmt.Sprintf("- **Inicio:** %s\n", task.StartedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("- **Fin:** %s\n", task.EndedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("- **Duracion:** %s\n\n", task.Duration))
	sb.WriteString(fmt.Sprintf("## Objetivo\n%s\n\n", task.Objective))
	sb.WriteString(fmt.Sprintf("## Informe y Resultados\n%s\n", task.Result))

	if task.Error != "" {
		sb.WriteString(fmt.Sprintf("\n## Errores Registrados\n```\n%s\n```\n", task.Error))
	}

	_ = os.WriteFile(filePath, []byte(sb.String()), 0644)
	return filePath
}

func cleanSlug(s string) string {
	reg := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	slug := reg.ReplaceAllString(strings.ToLower(s), "_")
	slug = strings.Trim(slug, "_")
	if len(slug) > 30 {
		slug = slug[:30]
	}
	if slug == "" {
		slug = "tarea"
	}
	return slug
}

// GetTaskReport retorna el informe completo en Markdown generado por un subagente.
func (sm *SubagentManager) GetTaskReport(taskID string) (string, error) {
	sm.mu.Lock()
	task, exists := sm.tasks[taskID]
	sm.mu.Unlock()

	if exists && task.ReportPath != "" {
		if data, err := os.ReadFile(task.ReportPath); err == nil {
			return string(data), nil
		}
	}

	var title, objective, role, status, duration, result, errStr, reportPath string
	var startedAt time.Time
	query := `SELECT title, objective, COALESCE(role, 'general'), status, started_at, COALESCE(duration, ''), COALESCE(result, ''), COALESCE(error, ''), COALESCE(report_path, '') FROM subagent_tasks WHERE id = ?;`
	err := sm.db.QueryRow(query, taskID).Scan(&title, &objective, &role, &status, &startedAt, &duration, &result, &errStr, &reportPath)
	if err != nil {
		return "", fmt.Errorf("tarea %s no encontrada: %w", taskID, err)
	}

	if reportPath != "" {
		if data, err := os.ReadFile(reportPath); err == nil {
			return string(data), nil
		}
	}

	return fmt.Sprintf("# Reporte de Tarea %s\n- Titulo: %s\n- Rol: %s\n- Estado: %s (Duracion: %s)\n\n## Objetivo\n%s\n\n## Resultado\n%s\n",
		taskID, title, role, status, duration, objective, result), nil
}

func (sm *SubagentManager) formatReport(task *SubagentTask) string {
	var sb strings.Builder
	sb.WriteString("*REPORTE DE SUBAGENTE AUTONOMO FINALIZADO*\n")
	sb.WriteString(fmt.Sprintf("- Mision: *%s*\n", task.Title))
	sb.WriteString(fmt.Sprintf("- Rol: `%s` | ID: `%s`\n", task.Role, task.ID))
	sb.WriteString(fmt.Sprintf("- Estado: *%s*\n", strings.ToUpper(task.Status)))
	sb.WriteString(fmt.Sprintf("- Duracion: %s\n", task.Duration))
	if task.ReportPath != "" {
		sb.WriteString(fmt.Sprintf("- Entregable guardado: `%s`\n\n", task.ReportPath))
	} else {
		sb.WriteString("\n")
	}

	if task.Status == "completed" {
		sb.WriteString("*Resultados e informe tecnico:*\n")
		sb.WriteString(strings.TrimSpace(task.Result))
	} else {
		sb.WriteString(fmt.Sprintf("*Detalle del fallo / cancelacion:*\n%s", task.Error))
	}

	return sb.String()
}

// List retorna las tareas en memoria o consultadas de SQLite.
func (sm *SubagentManager) List(limit int) ([]*SubagentTask, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if limit <= 0 {
		limit = 10
	}

	query := `
	SELECT id, title, objective, COALESCE(role, 'general'), status, started_at, COALESCE(ended_at, ''), COALESCE(duration, ''), COALESCE(result, ''), COALESCE(error, ''), COALESCE(report_path, '')
	FROM subagent_tasks
	ORDER BY started_at DESC
	LIMIT ?;
	`
	rows, err := sm.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*SubagentTask
	for rows.Next() {
		var t SubagentTask
		var endedAtStr string
		if err := rows.Scan(&t.ID, &t.Title, &t.Objective, &t.Role, &t.Status, &t.StartedAt, &endedAtStr, &t.Duration, &t.Result, &t.Error, &t.ReportPath); err != nil {
			continue
		}
		list = append(list, &t)
	}
	return list, nil
}

// Cancel detiene la ejecucion de un subagente en curso.
func (sm *SubagentManager) Cancel(taskID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	task, exists := sm.tasks[taskID]
	if !exists || task.cancel == nil {
		return fmt.Errorf("subagente %s no encontrado o ya concluido", taskID)
	}

	task.cancel()
	task.Status = "cancelled"
	_, _ = sm.db.Exec("UPDATE subagent_tasks SET status = 'cancelled' WHERE id = ?;", taskID)
	return nil
}
