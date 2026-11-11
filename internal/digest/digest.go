package digest

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemSnapshot representa el estado consolidado de la infraestructura.
type SystemSnapshot struct {
	CPUTemp    float64
	RAMUsedMB  int
	RAMTotalMB int
	DiskRoot   string
	DiskSSD    string
	PM2Online  int
	PM2Total   int
	PM2Issues  []string
	UpdatedAt  time.Time
}

// Watcher ejecuta un muestreo periodico cada N minutos para mantener un digest fresco en memoria.
type Watcher struct {
	mu       sync.RWMutex
	current  SystemSnapshot
	interval time.Duration
}

// NewWatcher inicializa el recolector de telemetria asincrono.
func NewWatcher(interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	w := &Watcher{
		interval: interval,
	}
	// Primer muestreo inmediato
	w.sample()
	return w
}

// StartRunner inicia el bucle de muestreo en segundo plano.
func (w *Watcher) StartRunner(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.sample()
			}
		}
	}()
}

func (w *Watcher) sample() {
	var snap SystemSnapshot
	snap.UpdatedAt = time.Now()

	// 1. Temperatura CPU
	if tempBytes, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		if raw, err := strconv.Atoi(strings.TrimSpace(string(tempBytes))); err == nil {
			snap.CPUTemp = float64(raw) / 1000.0
		}
	}

	// 2. Memoria RAM
	cmdFree := exec.Command("free", "-m")
	if out, err := cmdFree.Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 3 {
				snap.RAMTotalMB, _ = strconv.Atoi(fields[1])
				snap.RAMUsedMB, _ = strconv.Atoi(fields[2])
			}
		}
	}

	// 3. Almacenamiento
	cmdDf := exec.Command("df", "-h", "/", "/mnt/ssd")
	if out, err := cmdDf.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, l := range lines[1:] {
			f := strings.Fields(l)
			if len(f) >= 6 {
				if f[5] == "/" {
					snap.DiskRoot = fmt.Sprintf("%s disp (uso %s)", f[3], f[4])
				} else if f[5] == "/mnt/ssd" {
					snap.DiskSSD = fmt.Sprintf("%s disp (uso %s)", f[3], f[4])
				}
			}
		}
	}

	// 4. Servicios PM2
	cmdPM2 := exec.Command("pm2", "jlist")
	if out, err := cmdPM2.Output(); err == nil {
		// Analisis rapido de texto sin sobrecargar decodificacion completa
		raw := string(out)
		snap.PM2Total = strings.Count(raw, "\"pm_id\":")
		snap.PM2Online = strings.Count(raw, "\"status\":\"online\"")
		if snap.PM2Total > snap.PM2Online {
			snap.PM2Issues = append(snap.PM2Issues, fmt.Sprintf("%d servicios fuera de linea", snap.PM2Total-snap.PM2Online))
		}
	}

	w.mu.Lock()
	w.current = snap
	w.mu.Unlock()

	log.Printf("[Digest Watcher] Telemetria actualizada: CPU %.1fC | RAM %d/%d MB | PM2 %d/%d online",
		snap.CPUTemp, snap.RAMUsedMB, snap.RAMTotalMB, snap.PM2Online, snap.PM2Total)
}

// GetSnapshot retorna una copia del snapshot mas reciente.
func (w *Watcher) GetSnapshot() SystemSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}

// GetSummaryFormatted produce un bloque conciso de 3 lineas para inyeccion contextual o respuesta inmediata.
func (w *Watcher) GetSummaryFormatted() string {
	snap := w.GetSnapshot()
	if snap.UpdatedAt.IsZero() {
		return "Telemetria de infraestructura no disponible aun."
	}

	loc, _ := time.LoadLocation("America/Buenos_Aires")
	tStr := snap.UpdatedAt.In(loc).Format("15:04")

	var pm2Info string
	if len(snap.PM2Issues) == 0 {
		pm2Info = fmt.Sprintf("%d/%d online (todos OK)", snap.PM2Online, snap.PM2Total)
	} else {
		pm2Info = fmt.Sprintf("%d/%d online (%s)", snap.PM2Online, snap.PM2Total, strings.Join(snap.PM2Issues, ", "))
	}

	return fmt.Sprintf("Homelab Debian (Raspberry Pi) [%s]: CPU %.1f°C | RAM %d/%d MB | Disco: SD /: %s, SSD: %s | PM2: %s",
		tStr, snap.CPUTemp, snap.RAMUsedMB, snap.RAMTotalMB, snap.DiskRoot, snap.DiskSSD, pm2Info)
}
