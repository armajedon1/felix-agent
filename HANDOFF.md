# Documento de Arquitectura y Traspaso Técnico (Félix Open Source)

## 1. Visión General del Sistema
Félix es una plataforma integral de agente de inteligencia artificial autónomo desarrollada en **Go** para administración de infraestructura Linux, monitoreo y desarrollo de software de alta fidelidad a través de **WhatsApp** y **Telegram**.

El agente combina comunicación nativa E2E mediante WhatsMeow y Telegram Bot API con un motor de grafos de estado determinista (LangGraph-style), un arnés nativo de ingeniería de software (AST repomap + fuzzy search/replace + sandbox Git), persistencia transaccional con Time-Travel en SQLite WAL, ejecución paralela de 39 herramientas de sistema, gestión de subagentes especializados y compatibilidad con el estándar Model Context Protocol (MCP).

---

## 2. Componentes Arquitectónicos

### 2.1. Arquitectura Omnicanal (WhatsApp y Telegram)
* **Implementación:** `internal/channel/`
* **Interfaz Channel:** Contrato desacoplado (`Name`, `Type`, `Start`, `Stop`, `Send`, `IsConnected`) para cualquier capa de transporte.
* **Canal WhatsApp (`internal/channel/whatsapp`):** WhatsMeow (`go.mau.fi/whatsmeow`) sobre WebSockets nativos y cifrado Signal Protocol con renderizado QR interactivo en terminal (`github.com/mdp/qrterminal/v3`) y fallback automático de resolución de sesiones históricas.
* **Canal Telegram (`internal/channel/telegram`):** Conector ultraligero implementado directamente sobre la Telegram Bot API oficial con long-polling HTTP, soporte de mensajes Markdown (con fallback automático a texto plano), notas de voz Opus, fotos y documentos adjuntos sin librerías externas pesadas.
* **Activación Condicional:** Cada canal se habilita o deshabilita mediante booleanos (`whatsapp_enabled`, `telegram_enabled`) en `config.json`.
* **Pipeline Multimodal Estandarizado (`internal/channel/multimodal.go`):** Procesa de forma común notas de voz y PTT (transcripción automática con Groq Whisper o API OpenAI), imágenes (inspección visual) y documentos adjuntos de cualquier canal antes de enviarlos al razonamiento agéntico.
* **Autodetección de Administrador:** El primer usuario en comunicarse con Félix desde cualquiera de los canales habilitados se registra como Administrador Principal.

### 2.2. Motor de Grafos de Estado Determinista
* **Implementación:** `internal/graph/`
* **Topología:** Soporte para grafos dirigidos acíclicos (DAG) y cíclicos con control de pasos máximos (`maxSteps`).
* **Nodos y Aristas:** Nodos atómicos (`NodeFunc`) y aristas fijas o condicionales (`EdgeRouter`) que bifurcan el flujo evaluando el estado (`State`).
* **Inmutabilidad de Estado (`state.go`):** Copia profunda del estado (`Clone()`) en cada paso de transición para garantizar aislamiento y consistencia.
* **Suspensión Human-in-the-Loop:** Capacidad para suspender tareas (`ErrSuspend`) ante operaciones críticas esperando aprobación del usuario sin bloquear hilos del sistema.

### 2.3. Persistencia Transaccional y Time-Travel (Checkpoints)
* **Implementación:** `internal/checkpoint/`
* **Base de Datos SQLite en Modo WAL (`sqlite_store.go`):** Tablas `graph_runs` y `graph_checkpoints` para registrar snapshots inmutables por cada paso ejecutado.
* **Recuperación ante Fallos:** Si el proceso se interrumpe abruptamente (`kill -9`), el grafo reanuda exactamente desde el último checkpoint completado con `Resume()`.
* **Time-Travel y Rollback:** Capacidad para retroceder el estado a cualquier paso previo (`Rollback()`), descartando código defectuoso y liberando memoria.

### 2.4. Arnés de Desarrollo Nativo en Go
* **Implementación:** `internal/harness/` y `internal/tools/harness_tool.go`
* **Mapeador Sintáctico AST (`internal/harness/repomap`):** Extracción de firmas de métodos, funciones, estructuras e interfaces exportadas con `go/parser` (más analizadores para Python y TypeScript) entregando un mapa compacto de menos de 1.500 tokens.
* **Motor de Diff y Fuzzy Search/Replace (`internal/harness/patcher`):** Sustitución basada en bloques `SEARCH/REPLACE` con algoritmo de distancia de Levenshtein para tolerancia a discrepancias de sangría y espacios (umbral > 90%) y escrituras atómicas en disco.
* **Sandbox Git Aislado (`internal/harness/sandbox`):** Creación automática de ramas efímeras `felix-sandbox/task-<id>`, commits atómicos por paso y fusión controlada (`MergeToMain`) o descarte total (`Discard`) si las pruebas fallan.
* **Bucle REPL de Compilación y Linter (`internal/harness/runner`):** Detección automática de proyectos (Go, Rust, Node, Python), ejecución de pruebas con timeout y sanitización de `stderr` para aislar causas raíz sin desbordar el contexto.
* **Plantillas Maestras Embebidas (`internal/harness/prompts`):** Prompts de ingeniería, reglas de diff y plantillas de corrección embebidas en el binario estático mediante `//go:embed`.

### 2.5. Gateway de Inferencia Integrado y Rotación de Claves
* **Implementación:** `internal/inference/gateway.go` y `internal/inference/provider.go`
* **Servidor Proxy OpenAI-Compatible:** Expone `/v1/chat/completions` y `/v1/models` en el puerto HTTP interno (`8055`).
* **Pool de Claves (KeyPool):** Gestión concurrente de claves con rotación automática inmediata ante códigos HTTP 429 (Rate Limit o cuota agotada).
* **Presets de Fábrica:** Gemini (`gemini-2.5-flash`), OpenAI (`gpt-4o`), Groq (`llama-3.3-70b-versatile` + Whisper), Ollama (`llama3`), Antigravity Tools (`http://127.0.0.1:8045/v1`).

### 2.6. Motor Agéntico (ReAct Loop)
* **Implementación:** `internal/agent/engine.go`
* **Capacidades:**
  * Invocación concurrente de herramientas mediante `sync.WaitGroup` cuando el modelo emite múltiples tool calls simultáneas.
  * Blindaje con `defer recover()` en el despachador de herramientas para garantizar tolerancia a panics de herramientas externas.
  * Reporte de progreso en tiempo real para operaciones extensas (navegación web, subagentes, descargas).
  * Tolerancia a fallos: conmutación automática de inferencia (failover a Groq/OpenAI si el endpoint primario se degrada).
  * Truncamiento inteligente de salidas excesivas para no saturar la ventana de contexto.

### 2.7. Orquestación de Subagentes Concurrentes
* **Implementación:** `internal/agent/subagent.go` y `internal/tools/subagent_tool.go`
* **Arquetipos Especializados:**
  * `researcher`: Análisis web, investigación de documentación y síntesis.
  * `coder`: Escritura de código, resolución de bugs, refactorización y pruebas.
  * `sysadmin`: Diagnóstico de servidores, contenedores Docker, redes y logs.
  * `auditor`: Revisión de seguridad, detección de vulnerabilidades y buenas prácticas.
  * `general`: Tareas multipropósito.
* **Semáforo de Concurrencia:** Límite de 3 subagentes paralelos con encolado de tareas excedentes en estado `pending`.
* **Reportes Persistentes:** Al finalizar, cada subagente genera un archivo Markdown detallado en `workspace/reports/<id>_<slug>.md` y despacha un resumen ejecutivo al chat.

### 2.8. Catálogo de 39 Herramientas Nativas (Tool Registry)
* **Implementación:** `internal/tools/`
* **Herramientas Destacadas:**
  * `harness_develop`: Desarrollo de software autónomo mediante StateGraph y LLM Completer.
  * `git_tool`: Gestión de repositorios Git locales.
  * `docker`: Administración de contenedores e imágenes Docker.
  * `sqlite_inspector` y `sqlite_query`: Inspección analítica de bases de datos.
  * `exec` y `bash`: Ejecución de comandos seguros con truncamiento.
  * `browser`: Automatización e interacción web headless con Rod.
  * `doc_engine`: Análisis de documentos y texto enriquecido.
  * `net_sentinel`: Diagnóstico de interfaces de red y puertos.
  * `healer`: Auto-curación de servicios degradados.

### 2.9. Persistencia y Almacenamiento SQLite
* **Directorio de Datos:** `data/`
  * `whatsapp.db`: Sesión y claves criptográficas de WhatsMeow (con soporte para base histórica `felix.db`).
  * `checkpoints.db`: Estados inmutables y snapshots de ejecución de grafos.
  * `sessions.db`: Historial persistente de conversaciones, scratchpads y subagentes.
  * `users.db`: Control de acceso por roles (Admin, Contacto, Bloqueado) y directivas de confidencialidad.
  * `reminders.db`: Tareas programadas, recordatorios y alertas cron.
  * `memory_index.db`: Índice semántico vectorial/FTS5 de la base de conocimiento local.

---

## 3. Comandos de Chat Disponibles
| Comando | Descripción |
| :--- | :--- |
| `/help` | Muestra la guía completa de comandos directos. |
| `/status` | Diagnóstico completo en tiempo real de hardware, red y servicios. |
| `/menu` | Menú interactivo de bienvenida y presets de configuración de inferencia. |
| `/config` | Configuración detallada de credenciales, pool de claves y servicios. |
| `/services` | Estado detallado de los servicios monitoreados (PM2, Docker, Systemd). |
| `/sub <id>` | Lectura de reportes técnicos generados por subagentes autónomos. |
| `/backup` | Ejecuta un respaldo atómico inmediato de todas las bases SQLite (`VACUUM INTO`). |
| `/logs` | Diagnóstico y rotación de logs pesados de procesos. |
| `/voz [on\|off]` | Activa o desactiva las respuestas por nota de voz nativa. |
| `/clear` | Reinicia la memoria de sesión activa. |
| `/clean` | Limpia archivos temporales y descargas antiguas. |

---

## 4. Verificación y Pruebas
* **Compilación:** `go build -o bin/felix cmd/felix/main.go`
* **Suite de Pruebas:** `go test -count=1 ./...` (100% de tests aprobados).
* **Consumo de Memoria:** ~30-35 MB RSS en reposo.
