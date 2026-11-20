#!/usr/bin/env bash
set -euo pipefail

echo "=============================================================="
echo "         FELIX - ASISTENTE AUTONOMO OPEN SOURCE               "
echo "              Script de Instalacion Rapida                    "
echo "=============================================================="

# 1. Comprobar arquitectura y sistema operativo
OS="$(uname -s)"
ARCH="$(uname -m)"
echo "Sistema detectado: $OS ($ARCH)"
if [ "$OS" != "Linux" ]; then
    echo "Advertencia: Felix esta optimizado para Linux. Se compilara para su entorno actual."
fi

# 2. Verificar Go
if ! command -v go &>/dev/null; then
    echo "Error: Go no esta instalado o no se encuentra en el PATH."
    echo "Por favor instale Go (>= 1.22): https://go.dev/doc/install"
    exit 1
fi

GO_VERSION="$(go version | awk '{print $3}')"
echo "Go detectado: $GO_VERSION"

# 3. Dependencias multimedia opcionales
if ! command -v ffmpeg &>/dev/null; then
    echo "[Aviso] ffmpeg no esta instalado (opcional para transcripcion y notas de voz)."
fi

# 4. Crear estructura de carpetas
echo "Creando estructura de directorios locales..."
mkdir -p bin data/downloads data/backups data/avatars workspace/reports workspace/memory skills

# 5. Configurar archivo de configuracion inicial si no existe
if [ ! -f config.json ]; then
    echo "Generando config.json desde plantilla config.example.json..."
    cp config.example.json config.json
fi

# 6. Compilar el binario
echo "Compilando binario nativo de Felix..."
go build -o bin/felix cmd/felix/main.go
echo "Compilacion completada con exito en bin/felix."

# 7. Instalacion global en ~/.local/bin
TARGET_DIR="$HOME/.local/bin"
mkdir -p "$TARGET_DIR"
install -m 755 bin/felix "$TARGET_DIR/felix"
echo "Binario instalado en $TARGET_DIR/felix"

# Asegurar que ~/.local/bin este en PATH
if [[ ":$PATH:" != *":$TARGET_DIR:"* ]]; then
    echo "Aviso: $TARGET_DIR no figura en su PATH."
    if [ -f "$HOME/.bashrc" ]; then
        if ! grep -q 'export PATH="$HOME/.local/bin:$PATH"' "$HOME/.bashrc"; then
            echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.bashrc"
            echo "Se agrego $TARGET_DIR al PATH en su ~/.bashrc."
        fi
    fi
fi

# 8. Integracion de escritorio (Linux Desktop)
DESKTOP_DIR="$HOME/.local/share/applications"
if [ -d "$DESKTOP_DIR" ] || [ -n "${XDG_CURRENT_DESKTOP:-}" ]; then
    mkdir -p "$DESKTOP_DIR"
    cat <<EOF > "$DESKTOP_DIR/felix.desktop"
[Desktop Entry]
Name=Felix Agent
Comment=Consola y Panel de Control de Felix Agent
Exec=$TARGET_DIR/felix desktop
Icon=utilities-terminal
Terminal=false
Type=Application
Categories=Development;Utility;
EOF
    echo "Lanzador de escritorio registrado en $DESKTOP_DIR/felix.desktop"
fi

echo "=============================================================="
echo "         INSTALACION COMPLETADA EXITOSAMENTE                  "
echo "=============================================================="
echo " El ejecutable 'felix' ya esta disponible en su terminal."
echo ""
echo " Opciones recomendadas para comenzar:"
echo "   felix                  -> Menu interactivo y presentacion ASCII"
echo "   felix desktop          -> Ventana de escritorio con panel ABM"
echo "   felix web              -> Consola web en navegador (puerto 8056)"
echo "   felix cli              -> Consola de chat interactivo en terminal"
echo "   felix status           -> Estado de salud del sistema y modelos"
echo "   felix bots             -> Administrador de catalogo de bots"
echo "   felix server           -> Iniciar servicio en segundo plano (daemon)"
echo "=============================================================="
