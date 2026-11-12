# Documento de Arquitectura y Traspaso Tecnico (Felix Open Source)

## 1. Vision General del Sistema
Felix es una plataforma integral de agente de inteligencia artificial autonomo desarrollada en Go para administracion remota de servidores e infraestructura Linux a traves de WhatsApp.

El agente combina comunicacion nativa E2E mediante el protocolo WhatsMeow con un motor ReAct Loop de razonamiento continuo, ejecucion paralela de herramientas, persistencia en SQLite modo WAL, gestion de subagentes especializados y compatibilidad con el estandar Model Context Protocol (MCP).

---

## 2. Componentes Arquitectonicos

### 2.1. Arquitectura Omnicanal (WhatsApp y Telegram)
- **Implementacion:** `internal/channel/`
- **Interfaz Channel:** Define un contrato agnostico (`Name`, `Type`, `Start`, `Stop`, `Send`, `IsConnected`) para cualquier transporte de mensajeria.
- **Canal WhatsApp (`internal/channel/whatsapp`):** WhatsMeow (`go.mau.fi/whatsmeow`) sobre WebSockets y Signal Protocol con renderizado QR interactivo en terminal (`github.com/mdp/qrterminal/v3`).
- **Canal Telegram (`internal/channel/telegram`):** Conector ultraligero implementado directamente sobre la Telegram Bot API oficial con long-polling HTTP, soporte de mensajes Markdown, notas de voz (Opus), fotos y documentos adjuntos sin librerias externas pesadas.
- **Activacion Condicional:** Cada canal se habilita o deshabilita mediante booleanos (`whatsapp_enabled`, `telegram_enabled`) en `config.json`. Si un usuario prefiere unicamente Telegram, el agente inicia limpiamente sin solicitar codigo QR de WhatsApp.
- **Pipeline Multimodal Estandarizado (`internal/channel/multimodal.go`):** Procesa de forma comun audios (STT Whisper Groq/OpenAI), imagenes y documentos adjuntos de cualquier canal antes de enviarlos al razonamiento agentico.
- **Autodeteccion de Administrador:** El primer usuario en comunicarse con Felix desde cualquiera de los canales habilitados se registra como Administrador Principal.

### 2.2. Gateway de Inferencia Integrado y Rotacion de Claves
- **Implementacion:** `internal/inference/gateway.go` y `internal/inference/provider.go`
- **Servidor Proxy OpenAI-Compatible:** Expone `/v1/chat/completions` y `/v1/models` en el puerto HTTP interno (`8055`).
- **Pool de Claves (KeyPool):** Gestion concurrente de claves con rotacion automatica inmediata ante codigos HTTP 429 (Rate Limit o cuota agotada).
- **Presets de Fabrica:** Gemini (`gemini-2.5-flash`), OpenAI (`gpt-4o`), Groq (`llama-3.3-70b-versatile` + Whisper), Ollama (`llama3`), Antigravity Tools (`http://127.0.0.1:8045/v1`).

### 2.3. Motor Agentico (ReAct Loop)
- **Implementacion:** `internal/agent/engine.go`
- **Capacidades:**
  - Invocacion concurrente de herramientas mediante `sync.WaitGroup` cuando el modelo emite multiples tool calls simultaneas.
  - Reporte de progreso en tiempo real para operaciones extensas (navegacion web, subagentes, descargas).
  - Tolerancia a fallos: soporte para conmutacion automatica de inferencia (failover a Groq/OpenAI si el endpoint primario se degrada).
  - Truncamiento inteligente de salidas excesivas para no saturar la ventana de contexto.

### 2.3. Orquestacion de Subagentes Concurrentes
- **Implementacion:** `internal/agent/subagent.go` y `internal/tools/subagent_tool.go`
- **Arquetipos Especializados:**
  - `researcher`: Analisis web, investigacion de documentacion y sintesis.
  - `coder`: Escritura de codigo, resolucion de bugs, refactorizacion y pruebas.
  - `sysadmin`: Diagnostico de servidores, contenedores Docker, redes y logs.
  - `auditor`: Revision de seguridad, deteccion de vulnerabilidades y buenas practicas.
  - `general`: Tareas multiproposito.
- **Semaforo de Concurrencia:** Limite de 3 subagentes paralelos con encolado de tareas excedentes en estado `pending`.
- **Reportes Persistentes:** Al finalizar, cada subagente genera un archivo Markdown detallado en `workspace/reports/<id>_<slug>.md` y despacha un resumen ejecutivo al chat de WhatsApp.

### 2.4. Protocolo MCP (Model Context Protocol)
- **Implementacion:** `internal/mcp/`
- **Transporte:** Cliente STDIO sobre JSON-RPC 2.0.
- **Funcionalidad:** Permite conectar cualquier servidor MCP externo (PostgreSQL, GitHub, Brave Search, etc.) configurandolo simplemente en `config.json`.

### 2.5. Fast-Path y Latencia Cero
- **Implementacion:** `internal/fastpath/fastpath.go`
- **Ruta Rapida:** Intercepta consultas deterministicas cotidianas (hora, fecha, uptime, interfaces de red, estado de memoria y disco) y responde en < 5ms sin consumir tokens ni realizar llamadas al LLM.

### 2.6. Motor de Grafos de Estado y Persistencia Transaccional
- **Implementacion:** `internal/graph/` y `internal/checkpoint/`
- **StateGraph:** Maquina de estados determinista inspirada en LangGraph, con soporte para nodos (`NodeFunc`), aristas directas y aristas condicionales (`EdgeRouter`), suspension de ejecucion (`ErrSuspend`) y control estricto de pasos maximos.
- **Persistencia en SQLite WAL (`internal/checkpoint/sqlite_store.go`):** Cada transicion de nodo genera un snapshot inmutable en `graph_checkpoints`.
- **Time-Travel y Rollback:** Permite pausar corridas, reanudarlas tras caidas del proceso (`Resume`) o retroceder el estado a cualquier paso anterior (`Rollback`).
- **Human-in-the-Loop (HITL):** Capacidad para suspender tareas ante operaciones criticas (ej. migraciones de base de datos) esperando aprobacion explicita del usuario sin mantener goroutines vivas.

### 2.7. Arnes de Desarrollo Nativo en Go
- **Implementacion:** `internal/harness/` y `internal/tools/harness_tool.go`
- **Mapeador Sintactico AST (`internal/harness/repomap`):** Analisis del arbol de tipos y funciones exportadas del repositorio mediante `go/parser` de la biblioteca estandar de Go (con soporte adicional para Python y TypeScript/JavaScript) para proveer contexto en menos de 1.500 tokens.
- **Motor de Diff y Fuzzy Matching (`internal/harness/patcher`):** Sustitucion basada en bloques `SEARCH/REPLACE` con algoritmo de distancia de Levenshtein para tolerancia a sangrias y espaciado (umbral > 90%) y escrituras atomicas en disco.
- **Sandbox Git Aislado (`internal/harness/sandbox`):** Creacion y aislamiento en ramas efimeras `felix-sandbox/task-<id>`, commits atomicos por paso y fusion controlada (`MergeToMain`) o descarte total (`Discard`) en caso de error.
- **Bucle REPL de Compilacion y Linter (`internal/harness/runner`):** Deteccion automatica de proyectos (Go, Rust, Node, Python), ejecucion de pruebas y sanitizacion de `stderr` para aislar causas raiz sin desbordar el contexto del modelo.
- **Plantillas Maestras Embebidas (`internal/harness/prompts`):** Prompts de ingenieria, reglas de diff y plantillas de correccion embebidas en el binario estatico mediante `//go:embed`.

### 2.8. Persistencia y Almacenamiento SQLite
- **Directorio de Datos:** `data/`
  - `whatsapp.db`: Sesion y claves criptograficas de WhatsMeow.
  - `sessions.db`: Historial persistente de conversaciones, scratchpads y subagentes.
  - `users.db`: Control de acceso por roles (Admin, Contacto, Bloqueado) y directivas de veto de confidencialidad.
  - `reminders.db`: Tareas programadas, recordatorios y alertas cron.
  - `memory_index.db`: Indice semantico vectorial/FTS5 de la base de conocimiento local.

---

## 3. Comandos de WhatsApp Disponibles
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
