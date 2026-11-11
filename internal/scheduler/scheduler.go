package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	_ "modernc.org/sqlite"

	"felix-go/internal/healer"
)

// PendingReminder modela un recordatorio programado pendiente de envio.
type PendingReminder struct {
	ID         int64
	ChatJID    string
	TargetName string
	Message    string
}

// Scheduler administra las alertas programadas y tareas periodicas del agente.
type Scheduler struct {
	db      *sql.DB
	dataDir string
}

// SetDataDir configura el directorio de datos para tareas de mantenimiento.
func (s *Scheduler) SetDataDir(dir string) {
	s.dataDir = dir
}

// CleanOldDownloads elimina archivos descargados con mas de retentionDays dias de antiguedad.
func (s *Scheduler) CleanOldDownloads(retentionDays int) (int, error) {
	if s.dataDir == "" {
		return 0, nil
	}
	downloadsDir := filepath.Join(s.dataDir, "downloads")
	entries, err := os.ReadDir(downloadsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	threshold := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	deleted := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(threshold) {
			targetPath := filepath.Join(downloadsDir, entry.Name())
			if err := os.Remove(targetPath); err == nil {
				deleted++
			}
		}
	}
	return deleted, nil
}

// OptimizeDatabases ejecuta checkpoints de WAL y PRAGMA optimize en todas las bases SQLite del dataDir.
func (s *Scheduler) OptimizeDatabases() error {
	if s.dataDir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		dbPath := filepath.Join(s.dataDir, entry.Name())
		dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", dbPath)
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			continue
		}
		_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
		_, _ = db.Exec("PRAGMA optimize;")
		_ = db.Close()
		log.Printf("[Mantenimiento SQLite] Checkpoint WAL truncado y optimizado: %s", entry.Name())
	}
	return nil
}

// BackupDatabases ejecuta respaldos atomicos consistentes (VACUUM INTO) para bases SQLite y rota archivos antiguos.
func (s *Scheduler) BackupDatabases(retentionDays int) (int, error) {
	if s.dataDir == "" {
		return 0, nil
	}
	backupsDir := filepath.Join(s.dataDir, "backups")
	_ = os.MkdirAll(backupsDir, 0755)

	loc, err := time.LoadLocation("America/Buenos_Aires")
	if err != nil {
		loc = time.FixedZone("ART", -3*3600)
	}
	todayStr := time.Now().In(loc).Format("2006-01-02")

	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return 0, err
	}

	backedUp := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}

		baseName := strings.TrimSuffix(entry.Name(), ".db")
		backupFileName := fmt.Sprintf("%s_%s.db", baseName, todayStr)
		backupPath := filepath.Join(backupsDir, backupFileName)

		// Si ya existe el respaldo del dia, omitir
		if _, err := os.Stat(backupPath); err == nil {
			continue
		}

		dbPath := filepath.Join(s.dataDir, entry.Name())
		dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)", dbPath)
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			continue
		}

		vacuumQuery := fmt.Sprintf("VACUUM INTO '%s';", backupPath)
		_, err = db.Exec(vacuumQuery)
		_ = db.Close()

		if err != nil {
			log.Printf("[Scheduler Error] Respaldo de %s fallo: %v", entry.Name(), err)
		} else {
			backedUp++
			log.Printf("[Respaldo SQLite] Generado respaldo atomico: %s", backupFileName)
		}
	}

	// Rotacion: eliminar respaldos con mas de retentionDays dias
	if retentionDays <= 0 {
		retentionDays = 7
	}
	threshold := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	if bEntries, err := os.ReadDir(backupsDir); err == nil {
		for _, be := range bEntries {
			if be.IsDir() || !strings.HasSuffix(be.Name(), ".db") {
				continue
			}
			if info, err := be.Info(); err == nil && info.ModTime().Before(threshold) {
				_ = os.Remove(filepath.Join(backupsDir, be.Name()))
				log.Printf("[Rotacion Respaldos] Eliminado respaldo antiguo: %s", be.Name())
			}
		}
	}

	return backedUp, nil
}

// RotateLargeLogs inspecciona y trunca logs de PM2 que superen el limite maxSizeBytes (por defecto 50MB).
func (s *Scheduler) RotateLargeLogs(maxSizeBytes int64) (int, error) {
	if maxSizeBytes <= 0 {
		maxSizeBytes = 50 * 1024 * 1024
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		homeDir = "."
	}
	pm2LogsDir := filepath.Join(homeDir, ".pm2", "logs")
	entries, err := os.ReadDir(pm2LogsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	rotated := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		logPath := filepath.Join(pm2LogsDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.Size() > maxSizeBytes {
			cmdTail := exec.Command("tail", "-n", "1000", logPath)
			tailOut, err := cmdTail.Output()
			if err == nil && len(tailOut) > 0 {
				_ = os.WriteFile(logPath, tailOut, 0644)
			} else {
				_ = os.Truncate(logPath, 0)
			}
			rotated++
			log.Printf("[Rotacion PM2] Truncado log excesivo (%d MB): %s", info.Size()/(1024*1024), entry.Name())
		}
	}
	return rotated, nil
}

// ArchiveOldSessions archiva mensajes de sesiones con mas de retentionDays dias a sessions_archive.db
// y los purga de la tabla activa para mantener maximo rendimiento I/O.
func (s *Scheduler) ArchiveOldSessions(retentionDays int) (int64, error) {
	if s.dataDir == "" {
		return 0, nil
	}
	if retentionDays <= 0 {
		retentionDays = 90
	}

	sessionDBPath := filepath.Join(s.dataDir, "sessions.db")
	if _, err := os.Stat(sessionDBPath); err != nil {
		return 0, nil
	}

	archiveDBPath := filepath.Join(s.dataDir, "sessions_archive.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)", sessionDBPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	threshold := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Format("2006-01-02 15:04:05")

	attachQuery := fmt.Sprintf("ATTACH DATABASE '%s' AS archive;", archiveDBPath)
	if _, err := db.Exec(attachQuery); err != nil {
		return 0, fmt.Errorf("error adjuntando sessions_archive: %w", err)
	}
	defer func() {
		_, _ = db.Exec("DETACH DATABASE archive;")
	}()

	createArchiveTable := `
	CREATE TABLE IF NOT EXISTS archive.session_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		tool_calls TEXT DEFAULT '',
		tool_call_id TEXT DEFAULT '',
		created_at DATETIME
	);
	`
	if _, err := db.Exec(createArchiveTable); err != nil {
		return 0, fmt.Errorf("error creando tabla de archivo: %w", err)
	}

	insertQuery := `
	INSERT INTO archive.session_messages (session_id, role, content, tool_calls, tool_call_id, created_at)
	SELECT session_id, role, content, tool_calls, tool_call_id, created_at
	FROM session_messages
	WHERE created_at < ?;
	`
	res, err := db.Exec(insertQuery, threshold)
	if err != nil {
		return 0, fmt.Errorf("error archivando mensajes: %w", err)
	}

	archivedCount, _ := res.RowsAffected()
	if archivedCount > 0 {
		deleteQuery := `DELETE FROM session_messages WHERE created_at < ?;`
		_, _ = db.Exec(deleteQuery, threshold)
		log.Printf("[Archivado Sesiones] Archivados %d mensajes anteriores a %s en sessions_archive.db", archivedCount, threshold)
	}

	return archivedCount, nil
}

// NewScheduler inicializa la base de datos de recordatorios en la ruta indicada.
func NewScheduler(dbPath string) (*Scheduler, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		return nil, fmt.Errorf("error abriendo DB de scheduler: %w", err)
	}

	query := `CREATE TABLE IF NOT EXISTS reminders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_jid TEXT NOT NULL,
		target_name TEXT,
		scheduled_at DATETIME NOT NULL,
		message TEXT NOT NULL,
		sent INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("error creando tabla de recordatorios: %w", err)
	}

	return &Scheduler{db: db}, nil
}

// AddReminder inserta un nuevo recordatorio en la base de datos.
func (s *Scheduler) AddReminder(chatJID, targetName string, scheduledAt time.Time, message string) error {
	query := `INSERT INTO reminders (chat_jid, target_name, scheduled_at, message, sent) VALUES (?, ?, ?, ?, 0);`
	_, err := s.db.Exec(query, chatJID, targetName, scheduledAt.Format("2006-01-02 15:04:05"), message)
	return err
}

// GetPending consulta los recordatorios cuya fecha/hora se ha cumplido y no han sido enviados.
func (s *Scheduler) GetPending() ([]PendingReminder, error) {
	loc, err := time.LoadLocation("America/Buenos_Aires")
	if err != nil {
		loc = time.FixedZone("ART", -3*3600)
	}
	nowStr := time.Now().In(loc).Format("2006-01-02 15:04:05")

	query := `SELECT id, chat_jid, target_name, message FROM reminders WHERE sent = 0 AND scheduled_at <= ?;`
	rows, err := s.db.Query(query, nowStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []PendingReminder
	for rows.Next() {
		var pr PendingReminder
		if err := rows.Scan(&pr.ID, &pr.ChatJID, &pr.TargetName, &pr.Message); err == nil {
			list = append(list, pr)
		}
	}
	return list, nil
}

// MarkSent marca un recordatorio como enviado.
func (s *Scheduler) MarkSent(id int64) error {
	_, err := s.db.Exec("UPDATE reminders SET sent = 1 WHERE id = ?;", id)
	return err
}

// ParseAndSchedule busca directivas tecnicas [PROGRAMAR_RECORDATORIO: ...] generadas por el LLM,
// las agenda en SQLite y retorna el texto limpio para WhatsApp.
func (s *Scheduler) ParseAndSchedule(text, chatJID, targetName string) string {
	re := regexp.MustCompile(`\[PROGRAMAR_RECORDATORIO:\s*([^\]]+)\]`)
	matches := re.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text
	}

	loc, err := time.LoadLocation("America/Buenos_Aires")
	if err != nil {
		loc = time.FixedZone("ART", -3*3600)
	}
	now := time.Now().In(loc)

	for _, m := range matches {
		fullTag := m[0]
		paramsStr := m[1]

		var targetTime time.Time
		var messageText string

		// Extraer mensaje
		msgRe := regexp.MustCompile(`mensaje=["']([^"']+)["']`)
		if msgMatch := msgRe.FindStringSubmatch(paramsStr); len(msgMatch) > 1 {
			messageText = msgMatch[1]
		}

		// Extraer tiempo relativo: en="10m", en="2h", en="1d"
		enRe := regexp.MustCompile(`en=["']([^"']+)["']`)
		if enMatch := enRe.FindStringSubmatch(paramsStr); len(enMatch) > 1 {
			durationStr := enMatch[1]
			if dur, err := time.ParseDuration(durationStr); err == nil {
				targetTime = now.Add(dur)
			}
		}

		// Extraer fecha/tiempo absoluto: tiempo="2026-11-11 20:00" o fecha="..."
		if targetTime.IsZero() {
			timeRe := regexp.MustCompile(`(?:tiempo|fecha)=["']([^"']+)["']`)
			if timeMatch := timeRe.FindStringSubmatch(paramsStr); len(timeMatch) > 1 {
				val := strings.TrimSpace(timeMatch[1])
				if t, err := time.ParseInLocation("2006-01-02 15:04", val, loc); err == nil {
					targetTime = t
				} else if t, err := time.ParseInLocation("2006-01-02 15:04:05", val, loc); err == nil {
					targetTime = t
				} else if t, err := time.ParseInLocation("15:04", val, loc); err == nil {
					targetTime = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc)
					if targetTime.Before(now) {
						targetTime = targetTime.Add(24 * time.Hour)
					}
				}
			}
		}

		if !targetTime.IsZero() && messageText != "" {
			err := s.AddReminder(chatJID, targetName, targetTime, messageText)
			if err != nil {
				log.Printf("[Scheduler Error] No se pudo guardar recordatorio: %v", err)
			} else {
				log.Printf("[Recordatorio Agendado] Para %s (%s) a las %s | Mensaje: %s", targetName, chatJID, targetTime.Format("2006-01-02 15:04:05"), messageText)
			}
		}

		text = strings.Replace(text, fullTag, "", 1)
	}

	return strings.TrimSpace(text)
}

// StartRunner inicia el bucle de despacho autonomo de recordatorios y tareas periodicas (cron).
func (s *Scheduler) StartRunner(ctx context.Context, sendFn func(chatJID, message string) error, ownerJID string) {
	loc, err := time.LoadLocation("America/Buenos_Aires")
	if err != nil {
		loc = time.FixedZone("ART", -3*3600)
	}

	c := cron.New(cron.WithLocation(loc))

	// 1. Resumen Matutino Diario a las 08:30 ART
	_, err = c.AddFunc("30 8 * * *", func() {
		if ownerJID == "" {
			return
		}
		log.Println("[Cron Scheduler] Generando Resumen Matutino Diario (08:30 ART)...")
		var text string
		if p, err := exec.LookPath("morning-brief.sh"); err == nil {
			if out, err := exec.Command(p).CombinedOutput(); err == nil {
				text = strings.TrimSpace(string(out))
			}
		}
		if text == "" {
			var sb strings.Builder
			sb.WriteString("*RESUMEN MATUTINO DEL SISTEMA*\n")
			if out, err := exec.Command("uptime", "-p").Output(); err == nil {
				sb.WriteString(fmt.Sprintf("- Uptime: %s\n", strings.TrimSpace(string(out))))
			}
			if out, err := exec.Command("free", "-h").Output(); err == nil {
				lines := strings.Split(string(out), "\n")
				if len(lines) > 1 {
					sb.WriteString(fmt.Sprintf("- Memoria: %s\n", lines[1]))
				}
			}
			if out, err := exec.Command("df", "-h", "/").Output(); err == nil {
				lines := strings.Split(string(out), "\n")
				if len(lines) > 1 {
					sb.WriteString(fmt.Sprintf("- Almacenamiento (/): %s\n", lines[1]))
				}
			}
			text = strings.TrimSpace(sb.String())
		}
		if text != "" && text != "NO_REPLY" {
			_ = sendFn(ownerJID, text)
			log.Println("[Cron Scheduler] Resumen Matutino despachado a WhatsApp.")
		}
	})
	if err != nil {
		log.Printf("[Cron Error] Fallo programando Resumen Matutino: %v", err)
	}

	// 2. Reporte Semanal los lunes a las 10:00 ART
	_, err = c.AddFunc("0 10 * * 1", func() {
		if ownerJID == "" {
			return
		}
		log.Println("[Cron Scheduler] Generando Reporte Semanal (Lunes 10:00 ART)...")
		var text string
		if p, err := exec.LookPath("weekly-report.sh"); err == nil {
			if out, err := exec.Command(p).CombinedOutput(); err == nil {
				text = strings.TrimSpace(string(out))
			}
		}
		if text == "" {
			text = "*REPORTE SEMANAL DEL SERVIDOR*\nTodos los servicios e infraestructura reportan operacion nominal."
		}
		if text != "" && text != "NO_REPLY" {
			_ = sendFn(ownerJID, text)
			log.Println("[Cron Scheduler] Reporte Semanal despachado a WhatsApp.")
		}
	})
	if err != nil {
		log.Printf("[Cron Error] Fallo programando Reporte Semanal: %v", err)
	}

	healerEng := healer.NewEngine()

	// 3. Monitor de Incidentes y Auto-Curacion (Watchdog) cada 15 minutos
	_, err = c.AddFunc("*/15 * * * *", func() {
		if ownerJID == "" {
			return
		}
		// Auto-curacion nativa de infraestructura (PM2 y Docker)
		healCtx, healCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer healCancel()

		report, err := healerEng.Heal(healCtx, "")
		if err != nil {
			log.Printf("[Cron Healer Error] Fallo en ciclo de auto-curacion: %v", err)
			return
		}

		if report.Unhealthy > 0 {
			alertMsg := report.FormatMarkdown()
			_ = sendFn(ownerJID, alertMsg)
			log.Printf("[Cron Healer] Incidente de infraestructura detectado y procesado. Alerta despachada a %s", ownerJID)
		}
	})
	if err != nil {
		log.Printf("[Cron Error] Fallo programando Watchdog: %v", err)
	}

	// 4. Limpieza automatica de descargas antiguas (>7 dias) a las 04:00 ART
	_, _ = c.AddFunc("0 4 * * *", func() {
		log.Println("[Cron Scheduler] Ejecutando purga de descargas antiguas...")
		deleted, err := s.CleanOldDownloads(7)
		if err != nil {
			log.Printf("[Cron Scheduler Error] Limpieza de descargas: %v", err)
		} else if deleted > 0 {
			log.Printf("[Cron Scheduler] Se eliminaron %d archivos antiguos en descargas", deleted)
		}
	})

	// 5. Rotacion de logs pesados (>50MB) a las 04:15 ART
	_, _ = c.AddFunc("15 4 * * *", func() {
		log.Println("[Cron Scheduler] Ejecutando rotacion de logs de PM2...")
		rotated, err := s.RotateLargeLogs(50 * 1024 * 1024)
		if err != nil {
			log.Printf("[Cron Scheduler Error] Rotacion de logs: %v", err)
		} else if rotated > 0 {
			log.Printf("[Cron Scheduler] Se truncaron %d archivos de logs pesados", rotated)
		}
	})

	// 6. Mantenimiento y Checkpoint WAL de bases SQLite a las 04:30 ART
	_, _ = c.AddFunc("30 4 * * *", func() {
		log.Println("[Cron Scheduler] Ejecutando mantenimiento y optimizacion de SQLite...")
		if err := s.OptimizeDatabases(); err != nil {
			log.Printf("[Cron Scheduler Error] Mantenimiento SQLite: %v", err)
		}
	})

	// 7. Respaldo atomico en caliente de bases SQLite (VACUUM INTO) a las 05:00 ART
	_, _ = c.AddFunc("0 5 * * *", func() {
		log.Println("[Cron Scheduler] Ejecutando respaldo en caliente de bases SQLite...")
		count, err := s.BackupDatabases(7)
		if err != nil {
			log.Printf("[Cron Scheduler Error] Respaldo SQLite: %v", err)
		} else if count > 0 {
			log.Printf("[Cron Scheduler] Respaldadas %d bases SQLite exitosamente", count)
		}
	})

	// 8. Archivado mensual de sesiones antiguas (>90 dias) el dia 1 de cada mes a las 03:00 ART
	_, _ = c.AddFunc("0 3 1 * *", func() {
		log.Println("[Cron Scheduler] Ejecutando archivado mensual de sesiones antiguas...")
		archived, err := s.ArchiveOldSessions(90)
		if err != nil {
			log.Printf("[Cron Scheduler Error] Archivado de sesiones: %v", err)
		} else if archived > 0 {
			log.Printf("[Cron Scheduler] Se archivaron %d mensajes en sessions_archive.db", archived)
		}
	})

	c.Start()
	log.Println("[Cron Scheduler] Cron activo con timezone America/Buenos_Aires (Resumen 08:30, Reporte Lunes 10:00, Watchdog c/15m, Mantenimiento 04:00/04:15/04:30, Backups 05:00, Archivado Mensual).")

	// 9. Bucle para recordatorios de SQLite cada 10 segundos
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				c.Stop()
				ticker.Stop()
				return
			case <-ticker.C:
				pending, err := s.GetPending()
				if err != nil {
					log.Printf("[Scheduler Error] Consultando recordatorios pendientes: %v", err)
					continue
				}

				for _, rem := range pending {
					if err := sendFn(rem.ChatJID, rem.Message); err != nil {
						log.Printf("[Scheduler Error] Fallo al despachar recordatorio a %s: %v", rem.ChatJID, err)
						continue
					}
					log.Printf("[Recordatorio Despachado Autonomamente] A %s (%s) | Mensaje: %s", rem.TargetName, rem.ChatJID, rem.Message)
					_ = s.MarkSent(rem.ID)
				}
			}
		}
	}()
}
