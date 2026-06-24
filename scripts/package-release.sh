#!/usr/bin/env sh
set -eu

TARGET_OS="${GOOS:-$(uname | tr '[:upper:]' '[:lower:]')}"
TARGET_ARCH="${GOARCH:-amd64}"
OUT_DIR="${OUT_DIR:-dist}"

case "$TARGET_OS" in
  mingw*|msys*|cygwin*) TARGET_OS="windows" ;;
  darwin|linux|windows) ;;
  *) echo "Unsupported GOOS: $TARGET_OS" >&2; exit 2 ;;
esac

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

PACKAGE_NAME="vivurouter-$TARGET_OS-$TARGET_ARCH"
PACKAGE_DIR="$OUT_DIR/$PACKAGE_NAME"
mkdir -p "$PACKAGE_DIR"

if [ "$TARGET_OS" = "windows" ]; then
  EXE_NAME="vivurouter.exe"
  RTK_NAME="rtk.exe"
else
  EXE_NAME="vivurouter"
  RTK_NAME="rtk"
fi

GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -o "$PACKAGE_DIR/$EXE_NAME" ./cmd/vivurouter-go

if [ -f "$ROOT/$RTK_NAME" ]; then
  cp "$ROOT/$RTK_NAME" "$PACKAGE_DIR/$RTK_NAME"
else
  echo "warning: RTK binary '$RTK_NAME' not found at project root. Package will rely on PATH or user-provided RTK path." >&2
fi

[ -f "$ROOT/README.md" ] && cp "$ROOT/README.md" "$PACKAGE_DIR/README.md" || true
[ -d "$ROOT/docs" ] && cp -R "$ROOT/docs" "$PACKAGE_DIR/docs" || true

echo "Package created: $PACKAGE_DIR"
echo "Expected RTK binary for $TARGET_OS: $RTK_NAME"
