# Directrices de Desarrollo para Agentes y Desarrolladores (Felix Open Source)

## 1. Descripcion del Proyecto
Felix es un asistente y agente autonomo de ingenieria, DevOps y desarrollo de software para sistemas Linux desplegado en Go. Cuenta con una arquitectura omnicanal desacoplada (WhatsApp via WhatsMeow y Telegram via Bot API), un motor de grafos de estado determinista (LangGraph-style), un arnes nativo de desarrollo de software (AST repomap + fuzzy search/replace + sandbox Git), persistencia transaccional con Time-Travel en SQLite WAL, un catalogo de 39 herramientas de sistema, orquestacion concurrente de subagentes y compatibilidad con el estandar Model Context Protocol (MCP).

## 2. Filosofia de Diseno y Reglas Estrictas
1. **Prohibicion estricta de emojis:** NUNCA usar emojis en ningun mensaje, codigo fuente, comentarios, commits, scripts o documentacion tecnica.
2. **KISS y YAGNI (Cero sobreingenieria):** Resolver problemas con funciones puras, modulos directos y APIs nativas.
3. **Tipado estricto y sobriedad tecnica:** Salidas concisas orientadas a la maxima eficiencia de tokens.
4. **Ciclo autonomo de verificacion (TDD):** Antes de entregar o desplegar cambios, verificar la suite completa con `go test -count=1 ./...` asegurando 100% de exito.
5. **Aislamiento y portabilidad:** Todo codigo debe ser portable a cualquier distribucion Linux (Debian, Ubuntu, Arch, Fedora) sin asumir nombres de usuario fijos ni rutas absolutas predeterminadas.

## 3. Estructura de Directorios
```text
felix-open-source/
├── cmd/felix/main.go            # Punto de entrada y CLI
├── config.example.json          # Plantilla base de configuracion limpia
├── data/                        # Bases de datos SQLite y descargas locales (ignorado por git)
├── internal/
│   ├── agent/                   # ReAct Loop, Engine, Failover y Subagentes
│   ├── channel/                 # Abstraccion omnicanal y pipeline multimodal
│   │   ├── telegram/            # Conector nativo Telegram Bot API en Go puro
│   │   └── whatsapp/            # Adaptador desacoplado WhatsMeow
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
└── workspace/                   # Espacio de trabajo del agente y reportes Markdown
```

## 4. Comandos de Verificacion
* Compilar binario: `go build -o bin/felix cmd/felix/main.go`
* Ejecutar suite de pruebas: `go test -count=1 ./...`
* Iniciar en modo demonio: `./bin/felix`
* Asistente de configuracion: `./bin/felix setup`
