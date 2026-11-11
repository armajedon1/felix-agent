# Directrices de Desarrollo para Agentes y Desarrolladores (Felix Open Source)

## 1. Descripcion del Proyecto
Felix es un asistente y agente autonomo de ingenieria, DevOps y administracion de sistemas Linux desplegado en Go. Se comunica bidireccionalmente con el usuario a traves de WhatsApp utilizando WhatsMeow (protocolo nativo WebSocket E2E), soporta ejecucion autonoma de herramientas (ReAct Loop), orquestacion concurrente de subagentes, telemetria en tiempo real y protocolo MCP.

## 2. Filosofia de Diseno y Reglas Estrictas
1. **Prohibicion estricta de emojis:** NUNCA usar emojis en ningun mensaje, codigo fuente, comentarios, commits, scripts o documentacion tecnica.
2. **KISS y YAGNI (Cero sobreingenieria):** Resolver problemas con funciones directas y APIs nativas.
3. **Tipado estricto y sobriedad tecnica:** Respuestas y salidas limpias, orientadas a la maxima eficiencia de tokens.
4. **Ciclo autonomo de verificacion (TDD):** Antes de entregar o desplegar cambios, verificar la suite completa con `go test -count=1 ./...` asegurando 100% de exito.
5. **Aislamiento y portabilidad:** Todo codigo debe ser portable a cualquier distribucion Linux (Debian, Ubuntu, Arch, Fedora) sin asumir nombres de usuario fijos ni rutas absolutas predeterminadas.

## 3. Estructura de Directorios
```text
felix-open-source/
├── cmd/felix/main.go            # Punto de entrada y CLI
├── config.example.json          # Plantilla de configuracion
├── data/                        # Bases de datos SQLite y descargas (excluido de git)
├── internal/
│   ├── agent/                   # ReAct Loop, Engine, Subagentes y Persistencia
│   ├── config/                  # Carga y persistencia atomica de config.json
│   ├── digest/                  # Telemetria asincrona en memoria
│   ├── fastpath/                # Enrutador deterministico (< 5ms)
│   ├── healer/                  # Diagnostico y auto-curacion de servicios
│   ├── health/                  # Servidor HTTP Prometheus y webhook (/healthz, /metrics)
│   ├── mcp/                     # Cliente STDIO para Model Context Protocol
│   ├── memory/                  # RAG local, indexacion FTS5 y SOPs procedurales
│   ├── minecraft/               # Protocolo RCON para servidores de juego
│   ├── scheduler/               # Planificador cron y recordatorios persistentes
│   ├── tools/                   # Catalogo de 38 herramientas agenticas
│   ├── users/                   # Control de acceso basado en roles (RBAC) y vetos
│   └── whatsapp/                # WhatsMeow, impresion QR, transcriptor Whisper y comandos
├── skills/                      # Catalogo extensible de habilidades
└── workspace/                   # Espacio de trabajo y reportes
```

## 4. Comandos de Verificacion
- Compilar binario: `go build -o bin/felix cmd/felix/main.go`
- Ejecutar suite de pruebas: `go test -count=1 ./...`
- Iniciar en modo demonio: `./bin/felix`
- Asistente de configuracion: `./bin/felix setup`
