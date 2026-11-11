# Documento de Arquitectura y Traspaso Tecnico (Felix Open Source)

## 1. Vision General del Sistema
Felix es una plataforma integral de agente de inteligencia artificial autonomo desarrollada en Go para administracion remota de servidores e infraestructura Linux a traves de WhatsApp.

El agente combina comunicacion nativa E2E mediante el protocolo WhatsMeow con un motor ReAct Loop de razonamiento continuo, ejecucion paralela de herramientas, persistencia en SQLite modo WAL, gestion de subagentes especializados y compatibilidad con el estandar Model Context Protocol (MCP).

---

## 2. Componentes Arquitectonicos

### 2.1. Conectividad WhatsApp y Onboarding Instantaneo
- **Libreria Central:** WhatsMeow (`go.mau.fi/whatsmeow`) sobre WebSockets y cifrado Signal Protocol.
- **Renderizado de QR en Terminal:** Si el dispositivo no esta vinculado al arrancar, el cliente captura el canal QR y renderiza un codigo QR interactivo en formato ANSI half-block directamente en la terminal (`github.com/mdp/qrterminal/v3`).
- **Autodeteccion de Administrador:** El primer usuario en comunicarse con Felix tras escanear el QR se registra automaticamente como Administrador Principal y `OwnerJID`.
- **Menu Interactivo en WhatsApp:** Despacha de inmediato el menu de bienvenida con presets listos para copiar y pegar (`/config gemini <key>`, `/config openai <key>`, `/config groq <key>`, `/config ollama`, `/config antigravity`).
- **Menu y Subcomandos:** `/menu` accesible en cualquier momento y comandos `/config` para endpoints personalizados y pool de claves.

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

### 2.6. Persistencia y Almacenamiento SQLite
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
