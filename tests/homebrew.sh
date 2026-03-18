#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
VERSION=$(tr -d '\n' < "$REPO_DIR/VERSION")

if ! command -v brew >/dev/null 2>&1; then
  printf 'brew not found; skipping Homebrew test.\n'
  exit 0
fi

TMP_ROOT=$(mktemp -d)
TAP_DIR="$TMP_ROOT/homebrew-tap"
TAP_NAME="local/sonyport-test"
FORMULA_NAME="$TAP_NAME/sonyport"
ARCHIVE_DIR="$TMP_ROOT/archive"
PACKAGE_DIR="$ARCHIVE_DIR/sonyport_${VERSION}_darwin_amd64"
ARCHIVE_PATH="$TMP_ROOT/sonyport_${VERSION}_darwin_amd64.zip"
BUILT_BIN="$PACKAGE_DIR/sonyport"

cleanup() {
  HOMEBREW_NO_AUTO_UPDATE=1 brew uninstall --force "$FORMULA_NAME" >/dev/null 2>&1 || true
  HOMEBREW_NO_AUTO_UPDATE=1 brew untap "$TAP_NAME" >/dev/null 2>&1 || true
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

mkdir -p "$TAP_DIR/Formula" "$PACKAGE_DIR"

(
  cd "$REPO_DIR"
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$BUILT_BIN" ./cmd/sonyport
)

( cd "$ARCHIVE_DIR" && zip -qr "$ARCHIVE_PATH" "$(basename "$PACKAGE_DIR")" )
ARCHIVE_SHA256=$(shasum -a 256 "$ARCHIVE_PATH" | awk '{print $1}')

cat > "$TAP_DIR/Formula/sonyport.rb" <<EOF
class Sonyport < Formula
  desc "Unofficial macOS CLI for importing photos and videos from mounted Sony cameras"
  homepage "https://github.com/marcy326/sonyport"
  url "file://$ARCHIVE_PATH"
  version "$VERSION"
  sha256 "$ARCHIVE_SHA256"
  license "MIT"

  def install
    bin.install "sonyport"
  end

  test do
    assert_match "Usage:", shell_output("#{bin}/sonyport --help")
  end
end
EOF

git init -b main "$TAP_DIR" >/dev/null
( cd "$TAP_DIR" && git add . && git -c user.name='codex' -c user.email='codex@example.com' commit -m 'test tap' >/dev/null )

HOMEBREW_NO_AUTO_UPDATE=1 brew untap "$TAP_NAME" >/dev/null 2>&1 || true
HOMEBREW_NO_AUTO_UPDATE=1 brew tap "$TAP_NAME" "$TAP_DIR" >/dev/null
HOMEBREW_NO_AUTO_UPDATE=1 brew install --formula --skip-link "$FORMULA_NAME" >/dev/null

CELLAR_BIN="$(brew --cellar sonyport)/$VERSION/bin/sonyport"
case "$("$CELLAR_BIN" --help)" in
  *"Usage:"* ) ;;
  * )
    printf 'Expected help output from Homebrew-installed formula binary.\n' >&2
    exit 1
    ;;
esac
