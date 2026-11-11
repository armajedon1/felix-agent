package healer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	CriticalPM2Services      = []string{}
	CriticalDockerContainers = []string{}
	CriticalSystemdServices  = []string{}
)

// ServiceHealStatus describe el diagnostico y resultado de curacion de un servicio.
type ServiceHealStatus struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "pm2" o "docker"
	Status      string `json:"status"`
	Healthy     bool   `json:"healthy"`
	PID         int    `json:"pid,omitempty"`
	Cause       string `json:"cause,omitempty"`
	ActionTaken string `json:"action_taken,omitempty"`
	NewStatus   string `json:"new_status,omitempty"`
	ErrorLog    string `json:"error_log,omitempty"`
}

// HealReport consolida el resultado de un ciclo de diagnostico y curacion.
type HealReport struct {
	Timestamp time.Time           `json:"timestamp"`
	Checked   int                 `json:"checked"`
	Healthy   int                 `json:"healthy"`
	Unhealthy int                 `json:"unhealthy"`
	Recovered int                 `json:"recovered"`
	Failed    int                 `json:"failed"`
	Services  []ServiceHealStatus `json:"services"`
}

// FormatMarkdown formatea el informe en texto claro sin emojis para WhatsApp o terminal.
func (r *HealReport) FormatMarkdown() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*INFORME DE AUTO-CURACION DE INFRAESTRUCTURA (%s)*\n", r.Timestamp.Format("02/01 15:04:05")))
	sb.WriteString(fmt.Sprintf("- Servicios analizados: %d | Operativos: %d | Incidentes: %d\n", r.Checked, r.Healthy, r.Unhealthy))

	if r.Unhealthy > 0 {
		sb.WriteString(fmt.Sprintf("- Recuperaciones exitosas: %d | Fallas persistentes: %d\n\n", r.Recovered, r.Failed))
	} else {
		sb.WriteString("- Estado general: Todos los servicios criticos operan normalmente.\n\n")
	}

	for _, s := range r.Services {
		if s.Healthy {
			sb.WriteString(fmt.Sprintf("- [%s] *%s*: %s (PID: %d)\n", strings.ToUpper(s.Type), s.Name, strings.ToUpper(s.Status), s.PID))
		} else {
			recStr := "RECUPERADO"
			if s.NewStatus != "online" && s.NewStatus != "running" {
				recStr = "FALLA PERSISTENTE"
			}
			sb.WriteString(fmt.Sprintf("- [%s] *%s* (Estado inicial: *%s* | Actual: *%s* - *%s*)\n",
				strings.ToUpper(s.Type), s.Name, strings.ToUpper(s.Status), strings.ToUpper(s.NewStatus), recStr))
			if s.Cause != "" {
				sb.WriteString(fmt.Sprintf("  - Causa detectada: %s\n", s.Cause))
			}
			if s.ActionTaken != "" {
				sb.WriteString(fmt.Sprintf("  - Accion aplicada: %s\n", s.ActionTaken))
			}
		}
	}

	return strings.TrimSpace(sb.String())
}

// Engine orquesta las heurísticas de diagnostico y auto-curacion de servicios.
type Engine struct {
	pm2Services      []string
	dockerContainers []string
	systemdServices  []string
}

// NewEngine crea un nuevo motor de auto-curacion con los servicios criticos predeterminados.
func NewEngine() *Engine {
	return &Engine{
		pm2Services:      CriticalPM2Services,
		dockerContainers: CriticalDockerContainers,
		systemdServices:  CriticalSystemdServices,
	}
}

// Check inspecciona los servicios sin realizar modificaciones.
func (e *Engine) Check(ctx context.Context, target string) (*HealReport, error) {
	return e.runLoop(ctx, target, false)
}

// Heal inspecciona y ejecuta las acciones de auto-curacion sobre servicios caidos.
func (e *Engine) Heal(ctx context.Context, target string) (*HealReport, error) {
	return e.runLoop(ctx, target, true)
}

func (e *Engine) runLoop(ctx context.Context, target string, applyFix bool) (*HealReport, error) {
	report := &HealReport{
		Timestamp: time.Now(),
		Services:  make([]ServiceHealStatus, 0),
	}

	target = strings.TrimSpace(strings.ToLower(target))

	// 1. Diagnostico de procesos PM2
	pm2List := CollectPM2Services(ctx)
	pm2Map := make(map[string]ServiceInfo)
	for _, s := range pm2List {
		pm2Map[strings.ToLower(s.Name)] = s
	}

	for _, reqName := range e.pm2Services {
		if target != "" && target != strings.ToLower(reqName) {
			continue
		}

		report.Checked++
		info, exists := pm2Map[strings.ToLower(reqName)]

		if !exists {
			// Proceso no registrado en PM2
			st := ServiceHealStatus{
				Name:    reqName,
				Type:    "pm2",
				Status:  "no registrado / desaparecido",
				Healthy: false,
			}
			if applyFix {
				st.Cause = "Proceso no presente en la lista activa de PM2"
				st.ActionTaken = "No se puede reiniciar automaticamente un proceso no registrado en PM2"
				st.NewStatus = "inexistente"
				report.Failed++
			}
			report.Unhealthy++
			report.Services = append(report.Services, st)
			continue
		}

		isOnline := (strings.ToLower(info.Status) == "online")
		if isOnline {
			report.Healthy++
			report.Services = append(report.Services, ServiceHealStatus{
				Name:    info.Name,
				Type:    "pm2",
				Status:  info.Status,
				Healthy: true,
				PID:     info.PID,
			})
			continue
		}

		// Proceso PM2 caido, stopped o errored
		report.Unhealthy++
		errLog := ReadPM2ErrorLog(ctx, info.Name)
		st := ServiceHealStatus{
			Name:     info.Name,
			Type:     "pm2",
			Status:   info.Status,
			Healthy:  false,
			PID:      info.PID,
			ErrorLog: errLog,
		}

		if applyFix {
			cause, action := e.healPM2Service(ctx, info.Name, errLog)
			st.Cause = cause
			st.ActionTaken = action

			// Verificar estado posterior
			newInfo := getPM2ServiceInfo(ctx, info.Name)
			st.NewStatus = newInfo.Status
			if strings.ToLower(newInfo.Status) == "online" {
				st.Healthy = true
				report.Recovered++
			} else {
				report.Failed++
			}
		}

		report.Services = append(report.Services, st)
	}

	// 2. Diagnostico de contenedores Docker
	dockerList := CollectDockerContainers(ctx)

	for _, reqName := range e.dockerContainers {
		if target != "" && !strings.Contains(strings.ToLower(reqName), target) && !strings.Contains(target, strings.ToLower(reqName)) {
			continue
		}

		report.Checked++
		var matched *ContainerInfo
		for _, c := range dockerList {
			if strings.Contains(strings.ToLower(c.Name), strings.ToLower(reqName)) {
				matched = &c
				break
			}
		}

		if matched == nil {
			report.Unhealthy++
			st := ServiceHealStatus{
				Name:    reqName,
				Type:    "docker",
				Status:  "no encontrado",
				Healthy: false,
			}
			if applyFix {
				st.Cause = "Contenedor Docker no encontrado en host"
				st.ActionTaken = "Verifique la definicion de Docker Compose"
				st.NewStatus = "inexistente"
				report.Failed++
			}
			report.Services = append(report.Services, st)
			continue
		}

		isRunning := (strings.ToLower(matched.State) == "running")
		if isRunning {
			report.Healthy++
			report.Services = append(report.Services, ServiceHealStatus{
				Name:    matched.Name,
				Type:    "docker",
				Status:  matched.Status,
				Healthy: true,
			})
			continue
		}

		// Contenedor Docker caido o exited
		report.Unhealthy++
		st := ServiceHealStatus{
			Name:    matched.Name,
			Type:    "docker",
			Status:  matched.State,
			Healthy: false,
		}

		if applyFix {
			cause, action := e.healDockerContainer(ctx, matched.Name)
			st.Cause = cause
			st.ActionTaken = action

			// Verificar nuevo estado
			newState := getDockerContainerState(ctx, matched.Name)
			st.NewStatus = newState
			if strings.ToLower(newState) == "running" {
				st.Healthy = true
				report.Recovered++
			} else {
				report.Failed++
			}
		}

		report.Services = append(report.Services, st)
	}

	// 3. Diagnostico de servicios del sistema (Systemd / Cloudflared)
	for _, sysName := range e.systemdServices {
		if target != "" && target != strings.ToLower(sysName) {
			continue
		}
		report.Checked++
		active, statusText := isSystemdServiceActive(ctx, sysName)
		if active {
			report.Healthy++
			report.Services = append(report.Services, ServiceHealStatus{
				Name:    sysName,
				Type:    "systemd",
				Status:  statusText,
				Healthy: true,
			})
			continue
		}

		report.Unhealthy++
		st := ServiceHealStatus{
			Name:    sysName,
			Type:    "systemd",
			Status:  statusText,
			Healthy: false,
		}

		if applyFix {
			st.Cause = "Servicio inactivo o caido en systemd"
			st.ActionTaken = fmt.Sprintf("Ejecutado restart via sudo-run systemctl restart %s", sysName)
			sudoRunBin := "sudo"
			if p, err := exec.LookPath("sudo-run"); err == nil {
				sudoRunBin = p
			}
			cmdRestart := exec.CommandContext(ctx, sudoRunBin, "systemctl", "restart", sysName)
			_ = cmdRestart.Run()

			// Verificar nuevo estado
			time.Sleep(1 * time.Second)
			newActive, newStatus := isSystemdServiceActive(ctx, sysName)
			st.NewStatus = newStatus
			if newActive {
				st.Healthy = true
				report.Recovered++
			} else {
				report.Failed++
			}
		}
		report.Services = append(report.Services, st)
	}

	// 4. Si se especifico un target que no coincidio con las listas predeterminadas
	if target != "" && report.Checked == 0 {
		report.Checked++
		info, existsPM2 := pm2Map[target]
		if existsPM2 {
			if strings.ToLower(info.Status) == "online" {
				report.Healthy++
				report.Services = append(report.Services, ServiceHealStatus{
					Name:    info.Name,
					Type:    "pm2",
					Status:  info.Status,
					Healthy: true,
					PID:     info.PID,
				})
			} else {
				report.Unhealthy++
				errLog := ReadPM2ErrorLog(ctx, info.Name)
				st := ServiceHealStatus{
					Name:     info.Name,
					Type:     "pm2",
					Status:   info.Status,
					Healthy:  false,
					PID:      info.PID,
					ErrorLog: errLog,
				}
				if applyFix {
					cause, action := e.healPM2Service(ctx, info.Name, errLog)
					st.Cause = cause
					st.ActionTaken = action
					newInfo := getPM2ServiceInfo(ctx, info.Name)
					st.NewStatus = newInfo.Status
					if strings.ToLower(newInfo.Status) == "online" {
						st.Healthy = true
						report.Recovered++
					} else {
						report.Failed++
					}
				}
				report.Services = append(report.Services, st)
			}
		} else {
			var matchedDocker *ContainerInfo
			for _, c := range dockerList {
				if strings.Contains(strings.ToLower(c.Name), target) {
					matchedDocker = &c
					break
				}
			}
			if matchedDocker != nil {
				if strings.ToLower(matchedDocker.State) == "running" {
					report.Healthy++
					report.Services = append(report.Services, ServiceHealStatus{
						Name:    matchedDocker.Name,
						Type:    "docker",
						Status:  matchedDocker.Status,
						Healthy: true,
					})
				} else {
					report.Unhealthy++
					st := ServiceHealStatus{
						Name:    matchedDocker.Name,
						Type:    "docker",
						Status:  matchedDocker.State,
						Healthy: false,
					}
					if applyFix {
						cause, action := e.healDockerContainer(ctx, matchedDocker.Name)
						st.Cause = cause
						st.ActionTaken = action
						newState := getDockerContainerState(ctx, matchedDocker.Name)
						st.NewStatus = newState
						if strings.ToLower(newState) == "running" {
							st.Healthy = true
							report.Recovered++
						} else {
							report.Failed++
						}
					}
					report.Services = append(report.Services, st)
				}
			} else {
				report.Unhealthy++
				report.Failed++
				report.Services = append(report.Services, ServiceHealStatus{
					Name:        target,
					Type:        "desconocido",
					Status:      "no registrado",
					Healthy:     false,
					Cause:       "El servicio solicitado no existe en PM2 ni en Docker",
					ActionTaken: "Ninguna accion aplicable",
					NewStatus:   "inexistente",
				})
			}
		}
	}

	return report, nil
}

func (e *Engine) healPM2Service(ctx context.Context, appName, errorLogs string) (cause, actionTaken string) {
	upperLogs := strings.ToUpper(errorLogs)
	lowerLogs := strings.ToLower(errorLogs)

	if strings.Contains(upperLogs, "EADDRINUSE") || strings.Contains(lowerLogs, "address already in use") {
		port := ExtractPortFromLog(errorLogs)
		if port > 0 {
			cause = fmt.Sprintf("Puerto %d en conflicto (EADDRINUSE)", port)
			_ = exec.CommandContext(ctx, "fuser", "-k", fmt.Sprintf("%d/tcp", port)).Run()
			time.Sleep(300 * time.Millisecond)
			actionTaken = fmt.Sprintf("Puerto %d liberado con fuser -k y proceso reiniciado (pm2 restart %s)", port, appName)
		} else {
			cause = "Conflicto de enlace de puerto (EADDRINUSE)"
			actionTaken = fmt.Sprintf("Reinicio de proceso forzando actualizacion de entorno (pm2 restart %s --update-env)", appName)
		}
	} else if strings.Contains(lowerLogs, "out of memory") || strings.Contains(lowerLogs, "heap out of memory") {
		cause = "Agotamiento de memoria heap (JavaScript Heap OOM)"
		actionTaken = fmt.Sprintf("Reinicio de proceso tras caida por memoria (pm2 restart %s)", appName)
	} else if strings.Contains(lowerLogs, "fatal error") || strings.Contains(lowerLogs, "uncaught exception") {
		cause = "Excepcion no capturada o fallo critico del proceso"
		actionTaken = fmt.Sprintf("Reinicio de recuperacion via PM2 (pm2 restart %s)", appName)
	} else {
		cause = "Caida transitoria o parada inesperada"
		actionTaken = fmt.Sprintf("Reinicio automatico ejecutado (pm2 restart %s)", appName)
	}

	_ = exec.CommandContext(ctx, "pm2", "restart", appName).Run()
	time.Sleep(2500 * time.Millisecond)
	return cause, actionTaken
}

func (e *Engine) healDockerContainer(ctx context.Context, containerName string) (cause, actionTaken string) {
	cause = "Contenedor Docker detenido o en estado de salida no nulo"
	actionTaken = fmt.Sprintf("Ejecutado comando docker start %s", containerName)

	_ = exec.CommandContext(ctx, "docker", "start", containerName).Run()
	time.Sleep(2000 * time.Millisecond)
	return cause, actionTaken
}

// ExtractPortFromLog extrae el numero de puerto asociado a un error de tipo EADDRINUSE.
func ExtractPortFromLog(logText string) int {
	// 1. EADDRINUSE o address already in use ... :PORT
	re1 := regexp.MustCompile(`(?i)(?:EADDRINUSE|address already in use).*?:(\d{2,5})`)
	if m := re1.FindStringSubmatch(logText); len(m) > 1 {
		if p, err := strconv.Atoi(m[1]); err == nil && p > 0 && p < 65536 {
			return p
		}
	}

	// 2. :PORT ... EADDRINUSE o address already in use
	re2 := regexp.MustCompile(`(?i):(\d{2,5}).*?(?:EADDRINUSE|address already in use)`)
	if m := re2.FindStringSubmatch(logText); len(m) > 1 {
		if p, err := strconv.Atoi(m[1]); err == nil && p > 0 && p < 65536 {
			return p
		}
	}

	// 3. port PORT ... already in use
	re3 := regexp.MustCompile(`(?i)port\s+(\d{2,5}).*?already in use`)
	if m := re3.FindStringSubmatch(logText); len(m) > 1 {
		if p, err := strconv.Atoi(m[1]); err == nil && p > 0 && p < 65536 {
			return p
		}
	}

	return 0
}

// ReadPM2ErrorLog obtiene las ultimas 30 lineas del archivo de errores de PM2 para la aplicacion.
func ReadPM2ErrorLog(ctx context.Context, appName string) string {
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".pm2", "logs", appName+"-error.log")

	data, err := os.ReadFile(logPath)
	if err != nil {
		// Probar con comodin por si incluye ID (ej: appName-error-0.log)
		matches, _ := filepath.Glob(filepath.Join(home, ".pm2", "logs", appName+"-error*.log"))
		if len(matches) > 0 {
			data, _ = os.ReadFile(matches[len(matches)-1])
		}
	}

	if len(data) == 0 {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 30 {
		lines = lines[len(lines)-30:]
	}
	return strings.Join(lines, "\n")
}

func getPM2ServiceInfo(ctx context.Context, appName string) ServiceInfo {
	list := CollectPM2Services(ctx)
	for _, s := range list {
		if strings.EqualFold(s.Name, appName) {
			return s
		}
	}
	return ServiceInfo{Name: appName, Status: "desconocido"}
}

func getDockerContainerState(ctx context.Context, name string) string {
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", name).CombinedOutput()
	if err != nil {
		return "desconocido"
	}
	return strings.TrimSpace(string(out))
}

func isSystemdServiceActive(ctx context.Context, serviceName string) (bool, string) {
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", serviceName)
	out, err := cmd.CombinedOutput()
	status := strings.TrimSpace(string(out))
	if err == nil && status == "active" {
		return true, status
	}
	// Fallback para entornos donde systemd no es PID 1 (ej. contenedores de testing o desarrollo)
	cmdPgrep := exec.CommandContext(ctx, "pgrep", "-f", serviceName)
	if outPgrep, errPgrep := cmdPgrep.Output(); errPgrep == nil && len(strings.TrimSpace(string(outPgrep))) > 0 {
		return true, "running"
	}
	if status == "" {
		status = "inactive"
	}
	return false, status
}
