#!/usr/bin/env bash
# Сборка статических бинарников монитора и графика для linux amd64/arm64.
# Результат: monitor-linux-amd64|arm64, graph-linux-amd64|arm64
set -euo pipefail
cd "$(dirname "$0")"

LDFLAGS="-s -w"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o monitor-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o monitor-linux-arm64 .

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o graph-linux-amd64 ./cmd/graph
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o graph-linux-arm64 ./cmd/graph

ls -lh monitor-linux-* graph-linux-*

# Опционально: сжать бинарники UPX (примерно 3-4 МБ вместо 7-9):
#   upx -9 monitor-linux-amd64 monitor-linux-arm64 graph-linux-amd64 graph-linux-arm64
