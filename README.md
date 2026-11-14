# Félix - Agente Autónomo Omnicanal de Ingeniería, DevOps y Desarrollo en Go

Félix es una plataforma de agente de inteligencia artificial autónomo de alto rendimiento desarrollada en **Go**, diseñada para la administración remota de infraestructura Linux, monitoreo de sistemas y desarrollo autónomo de software a través de **WhatsApp** y **Telegram**.

Construido para operar de forma continua, silenciosa y con una huella de memoria ultra-reducida (**< 35 MB de RAM RSS**), Félix incorpora un motor de grafos de estado determinista, un arnés de ingeniería de software nativo con mapeo sintáctico AST y parches difusos, persistencia transaccional con Time-Travel en SQLite WAL, una capa omnicanal desacoplada y un catálogo de 39 herramientas de sistema y diagnóstico.

---

## Características Principales

### 1. Motor de Grafos de Estado Determinista (`internal/graph`)
* **Modelo Basado en Grafos (LangGraph-style):** Modela flujos técnicos complejos como grafos dirigidos acíclicos o cíclicos con control riguroso de iteraciones máximas.
* **Nodos y Aristas Condicionales:** Nodos atómicos (`NodeFunc`) enrutados dinámicamente mediante evaluadores deterministas (`EdgeRouter`) según el estado resultante.
* **Persistencia Transaccional y Time-Travel (`internal/checkpoint`):** Cada transición de nodo genera un snapshot inmutable en SQLite (modo WAL). Permite retroceder a cualquier paso previo (`Rollback`) o reanudar tareas tras caídas del proceso (`Resume`).
* **Pausas Human-in-the-Loop (HITL) Asíncronas:** Suspende la ejecución (`ErrSuspend`) ante operaciones sensibles (esquemas de base de datos, APIs críticas) esperando confirmación humana sin mantener goroutines bloqueadas.

### 2. Arnés de Desarrollo Nativo en Go (`internal/harness`)
* **Reemplazo Nativo de Herramientas CLI Externas:** Elimina el sobrecoste de procesos externos pesados, ejecutando todo el ciclo de desarrollo dentro del propio binario en Go.
* **Mapeo Sintáctico AST de Repositorios (`internal/harness/repomap`):** Analiza el árbol de código del proyecto utilizando `go/parser` (y analizadores integrados para Python y TypeScript) para proveer contexto arquitectónico estructurado en menos de 1.500 tokens.
* **Motor de Diff y Fuzzy Search/Replace (`internal/harness/patcher`):** Motor de sustitución atómica basado en bloques `SEARCH/REPLACE` con algoritmo de distancia de Levenshtein para tolerancia a discrepancias de sangría y espacios (umbral > 90%).
* **Sandbox Git Aislado (`internal/harness/sandbox`):** Ejecución protegida en ramas efímeras (`felix-sandbox/task-<id>`), commits atómicos por paso y fusión controlada (`MergeToMain`) o descarte total (`Discard`) si las pruebas fallan.
* **Bucle REPL de Compilación y Linter (`internal/harness/runner`):** Detección automática de ecosistemas (Go, Rust, Node, Python), ejecución de pruebas con timeout estricto y sanitización de `stderr` para aislar causas raíz sin desbordar el contexto.
* **Plantillas Maestras Embebidas (`internal/harness/prompts`):** Prompts de sistema, reglas sintácticas de diff y moldes de retroalimentación embebidos en el binario estático mediante `//go:embed`.

### 3. Arquitectura Omnicanal Desacoplada (`internal/channel`)
* **Soporte Simultáneo o Independiente:** Opera a través de WhatsApp, Telegram o ambos canales en paralelo.
* **Activación Condicional:** Cada canal se activa o desactiva por configuración (`whatsapp_enabled`, `telegram_enabled`). Si solo se requiere Telegram, el servicio levanta inmediatamente sin solicitar código QR ni crear bases de datos de WhatsApp.
* **Conector Telegram en Go Puro (`internal/channel/telegram`):** Cliente ultraligero sobre la Telegram Bot API oficial con long-polling HTTP, soporte de mensajes Markdown (con fallback a texto plano), notas de voz Opus, imágenes y documentos.
* **Canal WhatsApp E2E (`internal/channel/whatsapp`):** WhatsMeow (`go.mau.fi/whatsmeow`) sobre WebSockets nativos y cifrado Signal Protocol con renderizado QR interactivo en terminal.
* **Pipeline Multimodal Estandarizado (`internal/channel/multimodal.go`):** Procesamiento unificado de notas de voz (transcripción automática con Whisper Groq/OpenAI), imágenes (inspección visual) y documentos adjuntos de cualquier canal.

### 4. Núcleo de Inferencia y Rendimiento
* **Gateway de Inferencia Integrado y Rotación 429 (`internal/inference`):** Servidor proxy compatible con OpenAI (`/v1/chat/completions`) en el puerto `8055` con pool de claves y rotación automática inmediata ante agotamiento de cuota (HTTP 429).
* **Ruta Rápida de Latencia Cero (Fast-Path < 5ms):** Consultas determinísticas de telemetría (uptime, memoria, CPU, interfaces de red, servicios) respondidas al instante sin consumir tokens.
* **Subagentes Concurrentes Especializados:** Delegación de tareas pesadas en segundo plano (`researcher`, `coder`, `sysadmin`, `auditor`) con semáforo de concurrencia y reportes estructurados en Markdown.
* **Protocolo MCP (Model Context Protocol):** Integración nativa de servidores MCP externos vía STDIO y JSON-RPC 2.0.
* **Catálogo de 39 Herramientas de Sistema:** Incluye `harness_develop`, `git_tool`, `docker`, `sqlite_inspector`, `exec`, `web_search`, `browser` headless, `doc_engine` y control de servicios.

---

## Requisitos del Sistema

* **Sistema Operativo:** Linux (Debian, Ubuntu, Linux Mint, Fedora, Arch Linux, Alpine).
* **Go:** Versión 1.22 o superior.
* **Dependencias Recomendadas:**
  * `ffmpeg` (para codificación de audio Opus en notas de voz).
  * `git` (para versionado y sandbox).
  * `build-essential` (para extensiones de compilación si aplica).

Instalación en Debian / Ubuntu:
```bash
sudo apt update && sudo apt install -y golang ffmpeg build-essential git
```

---

## Inicio Rápido

### 1. Clonar el Repositorio
```bash
git clone https://github.com/armajedon1/felix-agent.git
cd felix-agent
```

### 2. Ejecutar el Instalador
```bash
./install.sh
```
El instalador verificará el entorno, generará la estructura de carpetas (`bin/`, `data/`, `workspace/`), creará `config.json` a partir de la plantilla y compilará el binario `bin/felix`.

---

## Modos de Operación y Canales

### Opción A: Operar Únicamente con Telegram
En `config.json`:
```json
{
  "whatsapp_enabled": false,
  "telegram_enabled": true,
  "telegram_bot_token": "TU_TELEGRAM_BOT_TOKEN",
  "telegram_allowed": ["TU_TELEGRAM_USER_ID"]
}
```
Inicie el servicio con `./bin/felix`. El agente iniciará de forma instantánea sin solicitar código QR ni conectar WhatsMeow.

### Opción B: Operar con WhatsApp
En `config.json`:
```json
{
  "whatsapp_enabled": true,
  "telegram_enabled": false
}
```
Al ejecutar `./bin/felix`, la terminal renderizará el código QR interactivo:
1. Abra WhatsApp en su teléfono.
2. Acceda a **Dispositivos vinculados** -> **Vincular un dispositivo**.
3. Escanee el código QR en la terminal. La sesión quedará almacenada de forma segura en `data/whatsapp.db`.

### Opción C: Operación Dual Omnicanal
Active ambos canales (`"whatsapp_enabled": true`, `"telegram_enabled": true`) para atender consultas simultáneas desde WhatsApp y Telegram con memoria e historial compartidos.

---

## Configuración desde el Chat

Al enviar el primer mensaje ("hola" o `/menu`), Félix detecta al remitente y lo registra como Administrador Principal, respondiendo con el menú interactivo:

* **Google Gemini (Recomendado):** `/config gemini <TU_API_KEY>`
* **OpenAI (GPT-4o):** `/config openai <TU_API_KEY>`
* **Groq Cloud (Whisper + Llama 3.3):** `/config groq <TU_API_KEY>`
* **Ollama Local (Sin claves de API):** `/config ollama`
* **Antigravity Tools / Proxy Local:** `/config antigravity`
* **Agregar Claves al Pool (Rotación 429):** `/config keys add <OTRA_API_KEY>`
* **Endpoint Personalizado:** `/config apibase <URL>` / `/config apikey <KEY>` / `/config model <NAME>`

---

## Comandos Directos Disponibles

Los comandos directos son procesados por el enrutador rápido con respuesta instantánea sin consumo de tokens:

| Comando | Descripción |
| :--- | :--- |
| `/help` | Guía interactiva de comandos disponibles. |
| `/status` | Diagnóstico completo en vivo: CPU, memoria, almacenamiento, red y servicios. |
| `/config` | Panel de configuración interactiva de inferencia y parámetros del agente. |
| `/services` | Estado en tiempo real de servicios monitoreados (PM2, systemd, Docker). |
| `/sub <id>` | Lectura de reportes generados por subagentes en segundo plano. |
| `/backup` | Respaldo atómico inmediato de bases SQLite (`VACUUM INTO`). |
| `/logs [rotate]` | Diagnóstico de consumo de logs y rotación preventiva. |
| `/voz [on\|off]` | Modo manos libres: respuestas narradas por notas de voz Opus. |
| `/clear` | Reinicio de la memoria conversacional activa de la sesión. |
| `/clean` | Purga inmediata de archivos temporales y descargas antiguas. |

---

## Estructura de Directorios

```text
felix-agent/
├── bin/                         # Binarios compilados (ignorado por git)
├── cmd/
│   ├── benchmark/main.go        # Suite de evaluación de rendimiento y telemetría
│   └── felix/main.go            # Punto de entrada de la aplicación
├── BENCHMARKS.md                # Informe y metodología de evaluación de rendimiento
├── config.example.json          # Plantilla base de configuración limpia
├── data/                        # Bases de datos SQLite y descargas locales
├── ecosystem.config.cjs         # Configuración para despliegue con PM2
├── install.sh                   # Script de instalación automatizada
├── Makefile                     # Reglas de compilación, prueba y ejecución
├── internal/
│   ├── agent/                   # ReAct Loop, Engine, Execution Gate y Subagentes
│   ├── channel/                 # Abstracción omnicanal y pipeline multimodal
│   │   ├── telegram/            # Conector nativo Telegram Bot API en Go puro
│   │   └── whatsapp/            # Adaptador desacoplado para WhatsMeow
│   ├── checkpoint/              # Persistencia transaccional SQLite WAL con Time-Travel
│   ├── config/                  # Validación y guardado atómico de configuración
│   ├── digest/                  # Telemetría asíncrona en memoria
│   ├── fastpath/                # Enrutador de latencia cero (< 5ms)
│   ├── graph/                   # Motor de grafos de estado determinista (LangGraph-style)
│   ├── harness/                 # Arnés nativo de desarrollo de software
│   │   ├── patcher/             # Motor de diffs y fuzzy search/replace Levenshtein
│   │   ├── prompts/             # Directivas de ingeniería embebidas (//go:embed)
│   │   ├── repomap/             # Mapeador sintáctico AST de repositorios (< 1500 tokens)
│   │   ├── runner/              # Ejecutor de pruebas y compilación con sanitizador
│   │   └── sandbox/             # Aislamiento en ramas efímeras de Git
│   ├── healer/                  # Motor de diagnóstico y auto-curación de servicios
│   ├── health/                  # Servidor HTTP Prometheus y webhooks (:8055)
│   ├── inference/               # Gateway proxy OpenAI con pool de claves y rotación 429
│   ├── mcp/                     # Cliente STDIO para Model Context Protocol
│   ├── memory/                  # RAG local, indexación FTS5 y procedimientos operativos
│   ├── minecraft/               # Protocolo RCON para servidores de juego
│   ├── scheduler/               # Planificador cron y recordatorios persistentes
│   ├── tools/                   # Catálogo de 39 herramientas de sistema (harness_develop, etc.)
│   ├── users/                   # Control de acceso RBAC y directivas de confidencialidad
│   └── whatsapp/                # WhatsMeow, renderizado QR y transcriptor Whisper
├── skills/                      # Catálogo extensible de habilidades
├── systemd/felix.service        # Archivo de unidad para Systemd
└── workspace/                   # Espacio de trabajo del agente y reportes Markdown
```

---

## Despliegue en Producción

### Con PM2 (Recomendado)
```bash
npm install -g pm2
pm2 start ecosystem.config.cjs
pm2 save
pm2 startup
```

### Con Systemd
```bash
sudo cp systemd/felix.service /etc/systemd/system/felix.service
sudo systemctl daemon-reload
sudo systemctl enable --now felix
```

---

## Evaluación de Rendimiento y Telemetría

Félix cuenta con una suite nativa de evaluación de rendimiento y telemetría concurrente de hardware ([`cmd/benchmark/main.go`](cmd/benchmark/main.go)) para validar la latencia y el consumo de recursos en hardware embebido (Raspberry Pi 5 / Debian arm64):

| Subsistema Evaluado | Carga / Operaciones | Tiempo Total | Rendimiento | Latencia Promedio |
| :--- | :--- | :--- | :--- | :--- |
| **Motor StateGraph (LangGraph en Go)** | 500 corridas (1.000 transiciones + SQLite WAL) | `97,90 ms` | **10.213 pasos/seg** | **97,90 µs** / paso |
| **Arnés de Desarrollo (`harness_develop`)** | Ciclo E2E completo (AST $\rightarrow$ Sandbox $\rightarrow$ Patch $\rightarrow$ Test $\rightarrow$ Merge) | `1,41 s` | **1 ciclo E2E completo** | **1,41 s** (con `go test` real) |
| **Compuerta de Ejecución (*Execution Gate*)** | 200.000 evaluaciones semánticas (confirmación/hesitación) | `642,08 ms` | **311.484 ops/seg** | **3,21 µs** / evaluación |

* **Consumo de Memoria:** Huella residente pico (RSS) de **18,36 MB** y heap activo final de **1,43 MB** (muy por debajo del límite de diseño de 35 MB).
* **Impacto en CPU:** Carga media controlada de **32,43%** en procesador quad-core Cortex-A76.
* **Metodología y Reproducción:** Consulte [`BENCHMARKS.md`](BENCHMARKS.md) para el desglose detallado de pruebas, telemetría y comandos de ejecución.

---

## Servidor de Telemetría y Webhooks

Félix expone un servidor HTTP interno en el puerto `8055`:
* `GET /healthz`: Comprobación de salud con estado de canales activos (WhatsApp, Telegram) y bases de datos.
* `GET /metrics`: Métricas en formato Prometheus (tokens consumidos, consultas a LLMs, llamadas a herramientas).
* `POST /api/notify`: Webhook para envío de alertas push directas al administrador:

```bash
curl -X POST http://127.0.0.1:8055/api/notify \
  -H "Content-Type: application/json" \
  -d '{"message": "Alerta: Despliegue de producción completado", "source": "CI/CD"}'
```

---

## Licencia y Uso Comercial

Este proyecto se distribuye bajo términos libres para adaptación comercial, despliegues corporativos o desarrollo de código abierto. No contiene dependencias propietarias ni datos personales vinculados.
