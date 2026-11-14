package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"felix-go/internal/agent"
	"felix-go/internal/checkpoint"
	"felix-go/internal/graph"
	"felix-go/internal/harness"
)

type TelemetrySample struct {
	Timestamp time.Time
	CPUPercent float64
	RSSBytes   uint64
	HeapAlloc  uint64
	HeapSys    uint64
	Goroutines int
}

type TelemetryCollector struct {
	stopChan chan struct{}
	wg       sync.WaitGroup
	samples  []TelemetrySample
	mu       sync.Mutex

	peakCPU   float64
	peakRSS   uint64
	peakHeap  uint64
	totalCPU  float64
	sampleCnt int
}

func NewTelemetryCollector() *TelemetryCollector {
	return &TelemetryCollector{
		stopChan: make(chan struct{}),
		samples:  make([]TelemetrySample, 0, 1000),
	}
}

func (tc *TelemetryCollector) Start(interval time.Duration) {
	tc.wg.Add(1)
	go func() {
		defer tc.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var prevProcessTime int64
		var prevTotalTime int64

		prevProcessTime, prevTotalTime = getCPUTimes()

		for {
			select {
			case <-tc.stopChan:
				return
			case <-ticker.C:
				procTime, totTime := getCPUTimes()
				deltaProc := procTime - prevProcessTime
				deltaTot := totTime - prevTotalTime
				cpuPct := 0.0
				if deltaTot > 0 && deltaProc >= 0 {
					cpuPct = (float64(deltaProc) / float64(deltaTot)) * 100.0 * float64(runtime.NumCPU())
				}
				prevProcessTime = procTime
				prevTotalTime = totTime

				rss := getProcessRSS()

				var m runtime.MemStats
				runtime.ReadMemStats(&m)

				tc.mu.Lock()
				sample := TelemetrySample{
					Timestamp:  time.Now(),
					CPUPercent: cpuPct,
					RSSBytes:   rss,
					HeapAlloc:  m.HeapAlloc,
					HeapSys:    m.HeapSys,
					Goroutines: runtime.NumGoroutine(),
				}
				tc.samples = append(tc.samples, sample)
				if cpuPct > tc.peakCPU {
					tc.peakCPU = cpuPct
				}
				if rss > tc.peakRSS {
					tc.peakRSS = rss
				}
				if m.HeapAlloc > tc.peakHeap {
					tc.peakHeap = m.HeapAlloc
				}
				tc.totalCPU += cpuPct
				tc.sampleCnt++
				tc.mu.Unlock()
			}
		}
	}()
}

func (tc *TelemetryCollector) Stop() {
	close(tc.stopChan)
	tc.wg.Wait()
}

func (tc *TelemetryCollector) Stats() (avgCPU float64, peakCPU float64, peakRSSMB float64, peakHeapMB float64, samples int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	avg := 0.0
	if tc.sampleCnt > 0 {
		avg = tc.totalCPU / float64(tc.sampleCnt)
	}
	return avg, tc.peakCPU, float64(tc.peakRSS) / (1024 * 1024), float64(tc.peakHeap) / (1024 * 1024), tc.sampleCnt
}

func getCPUTimes() (int64, int64) {
	// Proceso: utime + stime desde /proc/self/stat
	statData, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, 1
	}
	fields := strings.Fields(string(statData))
	if len(fields) < 15 {
		return 0, 1
	}
	var utime, stime int64
	fmt.Sscanf(fields[13], "%d", &utime)
	fmt.Sscanf(fields[14], "%d", &stime)
	procTotal := utime + stime

	// Sistema total: /proc/stat primera linea
	sysData, err := os.ReadFile("/proc/stat")
	if err != nil {
		return procTotal, 1
	}
	lines := strings.Split(string(sysData), "\n")
	if len(lines) == 0 {
		return procTotal, 1
	}
	cpuFields := strings.Fields(lines[0])
	var totalSys int64
	for i := 1; i < len(cpuFields); i++ {
		var v int64
		fmt.Sscanf(cpuFields[i], "%d", &v)
		totalSys += v
	}
	return procTotal, totalSys
}

func getProcessRSS() uint64 {
	statmData, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(statmData))
	if len(fields) < 2 {
		return 0
	}
	var residentPages uint64
	fmt.Sscanf(fields[1], "%d", &residentPages)
	pageSize := uint64(os.Getpagesize())
	return residentPages * pageSize
}

type mockCompleter struct{}

func (m *mockCompleter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return `### calc.go
<<<<<<< SEARCH
func Add(a, b int) int {
	return 0
}
=======
func Add(a, b int) int {
	return a + b
}
>>>>>>> REPLACE

### validator.go
<<<<<<< SEARCH
=======
package main

func IsValidInput(x int) bool {
	return x >= 0
}
>>>>>>> REPLACE
`, nil
}

func main() {
	fmt.Println("=================================================================")
	fmt.Println(" FELIX-GO: SUITE DE RENDIMIENTO Y TELEMETRIA DE ALTA CARGA")
	fmt.Println(" Componentes evaluados: StateGraph (LangGraph en Go),")
	fmt.Println(" Arnés de Desarrollo (AST/Sandbox/Patcher/Merge) y Execution Gate")
	fmt.Println("=================================================================")
	fmt.Printf("Fecha de ejecucion: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("Arquitectura: %s / %s | CPUs: %d | PageSize: %d bytes\n\n",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), os.Getpagesize())

	// Inicializar telemetria concurrente
	telemetry := NewTelemetryCollector()
	telemetry.Start(30 * time.Millisecond)

	var startMem runtime.MemStats
	runtime.ReadMemStats(&startMem)
	startRSS := getProcessRSS()

	// -------------------------------------------------------------
	// PRUEBA 1: StateGraph (LangGraph) & SQLite WAL Checkpointing
	// -------------------------------------------------------------
	fmt.Println("--- [PRUEBA 1/3] StateGraph: Transiciones y Persistencia WAL ---")
	benchDB := filepath.Join(os.TempDir(), fmt.Sprintf("bench_ckpt_%d.db", time.Now().UnixNano()))
	defer os.Remove(benchDB)
	defer os.Remove(benchDB + "-wal")
	defer os.Remove(benchDB + "-shm")

	store, err := checkpoint.NewSQLiteStore(benchDB)
	if err != nil {
		fmt.Printf("Error inicializando SQLiteStore: %v\n", err)
		return
	}
	defer store.Close()

	g := graph.NewStateGraph("bench_graph")
	g.SetCheckpointStore(store)
	g.SetMaxSteps(100)

	var node1Count int64
	var node2Count int64

	g.AddNode("step_a", func(ctx context.Context, s *graph.State) (*graph.State, error) {
		atomic.AddInt64(&node1Count, 1)
		clone := s.Clone()
		val := clone.GetInt("counter")
		clone.SetVar("counter", val+1)
		return clone, nil
	})
	g.AddNode("step_b", func(ctx context.Context, s *graph.State) (*graph.State, error) {
		atomic.AddInt64(&node2Count, 1)
		clone := s.Clone()
		val := clone.GetInt("counter")
		clone.SetVar("counter", val*2)
		return clone, nil
	})

	g.SetEntryPoint("step_a")
	g.AddEdge("step_a", "step_b")
	g.AddEdge("step_b", graph.END)

	compiledGraph, err := g.Compile()
	if err != nil {
		fmt.Printf("Error compilando grafo: %v\n", err)
		return
	}

	const graphIterations = 500
	t0 := time.Now()
	for i := 0; i < graphIterations; i++ {
		runID := fmt.Sprintf("run-%d", i)
		initialState := graph.NewState(runID)
		initialState.SetVar("counter", i)
		_, runErr := compiledGraph.Run(context.Background(), initialState)
		if runErr != nil {
			fmt.Printf("Fallo en run %d: %v\n", i, runErr)
			return
		}
	}
	graphDuration := time.Since(t0)
	opsPerSec := float64(graphIterations*2) / graphDuration.Seconds()
	avgStepLat := graphDuration / time.Duration(graphIterations*2)

	fmt.Printf("Total ejecuciones de grafos: %d corridas (1.000 transiciones de estado)\n", graphIterations)
	fmt.Printf("Tiempo total: %v | Rendimiento: %.2f pasos/seg | Latencia promedio/paso: %v\n",
		graphDuration, opsPerSec, avgStepLat)

	// Validar integridad de checkpoints en SQLite
	checkpoints, err := store.ListCheckpoints(context.Background(), "run-499")
	if err != nil || len(checkpoints) == 0 {
		fmt.Printf("Error recuperando checkpoints: %v\n", err)
	} else {
		fmt.Printf("Checkpoints registrados exitosamente en SQLite WAL (%d checkpoints para run-499)\n\n", len(checkpoints))
	}

	// -------------------------------------------------------------
	// PRUEBA 2: Arnés de Desarrollo (AST -> Sandbox -> Patch -> Test -> Merge)
	// -------------------------------------------------------------
	fmt.Println("--- [PRUEBA 2/3] Arnés de Desarrollo sobre Proyecto Simulado ---")
	tempRepo := filepath.Join(os.TempDir(), fmt.Sprintf("bench_repo_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempRepo, 0755); err != nil {
		fmt.Printf("Error creando repositorio simulado: %v\n", err)
		return
	}
	defer os.RemoveAll(tempRepo)

	// Crear archivos base del proyecto
	goModContent := "module benchproject\n\ngo 1.24\n"
	_ = os.WriteFile(filepath.Join(tempRepo, "go.mod"), []byte(goModContent), 0644)

	calcCode := `package main

func Add(a, b int) int {
	return 0
}
`
	_ = os.WriteFile(filepath.Join(tempRepo, "calc.go"), []byte(calcCode), 0644)

	calcTestCode := `package main

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, esperado 5", got)
	}
}

func TestValidator(t *testing.T) {
	if !IsValidInput(10) {
		t.Fatalf("IsValidInput(10) debio ser true")
	}
}
`
	_ = os.WriteFile(filepath.Join(tempRepo, "calc_test.go"), []byte(calcTestCode), 0644)

	// Inicializar Git
	cmdInit := exec.Command("git", "init")
	cmdInit.Dir = tempRepo
	_ = cmdInit.Run()

	cmdConfig1 := exec.Command("git", "config", "user.name", "Benchmark Felix")
	cmdConfig1.Dir = tempRepo
	_ = cmdConfig1.Run()

	cmdConfig2 := exec.Command("git", "config", "user.email", "bench@felix.local")
	cmdConfig2.Dir = tempRepo
	_ = cmdConfig2.Run()

	cmdAdd := exec.Command("git", "add", ".")
	cmdAdd.Dir = tempRepo
	_ = cmdAdd.Run()

	cmdCommit := exec.Command("git", "commit", "-m", "chore: baseline initial commit")
	cmdCommit.Dir = tempRepo
	_ = cmdCommit.Run()

	harnessT0 := time.Now()
	devGraph, err := harness.NewDevelopmentGraph(harness.HarnessOptions{
		RepoDir:         tempRepo,
		Completer:       &mockCompleter{},
		CheckpointStore: store,
		MaxRetries:      3,
		AutoMerge:       true,
	})
	if err != nil {
		fmt.Printf("Error construyendo grafo de desarrollo: %v\n", err)
		return
	}

	harnessState := graph.NewState(fmt.Sprintf("bench-harness-%d", time.Now().UnixNano()))
	harnessState.SetVar("task_description", "Implementar la funcion Add y crear el validador con sus pruebas")
	harnessFinalState, err := devGraph.Run(context.Background(), harnessState)
	if err != nil {
		fmt.Printf("Error ejecutando arnés de desarrollo: %v\n", err)
		return
	}
	harnessDuration := time.Since(harnessT0)

	fmt.Printf("Resultado del arnés: Finalizado en nodo '%s' (Pasos ejecutados: %d)\n",
		harnessFinalState.CurrentNode, harnessFinalState.StepNumber)
	fmt.Printf("Tiempo total ciclo arnés: %v\n", harnessDuration)
	fmt.Printf("Archivos modificados/creados (%d):\n", len(harnessFinalState.FilesChanged))
	for _, f := range harnessFinalState.FilesChanged {
		fmt.Printf("  - %s\n", f)
	}

	// Validar que el commit de merge existe en la rama principal
	gitLogCmd := exec.Command("git", "log", "--oneline", "-n", "3")
	gitLogCmd.Dir = tempRepo
	logOut, _ := gitLogCmd.CombinedOutput()
	fmt.Printf("Historial Git resultante:\n%s", string(logOut))

	// Validar ejecucion directa de tests en el repositorio tras el merge
	testCmd := exec.Command("go", "test", "-v", "./...")
	testCmd.Dir = tempRepo
	testOut, testErr := testCmd.CombinedOutput()
	if testErr != nil {
		fmt.Printf("Fallo de pruebas post-merge: %s\n", string(testOut))
	} else {
		fmt.Printf("Verificacion de pruebas en rama principal: PASS 100%%\n\n")
	}

	// -------------------------------------------------------------
	// PRUEBA 3: Compuerta de Ejecución y Sistema de Confirmación Unificada
	// -------------------------------------------------------------
	fmt.Println("--- [PRUEBA 3/3] Execution Gate: Detección y Prevención de Hesitación ---")
	testAffirmations := []string{
		"Ok, mandale", "Okey, hacelo", "dale", "ejecutá todo", "si", "sí, dale",
		"confirmo", "adelante", "hacelo de una vez", "OK, DAME LOS RESULTADOS DE UNA VEZ",
		"probá la suite", "creá el archivo", "inicializá el repo", "compilá el proyecto",
	}
	testHesitations := []string{
		"Para habilitar el sandbox en dicho proyecto, se requiere ejecutar: ```bash ...",
		"El diagnóstico y los ajustes quedaron definidos. Confirmame si procedo con la inicialización",
		"Vos no tenés que hacer nada. ¿Avanzo con la ejecución ahora?",
		"Resultados de la preparación. ¿Disparo la corrida completa ahora?",
		"Aguardo tu confirmación para proceder.",
	}

	const gateIterations = 100000
	gateT0 := time.Now()
	for i := 0; i < gateIterations; i++ {
		affInput := testAffirmations[i%len(testAffirmations)]
		hesReply := testHesitations[i%len(testHesitations)]

		isAff := agent.IsAffirmativeOrExecutionCommand(affInput)
		isHes := agent.IsHesitatingOrAskingConfirmation(hesReply)

		if !isAff || !isHes {
			fmt.Printf("Fallo de deteccion en iteracion %d\n", i)
			return
		}
	}
	gateDuration := time.Since(gateT0)
	gateOpsPerSec := float64(gateIterations*2) / gateDuration.Seconds()
	gateLatencyPerOp := gateDuration / time.Duration(gateIterations*2)

	fmt.Printf("Total evaluaciones semanticas: %d (100.000 pares confirmacion/hesitacion)\n", gateIterations*2)
	fmt.Printf("Tiempo total: %v | Rendimiento: %.2f ops/seg | Latencia: %v por evaluacion\n\n",
		gateDuration, gateOpsPerSec, gateLatencyPerOp)

	// Detener telemetria concurrente y consolidar metricas
	telemetry.Stop()

	var endMem runtime.MemStats
	runtime.ReadMemStats(&endMem)
	endRSS := getProcessRSS()
	avgCPU, peakCPU, peakRSSMB, peakHeapMB, samples := telemetry.Stats()

	fmt.Println("=================================================================")
	fmt.Println(" TELEMETRIA CONSOLIDADA DE RENDIMIENTO (CPU & MEMORIA)")
	fmt.Println("=================================================================")
	fmt.Printf("Muestras de telemetria recolectadas: %d (frecuencia: 30ms)\n", samples)
	fmt.Println("\n[Consumo de CPU]")
	fmt.Printf("  - CPU Promedio: %.2f %%\n", avgCPU)
	fmt.Printf("  - CPU Pico:     %.2f %%\n", peakCPU)

	fmt.Println("\n[Consumo de Memoria RAM]")
	fmt.Printf("  - RSS Inicial:      %.2f MB\n", float64(startRSS)/(1024*1024))
	fmt.Printf("  - RSS Pico:         %.2f MB\n", peakRSSMB)
	fmt.Printf("  - RSS Final:        %.2f MB\n", float64(endRSS)/(1024*1024))
	fmt.Printf("  - Heap Alloc Pico:  %.2f MB\n", peakHeapMB)
	fmt.Printf("  - Heap Alloc Final: %.2f MB\n", float64(endMem.HeapAlloc)/(1024*1024))
	fmt.Printf("  - Heap Sys Total:   %.2f MB\n", float64(endMem.HeapSys)/(1024*1024))
	fmt.Printf("  - Ciclos de GC:     %d\n", endMem.NumGC-startMem.NumGC)

	fmt.Println("\n[Evaluacion de Estabilidad y Desempeno]")
	if peakRSSMB < 60.0 {
		fmt.Printf("  - Footprint de Memoria: EXCELENTE (Pico de %.2f MB en RAM, por debajo del umbral de 60 MB)\n", peakRSSMB)
	} else {
		fmt.Printf("  - Footprint de Memoria: ACEPTABLE (Pico de %.2f MB)\n", peakRSSMB)
	}

	if avgStepLat < 5*time.Millisecond {
		fmt.Printf("  - Latencia StateGraph: ULTRA RAPIDA (%v por transicion con SQLite WAL)\n", avgStepLat)
	}

	if gateLatencyPerOp < 50*time.Microsecond {
		fmt.Printf("  - Latencia Execution Gate: INAPRECIABLE (%v por evaluacion, 0 impacto conversacional)\n", gateLatencyPerOp)
	}
	fmt.Println("=================================================================")
}
