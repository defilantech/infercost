#!/usr/bin/env bash
# load-gen.sh — Generate sustained inference traffic for InferCost PoC testing.
#
# Usage:
#   ./hack/load-gen.sh <endpoint> [requests] [concurrency]
#
# Example:
#   ./hack/load-gen.sh http://10.1.19.122:8080 50 2
#
# This sends a series of chat completion requests to a llama.cpp endpoint
# to produce enough token throughput for InferCost to compute meaningful
# cost-per-token metrics.

set -euo pipefail

ENDPOINT="${1:?Usage: $0 <endpoint> [requests] [concurrency]}"
REQUESTS="${2:-20}"
CONCURRENCY="${3:-1}"

PROMPTS=(
  "Explain the concept of GPU amortization in 3 sentences."
  "Write a short paragraph about why companies track inference costs."
  "What is Power Usage Effectiveness (PUE) and why does it matter?"
  "Compare the cost of cloud API inference vs on-premises inference."
  "Describe how Kubernetes operators work in 2 sentences."
  "What are the benefits of running LLMs on your own hardware?"
  "Explain token-based pricing for language models."
  "Write a brief explanation of the FinOps methodology."
  "What is DCGM and how does it help with GPU monitoring?"
  "Describe the concept of cost-per-token for AI inference."
)

send_request() {
  local i=$1
  local prompt="${PROMPTS[$((i % ${#PROMPTS[@]}))]}"
  local start=$(date +%s%N)

  response=$(curl -s -w "\n%{http_code}" "${ENDPOINT}/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -d "{
      \"model\": \"any\",
      \"messages\": [{\"role\": \"user\", \"content\": \"${prompt}\"}],
      \"max_tokens\": 200
    }" 2>/dev/null)

  local http_code=$(echo "$response" | tail -1)
  local body=$(echo "$response" | head -n -1)
  local end=$(date +%s%N)
  local duration_ms=$(( (end - start) / 1000000 ))

  if [ "$http_code" = "200" ]; then
    local total_tokens=$(echo "$body" | grep -o '"total_tokens":[0-9]*' | grep -o '[0-9]*')
    printf "[%3d] %s %4d tokens %5dms\n" "$i" "$http_code" "${total_tokens:-0}" "$duration_ms"
  else
    printf "[%3d] %s FAILED %5dms\n" "$i" "$http_code" "$duration_ms"
  fi
}

echo "InferCost Load Generator"
echo "========================"
echo "Endpoint:    ${ENDPOINT}"
echo "Requests:    ${REQUESTS}"
echo "Concurrency: ${CONCURRENCY}"
echo ""

active=0
for i in $(seq 1 "$REQUESTS"); do
  send_request "$i" &
  active=$((active + 1))

  if [ "$active" -ge "$CONCURRENCY" ]; then
    wait -n 2>/dev/null || true
    active=$((active - 1))
  fi
done

wait
echo ""
echo "Done. Check InferCost metrics in ~30 seconds for updated cost-per-token data."
