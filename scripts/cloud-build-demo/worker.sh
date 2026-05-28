#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: worker.sh <server-url> <worker-id> <worker-token>" >&2
  exit 2
fi

server_url="${1%/}"
worker_id="$2"
worker_token="$3"

signal_json="$(curl -fsS \
  -H "Authorization: Bearer ${worker_token}" \
  "${server_url}/workers/${worker_id}/signal?timeout=300s")"
command="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["command"])' <<<"${signal_json}")"
job_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["jobID"])' <<<"${signal_json}")"

set +e
output="$(bash -lc "${command}" 2>&1)"
status=$?
set -e

if [[ ${status} -eq 0 ]]; then
  event_type="status"
  event_status="completed"
  error=""
else
  event_type="status"
  event_status="failed"
  error="exit status ${status}"
fi

EVENT_TYPE="${event_type}" EVENT_STATUS="${event_status}" OUTPUT="${output}" ERROR="${error}" \
SERVER_URL="${server_url}" JOB_ID="${job_id}" \
python3 - <<'PY' | curl -fsS -X POST \
  -H 'Content-Type: application/json' \
  --data-binary @- \
  "${server_url}/jobs/${job_id}/events"
import json
import os

print(json.dumps({
    "type": os.environ["EVENT_TYPE"],
    "status": os.environ["EVENT_STATUS"],
    "output": os.environ["OUTPUT"],
    "error": os.environ["ERROR"],
}))
PY
