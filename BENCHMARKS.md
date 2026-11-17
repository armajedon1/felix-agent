# Evaluación de Rendimiento y Telemetría de Alta Carga - Félix Agent

Este documento detalla los resultados oficiales de la suite de evaluación de rendimiento (*benchmarks*) y telemetría de hardware concurrente para **Félix Agent**.

Las pruebas fueron ejecutadas utilizando la suite nativa [`cmd/benchmark/main.go`](cmd/benchmark/main.go) sobre la arquitectura de producción, midiendo en paralelo el motor de grafos de estado determinista, el arnés de ingeniería de software con aislamiento en Git y la compuerta de ejecución semántica.

---

## 1. Entorno de Evaluación de Referencia

* **Dispositivo:** Raspberry Pi 5 (Revisión 1.0).
* **Procesador:** Broadcom BCM2712 quad-core Cortex-A76 @ 2.4 GHz (arquitectura aarch64).
* **Memoria RAM:** 8 GB LPDDR4X.
* **Almacenamiento:** Unidad de estado sólido NVMe SSD (operaciones directas de I/O en disco).
* **Sistema Operativo:** Debian GNU/Linux 13 (trixie) / Kernel Linux 6.18 arm64.
* **Runtime:** Go v1.22.x (compilado nativo aarch64 con optimizaciones de compilador).
* **Motor de Base de Datos:** SQLite 3 con modo WAL (`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`).

---

## 2. Resumen Ejecutivo de Rendimiento

| Subsistema Evaluado | Carga / Operaciones | Tiempo Total | Rendimiento | Latencia Promedio |
| :--- | :--- | :--- | :--- | :--- |
| **Motor StateGraph (LangGraph en Go)** | 500 corridas (1.000 transiciones de estado + checkpoints WAL) | `97,90 ms` | **10.213 pasos/seg** | **97,90 µs** / paso |
| **Arnés de Desarrollo (`harness_develop`)** | Ciclo E2E completo (AST -> Sandbox -> Patch -> Test -> Merge) | `1,41 s` | **1 ciclo E2E completo** | **1,41 s** (incluye `go test` real) |
| **Compuerta de Ejecución (*Execution Gate*)** | 200.000 evaluaciones semánticas (100.000 pares confirmación/hesitación) | `642,08 ms` | **311.484 ops/seg** | **3,21 µs** / evaluación |

---

## 3. Desglose Técnico por Subsistema

### A. Motor de Grafos de Estado (`internal/graph` y `internal/checkpoint`)

El motor de grafos orquesta flujos deterministas con soporte para transiciones condicionales y serialización inmutable de estados.

* **Metodología de Prueba:**
  * Se configuró un grafo de 2 nodos con propagación inmutable de estados de diccionario.
  * Se ejecutaron 500 flujos completos consecutivos (1.000 transiciones atómicas).
  * Cada transición guardó un checkpoint serializado en JSON dentro de SQLite bajo el modo WAL (`bench_ckpt.db`).
* **Métricas Obtenidas:**
  * **Tiempo total acumulado:** `97,90 ms`.
  * **Rendimiento de procesamiento:** `10.213 transiciones por segundo`.
  * **Latencia promedio por transición:** `97,90 microsegundos` (incluye serialización de payload e inserción física en SQLite).
  * **Integridad de datos:** Validación al 100% de checkpoints recuperables vía `ListCheckpoints`.

### B. Arnés de Desarrollo Nativo (`internal/harness`) sobre Proyecto Sintético

Evalúa el ciclo de vida completo de ingeniería de software autónoma sin invocar herramientas CLI pesadas externas.

* **Metodología de Prueba:**
  * Se generó un proyecto real sintético en un directorio temporal con `go.mod`, un módulo de cálculo (`calc.go`) y una suite de pruebas unitarias (`calc_test.go`).
  * Se orquestaron 7 nodos del StateGraph del arnés:
    1. `node_analyze`: Extracción sintáctica AST mediante `repomap` para obtener contexto estructurado.
    2. `node_plan`: Definición de la estrategia de modificación de código.
    3. `node_sandbox`: Creación atómica de rama efímera aislada en Git (`felix-sandbox/task-...`).
    4. `node_patch`: Aplicación de bloques `SEARCH/REPLACE` con motor Levenshtein sobre `calc.go` y creación de un nuevo archivo (`validator.go`).
    5. `node_verify`: Ejecución física del ejecutor de pruebas (`go test -v ./...`) con captura y sanitización de salida.
    6. `node_evaluate_hitl`: Verificación de directivas de seguridad.
    7. `node_merge`: Fusión automática de la rama efímera a la rama principal (`main`) y limpieza del sandbox.
* **Métricas Obtenidas:**
  * **Tiempo total del ciclo E2E:** `1,41 segundos`.
  * **Resultado de pruebas:** 100% superadas (`PASS` en 2/2 pruebas).
  * **Persistencia Git:** Commit generado y fusionado limpiamente en el historial de la rama principal.

### C. Compuerta de Ejecución (*Execution Gate* en `internal/agent`)

La compuerta de ejecución intercepta solicitudes y respuestas en el bucle ReAct para asegurar que una confirmación afirmativa del usuario dispare la ejecución inmediata de herramientas sin generar bucles de vacilación ni preguntas redundantes.

* **Metodología de Prueba:**
  * Se evaluaron 200.000 cadenas de entrada combinando patrones afirmativos en español (ej. *"procedé"*, *"dale"*, *"sí hacelo"*, *"adelante"*, *"confirmado"*) y textos de respuesta del modelo con potencial vacilación (ej. *"¿desea que proceda?"*, *"¿está seguro?"*).
  * Se midió la normalización léxica y la detección determinista de intenciones.
* **Métricas Obtenidas:**
  * **Tiempo total:** `642,08 ms`.
  * **Rendimiento:** `311.484 evaluaciones por segundo`.
  * **Latencia promedio:** `3,21 microsegundos por evaluación`.
  * **Impacto computacional:** Prácticamente nulo sobre el hilo conversacional.

---

## 4. Telemetría Concurrente de Hardware (CPU y Memoria RAM)

Durante la ejecución continua de la prueba completa bajo estrés (incluyendo compilación en caliente y tests de Go), un hilo de telemetría independiente muestreó los recursos del sistema cada **30 milisegundos** (80 muestras totales).

```text
=================================================================
 TELEMETRIA CONSOLIDADA DE RENDIMIENTO (CPU Y MEMORIA)
=================================================================
Muestras recolectadas: 80 (frecuencia de muestreo: 30 ms)

[Consumo de CPU]
  - CPU Promedio: 32.43 %
  - CPU Pico:     133.33 % (ráfaga puntual multihilo durante 'go test')

[Consumo de Memoria RAM]
  - RSS Inicial:      9.95 MB
  - RSS Pico:         18.36 MB
  - RSS Final:        18.36 MB
  - Heap Alloc Pico:  3.61 MB
  - Heap Alloc Final: 1.43 MB
  - Heap Sys Total:   7.53 MB
  - Ciclos de GC:     6
=================================================================
```

### Análisis de Recursos:
1. **Huella de Memoria Residente (RSS):**
   * El pico máximo de consumo de RAM del proceso completo durante la compilación, análisis AST, operaciones Git y persistencia SQLite fue de **18,36 MB**.
   * Se sitúa holgadamente por debajo del objetivo de diseño del proyecto (< 35 MB RSS), permitiendo la coexistencia con múltiples servicios en hardware embebido.
2. **Eficiencia del Recolector de Basura (GC):**
   * El heap activo asignado cerró en apenas **1,43 MB** con un sistema de heap total de **7,53 MB**, demostrando una liberación inmediata de estructuras transitorias sin fugas de memoria.
3. **Carga del Procesador (CPU):**
   * El promedio de uso de CPU durante toda la prueba se mantuvo en **32,43%**, con un pico puntual de 133,33% correspondiente al paralelismo de pruebas de Go en los 4 núcleos físicos.

---

## 5. Comparativa de Rendimiento y Recursos Frente a la Competencia

Mediciones comparativas de consumo de recursos, latencia de inicialización y características arquitecturales entre Félix Agent y las principales soluciones de software agéntico:

| Característica / Métrica | Félix Agent | OpenClaw | Antigravity CLI (`agy`) | Claude Code CLI | Frameworks Python (LangGraph / CrewAI) |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Lenguaje y Runtime** | **Go nativo** (binario estático único ~33 MB) | Node.js / TypeScript (V8 runtime + pnpm) | Go / Python / Node (motor híbrido con sidecars) | Node.js / TypeScript (npm / runtime V8) | Python 3.10+ (intérprete + PyPI) |
| **RAM en Reposo (RSS)** | **16 - 25 MB** | 180 - 320 MB | 150 - 350 MB | 220 - 450 MB | 250 - 600 MB |
| **RAM bajo Carga / Compilación** | **18 - 35 MB** (pico medido: 18,36 MB) | 350 - 650 MB | 300 - 800 MB | 450 - 900 MB | 500 - 1.200 MB |
| **Tiempo de Arranque (Cold Start)** | **< 15 ms** | ~1.200 - 2.500 ms | ~350 - 800 ms | ~800 - 1.800 ms | ~1.500 - 3.500 ms |
| **Canales de Mensajería Nativos** | **WhatsApp E2E (WhatsMeow) + Telegram** (simultáneos o independientes) | WhatsApp (gateway Baileys) | Solo terminal CLI / IDE (sin chat móvil directo) | Solo terminal interactiva (CLI) | Ninguno out-of-the-box (requiere armar API/bot) |
| **Arnés SWE Integrado** | **Nativo en binario** (AST `repomap`, fuzzy patcher Levenshtein, Git sandbox efímero, REPL) | Delegación a scripts bash y utilidades externas | Arnés completo de desarrollo con sandboxing | Edición de archivos por búsqueda/reemplazo en CLI | Sin arnés nativo (framework genérico de grafos) |
| **Persistencia y Time-Travel** | **SQLite WAL nativo** con Time-Travel (rollback/resume transaccional) | JSON / SQLite simple | Checkpoints en SQLite de sesión | Historial JSONL en disco | Requiere configurar SQLiteSaver o Postgres |
| **Resiliencia de Inferencia** | **Fast-Failover (20s) + Grace Period desacoplado (35s) + Degradación elegante** | Reintento lineal simple ante errores | Manejo de cuotas y switching de modelos | Reintento sobre endpoint único de Anthropic | Depende de la implementación manual del usuario |
| **Ruta Rápida (Fast-Path)** | **< 5 ms** (telemetría, RAM, disco, PM2 sin coste de tokens) | Interceptores regex básicos | No aplicable (todo consume tokens o slash commands) | Comandos fijos en consola | No disponible out-of-the-box |
| **Idoneidad en Hardware Edge / Embebido** | **Excelente** (ideal para Raspberry Pi, VPS de 512 MB - 2 GB) | Regular (overhead elevado de memoria en placas chicas) | Moderado (diseñado para estaciones de trabajo) | Regular (intensivo para servidores desatendidos) | Baja (consumo masivo de RAM y ciclos de CPU) |

### Observaciones Técnicas sobre la Evaluación Comparativa:
1. **Sobrecarga de Virtual Machine vs. Binario Nativo:** Las plataformas sustentadas en Node.js (OpenClaw, Claude Code) deben cargar el motor V8, el parser de módulos y los árboles de dependencias `node_modules`, elevando la memoria base por encima de 180 MB incluso sin procesar solicitudes. En contraste, el recolector de basura de Go y su compilación estática permiten a Félix operar en el rango de 16-25 MB.
2. **Arnés de Ingeniería en el Espacio del Proceso:** Al integrar el mapeo AST (`go/parser`), el motor de diffs Levenshtein y el aislamiento en Git dentro del binario de Go, Félix no incurre en el sobrecoste de invocar subprocesos externos para cada operación de edición ni sufre por latencias de I/O de procesos hijos pesados.
3. **Resiliencia de Conexión y Fast-Failover:** El desacoplamiento de contexto mediante `context.WithoutCancel` garantiza que una llamada a un LLM de respaldo cuente con un presupuesto de tiempo limpio (35 s), evitando cancelaciones en cascada comunes en agentes que comparten un único context deadline rígido.

---

## 6. Instrucciones para Reproducir la Evaluación

Para ejecutar la suite de rendimiento en cualquier entorno Linux con Go instalado:

### Ejecución Directa
```bash
go run ./cmd/benchmark/main.go
```

### Compilación y Ejecución del Binario
```bash
# Compilar binario optimizado
go build -o bin/benchmark ./cmd/benchmark/main.go

# Ejecutar benchmark y telemetría
./bin/benchmark
```

Los artefactos temporales generados durante la prueba (`bench_repo_*`, `bench_ckpt.db`, `bench_ckpt.db-wal`, `bench_ckpt.db-shm`) se limpian automáticamente al concluir la ejecución.
