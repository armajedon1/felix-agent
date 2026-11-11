# Felix - Asistente y Agente Autonomo de Ingenieria y DevOps en WhatsApp

Felix es un agente autonomo de alta eficiencia desarrollado en **Go** para la administracion, monitoreo y desarrollo en servidores **Linux** a traves de **WhatsApp**.

Diseñado para operar de forma continua, silenciosa y con consumo minimo de recursos (~30 MB de memoria RAM), Felix combina comunicacion nativa punto a punto (WebSockets E2E con WhatsMeow) con un motor de razonamiento ReAct, ejecucion concurrente de herramientas, enrutamiento deterministico de latencia cero, orquestacion de subagentes especializados y compatibilidad con el estandar **Model Context Protocol (MCP)**.

---

## Caracteristicas Principales

- **Instalacion con Un Solo Comando:** Script automatizado (`./install.sh` o `make run`) que verifica dependencias, crea la estructura de directorios, compila el binario y lanza el servicio.
- **Vinculacion Instantanea por Codigo QR en Terminal:** Al arrancar por primera vez, dibuja un codigo QR nativo en bloques ANSI dentro de la consola para vincular cualquier cuenta de WhatsApp en segundos sin interfaz grafica.
- **Configuracion Interactiva desde WhatsApp:** Menu interactivo con comandos `/config` para configurar o cambiar endpoints de inferencia (`api_base`), claves de API (`api_key`), modelos de lenguaje (`model`), servicios monitoreados y credenciales de Whisper directamente por chat.
- **Asistente de Terminal Integrado:** Subcomando `./bin/felix setup` para quienes prefieran configurar variables de entorno y claves antes de arrancar.
- **Ruta Rapida (Fast-Path < 5ms):** Diagnosticos del sistema (uptime, memoria RAM, espacio en disco, direcciones IP, servicios) respondidos inmediatamente sin consumir tokens ni invocar al LLM.
- **Catalogo de 38 Herramientas Nativas:**
  - Control de contenedores Docker y servicios systemd / PM2.
  - Ejecucion segura de comandos bash con control de tiempo limite y truncamiento asimetrico.
  - Inspeccion profunda y consultas analiticas sobre bases de datos SQLite locales.
  - Navegacion e interaccion web headless con Chromium (Rod).
  - Gestion de repositorios Git (status, diff, log, commit).
  - Sintesis y transcripcion de notas de voz nativas en audio Opus (Edge-TTS y Groq Whisper).
- **Subagentes Concurrentes Especializados:** Capacidad para delegar tareas complejas en segundo plano a subagentes con roles dedicados (`researcher`, `coder`, `sysadmin`, `auditor`), semaforo de concurrencia y generacion de reportes tecnicos persistentes en Markdown.
- **Protocolo MCP (Model Context Protocol):** Integracion nativa cliente STDIO para conectar cualquier servidor MCP externo configurandolo en `config.json`.
- **Persistencia Indestructible en SQLite:** Almacenamiento con modo WAL, respaldos atomicos programados (`VACUUM INTO`), recuperacion automatica tras reinicios y archivado historico mensual.
- **Seguridad y Confidencialidad RBAC:** Separacion estricta de privilegios entre Administradores y Contactos, incluyendo directivas de veto para proteger datos sensibles de terceros.

---

## Requisitos del Sistema

- **Sistema Operativo:** Linux (Debian, Ubuntu, Linux Mint, Fedora, Arch Linux, Alpine).
- **Go:** Version 1.22 o superior.
- **Dependencias del Sistema (Recomendadas):**
  - `ffmpeg` (necesario para codificacion Opus de notas de voz de WhatsApp).
  - `gcc` o `build-essential` (para extensiones SQLite nativas si aplica).
  - `git` (para herramientas de versionado).

En distribuciones basadas en Debian/Ubuntu:
```bash
sudo apt update && sudo apt install -y golang ffmpeg build-essential git
```

---

## Inicio Rapido

### 1. Clonar el repositorio
```bash
git clone <url-del-repositorio> felix
cd felix
```

### 2. Ejecutar el instalador
```bash
./install.sh
```

El script verificara el entorno, creara las carpetas necesarias (`bin/`, `data/`, `workspace/`), generara `config.json` si no existe y compilara el binario `bin/felix`.

### 3. Iniciar Felix y Escanear el Codigo QR
```bash
./bin/felix
```

En la pantalla de la terminal aparecera el codigo QR:
1. Abra WhatsApp en su telefono.
2. Vaya a **Dispositivos vinculados** -> **Vincular un dispositivo**.
3. Escanee el codigo QR que aparece en la terminal.
4. Una vez vinculado, la sesion queda guardada localmente en `data/whatsapp.db`.

### 4. Configuracion Instantanea desde WhatsApp
Al escanear el codigo QR y enviar su primer mensaje ("hola" o cualquier texto), Felix detecta automaticamente al remitente y lo registra como el **Administrador Principal**.

De forma instantanea, Felix responde con el **Menu de Configuracion Rapida**:
- **Opcion 1: Google Gemini (Recomendado):** `/config gemini <TU_API_KEY>`
- **Opcion 2: OpenAI (GPT-4o):** `/config openai <TU_API_KEY>`
- **Opcion 3: Groq Cloud (Ultra-rapido + Transcripcion Whisper):** `/config groq <TU_API_KEY>`
- **Opcion 4: Ollama Local (Sin claves de API):** `/config ollama`
- **Opcion 5: Antigravity Tools / Proxy Local:** `/config antigravity`
- **Opcion 6: Multiples Claves con Rotacion 429:** `/config keys add <OTRA_API_KEY>`
- **Opcion 7: Endpoint Personalizado:** `/config apibase <URL>` / `/config apikey <CLAVE>` / `/config model <MODELO>`

Simplemente copie y pegue la linea correspondiente en WhatsApp para que Felix quede 100% activo y listo para razonar. Puede enviar `/menu` en cualquier momento para volver a ver estas opciones.

---

## Gateway de Inferencia Integrado y Rotacion de Claves

Felix incluye una **puerta de enlace de inferencia integrada** (compatible con el estandar OpenAI) alojada en su servidor interno (puerto `8055` por defecto):
- **Endpoints Expuestos:**
  - `POST /v1/chat/completions`
  - `GET /v1/models`
- **Pool de Claves (KeyPool):** Soporte para multiples claves de API con rotacion atomica y balanceo.
- **Auto-Recuperacion ante Limites de Cuota (HTTP 429):** Si un proveedor agota la cuota (Resource Exhausted / Rate Limit), el gateway rota inmediatamente a la siguiente clave disponible en el pool y reintenta la solicitud sin interrumpir al usuario.
- **Compatibilidad con Clientes Externos:** Puede apuntar cualquier aplicacion externa (Claude Code, extensiones de VS Code, curl o agentes autonomos) a `http://127.0.0.1:8055/v1` para consumir la inferencia de Felix.

O si utiliza proxies compatibles con OpenAI (como Antigravity Tools, Ollama o LocalAI):
```text
/config apibase http://127.0.0.1:8045/v1
/config model gemini-2.5-flash
```

Para verificar el estado actual de la configuracion:
```text
/config status
```

---

## Comandos Directos de WhatsApp

Felix incluye comandos administrativos de ejecucion instantanea que operan sin latencia ni consumo de tokens:

| Comando | Descripcion |
| :--- | :--- |
| `/help` | Guia completa de comandos disponibles. |
| `/status` | Diagnostico completo en vivo: CPU, memoria, disco, red y servicios. |
| `/config` | Panel de configuracion interactiva de inferencia y parametros. |
| `/services` | Estado en vivo de procesos PM2, systemd o Docker monitoreados. |
| `/sub <id>` | Lectura de reportes generados por subagentes en segundo plano. |
| `/backup` | Respaldo atomico inmediato de todas las bases de datos en `data/backups/`. |
| `/logs [rotate]` | Diagnostico de consumo de logs o forzado de rotacion preventiva. |
| `/voz [on\|off]` | Modo manos libres: respuestas narradas por nota de voz. |
| `/clear` | Limpieza del historial de la sesion conversacional activa. |
| `/clean` | Purga inmediata de descargas antiguas y archivos temporales. |

---

## Estructura del Proyecto

```text
felix/
├── bin/                         # Binarios compilados (ignorado por git)
├── cmd/felix/main.go            # Punto de entrada de la aplicacion
├── config.example.json          # Plantilla base de configuracion
├── data/                        # Bases de datos SQLite y descargas locales
├── ecosystem.config.cjs         # Configuracion para despliegue con PM2
├── install.sh                   # Script automatizado de instalacion
├── run.sh                       # Script directo de ejecucion
├── Makefile                     # Metas de compilacion, prueba y limpieza
├── internal/
│   ├── agent/                   # ReAct Loop, Engine, Failover y Subagentes
│   ├── config/                  # Validacion y persistencia atomica de configuracion
│   ├── digest/                  # Telemetria asincrona en memoria
│   ├── fastpath/                # Respuestas de latencia cero (< 5ms)
│   ├── healer/                  # Motor de diagnostico y auto-curacion de servicios
│   ├── health/                  # Servidor HTTP Prometheus y webhooks (:8055)
│   ├── mcp/                     # Cliente STDIO para Model Context Protocol
│   ├── memory/                  # RAG local, indexacion FTS5 y SOPs
│   ├── minecraft/               # Administracion RCON para servidores de juego
│   ├── scheduler/               # Planificador cron, rotacion y respaldos
│   ├── tools/                   # Catalogo de 38 herramientas de sistema y desarrollo
│   ├── users/                   # Control de acceso basado en roles (RBAC) y vetos
│   └── whatsapp/                # WhatsMeow, renderizado QR, TTS y transcriptor Whisper
├── skills/                      # Catalogo extensible de habilidades
├── systemd/felix.service        # Unidad de servicio para Systemd
└── workspace/                   # Espacio de trabajo del agente y reportes Markdown
```

---

## Despliegue en Produccion

### Opcion A: Con PM2 (Recomendado)
```bash
npm install -g pm2
pm2 start ecosystem.config.cjs
pm2 save
pm2 startup
```

### Opcion B: Con Systemd
1. Copie el archivo de servicio:
   ```bash
   sudo cp systemd/felix.service /etc/systemd/system/felix.service
   ```
2. Ajuste las rutas y el usuario en `/etc/systemd/system/felix.service`.
3. Recargue y active el servicio:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now felix
   ```

---

## Servidor de Telemetria y Webhooks

Felix incluye un servidor HTTP ligero escuchando por defecto en el puerto `8055`:
- `GET /healthz`: Comprobacion de salud (liveness probe) con estado de conectividad a WhatsApp y base de datos.
- `GET /metrics`: Metricas en formato Prometheus (tokens consumidos, consultas al LLM, llamadas a herramientas, memoria activa).
- `POST /api/notify`: Webhook para que scripts locales o servicios de monitoreo (Uptime Kuma, cron jobs) envien notificaciones push directas al administrador en WhatsApp.

Ejemplo de notificacion via webhook:
```bash
curl -X POST http://127.0.0.1:8055/api/notify \
  -H "Content-Type: application/json" \
  -d '{"message": "Alerta: Despliegue completado exitosamente", "source": "CI/CD"}'
```

---

## Licencia y Uso Comercial

Este proyecto se distribuye bajo terminos listos para adaptacion comercial, despliegues corporativos o codigo abierto independiente. No contiene dependencias propietarias ni datos personales vinculados.
