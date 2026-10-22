#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

echo "======================================================"
echo "    AI Desk: Testing 429 Rate-Limit Auto-Failover     "
echo "======================================================"

MOCK_TOOL="$SCRIPT_DIR/mock_rate_limit_agent.sh"

cat << 'EOF' > "$MOCK_TOOL"
#!/usr/bin/env bash
# Mock agent CLI simulating 429 rate limit on first invocation and success on --resume

if [[ "$*" == *"--resume"* ]]; then
  echo "[Mock-Agent] Received --resume flag!"
  echo "[Mock-Agent] Session successfully continued with new credentials."
  exit 0
else
  echo "[Mock-Agent] Running query on current account..."
  echo "Error: HTTP 429 (Resource has been exhausted). Please wait for quota reset."
  exit 1
fi
EOF
chmod +x "$MOCK_TOOL"

cd "$ROOT_DIR"
echo "--> Executing desk-shim with simulated rate-limiting CLI..."
./bin/desk-shim "$MOCK_TOOL" -p "Generate feature"

echo ""
echo "======================================================"
echo "  ✓ 429 Auto-Failover & Resume verified successfully! "
echo "======================================================"
rm -f "$MOCK_TOOL"
