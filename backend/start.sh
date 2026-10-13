#!/bin/bash
# Script de démarrage du backend NOVA API
# Usage: ./start.sh (démarre en arrière-plan)
#        ./start.sh -f (démarre en premier plan)

cd "$(dirname "$0")"

# Charger la configuration
set -a
source .env 2>/dev/null || { echo "❌ Fichier .env manquant"; exit 1; }
set +a

# Forcer le DATABASE_URL Neon (écrase l'éventuel DATABASE_URL global SQLite)
export DATABASE_URL="${DATABASE_URL:?DATABASE_URL is required}"

# Tuer les instances existantes
pkill -9 -f "bin/nova-api" 2>/dev/null
sleep 1

# Compiler si le binaire n'existe pas ou si le code a changé
if [ ! -f bin/nova-api ] || [ "$(find internal cmd -name '*.go' -newer bin/nova-api 2>/dev/null | head -1)" ]; then
  echo "📦 Compilation..."
  export PATH="/home/z/.local/go/bin:$PATH"
  go build -o bin/nova-api ./cmd/server || { echo "❌ Build failed"; exit 1; }
fi

if [ "$1" = "-f" ]; then
  echo "🚀 Démarrage en premier plan (port 8080)..."
  exec ./bin/nova-api
else
  echo "🚀 Démarrage en arrière-plan (port 8080)..."
  nohup ./bin/nova-api > /tmp/nova-api.log 2>&1 &
  echo $! > /tmp/nova-api.pid
  echo "PID: $(cat /tmp/nova-api.pid)"
  echo "Logs: /tmp/nova-api.log"
  sleep 3
  if curl -s --max-time 2 http://localhost:8080/health > /dev/null 2>&1; then
    echo "✅ Backend prêt sur http://localhost:8080"
  else
    echo "⚠️ Le backend ne répond pas encore — vérifiez /tmp/nova-api.log"
  fi
fi
