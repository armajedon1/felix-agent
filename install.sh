#!/usr/bin/env bash
set -euo pipefail

echo "=============================================================="
echo "         FELIX - ASISTENTE AUTONOMO OPEN SOURCE               "
echo "              Script de Instalacion Rapida                    "
echo "=============================================================="

# 1. Comprobar arquitectura y sistema operativo
OS="$(uname -s)"
if [ "$OS" != "Linux" ]; then
    echo "Advertencia: Felix esta optimizado para sistemas Linux. Sistema detectado: $OS"
fi

# 2. Verificar Go
if ! command -v go &>/dev/null; then
    echo "Error: Go no esta instalado o no se encuentra en el PATH."
    echo "Por favor instale Go (>= 1.22): https://go.dev/doc/install"
    exit 1
fi

GO_VERSION="$(go version | awk '{print $3}')"
echo "Go detectado: $GO_VERSION"

# 3. Verificar dependencias opcionales de multimedia
if ! command -v ffmpeg &>/dev/null; then
    echo "[Aviso] ffmpeg no esta instalado. Es requerido para transcripcion y notas de voz OGG Opus."
    if command -v apt &>/dev/null; then
        echo "Puede instalarlo ejecutando: sudo apt install -y ffmpeg"
    elif command -v pacman &>/dev/null; then
        echo "Puede instalarlo ejecutando: sudo pacman -S ffmpeg"
    elif command -v dnf &>/dev/null; then
        echo "Puede instalarlo ejecutando: sudo dnf install -y ffmpeg"
    fi
fi

# 4. Crear estructura de carpetas de datos
echo "Creando estructura de directorios locales..."
mkdir -p bin data/downloads data/backups workspace/reports workspace/memory skills

# 5. Configurar archivo de configuracion inicial si no existe
if [ ! -f config.json ]; then
    echo "Generando config.json desde plantilla config.example.json..."
    cp config.example.json config.json
fi

# 6. Compilar el binario
echo "Compilando binario de Felix en bin/felix..."
go build -o bin/felix cmd/felix/main.go
echo "Compilacion completada con exito."

echo "=============================================================="
echo " INSTALACION COMPLETADA"
echo " Para iniciar Felix y escanear el codigo QR en pantalla, ejecute:"
echo "   ./bin/felix"
echo ""
echo " O para configurar sus claves de API antes de iniciar:"
echo "   ./bin/felix setup"
echo "=============================================================="
