#!/usr/bin/env bash
# Prints a shields.io endpoint JSON for the total coverage in coverage.out
# (go test -coverpkg=./... -coverprofile=coverage.out ./...).
set -euo pipefail
pct=$(go tool cover -func coverage.out | awk '/^total:/ {gsub("%","",$3); print $3}')
color=red
awk -v p="$pct" 'BEGIN{exit !(p>=80)}' && color=brightgreen || {
  awk -v p="$pct" 'BEGIN{exit !(p>=70)}' && color=green || {
    awk -v p="$pct" 'BEGIN{exit !(p>=60)}' && color=yellow || {
      awk -v p="$pct" 'BEGIN{exit !(p>=50)}' && color=orange; }; }; }
printf '{"schemaVersion":1,"label":"coverage","message":"%.0f%%","color":"%s"}\n' "$pct" "$color"
