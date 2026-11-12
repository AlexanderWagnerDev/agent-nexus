#!/usr/bin/env bash
# Install or update the AgentNexus host daemon without a Go toolchain.
#
# Development builds are authenticated GitHub Actions artifacts, not releases.
# GitHub CLI (https://cli.github.com) must be authenticated with read access to
# the private repository. No token is embedded in this script.
set -euo pipefail

REPO="${NEXUS_REPO:-AlexanderWagnerDev/agent-nexus}"
REF="${NEXUS_REF:-main}"
HUB=""
CODE=""
NAME="$(hostname 2>/dev/null || echo host)"
NO_SERVICE=0

usage() {
  cat <<'EOF'
Usage: install-host.sh --hub URL [--code CODE] [--name NAME] [options]

  --hub URL        Public hub URL (required with --code)
  --code CODE      One-time pairing code from Hosts -> Pair a host
  --name NAME      Host name shown in the hub (default: hostname)
  --repo OWNER/REPO
  --ref BRANCH     Branch whose newest development build is used (default: main)
  --no-service     Install only the binary; do not install the user service
  -h, --help       Show this help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --hub) HUB="$2"; shift 2 ;;
    --code) CODE="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    --ref) REF="$2"; shift 2 ;;
    --no-service) NO_SERVICE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

if [ -n "$CODE" ] && [ -z "$HUB" ]; then
  echo "--hub is required with --code" >&2
  exit 2
fi
if ! command -v gh >/dev/null 2>&1; then
  echo "GitHub CLI (gh) is required for authenticated development builds." >&2
  echo "Install it from https://cli.github.com and run: gh auth login" >&2
  exit 1
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  darwin) os="darwin" ;;
  linux) os="linux" ;;
  *) echo "unsupported operating system: $os" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

artifact="nexusd-${os}-${arch}"
run_json="$(gh run list --repo "$REPO" --workflow dev-build.yml --branch "$REF" --status success --limit 1 --json databaseId,headSha)"
if [ -z "$run_json" ] || [ "$run_json" = "[]" ]; then
  echo "No successful dev-build run found for $REF." >&2
  exit 1
fi
run_id="$(printf '%s' "$run_json" | sed -n 's/.*"databaseId":\([0-9]*\).*/\1/p')"
if [ -z "$run_id" ]; then
  echo "Could not parse the workflow run id." >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "Downloading $artifact from run $run_id..."
gh run download "$run_id" --repo "$REPO" --name "$artifact" --dir "$tmp"

bin="$(find "$tmp" -type f -name 'nexusd*' ! -name '*.sha256' | head -n 1)"
checksum="$(find "$tmp" -type f -name '*.sha256' | head -n 1)"
[ -n "$bin" ] || { echo "artifact did not contain nexusd" >&2; exit 1; }
if [ -n "$checksum" ]; then
  if command -v sha256sum >/dev/null 2>&1; then
    expected="$(cut -d' ' -f1 "$checksum")"
    actual="$(sha256sum "$bin" | cut -d' ' -f1)"
  else
    expected="$(cut -d' ' -f1 "$checksum")"
    actual="$(shasum -a 256 "$bin" | cut -d' ' -f1)"
  fi
  if [ "$expected" != "$actual" ]; then
    echo "SHA-256 mismatch for the downloaded daemon." >&2
    exit 1
  fi
  echo "Checksum verified."
fi

dest_dir="${XDG_BIN_HOME:-$HOME/.local/bin}"
mkdir -p "$dest_dir"
dest="$dest_dir/nexusd"
if [ -f "$dest" ]; then
  cp "$dest" "$dest.previous"
fi
install -m 0755 "$bin" "$dest"
echo "Installed $dest"

if [ -n "$CODE" ]; then
  "$dest" pair --hub "$HUB" --code "$CODE" --name "$NAME"
  echo "Compare the printed Pairing SAS with the Hosts page and confirm it there."
fi

if [ "$NO_SERVICE" -eq 0 ]; then
  "$dest" service install
  "$dest" service status
  "$dest" doctor
fi

case "$PATH" in
  *"$dest_dir"*) ;;
  *) echo "Note: add $dest_dir to PATH to run nexusd without the full path." ;;
esac
