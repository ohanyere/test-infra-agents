#!/usr/bin/env bash
set -euo pipefail
mapfile -t files < <(find backend -type f -name '*.go' -not -path '*/vendor/*' | sort)
unformatted=$(gofmt -l "${files[@]}")
if [[ -n "$unformatted" ]]; then
  echo "gofmt required:" >&2
  echo "$unformatted" >&2
  exit 1
fi
echo "gofmt: clean"
