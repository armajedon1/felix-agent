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

# Instalar binario en felix-server y .local/bin
SERVER_BIN_DIR="$HOME/felix-server/bin"
if [ -d "$SERVER_BIN_DIR" ]; then
    install -m 755 bin/felix "$SERVER_BIN_DIR/felix"
    echo "[OK] Binario instalado en $SERVER_BIN_DIR/felix"
fi

TARGET_DIR="$HOME/.local/bin"
mkdir -p "$TARGET_DIR"
install -m 755 bin/felix "$TARGET_DIR/felix"
echo "[OK] Binario instalado en $TARGET_DIR/felix"

# Limpieza de lanzadores de escritorio obsoletos
rm -f "$HOME/.local/share/applications/felix.desktop" 2>/dev/null || true

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
