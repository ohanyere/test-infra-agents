#!/usr/bin/env bash
set -euo pipefail

diagnostics_dir="${RUNNER_TEMP:?RUNNER_TEMP is required}/playwright-discovery-diagnostics"
mkdir -p "$diagnostics_dir"
summary="$diagnostics_dir/summary.log"
: > "$summary"

mark() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$summary"
}

failed=0
run_probe() {
  local label="$1" limit="$2" start status elapsed
  shift 2
  start="$(date +%s)"
  mark "$label BEGIN limit=${limit}s"
  if timeout --signal=TERM --kill-after=2s "${limit}s" "$@" > "$diagnostics_dir/$label.stdout" 2> "$diagnostics_dir/$label.stderr"; then
    status=0
  else
    status=$?
  fi
  elapsed=$(( $(date +%s) - start ))
  if [ "$status" -eq 0 ]; then
    mark "$label SUCCESS exit=$status elapsed=${elapsed}s"
  elif [ "$status" -eq 124 ] || [ "$status" -eq 137 ]; then
    mark "$label TIMEOUT exit=$status elapsed=${elapsed}s"
    failed=1
  else
    mark "$label FAILURE exit=$status elapsed=${elapsed}s"
    failed=1
  fi
  mark "$label stdout-begin"
  cat "$diagnostics_dir/$label.stdout" | tee -a "$summary"
  mark "$label stdout-end stderr-begin"
  cat "$diagnostics_dir/$label.stderr" | tee -a "$summary"
  mark "$label stderr-end"
  return "$status"
}

smoke_config="tests/operation-discovery-smoke.config.mjs"
smoke_spec="tests/operation-discovery-smoke.spec.mjs"
if [ -e "$smoke_config" ] || [ -e "$smoke_spec" ]; then
  mark "smoke-setup FAILURE temporary files already exist"
  exit 1
fi
trap 'rm -f "$smoke_config" "$smoke_spec"' EXIT
cat > "$smoke_config" <<'EOF'
import { defineConfig } from "@playwright/test";
export default defineConfig({ testDir: ".", testMatch: "operation-discovery-smoke.spec.mjs", reporter: "list" });
EOF
cat > "$smoke_spec" <<'EOF'
import { test } from "@playwright/test";
test("isolated discovery", () => {});
EOF

run_probe node-startup 5 node -e 'console.log("NODE_READY")' || true
run_probe playwright-package-import 15 node --input-type=module -e 'await import("@playwright/test"); console.log("PACKAGE_IMPORT_DONE")' || true
run_probe config-import 15 node --input-type=module -e 'await import("./tests/playwright.config.mjs"); console.log("CONFIG_IMPORT_DONE")' || true
run_probe reporter-import 10 node --input-type=module -e 'await import("./tests/operation-phase-reporter.mjs"); console.log("REPORTER_IMPORT_DONE")' || true
run_probe playwright-cli-version 10 ./node_modules/.bin/playwright --version || true
run_probe isolated-discovery 20 ./node_modules/.bin/playwright test --config="$smoke_config" --list || true
run_probe operation-discovery-list-reporter 30 ./node_modules/.bin/playwright test --config=tests/playwright.config.mjs --reporter=list --list || true
run_probe operation-discovery 45 ./node_modules/.bin/playwright test --config=tests/playwright.config.mjs --list || true

if ! grep -Fq 'Total: 1 test in 1 file' "$diagnostics_dir/isolated-discovery.stdout"; then
  mark "isolated-discovery FAILURE expected test count absent"
  failed=1
fi
if ! grep -Fq 'Total: 2 tests in 1 file' "$diagnostics_dir/operation-discovery.stdout"; then
  mark "operation-discovery FAILURE expected test count absent"
  failed=1
fi
if ! grep -Fq 'Total: 2 tests in 1 file' "$diagnostics_dir/operation-discovery-list-reporter.stdout"; then
  mark "operation-discovery-list-reporter FAILURE expected test count absent"
  failed=1
fi
if [ -n "${OPERATION_PHASE_FILE:-}" ]; then
  mark "operation-spec-marker contents-begin"
  cat "$OPERATION_PHASE_FILE" | tee -a "$summary"
  mark "operation-spec-marker contents-end"
fi
exit "$failed"
