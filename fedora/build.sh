#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")/.."
command -v go >/dev/null || { echo 'Instale Go: sudo dnf install golang' >&2; exit 1; }
case ${1:-build} in
  test)
    GOTOOLCHAIN=local go test ./...
    GOTOOLCHAIN=local go vet ./...
    ;;
  build)
    mkdir -p build
    CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -o build/discord-golive-bypass .
    echo "Compilado: $PWD/build/discord-golive-bypass"
    ;;
  *) echo 'Use: ./fedora/build.sh [build|test]' >&2; exit 1 ;;
esac
