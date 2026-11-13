# Felix - Agente Autonomo Omnicanal de Ingenieria, DevOps y Desarrollo en Go

Felix es una plataforma de agente de inteligencia artificial autonomo de alto rendimiento desarrollada en **Go**, disenada para la administracion remota de infraestructura Linux, monitoreo de sistemas y desarrollo autonomo de software a traves de **WhatsApp** y **Telegram**.

Construido para operar de forma continua, silenciosa y con una huella de memoria ultra-reducida (**< 35 MB de RAM RSS**), Felix incorpora un motor de grafos de estado determinista, un arnes de ingenieria de software nativo con mapeo sintactico AST y parches difusos, persistencia transaccional con Time-Travel en SQLite WAL, una capa omnicanal desacoplada y un catalogo de 39 herramientas de sistema y diagnostico.

---

## Caracteristicas Principales

### 1. Motor de Grafos de Estado Determinista (`internal/graph`)
* **Modelo Basado en Grafos (LangGraph-style):** Modela flujos tecnicos complejos como grafos dirigidos aciclicos o ciclicos con control riguroso de iteraciones maximas.
* **Nodos y Aristas Condicionales:** Nodos atomicos (`NodeFunc`) enrutados dinamicamente mediante evaluadores deterministas (`EdgeRouter`) segun el estado resultante.
* **Persistencia Transaccional y Time-Travel (`internal/checkpoint`):** Cada transicion de nodo genera un snapshot inmutable en SQLite (modo WAL). Permite retroceder a cualquier paso previo (`Rollback`) o reanudar tareas tras caidas del proceso (`Resume`).
* **Pausas Human-in-the-Loop (HITL) Asincronas:** Suspende la ejecucion (`ErrSuspend`) ante operaciones sensibles (esquemas de base de datos, APIs criticas) esperando confirmacion humana sin mantener goroutines bloqueadas.

### 2. Arnes de Desarrollo Nativo en Go (`internal/harness`)
* **Reemplazo Nativo de Herramientas CLI Externas:** Elimina el sobrecoste de procesos externos pesados, ejecutando todo el ciclo de desarrollo dentro del propio binario en Go.
* **Mapeo Sintactico AST de Repositorios (`internal/harness/repomap`):** Analiza el arbol de codigo del proyecto utilizando `go/parser` (y analizadores integrados para Python y TypeScript) para proveer contexto arquitectonico estructurado en menos de 1.500 tokens.
* **Motor de Diff y Fuzzy Search/Replace (`internal/harness/patcher`):** Motor de sustitucion atomica basado en bloques `SEARCH/REPLACE` con algoritmo de distancia de Levenshtein para tolerancia a discrepancias de sangria y espacios (umbral > 90%).
* **Sandbox Git Aislado (`internal/harness/sandbox`):** Ejecucion protegida en ramas efimeras (`felix-sandbox/task-<id>`), commits atomicos por paso y fusion controlada (`MergeToMain`) o descarte total (`Discard`) si las pruebas fallan.
* **Bucle REPL de Compilacion y Linter (`internal/harness/runner`):** Deteccion automatica de ecosistemas (Go, Rust, Node, Python), ejecucion de pruebas con timeout estricto y sanitizacion de `stderr` para aislar causas raiz sin desbordar el contexto.
* **Plantillas Maestras Embebidas (`internal/harness/prompts`):** Prompts de sistema, reglas sintacticas de diff y moldes de retroalimentacion embebidos en el binario estatico mediante `//go:embed`.

### 3. Arquitectura Omnicanal Desacoplada (`internal/channel`)
* **Soporte Simultaneo o Independiente:** Opera a traves de WhatsApp, Telegram o ambos canales en paralelo.
* **Activacion Condicional:** Cada canal se activa o desactiva por configuracion (`whatsapp_enabled`, `telegram_enabled`). Si solo se requiere Telegram, el servicio levanta inmediatamente sin solicitar codigo QR ni crear bases de datos de WhatsApp.
* **Conector Telegram en Go Puro (`internal/channel/telegram`):** Cliente ultraligero sobre la Telegram Bot API oficial con long-polling HTTP, soporte de mensajes Markdown (con fallback a texto plano), notas de voz Opus, imagenes y documentos.
* **Canal WhatsApp E2E (`internal/channel/whatsapp`):** WhatsMeow (`go.mau.fi/whatsmeow`) sobre WebSockets nativos y cifrado Signal Protocol con renderizado QR interactivo en terminal.
* **Pipeline Multimodal Estandarizado (`internal/channel/multimodal.go`):** Procesamiento unificado de notas de voz (transcripcion automatica con Whisper Groq/OpenAI), imagenes (inspeccion visual) y documentos adjuntos de cualquier canal.

### 4. Nucleo de Inferencia y Rendimiento
* **Gateway de Inferencia Integrado y Rotacion 429 (`internal/inference`):** Servidor proxy compatible con OpenAI (`/v1/chat/completions`) en el puerto `8055` con pool de claves y rotacion automatica inmediata ante agotamiento de cuota (HTTP 429).
* **Ruta Rapida de Latencia Cero (Fast-Path < 5ms):** Consultas deterministicas de telemetria (uptime, memoria, CPU, interfaces de red, servicios) respondidas al instante sin consumir tokens.
* **Subagentes Concurrentes Especializados:** Delegacion de tareas pesadas en segundo plano (`researcher`, `coder`, `sysadmin`, `auditor`) con semaforo de concurrencia y reportes estructurados en Markdown.
* **Protocolo MCP (Model Context Protocol):** Integracion nativa de servidores MCP externos via STDIO y JSON-RPC 2.0.
* **Catalogo de 39 Herramientas de Sistema:** Incluye `harness_develop`, `git_tool`, `docker`, `sqlite_inspector`, `exec`, `web_search`, `browser` headless, `doc_engine` y control de servicios.

---

## Requisitos del Sistema

* **Sistema Operativo:** Linux (Debian, Ubuntu, Linux Mint, Fedora, Arch Linux, Alpine).
* **Go:** Version 1.22 o superior.
* **Dependencias Recomendadas:**
  * `ffmpeg` (para codificacion de audio Opus en notas de voz).
  * `git` (para versionado y sandbox).
  * `build-essential` (para extensiones de compilacion si aplica).

Instalacion en Debian / Ubuntu:
```bash
sudo apt update && sudo apt install -y golang ffmpeg build-essential git
```

---

## Inicio Rapido

### 1. Clonar el Repositorio
```bash
git clone https://github.com/armajedon1/felix-agent.git
cd felix-agent
```

### 2. Ejecutar el Instalador
```bash
./install.sh
```
El instalador verificara el entorno, generara la estructura de carpetas (`bin/`, `data/`, `workspace/`), creara `config.json` a partir de la plantilla y compilara el binario `bin/felix`.

---

## Modos de Operacion y Canales

### Opcion A: Operar Unicamente con Telegram
En `config.json`:
```json
{
  "whatsapp_enabled": false,
  "telegram_enabled": true,
  "telegram_bot_token": "TU_TELEGRAM_BOT_TOKEN",
  "telegram_allowed": ["TU_TELEGRAM_USER_ID"]
}
```
Inicie el servicio con `./bin/felix`. El agente iniciara de forma instantanea sin solicitar codigo QR ni conectar WhatsMeow.

### Opcion B: Operar con WhatsApp
En `config.json`:
```json
{
  "whatsapp_enabled": true,
  "telegram_enabled": false
}
```
Al ejecutar `./bin/felix`, la terminal renderizara el codigo QR interactivo:
1. Abra WhatsApp en su telefono.
2. Acceda a **Dispositivos vinculados** -> **Vincular un dispositivo**.
3. Escanee el codigo QR en la terminal. La sesion quedara almacenada de forma segura en `data/whatsapp.db`.

### Opcion C: Operacion Dual Omnicanal
Active ambos canales (`"whatsapp_enabled": true`, `"telegram_enabled": true`) para atender consultas simultaneas desde WhatsApp y Telegram con memoria e historial compartidos.

---

## Configuracion desde el Chat

Al enviar el primer mensaje ("hola" o `/menu`), Felix detecta al remitente y lo registra como Administrador Principal, respondiendo con el menu interactivo:

* **Google Gemini (Recomendado):** `/config gemini <TU_API_KEY>`
* **OpenAI (GPT-4o):** `/config openai <TU_API_KEY>`
* **Groq Cloud (Whisper + Llama 3.3):** `/config groq <TU_API_KEY>`
* **Ollama Local (Sin claves de API):** `/config ollama`
* **Antigravity Tools / Proxy Local:** `/config antigravity`
* **Agregar Claves al Pool (Rotacion 429):** `/config keys add <OTRA_API_KEY>`
* **Endpoint Personalizado:** `/config apibase <URL>` / `/config apikey <KEY>` / `/config model <NAME>`

---

## Comandos Directos Disponibles

Los comandos directos son procesados por el enrutador rapido con respuesta instantanea sin consumo de tokens:

| Comando | Descripcion |
| :--- | :--- |
| `/help` | Guia interactiva de comandos disponibles. |
| `/status` | Diagnostico completo en vivo: CPU, memoria, almacenamiento, red y servicios. |
| `/config` | Panel de configuracion interactiva de inferencia y parametros del agente. |
| `/services` | Estado en tiempo real de servicios monitoreados (PM2, systemd, Docker). |
| `/sub <id>` | Lectura de reportes generados por subagentes en segundo plano. |
| `/backup` | Respaldo atomico inmediato de bases SQLite (`VACUUM INTO`). |
| `/logs [rotate]` | Diagnostico de consumo de logs y rotacion preventiva. |
| `/voz [on\|off]` | Modo manos libres: respuestas narradas por notas de voz Opus. |
| `/clear` | Reinicio de la memoria conversacional activa de la sesion. |
| `/clean` | Purga inmediata de archivos temporales y descargas antiguas. |

---

## Estructura de Directorios

```text
felix-agent/
├── bin/                         # Binarios compilados (ignorado por git)
├── cmd/felix/main.go            # Punto de entrada de la aplicacion
├── config.example.json          # Plantilla base de configuracion limpia
├── data/                        # Bases de datos SQLite y descargas locales
├── ecosystem.config.cjs         # Configuracion para despliegue con PM2
├── install.sh                   # Script de instalacion automatizada
├── Makefile                     # Reglas de compilacion, prueba y ejecucion
├── internal/
│   ├── agent/                   # ReAct Loop, Engine, Failover y Subagentes
│   ├── channel/                 # Abstraccion omnicanal y pipeline multimodal
│   │   ├── telegram/            # Conector nativo Telegram Bot API en Go puro
│   │   └── whatsapp/            # Adaptador desacoplado para WhatsMeow
│   ├── checkpoint/              # Persistencia transaccional SQLite WAL con Time-Travel
│   ├── config/                  # Validacion y guardado atomico de configuracion
│   ├── digest/                  # Telemetria asincrona en memoria
│   ├── fastpath/                # Enrutador de latencia cero (< 5ms)
│   ├── graph/                   # Motor de grafos de estado determinista (LangGraph-style)
│   ├── harness/                 # Arnes nativo de desarrollo de software
│   │   ├── patcher/             # Motor de diffs y fuzzy search/replace Levenshtein
│   │   ├── prompts/             # Directivas de ingenieria embebidas (//go:embed)
│   │   ├── repomap/             # Mapeador sintactico AST de repositorios (< 1500 tokens)
│   │   ├── runner/              # Ejecutor de pruebas y compilacion con sanitizador
│   │   └── sandbox/             # Aislamiento en ramas efimeras de Git
│   ├── healer/                  # Motor de diagnostico y auto-curacion de servicios
│   ├── health/                  # Servidor HTTP Prometheus y webhooks (:8055)
│   ├── inference/               # Gateway proxy OpenAI con pool de claves y rotacion 429
│   ├── mcp/                     # Cliente STDIO para Model Context Protocol
│   ├── memory/                  # RAG local, indexacion FTS5 y procedimientos operativos
│   ├── minecraft/               # Protocolo RCON para servidores de juego
│   ├── scheduler/               # Planificador cron y recordatorios persistentes
│   ├── tools/                   # Catalogo de 39 herramientas de sistema (harness_develop, etc.)
│   ├── users/                   # Control de acceso RBAC y directivas de confidencialidad
│   └── whatsapp/                # WhatsMeow, renderizado QR y transcriptor Whisper
├── skills/                      # Catalogo extensible de habilidades
├── systemd/felix.service        # Archivo de unidad para Systemd
└── workspace/                   # Espacio de trabajo del agente y reportes Markdown
```

---

## Despliegue en Produccion

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

## Servidor de Telemetria y Webhooks

Felix expone un servidor HTTP interno en el puerto `8055`:
* `GET /healthz`: Comprobacion de salud con estado de canales activos (WhatsApp, Telegram) y bases de datos.
* `GET /metrics`: Metricas en formato Prometheus (tokens consumidos, consultas a LLMs, llamadas a herramientas).
* `POST /api/notify`: Webhook para envio de alertas push directas al administrador:

```bash
curl -X POST http://127.0.0.1:8055/api/notify \
  -H "Content-Type: application/json" \
  -d '{"message": "Alerta: Despliegue de produccion completado", "source": "CI/CD"}'
```

---

## Licencia y Uso Comercial

Este proyecto se distribuye bajo terminos libres para adaptacion comercial, despliegues corporativos o desarrollo de codigo abierto. No contiene dependencias propietarias ni datos personales vinculados.
