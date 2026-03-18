#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
VERSION=$(tr -d '\n' < "$REPO_DIR/VERSION")

TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT

BIN="$TMP_ROOT/sonyport"
HOME_DIR="$TMP_ROOT/home"
mkdir -p "$HOME_DIR"

(
  cd "$REPO_DIR"
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$BIN" ./cmd/sonyport
)

"$BIN" --help >/dev/null
[ "$("$BIN" --version)" = "$VERSION" ]
[ "$("$BIN" --dry-run --help >/dev/null 2>&1; printf %s "$?")" = "0" ]

SRC="$TMP_ROOT/source"
mkdir -p "$SRC/DCIM/100MSDCF"
printf 'photo-data' > "$SRC/DCIM/100MSDCF/DSC0001.JPG"
printf 'video-data' > "$SRC/DCIM/100MSDCF/C0001.MP4"
touch -t 202503011122 "$SRC/DCIM/100MSDCF/DSC0001.JPG"
touch -t 202503011123 "$SRC/DCIM/100MSDCF/C0001.MP4"

CANCEL_DEST="$TMP_ROOT/cancel"
CANCEL_OUTPUT=$(printf 'n\n' | HOME="$HOME_DIR" "$BIN" --source "$SRC" "$CANCEL_DEST" 2>&1)
case "$CANCEL_OUTPUT" in
  *"Scanning source:"* ) ;;
  * )
    printf 'Expected scanning progress output for cancelled run.\n' >&2
    exit 1
    ;;
esac
case "$CANCEL_OUTPUT" in
  *"Import Summary"* ) ;;
  * )
    printf 'Expected summary output for cancelled run.\n' >&2
    exit 1
    ;;
esac
[ ! -d "$CANCEL_DEST" ]

FIRST_DEST="$TMP_ROOT/first"
HOME="$HOME_DIR" "$BIN" --yes --source "$SRC" "$FIRST_DEST" >/dev/null
[ -f "$FIRST_DEST/2025-03-01/DSC0001.JPG" ]
[ -f "$FIRST_DEST/2025-03-01/C0001.MP4" ]
[ -f "$SRC/DCIM/100MSDCF/DSC0001.JPG" ]
[ -f "$SRC/DCIM/100MSDCF/C0001.MP4" ]
STATE_PATH="$HOME_DIR/Library/Application Support/sonyport/state.json"
[ -f "$STATE_PATH" ]

SECOND_OUTPUT=$(HOME="$HOME_DIR" "$BIN" --yes --source "$SRC" 2>&1)
case "$SECOND_OUTPUT" in
  *"$FIRST_DEST"* ) ;;
  * )
    printf 'Expected previous destination in summary output.\n' >&2
    exit 1
  ;;
esac
[ "$(cat "$FIRST_DEST/2025-03-01/DSC0001.JPG")" = "photo-data" ]
[ "$(cat "$FIRST_DEST/2025-03-01/C0001.MP4")" = "video-data" ]
[ ! -e "$FIRST_DEST/2025-03-01/DSC0001_1.JPG" ]
[ ! -e "$FIRST_DEST/2025-03-01/C0001_1.MP4" ]

DRY_RUN_OUTPUT=$(HOME="$HOME_DIR" "$BIN" --dry-run --source "$SRC" "$TMP_ROOT/dry-run" 2>&1)
case "$DRY_RUN_OUTPUT" in
  *"Would import:"* )
    printf 'Did not expect per-file dry-run output without --verbose.\n' >&2
    exit 1
    ;;
esac

VERBOSE_DRY_RUN_OUTPUT=$(HOME="$HOME_DIR" "$BIN" --verbose --dry-run --source "$SRC" "$TMP_ROOT/dry-run-verbose" 2>&1)
case "$VERBOSE_DRY_RUN_OUTPUT" in
  *"Would import:"* ) ;;
  * )
    printf 'Expected per-file dry-run output with --verbose.\n' >&2
    exit 1
    ;;
esac

for MODE in skip rename overwrite; do
  DEST="$TMP_ROOT/$MODE"
  mkdir -p "$DEST/2025-03-01"
  printf 'old-photo' > "$DEST/2025-03-01/DSC0001.JPG"
  printf 'old-video' > "$DEST/2025-03-01/C0001.MP4"
  HOME="$HOME_DIR" "$BIN" --yes --source "$SRC" --duplicate "$MODE" "$DEST" >/dev/null
done

[ "$(cat "$TMP_ROOT/skip/2025-03-01/DSC0001.JPG")" = "old-photo" ]
[ "$(cat "$TMP_ROOT/skip/2025-03-01/C0001.MP4")" = "old-video" ]
[ "$(cat "$TMP_ROOT/rename/2025-03-01/DSC0001.JPG")" = "old-photo" ]
[ "$(cat "$TMP_ROOT/rename/2025-03-01/C0001.MP4")" = "old-video" ]
[ "$(cat "$TMP_ROOT/rename/2025-03-01/DSC0001_1.JPG")" = "photo-data" ]
[ "$(cat "$TMP_ROOT/rename/2025-03-01/C0001_1.MP4")" = "video-data" ]
[ "$(cat "$TMP_ROOT/overwrite/2025-03-01/DSC0001.JPG")" = "photo-data" ]
[ "$(cat "$TMP_ROOT/overwrite/2025-03-01/C0001.MP4")" = "video-data" ]
