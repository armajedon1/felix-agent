package healer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ServiceInfo describe el estado de un proceso PM2.
type ServiceInfo struct {
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	PID      int     `json:"pid"`
	RAMMB    float64 `json:"ram_mb"`
	Uptime   string  `json:"uptime"`
	Restarts int     `json:"restarts"`
}

// ContainerInfo describe el estado de un contenedor Docker.
type ContainerInfo struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
	State  string `json:"state"`
}

// SystemMetrics contiene metricas clave de hardware del homelab.
type SystemMetrics struct {
	RAMUsedMB  int    `json:"ram_used_mb"`
	RAMFreeMB  int    `json:"ram_free_mb"`
	RAMTotalMB int    `json:"ram_total_mb"`
	DiskFree   string `json:"disk_free"`
	DiskUsed   string `json:"disk_used"`
	DiskTotal  string `json:"disk_total"`
	CPUTemp    string `json:"cpu_temp"`
	LoadAvg    string `json:"load_avg"`
}

// Snapshot representa una instantanea del estado de la infraestructura en un momento dado.
type Snapshot struct {
	ID               int64           `json:"id"`
	Timestamp        time.Time       `json:"timestamp"`
	TCPPorts         []int           `json:"tcp_ports"`
	PM2Services      []ServiceInfo   `json:"pm2_services"`
	DockerContainers []ContainerInfo `json:"docker_containers"`
	Metrics          SystemMetrics   `json:"metrics"`
}

// SnapshotManager gestiona la persistencia y comparacion de snapshots en SQLite.
type SnapshotManager struct {
	dbPath string
	db     *sql.DB
}

// NewSnapshotManager crea o abre la base de datos de snapshots en dbPath.
func NewSnapshotManager(dbPath string) (*SnapshotManager, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		return nil, fmt.Errorf("error abriendo DB de snapshots: %w", err)
	}

	query := `CREATE TABLE IF NOT EXISTS infra_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		tcp_ports TEXT NOT NULL,
		pm2_services TEXT NOT NULL,
		docker_containers TEXT NOT NULL,
		metrics TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(query); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("error creando tabla infra_snapshots: %w", err)
	}

	return &SnapshotManager{
		dbPath: dbPath,
		db:     db,
	}, nil
}

// Close cierra la conexion a la base de datos.
func (sm *SnapshotManager) Close() error {
	if sm.db != nil {
		return sm.db.Close()
	}
	return nil
}

// TakeSnapshot recolecta metricas en vivo y persiste el snapshot en la base de datos.
func (sm *SnapshotManager) TakeSnapshot(ctx context.Context) (*Snapshot, error) {
	snap := &Snapshot{
		Timestamp:        time.Now(),
		TCPPorts:         CollectTCPPorts(ctx),
		PM2Services:      CollectPM2Services(ctx),
		DockerContainers: CollectDockerContainers(ctx),
		Metrics:          CollectSystemMetrics(ctx),
	}

	if err := sm.SaveSnapshot(snap); err != nil {
		return nil, err
	}

	return snap, nil
}

// SaveSnapshot persiste el snapshot en la tabla infra_snapshots.
func (sm *SnapshotManager) SaveSnapshot(snap *Snapshot) error {
	portsJSON, err := json.Marshal(snap.TCPPorts)
	if err != nil {
		return err
	}
	pm2JSON, err := json.Marshal(snap.PM2Services)
	if err != nil {
		return err
	}
	dockerJSON, err := json.Marshal(snap.DockerContainers)
	if err != nil {
		return err
	}
	metricsJSON, err := json.Marshal(snap.Metrics)
	if err != nil {
		return err
	}

	query := `INSERT INTO infra_snapshots (timestamp, tcp_ports, pm2_services, docker_containers, metrics)
		VALUES (?, ?, ?, ?, ?);`
	res, err := sm.db.Exec(query, snap.Timestamp.Format("2006-01-02 15:04:05"),
		string(portsJSON), string(pm2JSON), string(dockerJSON), string(metricsJSON))
	if err != nil {
		return fmt.Errorf("error insertando snapshot: %w", err)
	}

	id, err := res.LastInsertId()
	if err == nil {
		snap.ID = id
	}

	return nil
}

// GetLatestSnapshot obtiene el ultimo snapshot guardado en disco.
func (sm *SnapshotManager) GetLatestSnapshot(ctx context.Context) (*Snapshot, error) {
	query := `SELECT id, timestamp, tcp_ports, pm2_services, docker_containers, metrics
		FROM infra_snapshots ORDER BY id DESC LIMIT 1;`
	row := sm.db.QueryRowContext(ctx, query)

	var id int64
	var tsStr, portsStr, pm2Str, dockerStr, metricsStr string
	if err := row.Scan(&id, &tsStr, &portsStr, &pm2Str, &dockerStr, &metricsStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("error escaneando ultimo snapshot: %w", err)
	}

	ts, _ := time.Parse("2006-01-02 15:04:05", tsStr)

	var ports []int
	_ = json.Unmarshal([]byte(portsStr), &ports)

	var pm2List []ServiceInfo
	_ = json.Unmarshal([]byte(pm2Str), &pm2List)

	var dockerList []ContainerInfo
	_ = json.Unmarshal([]byte(dockerStr), &dockerList)

	var metrics SystemMetrics
	_ = json.Unmarshal([]byte(metricsStr), &metrics)

	return &Snapshot{
		ID:               id,
		Timestamp:        ts,
		TCPPorts:         ports,
		PM2Services:      pm2List,
		DockerContainers: dockerList,
		Metrics:          metrics,
	}, nil
}

// CompareSnapshots genera una comparacion textual clara entre dos snapshots.
func CompareSnapshots(older, newer *Snapshot) string {
	if older == nil || newer == nil {
		return "No es posible comparar: uno de los snapshots no esta disponible."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*DIFERENCIAS DE INFRAESTRUCTURA HOMELAB*\n"+
		"- Snapshot base: #%d (%s)\n"+
		"- Snapshot actual: #%d (%s)\n\n",
		older.ID, older.Timestamp.Format("02/01 15:04:05"),
		newer.ID, newer.Timestamp.Format("02/01 15:04:05")))

	hasChanges := false

	// 1. Diferencias en servicios PM2
	olderPM2 := make(map[string]ServiceInfo)
	for _, s := range older.PM2Services {
		olderPM2[s.Name] = s
	}
	newerPM2 := make(map[string]ServiceInfo)
	for _, s := range newer.PM2Services {
		newerPM2[s.Name] = s
	}

	var pm2Changes []string
	for name, nInfo := range newerPM2 {
		oInfo, exists := olderPM2[name]
		if !exists {
			pm2Changes = append(pm2Changes, fmt.Sprintf("- [+] Nuevo servicio PM2: *%s* (Estado: %s, PID: %d)", name, nInfo.Status, nInfo.PID))
			hasChanges = true
		} else {
			if oInfo.Status != nInfo.Status {
				pm2Changes = append(pm2Changes, fmt.Sprintf("- [~] Cambio estado PM2 en *%s*: %s -> *%s*", name, oInfo.Status, nInfo.Status))
				hasChanges = true
			}
			if nInfo.Restarts > oInfo.Restarts {
				pm2Changes = append(pm2Changes, fmt.Sprintf("- [~] Reinicios en PM2 *%s*: %d -> *%d*", name, oInfo.Restarts, nInfo.Restarts))
				hasChanges = true
			}
		}
	}
	for name, oInfo := range olderPM2 {
		if _, exists := newerPM2[name]; !exists {
			pm2Changes = append(pm2Changes, fmt.Sprintf("- [-] Servicio PM2 detenido o removido: *%s* (era %s)", name, oInfo.Status))
			hasChanges = true
		}
	}
	if len(pm2Changes) > 0 {
		sb.WriteString("*Servicios PM2:*\n")
		sb.WriteString(strings.Join(pm2Changes, "\n") + "\n\n")
	}

	// 2. Diferencias en contenedores Docker
	olderDocker := make(map[string]ContainerInfo)
	for _, c := range older.DockerContainers {
		olderDocker[c.Name] = c
	}
	newerDocker := make(map[string]ContainerInfo)
	for _, c := range newer.DockerContainers {
		newerDocker[c.Name] = c
	}

	var dockerChanges []string
	for name, nInfo := range newerDocker {
		oInfo, exists := olderDocker[name]
		if !exists {
			dockerChanges = append(dockerChanges, fmt.Sprintf("- [+] Nuevo contenedor Docker: *%s* (Estado: %s)", name, nInfo.State))
			hasChanges = true
		} else if oInfo.State != nInfo.State {
			dockerChanges = append(dockerChanges, fmt.Sprintf("- [~] Cambio estado Docker en *%s*: %s -> *%s*", name, oInfo.State, nInfo.State))
			hasChanges = true
		}
	}
	for name, oInfo := range olderDocker {
		if _, exists := newerDocker[name]; !exists {
			dockerChanges = append(dockerChanges, fmt.Sprintf("- [-] Contenedor Docker detenido o removido: *%s* (era %s)", name, oInfo.State))
			hasChanges = true
		}
	}
	if len(dockerChanges) > 0 {
		sb.WriteString("*Contenedores Docker:*\n")
		sb.WriteString(strings.Join(dockerChanges, "\n") + "\n\n")
	}

	// 3. Diferencias en puertos TCP
	olderPorts := make(map[int]bool)
	for _, p := range older.TCPPorts {
		olderPorts[p] = true
	}
	newerPorts := make(map[int]bool)
	for _, p := range newer.TCPPorts {
		newerPorts[p] = true
	}

	var portsOpened []int
	for p := range newerPorts {
		if !olderPorts[p] {
			portsOpened = append(portsOpened, p)
		}
	}
	sort.Ints(portsOpened)

	var portsClosed []int
	for p := range olderPorts {
		if !newerPorts[p] {
			portsClosed = append(portsClosed, p)
		}
	}
	sort.Ints(portsClosed)

	if len(portsOpened) > 0 || len(portsClosed) > 0 {
		sb.WriteString("*Puertos TCP:*\n")
		if len(portsOpened) > 0 {
			sb.WriteString(fmt.Sprintf("- [+] Puertos abiertos recientemente: %v\n", portsOpened))
			hasChanges = true
		}
		if len(portsClosed) > 0 {
			sb.WriteString(fmt.Sprintf("- [-] Puertos cerrados recientemente: %v\n", portsClosed))
			hasChanges = true
		}
		sb.WriteString("\n")
	}

	// 4. Metricas del sistema
	ramDiff := newer.Metrics.RAMUsedMB - older.Metrics.RAMUsedMB
	sb.WriteString("*Metricas Generales del Sistema:*\n")
	sb.WriteString(fmt.Sprintf("- Memoria RAM en uso: %d MB (%+d MB)\n", newer.Metrics.RAMUsedMB, ramDiff))
	sb.WriteString(fmt.Sprintf("- Almacenamiento SSD libre: %s (anterior: %s)\n", newer.Metrics.DiskFree, older.Metrics.DiskFree))
	if newer.Metrics.CPUTemp != "" {
		sb.WriteString(fmt.Sprintf("- CPU Temp: %s (anterior: %s)\n", newer.Metrics.CPUTemp, older.Metrics.CPUTemp))
	}

	if !hasChanges {
		sb.WriteString("\n*Resultado:* Todos los servicios PM2, contenedores y puertos se mantienen estables sin anomalias.")
	}

	return strings.TrimSpace(sb.String())
}

// CollectTCPPorts analiza los puertos TCP en escucha mediante ss.
func CollectTCPPorts(ctx context.Context) []int {
	out, err := exec.CommandContext(ctx, "ss", "-tln").Output()
	if err != nil {
		return nil
	}

	portMap := make(map[int]bool)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "LISTEN" {
			localAddr := fields[3]
			idx := strings.LastIndex(localAddr, ":")
			if idx != -1 && idx < len(localAddr)-1 {
				portStr := localAddr[idx+1:]
				if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
					portMap[p] = true
				}
			}
		}
	}

	var ports []int
	for p := range portMap {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

// CollectPM2Services obtiene la lista completa de aplicaciones administradas por PM2.
func CollectPM2Services(ctx context.Context) []ServiceInfo {
	out, err := exec.CommandContext(ctx, "pm2", "jlist").Output()
	if err != nil {
		return nil
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
		return nil
	}

	var result []ServiceInfo
	for _, it := range items {
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
		result = append(result, ServiceInfo{
			Name:     it.Name,
			Status:   strings.ToLower(it.PM2Env.Status),
			PID:      it.PID,
			RAMMB:    it.Monit.Memory / (1024 * 1024),
			Uptime:   upStr,
			Restarts: it.PM2Env.RestartTime,
		})
	}
	return result
}

// CollectDockerContainers obtiene la lista de contenedores Docker en el host.
func CollectDockerContainers(ctx context.Context) []ContainerInfo {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.State}}").Output()
	if err != nil {
		return nil
	}

	var result []ContainerInfo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			result = append(result, ContainerInfo{
				Name:   parts[0],
				Image:  parts[1],
				Status: parts[2],
				State:  strings.ToLower(parts[3]),
			})
		}
	}
	return result
}

// CollectSystemMetrics recopila metricas de memoria, disco y temperatura.
func CollectSystemMetrics(ctx context.Context) SystemMetrics {
	var metrics SystemMetrics

	// RAM
	if out, err := exec.CommandContext(ctx, "free", "-m").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Mem:") {
				f := strings.Fields(line)
				if len(f) >= 7 {
					total, _ := strconv.Atoi(f[1])
					used, _ := strconv.Atoi(f[2])
					free, _ := strconv.Atoi(f[6])
					metrics.RAMTotalMB = total
					metrics.RAMUsedMB = used
					metrics.RAMFreeMB = free
				}
				break
			}
		}
	}

	// Disco SSD (/mnt/ssd)
	if out, err := exec.CommandContext(ctx, "df", "-h", "/mnt/ssd").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) >= 2 {
			f := strings.Fields(lines[1])
			if len(f) >= 5 {
				metrics.DiskTotal = f[1]
				metrics.DiskUsed = f[2]
				metrics.DiskFree = f[3]
			}
		}
	}

	// Temp CPU
	if b, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		t := strings.TrimSpace(string(b))
		var m int
		if _, err := fmt.Sscanf(t, "%d", &m); err == nil {
			metrics.CPUTemp = fmt.Sprintf("%.1f °C", float64(m)/1000.0)
		}
	}

	// Load Avg
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(b))
		if len(parts) >= 3 {
			metrics.LoadAvg = fmt.Sprintf("%s, %s, %s", parts[0], parts[1], parts[2])
		}
	}

	return metrics
}
