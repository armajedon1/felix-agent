# Directrices de Desarrollo para Agentes y Desarrolladores (Félix Open Source)

## 1. Descripción del Proyecto
Félix es un asistente y agente autónomo de ingeniería, DevOps y desarrollo de software para sistemas Linux desplegado en Go. Cuenta con una arquitectura omnicanal desacoplada (WhatsApp vía WhatsMeow y Telegram vía Bot API), un motor de grafos de estado determinista (LangGraph-style), un arnés nativo de desarrollo de software (AST repomap + fuzzy search/replace + sandbox Git), persistencia transaccional con Time-Travel en SQLite WAL, un catálogo de 39 herramientas de sistema, orquestación concurrente de subagentes y compatibilidad con el estándar Model Context Protocol (MCP).

## 2. Filosofía de Diseño y Reglas Estrictas
1. **Prohibición estricta de emojis:** NUNCA usar emojis en ningún mensaje, código fuente, comentarios, commits, scripts o documentación técnica.
2. **KISS y YAGNI (Cero sobreingeniería):** Resolver problemas con funciones puras, módulos directos y APIs nativas.
3. **Tipado estricto y sobriedad técnica:** Salidas concisas orientadas a la máxima eficiencia de tokens.
4. **Ciclo autónomo de verificación (TDD):** Antes de entregar o desplegar cambios, verificar la suite completa con `go test -count=1 ./...` asegurando 100% de éxito.
5. **Aislamiento y portabilidad:** Todo código debe ser portable a cualquier distribución Linux (Debian, Ubuntu, Arch, Fedora) sin asumir nombres de usuario fijos ni rutas absolutas predeterminadas.

## 3. Estructura de Directorios
```text
felix-open-source/
├── cmd/felix/main.go            # Punto de entrada y CLI
├── config.example.json          # Plantilla base de configuración limpia
├── data/                        # Bases de datos SQLite y descargas locales (ignorado por git)
├── internal/
│   ├── agent/                   # ReAct Loop, Engine, Failover y Subagentes
│   ├── channel/                 # Abstracción omnicanal y pipeline multimodal
│   │   ├── telegram/            # Conector nativo Telegram Bot API en Go puro
│   │   └── whatsapp/            # Adaptador desacoplado WhatsMeow
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
└── workspace/                   # Espacio de trabajo del agente y reportes Markdown
```

## 4. Comandos de Verificación
* Compilar binario: `go build -o bin/felix cmd/felix/main.go`
* Ejecutar suite de pruebas: `go test -count=1 ./...`
* Iniciar en modo demonio: `./bin/felix`
* Asistente de configuración: `./bin/felix setup`
