#!/usr/bin/env bash
# Deploy Antigravity Agent for Reefblast Game Definitions Updates
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
UPDATE_FILE="$ROOT_DIR/game_update_detected.json"
AGY_BIN="/home/agent/.local/bin/agy"

echo "========================================================"
echo "🛰️ Antigravity Reefblast Autonomous Update Fixer Agent"
echo "========================================================"

# Check if container has a newer update detected
if docker exec reefblast-bot-william test -f /app/logs/game_update_detected.json 2>/dev/null; then
  docker exec reefblast-bot-william cat /app/logs/game_update_detected.json > "$UPDATE_FILE"
elif docker exec reefblast-bot test -f /app/logs/game_update_detected.json 2>/dev/null; then
  docker exec reefblast-bot cat /app/logs/game_update_detected.json > "$UPDATE_FILE"
fi

if [ -f "$UPDATE_FILE" ]; then
  echo "Detected game definition updates:"
  cat "$UPDATE_FILE"
  echo ""
else
  echo "No unhandled schema drift detected. Game definitions are current."
fi

if [ "$1" == "--deploy" ] || [ "$1" == "-d" ]; then
  echo "🚀 Launching Antigravity agent in background to verify and remediate..."
  if [ -x "$AGY_BIN" ]; then
    SUMMARY=$(cat "$UPDATE_FILE" 2>/dev/null || echo "Reefblast live update check")
    $AGY_BIN -p "Reefblast update detected: $SUMMARY. Verify bot daemons, AM-GM skill weights, and equipment." &
    echo "Antigravity agent dispatched with PID $!"
  else
    echo "⚠️ agy CLI binary not found at $AGY_BIN"
  fi
else
  echo "To deploy Antigravity agent automatically, run with: $0 --deploy"
fi
