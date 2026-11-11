#!/usr/bin/env bash
set -euo pipefail

# Compilar si el binario no existe
if [ ! -f bin/felix ]; then
    echo "Compilando binario bin/felix..."
    mkdir -p bin
    go build -o bin/felix cmd/felix/main.go
fi

# Copiar configuracion base si no existe
if [ ! -f config.json ]; then
    cp config.example.json config.json
fi

# Iniciar Felix
exec ./bin/felix "$@"
