#!/usr/bin/env bash
#
# build.sh - cross-compile the Go device scanner for Linux and Windows.
#
# Usage:
#   chmod +x build.sh
#   ./build.sh
#
# Output:
#   bin/scan-devices        (linux/amd64)
#   bin/scan-devices.exe    (windows/amd64)
#
set -euo pipefail

cd "$(dirname "$0")"
mkdir -p bin

echo "==> Building Linux binary: bin/scan-devices"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -ldflags "-s -w -X main.buildOS=linux -X main.buildArch=amd64" \
    -o bin/scan-devices ./scanner

echo "==> Building Windows binary: bin/scan-devices.exe"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath \
    -ldflags "-s -w -X main.buildOS=windows -X main.buildArch=amd64" \
    -o bin/scan-devices.exe ./scanner

chmod +x bin/scan-devices
echo "==> Done"
ls -la bin