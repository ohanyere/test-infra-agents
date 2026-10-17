#!/usr/bin/env bash
set -euo pipefail

migration_dir="backend/migrations"
expected=1
for up in "$migration_dir"/*.up.sql; do
  base="${up##*/}"
  number="${base%%_*}"
  numeric=$((10#$number))
  if [[ "$numeric" -ne "$expected" ]]; then
    echo "migration sequence error: expected $(printf '%06d' "$expected"), found $number" >&2
    exit 1
  fi
  down="${up%.up.sql}.down.sql"
  if [[ ! -f "$down" ]]; then
    echo "migration pair missing: $down" >&2
    exit 1
  fi
  expected=$((expected + 1))
done
latest=$((expected - 1))
if [[ "$latest" -ne 28 ]]; then
  echo "migration latest version is $latest, expected 28; update the release gate deliberately" >&2
  exit 1
fi
down_count=$(find "$migration_dir" -maxdepth 1 -name '*.down.sql' | wc -l)
if [[ "$down_count" -ne "$latest" ]]; then
  echo "migration up/down count mismatch: up=$latest down=$down_count" >&2
  exit 1
fi
echo "migrations: contiguous 000001..$(printf '%06d' "$latest") with matching up/down files"
