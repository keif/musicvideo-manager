#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="$(dirname "$0")/../testdata/fixtures"
mkdir -p "$OUT_DIR"

ffmpeg -y \
  -f lavfi -i "testsrc=duration=3:size=320x240:rate=25" \
  -f lavfi -i "sine=duration=3" \
  -c:v libx264 -c:a aac -shortest \
  "$OUT_DIR/sample_short.mp4"

echo "this is not a real video file" > "$OUT_DIR/sample_corrupt.mp4"

echo "Wrote fixtures to $OUT_DIR"
