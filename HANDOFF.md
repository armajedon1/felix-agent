# Documento de Arquitectura y Traspaso Tecnico (Felix Open Source)

## 1. Vision General del Sistema
Felix es una plataforma integral de agente de inteligencia artificial autonomo desarrollada en **Go** para administracion de infraestructura Linux, monitoreo y desarrollo de software de alta fidelidad a traves de **WhatsApp** y **Telegram**.

El agente combina comunicacion nativa E2E mediante WhatsMeow y Telegram Bot API con un motor de grafos de estado determinista (LangGraph-style), un arnes nativo de ingenieria de software (AST repomap + fuzzy search/replace + sandbox Git), persistencia transaccional con Time-Travel en SQLite WAL, ejecucion paralela de 39 herramientas de sistema, gestion de subagentes especializados y compatibilidad con el estandar Model Context Protocol (MCP).

---

## 2. Componentes Arquitectonicos

### 2.1. Arquitectura Omnicanal (WhatsApp y Telegram)
* **Implementacion:** `internal/channel/`
* **Interfaz Channel:** Contrato desacoplado (`Name`, `Type`, `Start`, `Stop`, `Send`, `IsConnected`) para cualquier capa de transporte.
* **Canal WhatsApp (`internal/channel/whatsapp`):** WhatsMeow (`go.mau.fi/whatsmeow`) sobre WebSockets nativos y cifrado Signal Protocol con renderizado QR interactivo en terminal (`github.com/mdp/qrterminal/v3`) y fallback automatico de resolucion de sesiones historicas.
* **Canal Telegram (`internal/channel/telegram`):** Conector ultraligero implementado directamente sobre la Telegram Bot API oficial con long-polling HTTP, soporte de mensajes Markdown (con fallback automatico a texto plano), notas de voz Opus, fotos y documentos adjuntos sin librerias externas pesadas.
* **Activacion Condicional:** Cada canal se habilita o deshabilita mediante booleanos (`whatsapp_enabled`, `telegram_enabled`) en `config.json`.
* **Pipeline Multimodal Estandarizado (`internal/channel/multimodal.go`):** Procesa de forma comun notas de voz y PTT (transcripcion automatica con Groq Whisper o API OpenAI), imagenes (inspeccion visual) y documentos adjuntos de cualquier canal antes de enviarlos al razonamiento agentico.
* **Autodeteccion de Administrador:** El primer usuario en comunicarse con Felix desde cualquiera de los canales habilitados se registra como Administrador Principal.

### 2.2. Motor de Grafos de Estado Determinista
* **Implementacion:** `internal/graph/`
* **Topologia:** Soporte para grafos dirigidos aciclicos (DAG) y ciclicos con control de pasos maximos (`maxSteps`).
* **Nodos y Aristas:** Nodos atomicos (`NodeFunc`) y aristas fijas o condicionales (`EdgeRouter`) que bifurcan el flujo evaluando el estado (`State`).
* **Inmutabilidad de Estado (`state.go`):** Copia profunda del estado (`Clone()`) en cada paso de transicion para garantizar aislamiento y consistencia.
* **Suspension Human-in-the-Loop:** Capacidad para suspender tareas (`ErrSuspend`) ante operaciones criticas esperando aprobacion del usuario sin bloquear hilos del sistema.

### 2.3. Persistencia Transaccional y Time-Travel (Checkpoints)
* **Implementacion:** `internal/checkpoint/`
* **Base de Datos SQLite en Modo WAL (`sqlite_store.go`):** Tablas `graph_runs` y `graph_checkpoints` para registrar snapshots inmutables por cada paso ejecutado.
* **Recuperacion ante Fallos:** Si el proceso se interrumpe abruptamente (`kill -9`), el grafo reanuda exactamente desde el ultimo checkpoint completado con `Resume()`.
* **Time-Travel y Rollback:** Capacidad para retroceder el estado a cualquier paso previo (`Rollback()`), descartando codigo defectuoso y liberando memoria.

### 2.4. Arnes de Desarrollo Nativo en Go
* **Implementacion:** `internal/harness/` y `internal/tools/harness_tool.go`
* **Mapeador Sintactico AST (`internal/harness/repomap`):** Extraccion de firmas de metodos, funciones, estructuras e interfaces exportadas con `go/parser` (mas analizadores para Python y TypeScript) entregando un mapa compacto de menos de 1.500 tokens.
* **Motor de Diff y Fuzzy Search/Replace (`internal/harness/patcher`):** Sustitucion basada en bloques `SEARCH/REPLACE` con algoritmo de distancia de Levenshtein para tolerancia a discrepancias de sangria y espacios (umbral > 90%) y escrituras atomicas en disco.
* **Sandbox Git Aislado (`internal/harness/sandbox`):** Creacion automatica de ramas efimeras `felix-sandbox/task-<id>`, commits atomicos por paso y fusion controlada (`MergeToMain`) o descarte total (`Discard`) si las pruebas fallan.
* **Bucle REPL de Compilacion y Linter (`internal/harness/runner`):** Deteccion automatica de proyectos (Go, Rust, Node, Python), ejecucion de pruebas con timeout y sanitizacion de `stderr` para aislar causas raiz sin desbordar el contexto.
* **Plantillas Maestras Embebidas (`internal/harness/prompts`):** Prompts de ingenieria, reglas de diff y plantillas de correccion embebidas en el binario estatico mediante `//go:embed`.

### 2.5. Gateway de Inferencia Integrado y Rotacion de Claves
* **Implementacion:** `internal/inference/gateway.go` y `internal/inference/provider.go`
* **Servidor Proxy OpenAI-Compatible:** Expone `/v1/chat/completions` y `/v1/models` en el puerto HTTP interno (`8055`).
* **Pool de Claves (KeyPool):** Gestion concurrente de claves con rotacion automatica inmediata ante codigos HTTP 429 (Rate Limit o cuota agotada).
* **Presets de Fabrica:** Gemini (`gemini-2.5-flash`), OpenAI (`gpt-4o`), Groq (`llama-3.3-70b-versatile` + Whisper), Ollama (`llama3`), Antigravity Tools (`http://127.0.0.1:8045/v1`).

### 2.6. Motor Agentico (ReAct Loop)
* **Implementacion:** `internal/agent/engine.go`
* **Capacidades:**
  * Invocacion concurrente de herramientas mediante `sync.WaitGroup` cuando el modelo emite multiples tool calls simultaneas.
  * Blindaje con `defer recover()` en el despachador de herramientas para garantizar tolerancia a panics de herramientas externas.
  * Reporte de progreso en tiempo real para operaciones extensas (navegacion web, subagentes, descargas).
  * Tolerancia a fallos: conmutacion automatica de inferencia (failover a Groq/OpenAI si el endpoint primario se degrada).
  * Truncamiento inteligente de salidas excesivas para no saturar la ventana de contexto.

### 2.7. Orquestacion de Subagentes Concurrentes
* **Implementacion:** `internal/agent/subagent.go` y `internal/tools/subagent_tool.go`
* **Arquetipos Especializados:**
  * `researcher`: Analisis web, investigacion de documentacion y sintesis.
  * `coder`: Escritura de codigo, resolucion de bugs, refactorizacion y pruebas.
  * `sysadmin`: Diagnostico de servidores, contenedores Docker, redes y logs.
  * `auditor`: Revision de seguridad, deteccion de vulnerabilidades y buenas practicas.
  * `general`: Tareas multiproposito.
* **Semaforo de Concurrencia:** Limite de 3 subagentes paralelos con encolado de tareas excedentes en estado `pending`.
* **Reportes Persistentes:** Al finalizar, cada subagente genera un archivo Markdown detallado en `workspace/reports/<id>_<slug>.md` y despacha un resumen ejecutivo al chat.

### 2.8. Catalogo de 39 Herramientas Nativas (Tool Registry)
* **Implementacion:** `internal/tools/`
* **Herramientas Destacadas:**
  * `harness_develop`: Desarrollo de software autonomo mediante StateGraph.
  * `git_tool`: Gestion de repositorios Git locales.
  * `docker`: Administracion de contenedores e imagenes Docker.
  * `sqlite_inspector` y `sqlite_query`: Inspeccion analitica de bases de datos.
  * `exec` y `bash`: Ejecucion de comandos seguros con truncamiento.
  * `browser`: Automatizacion e interaccion web headless con Rod.
  * `doc_engine`: Analisis de documentos y texto enriquecido.
  * `net_sentinel`: Diagnostico de interfaces de red y puertos.
  * `healer`: Auto-curacion de servicios degradados.

### 2.9. Persistencia y Almacenamiento SQLite
* **Directorio de Datos:** `data/`
  * `whatsapp.db`: Sesion y claves criptograficas de WhatsMeow (con soporte para base historica `felix.db`).
  * `checkpoints.db`: Estados inmutables y snapshots de ejecucion de grafos.
  * `sessions.db`: Historial persistente de conversaciones, scratchpads y subagentes.
  * `users.db`: Control de acceso por roles (Admin, Contacto, Bloqueado) y directivas de confidencialidad.
  * `reminders.db`: Tareas programadas, recordatorios y alertas cron.
  * `memory_index.db`: Indice semantico vectorial/FTS5 de la base de conocimiento local.

---

## 3. Comandos de Chat Disponibles
| Comando | Descripcion |
| :--- | :--- |
| `/help` | Muestra la guia completa de comandos directos. |
| `/status` | Diagnostico completo en tiempo real de hardware, red y servicios. |
| `/menu` | Menu interactivo de bienvenida y presets de configuracion de inferencia. |
| `/config` | Configuracion detallada de credenciales, pool de claves y servicios. |
| `/services` | Estado detallado de los servicios monitoreados (PM2, Docker, Systemd). |
| `/sub <id>` | Lectura de reportes tecnicos generados por subagentes autonomos. |
| `/backup` | Ejecuta un respaldo atomico inmediato de todas las bases SQLite (`VACUUM INTO`). |
| `/logs` | Diagnostico y rotacion de logs pesados de procesos. |
| `/voz [on\|off]` | Activa o desactiva las respuestas por nota de voz nativa. |
| `/clear` | Reinicia la memoria de sesion activa. |
| `/clean` | Limpia archivos temporales y descargas antiguas. |

---

## 4. Verificacion y Pruebas
* **Compilacion:** `go build -o bin/felix cmd/felix/main.go`
* **Suite de Pruebas:** `go test -count=1 ./...` (100% de tests aprobados).
* **Consumo de Memoria:** ~30-35 MB RSS en reposo.
