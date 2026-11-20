#!/usr/bin/env bash
set -euo pipefail

echo "=============================================================="
echo "         FELIX - SISTEMA DE ACTUALIZACION AUTOMATICA          "
echo "=============================================================="

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_DIR"

echo "Directorio: $REPO_DIR"
echo "Comprobando actualizaciones en GitHub..."

# Comprobar conexion con remoto
if ! git fetch origin main; then
    echo "[ERROR] No se pudo conectar con el repositorio remoto en GitHub."
    exit 1
fi

LOCAL_COMMIT="$(git rev-parse HEAD)"
REMOTE_COMMIT="$(git rev-parse origin/main)"

if [ "$LOCAL_COMMIT" = "$REMOTE_COMMIT" ]; then
    echo "[OK] Felix ya se encuentra en la version mas reciente ($(git rev-parse --short HEAD))."
    exit 0
fi

echo "--------------------------------------------------------------"
echo "Nuevas actualizaciones encontradas:"
git log HEAD..origin/main --oneline
echo "--------------------------------------------------------------"

echo "Descargando ultimos cambios..."
git pull origin main

echo "Compilando nueva version..."
go build -o bin/felix cmd/felix/main.go

TARGET_DIR="$HOME/.local/bin"
mkdir -p "$TARGET_DIR"
install -m 755 bin/felix "$TARGET_DIR/felix"
echo "[OK] Binario instalado en $TARGET_DIR/felix"

# Actualizar lanzador de escritorio
DESKTOP_FILE="$HOME/.local/share/applications/felix.desktop"
if [ -f "$DESKTOP_FILE" ] || [ -n "${XDG_CURRENT_DESKTOP:-}" ]; then
    mkdir -p "$(dirname "$DESKTOP_FILE")"
    cat <<EOF > "$DESKTOP_FILE"
[Desktop Entry]
Name=Felix Desktop
Comment=Centro de Control, Chat y Administracion de Bots
Exec=$TARGET_DIR/felix desktop
Icon=utilities-terminal
Terminal=false
Type=Application
Categories=Development;Utility;
EOF
fi

# Reiniciar servicio PM2 si existe
if command -v pm2 &>/dev/null; then
    if pm2 list 2>/dev/null | grep -q "felix-server"; then
        echo "Reiniciando servicio en segundo plano felix-server..."
        pm2 restart felix-server || true
    fi
fi

echo "=============================================================="
echo "         ACTUALIZACION COMPLETADA EXITOSAMENTE                "
echo "=============================================================="
echo "Version activa: $(git rev-parse --short HEAD)"
