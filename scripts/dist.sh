#!/usr/bin/env bash
# dist.sh VERSION: builds sltty for every release platform into dist/, one
# archive each (tar.gz; zip for Windows) with the README and the license,
# and SHA256SUMS. The Telegram app credentials are taken from
# SLTTY_TELEGRAM_API_ID / SLTTY_TELEGRAM_API_HASH when set.
set -euo pipefail

version=${1:?usage: dist.sh VERSION}
platforms=${PLATFORMS:-"darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64"}
out=dist
ldflags="-s -w -X main.version=$version"
if [[ -n "${SLTTY_TELEGRAM_API_ID:-}" ]]; then
  ldflags+=" -X github.com/oscarmlage/sltty/internal/chat.appID=$SLTTY_TELEGRAM_API_ID"
  ldflags+=" -X github.com/oscarmlage/sltty/internal/chat.appHash=${SLTTY_TELEGRAM_API_HASH:?SLTTY_TELEGRAM_API_HASH too}"
else
  echo "warning: no SLTTY_TELEGRAM_API_ID: the binaries have no built-in Telegram app" >&2
fi

rm -rf "$out"
mkdir -p "$out"
for p in $platforms; do
  os=${p%/*} arch=${p#*/}
  name="sltty_${version#v}_${os}_${arch}"
  dir="$out/$name"
  exe=sltty
  [[ $os == windows ]] && exe=sltty.exe
  mkdir -p "$dir"
  echo "building $name"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$dir/$exe" ./cmd/sltty
  cp README.md LICENSE NOTICE.md "$dir/"
  if [[ $os == windows ]]; then
    (cd "$out" && zip -qr "$name.zip" "$name")
  else
    tar -C "$out" -czf "$out/$name.tar.gz" "$name"
  fi
  rm -rf "$dir"
done

(cd "$out" && if command -v sha256sum >/dev/null; then sha256sum ./*.tar.gz ./*.zip; else shasum -a 256 ./*.tar.gz ./*.zip; fi | sed 's# \./# #' > SHA256SUMS)
ls -l "$out"
