package healer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotManager_SaveAndGetLatest(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_snapshots.db")

	sm, err := NewSnapshotManager(dbPath)
	if err != nil {
		t.Fatalf("error creando SnapshotManager: %v", err)
	}
	defer sm.Close()

	ctx := context.Background()

	// 1. Base vacia retorna nil
	latest, err := sm.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetLatestSnapshot fallo en DB vacia: %v", err)
	}
	if latest != nil {
		t.Errorf("Se esperaba nil en DB vacia, obtenido: %+v", latest)
	}

	// 2. Guardar snapshot
	snap := &Snapshot{
		Timestamp: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
		TCPPorts:  []int{22, 80, 443, 25575},
		PM2Services: []ServiceInfo{
			{Name: "web-service", Status: "online", PID: 100, RAMMB: 75.5, Uptime: "2d", Restarts: 1},
			{Name: "minecraft", Status: "online", PID: 200, RAMMB: 1400.0, Uptime: "1d", Restarts: 0},
		},
		DockerContainers: []ContainerInfo{
			{Name: "vaultwarden-1", Image: "vaultwarden/server", Status: "Up 3 days", State: "running"},
		},
		Metrics: SystemMetrics{
			RAMUsedMB:  3000,
			RAMFreeMB:  5000,
			RAMTotalMB: 8000,
			DiskFree:   "45G",
			DiskUsed:   "50G",
			DiskTotal:  "100G",
			CPUTemp:    "48.5 °C",
			LoadAvg:    "0.15, 0.20, 0.18",
		},
	}

	if err := sm.SaveSnapshot(snap); err != nil {
		t.Fatalf("SaveSnapshot fallo: %v", err)
	}

	if snap.ID <= 0 {
		t.Errorf("Se esperaba ID positivo para snapshot guardado, obtenido %d", snap.ID)
	}

	// 3. Recuperar y verificar
	retrieved, err := sm.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetLatestSnapshot fallo: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Se esperaba snapshot recuperado no nulo")
	}

	if len(retrieved.TCPPorts) != 4 {
		t.Errorf("Se esperaban 4 puertos, obtenidos %d", len(retrieved.TCPPorts))
	}
	if len(retrieved.PM2Services) != 2 {
		t.Errorf("Se esperaban 2 servicios PM2, obtenidos %d", len(retrieved.PM2Services))
	}
	if len(retrieved.DockerContainers) != 1 {
		t.Errorf("Se esperaba 1 contenedor Docker, obtenidos %d", len(retrieved.DockerContainers))
	}
	if retrieved.Metrics.RAMUsedMB != 3000 {
		t.Errorf("RAMUsedMB esperado 3000, obtenido %d", retrieved.Metrics.RAMUsedMB)
	}
}

func TestCompareSnapshots_Differences(t *testing.T) {
	t1 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	older := &Snapshot{
		ID:        1,
		Timestamp: t1,
		TCPPorts:  []int{22, 80, 8080},
		PM2Services: []ServiceInfo{
			{Name: "app-a", Status: "online", PID: 10, Restarts: 0},
			{Name: "app-b", Status: "online", PID: 20, Restarts: 2},
		},
		DockerContainers: []ContainerInfo{
			{Name: "cont-x", State: "running"},
		},
		Metrics: SystemMetrics{
			RAMUsedMB: 2000,
			DiskFree:  "50G",
			CPUTemp:   "45.0 °C",
		},
	}

	newer := &Snapshot{
		ID:        2,
		Timestamp: t2,
		TCPPorts:  []int{22, 80, 25575}, // 8080 cerrado, 25575 abierto
		PM2Services: []ServiceInfo{
			{Name: "app-a", Status: "errored", PID: 0, Restarts: 5}, // Caido y mas reinicios
			{Name: "app-b", Status: "online", PID: 20, Restarts: 2},
			{Name: "app-c", Status: "online", PID: 30, Restarts: 0}, // Nuevo
		},
		DockerContainers: []ContainerInfo{
			{Name: "cont-x", State: "exited"}, // Cambio a exited
		},
		Metrics: SystemMetrics{
			RAMUsedMB: 2500, // +500MB
			DiskFree:  "48G",
			CPUTemp:   "49.2 °C",
		},
	}

	diff := CompareSnapshots(older, newer)

	expectedSnippets := []string{
		"DIFERENCIAS DE INFRAESTRUCTURA",
		"Snapshot base: #1",
		"Snapshot actual: #2",
		"Nuevo servicio PM2: *app-c*",
		"Cambio estado PM2 en *app-a*: online -> *errored*",
		"Reinicios en PM2 *app-a*: 0 -> *5*",
		"Cambio estado Docker en *cont-x*: running -> *exited*",
		"Puertos abiertos recientemente: [25575]",
		"Puertos cerrados recientemente: [8080]",
		"Memoria RAM en uso: 2500 MB (+500 MB)",
		"48G",
	}

	for _, s := range expectedSnippets {
		if !strings.Contains(diff, s) {
			t.Errorf("CompareSnapshots deberia contener %q. Resultado:\n%s", s, diff)
		}
	}
}

func TestCompareSnapshots_NoDifferences(t *testing.T) {
	t1 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 11, 9, 10, 15, 0, 0, time.UTC)

	older := &Snapshot{
		ID:        10,
		Timestamp: t1,
		TCPPorts:  []int{22, 80},
		PM2Services: []ServiceInfo{
			{Name: "sitio_web", Status: "online", Restarts: 1},
		},
		DockerContainers: []ContainerInfo{
			{Name: "vaultwarden", State: "running"},
		},
		Metrics: SystemMetrics{
			RAMUsedMB: 1500,
			DiskFree:  "60G",
		},
	}

	newer := &Snapshot{
		ID:        11,
		Timestamp: t2,
		TCPPorts:  []int{22, 80},
		PM2Services: []ServiceInfo{
			{Name: "sitio_web", Status: "online", Restarts: 1},
		},
		DockerContainers: []ContainerInfo{
			{Name: "vaultwarden", State: "running"},
		},
		Metrics: SystemMetrics{
			RAMUsedMB: 1500,
			DiskFree:  "60G",
		},
	}

	diff := CompareSnapshots(older, newer)
	if !strings.Contains(diff, "se mantienen estables sin anomalias") {
		t.Errorf("CompareSnapshots deberia indicar estabilidad. Resultado:\n%s", diff)
	}
}
