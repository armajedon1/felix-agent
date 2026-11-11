package fastpath

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"felix-go/internal/digest"
)

// Router intercepta consultas deterministicas frecuentes y responde en < 5ms sin pasar por el LLM.
type Router struct {
	watcher *digest.Watcher
}

func NewRouter(w *digest.Watcher) *Router {
	return &Router{watcher: w}
}

// Route evalua el texto y retorna (handled bool, responseText string).
func (r *Router) Route(rawText string, isAdmin bool) (bool, string) {
	text := strings.TrimSpace(rawText)
	lower := strings.ToLower(text)
	clean := strings.TrimRight(lower, "?!.,:;")

	// 1. Saludos cotidianos
	switch clean {
	case "hola", "buenas", "buen dia", "buenas tardes", "buenas noches", "hey", "hola felix":
		return true, "Hola. En que puedo ayudarte en el servidor hoy?"
	case "gracias", "muchas gracias", "joya", "genial", "excelente", "de diez":
		return true, "A disposicion. Si necesitas algo mas, avisame."
	case "ping":
		return true, "Pong! Felix operativo y respondiendo al instante."
	}

	// 2. Consulta de Hora
	if clean == "que hora es" || clean == "hora" || clean == "hora actual" || clean == "dime la hora" {
		loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
		now := time.Now()
		if err == nil {
			now = now.In(loc)
		}
		return true, fmt.Sprintf("Son las *%s*.", now.Format("15:04:05"))
	}

	// 3. Consulta de Fecha
	if clean == "que dia es hoy" || clean == "fecha" || clean == "fecha de hoy" || clean == "que fecha es" {
		loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
		now := time.Now()
		if err == nil {
			now = now.In(loc)
		}
		dias := []string{"domingo", "lunes", "martes", "miercoles", "jueves", "viernes", "sabado"}
		meses := []string{"", "enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
		diaSem := dias[now.Weekday()]
		mes := meses[now.Month()]
		return true, fmt.Sprintf("Hoy es *%s %d de %s de %d*.", diaSem, now.Day(), mes, now.Year())
	}

	// 4. Telemetria de CPU / Hardware
	if r.watcher != nil && (clean == "temperatura" || clean == "temp" || clean == "temperatura cpu") {
		snap := r.watcher.GetSnapshot()
		if snap.CPUTemp > 0 {
			return true, fmt.Sprintf("Temperatura actual de la CPU: *%.1f°C*", snap.CPUTemp)
		}
	}

	// 5. Operaciones de Administracion del Sistema Frecuentes
	if isAdmin {
		switch clean {
		case "docker ps", "estado docker", "contenedores":
			cmd := exec.Command("docker", "ps", "--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return true, fmt.Sprintf("Error consultando Docker: %v", err)
			}
			return true, fmt.Sprintf("*CONTENEDORES DOCKER (Fast-Path)*\n```\n%s```", strings.TrimSpace(string(out)))

		case "memoria", "ram", "memoria ram":
			cmd := exec.Command("free", "-h")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return true, fmt.Sprintf("Error consultando memoria: %v", err)
			}
			return true, fmt.Sprintf("*MEMORIA DEL SERVIDOR (Fast-Path)*\n```\n%s```", strings.TrimSpace(string(out)))

		case "espacio", "disco", "almacenamiento":
			cmd := exec.Command("df", "-h", "/")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return true, fmt.Sprintf("Error consultando almacenamiento: %v", err)
			}
			return true, fmt.Sprintf("*ALMACENAMIENTO (Fast-Path)*\n```\n%s```", strings.TrimSpace(string(out)))

		case "uptime", "tiempo activo", "tiempo encendido":
			cmd := exec.Command("uptime", "-p")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return true, fmt.Sprintf("Error consultando uptime: %v", err)
			}
			return true, fmt.Sprintf("Tiempo activo del servidor: *%s*", strings.TrimSpace(string(out)))

		case "ip", "ips", "mi ip", "tailscale ip", "ip tailscale", "red":
			var lines []string
			if out, err := exec.Command("tailscale", "ip", "-4").CombinedOutput(); err == nil && len(out) > 0 {
				lines = append(lines, fmt.Sprintf("- Tailscale: `%s`", strings.TrimSpace(string(out))))
			}
			if ifaces, err := net.Interfaces(); err == nil {
				for _, iface := range ifaces {
					if (iface.Flags&net.FlagUp) != 0 && (iface.Flags&net.FlagLoopback) == 0 {
						addrs, _ := iface.Addrs()
						for _, a := range addrs {
							if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
								if ipnet.IP.To4() != nil {
									lines = append(lines, fmt.Sprintf("- Interfaz %s: `%s`", iface.Name, ipnet.IP.String()))
								}
							}
						}
					}
				}
			}
			if len(lines) == 0 {
				lines = append(lines, "- No se detectaron interfaces de red activas.")
			}
			return true, fmt.Sprintf("*DIRECCIONES IP DEL SERVIDOR (Fast-Path)*\n%s", strings.Join(lines, "\n"))

		case "procesos", "pm2 status", "estado pm2", "servicios":
			cmd := exec.Command("pm2", "status")
			out, err := cmd.CombinedOutput()
			if err != nil {
				cmdSys := exec.Command("systemctl", "list-units", "--type=service", "--state=running", "--no-pager", "--no-legend")
				outSys, errSys := cmdSys.CombinedOutput()
				if errSys != nil {
					return true, fmt.Sprintf("Error consultando servicios (PM2/Systemd): %v", err)
				}
				lines := strings.Split(string(outSys), "\n")
				if len(lines) > 20 {
					lines = lines[:20]
				}
				return true, fmt.Sprintf("*SERVICIOS SYSTEMD ACTIVOS (Fast-Path)*\n```\n%s```", strings.Join(lines, "\n"))
			}
			return true, fmt.Sprintf("*ESTADO DE PROCESOS PM2 (Fast-Path)*\n```\n%s```", strings.TrimSpace(string(out)))
		}
	}

	return false, ""
}
