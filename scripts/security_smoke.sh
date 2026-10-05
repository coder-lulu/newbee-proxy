#!/usr/bin/env bash
set -euo pipefail

# Security smoke test for worker agent
# Requirements: curl, openssl
# Usage: BASE_URL=http://127.0.0.1:8080 JWT_SECRET=dev-secret ORIGIN_ALLOWED=http://localhost ORIGIN_BLOCKED=http://evil \
#        bash newbee-proxy/scripts/security_smoke.sh

BASE_URL=${BASE_URL:-"http://127.0.0.1:8080"}
JWT_SECRET=${JWT_SECRET:-""}
ORIGIN_ALLOWED=${ORIGIN_ALLOWED:-""}
ORIGIN_BLOCKED=${ORIGIN_BLOCKED:-""}
REQUESTS=${REQUESTS:-20}

red() { echo -e "\033[31m$*\033[0m"; }
green() { echo -e "\033[32m$*\033[0m"; }
yellow() { echo -e "\033[33m$*\033[0m"; }

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

make_jwt() {
  # args: exp_seconds_from_now
  local exp=$(( $(date +%s) + $1 ))
  local header='{"alg":"HS256","typ":"JWT"}'
  local payload="{\"sessionId\":\"smoke\",\"exp\":$exp}"
  local h b sig
  h=$(printf '%s' "$header" | b64url)
  b=$(printf '%s' "$payload" | b64url)
  sig=$(printf '%s' "$h.$b" | openssl dgst -binary -sha256 -hmac "$JWT_SECRET" | b64url)
  printf '%s.%s.%s' "$h" "$b" "$sig"
}

ok=0; fail=0
check() { local code=$1 exp=$2 msg=$3; if [[ "$code" == "$exp" ]]; then green "PASS $msg ($code)"; ok=$((ok+1)); else red "FAIL $msg (got=$code exp=$exp)"; fail=$((fail+1)); fi }

echo "[1] Health endpoint"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/health")
check "$code" 200 "/health returns 200"

echo "[2] Protected path without JWT"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/api/ssh/websocket")
echo "got $code (401 expected when enforce=true; 400/101 possible when not enforced or WS upgrade)"

if [[ -n "$JWT_SECRET" ]]; then
  echo "[3] Protected path with expired JWT"
  expired=$(make_jwt -120)
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $expired" "$BASE_URL/api/ssh/websocket")
  echo "got $code (401 expected when enforce=true)"

  echo "[4] Protected path with valid JWT"
  valid=$(make_jwt 300)
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $valid" "$BASE_URL/api/ssh/websocket")
  echo "got $code (101 or 400 expected due to WS handshake)"
fi

if [[ -n "$ORIGIN_BLOCKED" ]]; then
  echo "[5] Origin whitelist - blocked origin preflight"
  code=$(curl -s -o /dev/null -w '%{http_code}' -X OPTIONS -H "Origin: $ORIGIN_BLOCKED" -H 'Access-Control-Request-Method: GET' "$BASE_URL/api/ssh/websocket")
  echo "got $code (403 expected when enforce=true; 204 when observe)"
fi

if [[ -n "$ORIGIN_ALLOWED" ]]; then
  echo "[6] Origin whitelist - allowed origin preflight"
  hdr=$(mktemp)
  code=$(curl -s -D "$hdr" -o /dev/null -w '%{http_code}' -X OPTIONS -H "Origin: $ORIGIN_ALLOWED" -H 'Access-Control-Request-Method: GET' "$BASE_URL/api/ssh/websocket")
  allow=$(grep -i '^Access-Control-Allow-Origin:' "$hdr" | awk '{print $2}' | tr -d '\r') || true
  rm -f "$hdr"
  echo "got $code, Allow-Origin=$allow (204 expected; origin echoed when list non-empty)"
fi

echo "[7] Rate limiting /status ($REQUESTS rapid requests)"
rl429=0
for i in $(seq 1 "$REQUESTS"); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/status")
  if [[ "$code" == "429" ]]; then rl429=$((rl429+1)); fi
done
echo "rate limit 429 count: $rl429 (>=1 expected when rate limit enabled and threshold exceeded)"

yellow "Summary: PASS=$ok FAIL=$fail"
exit 0
