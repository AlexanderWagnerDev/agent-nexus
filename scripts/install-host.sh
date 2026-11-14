#!/usr/bin/env bash
# Install, update or remove the AgentNexus host daemon and terminal client
# without a Go toolchain.
#
# Official releases are the default source: the script reads the public GitHub
# release feed, verifies the archive SHA-256 (and the cosign bundle when cosign
# is installed) and installs both nexusd and nexus. Development workflow
# artifacts remain available explicitly with --development; they require an
# authenticated GitHub CLI.
set -euo pipefail

REPO="${NEXUS_REPO:-AlexanderWagnerDev/agent-nexus}"
API="${NEXUS_UPDATE_API:-https://api.github.com}"
CHANNEL="${NEXUS_UPDATE_CHANNEL:-stable}"
VERSION=""
HUB=""
CODE=""
NAME="$(hostname 2>/dev/null || echo host)"
NO_SERVICE=0
FORCE=0
DEVELOPMENT=0
REQUIRE_SIGNATURE=0
ALLOW_CHECKSUM_ONLY=0
UNINSTALL=0
PURGE_CONFIG=0

usage() {
  cat <<'EOF'
Usage: install-host.sh [--hub URL [--code CODE] [--name NAME]] [options]

  --hub URL             Public hub URL (required with --code)
  --code CODE           One-time pairing code from Hosts -> Pair a host
  --name NAME           Host name shown in the hub (default: hostname)
  --channel CHANNEL     stable (default), prerelease or development
  --version VERSION     Install a specific release tag, for example v0.1.0
  --repo OWNER/REPO     Release repository (default: AlexanderWagnerDev/agent-nexus)
  --development         Use the newest GitHub Actions development build
                        (requires an authenticated gh CLI; no release needed)
  --no-service          Install the binaries only; do not install the user service
  --require-signature   Refuse to install when cosign is unavailable
  --allow-checksum-only Configure automatic updates to accept checksum-only
                        verification (weaker trust mode)
  --force               Allow replacing the daemon while runs are active
  --uninstall           Stop the service and remove the installed binaries
  --purge-config        With --uninstall, also remove the AgentNexus host config
  -h, --help            Show this help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --hub) HUB="$2"; shift 2 ;;
    --code) CODE="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --channel) CHANNEL="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    --development) DEVELOPMENT=1; shift ;;
    --no-service) NO_SERVICE=1; shift ;;
    --require-signature) REQUIRE_SIGNATURE=1; shift ;;
    --allow-checksum-only) ALLOW_CHECKSUM_ONLY=1; shift ;;
    --force) FORCE=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    --purge-config) PURGE_CONFIG=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

if [ -n "$CODE" ] && [ -z "$HUB" ]; then
  echo "--hub is required with --code" >&2
  exit 2
fi
case "$CHANNEL" in
  stable|prerelease|development) ;;
  *) echo "unsupported channel: $CHANNEL (use stable, prerelease or development)" >&2; exit 2 ;;
esac
if [ "$CHANNEL" = "development" ]; then
  DEVELOPMENT=1
fi
if [ "$DEVELOPMENT" -eq 1 ] && [ -n "$VERSION" ]; then
  echo "--version cannot be combined with --development or --channel development" >&2
  exit 2
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

dest_dir="${XDG_BIN_HOME:-$HOME/.local/bin}"
dest="$dest_dir/nexusd"
cli="$dest_dir/nexus"

stop_service() {
  if [ -x "$dest" ]; then
    "$dest" service uninstall >/dev/null 2>&1 || true
  fi
}

if [ "$UNINSTALL" -eq 1 ]; then
  stop_service
  rm -f "$dest" "$cli" "$dest.previous" "$cli.previous"
  echo "Removed $dest and $cli"
  if [ "$PURGE_CONFIG" -eq 1 ]; then
    case "$os" in
      linux) config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/AgentNexus" ;;
      darwin) config_dir="$HOME/Library/Application Support/AgentNexus" ;;
    esac
    if [ -n "${config_dir:-}" ] && [ -d "$config_dir" ]; then
      rm -rf "$config_dir"
      echo "Removed AgentNexus host config at $config_dir"
    fi
  else
    echo "The AgentNexus host config and pairing were kept. Native agent data was not touched."
  fi
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

download() {
  # The GitHub token is passed through a curl config on stdin so it never
  # appears in /proc/<pid>/cmdline.
  if [ -n "${NEXUS_GITHUB_TOKEN:-}" ]; then
    case "$1" in
      https://api.github.com/*|https://github.com/*)
        printf 'header = "Authorization: Bearer %s"\n' "$NEXUS_GITHUB_TOKEN" |
          curl -fsSL --retry 3 --connect-timeout 20 -K - -o "$2" "$1"
        return $?
        ;;
    esac
  fi
  curl -fsSL --retry 3 --connect-timeout 20 -o "$2" "$1"
}

# Print the final HTTP status into $HTTP_CODE. Returns non-zero on a transport
# failure. Without -f, a 404 is reported as 404 instead of an indistinguishable
# failure, so the caller can tell "asset does not exist" from "download broke".
HTTP_CODE=""
fetch_status() {
  local url="$1" out="$2" rc=0
  if [ -n "${NEXUS_GITHUB_TOKEN:-}" ]; then
    case "$url" in
      https://api.github.com/*|https://github.com/*)
        printf 'header = "Authorization: Bearer %s"\n' "$NEXUS_GITHUB_TOKEN" |
          curl -sSL --retry 3 --connect-timeout 20 -K - -o "$out" -w '%{http_code}' "$url" > "$tmp/http_code" || rc=$?
        HTTP_CODE="$(cat "$tmp/http_code" 2>/dev/null || true)"
        return $rc
        ;;
    esac
  fi
  curl -sSL --retry 3 --connect-timeout 20 -o "$out" -w '%{http_code}' "$url" > "$tmp/http_code" || rc=$?
  HTTP_CODE="$(cat "$tmp/http_code" 2>/dev/null || true)"
  return $rc
}

install_development() {
  if ! command -v gh >/dev/null 2>&1; then
    echo "GitHub CLI (gh) is required for --development builds." >&2
    echo "Install it from https://cli.github.com and run: gh auth login" >&2
    exit 1
  fi
  local artifact="nexusd-${os}-${arch}"
  local run_json run_id
  run_json="$(gh run list --repo "$REPO" --workflow dev-build.yml --branch "${NEXUS_REF:-main}" --status success --limit 1 --json databaseId)"
  if [ -z "$run_json" ] || [ "$run_json" = "[]" ]; then
    echo "No successful dev-build run found." >&2
    exit 1
  fi
  run_id="$(printf '%s' "$run_json" | sed -n 's/.*"databaseId":\([0-9]*\).*/\1/p')"
  echo "Downloading development build $artifact from run $run_id..."
  gh run download "$run_id" --repo "$REPO" --name "$artifact" --dir "$tmp"
  find "$tmp" -type f -name 'nexusd*' ! -name '*.sha256' -exec cp {} "$tmp/nexusd" \;
  local checksum
  checksum="$(find "$tmp" -type f -name '*.sha256' | head -n 1)"
  [ -n "$checksum" ] || { echo "artifact did not contain a SHA-256 checksum" >&2; exit 1; }
  verify_checksum "$tmp/nexusd" "$(cut -d' ' -f1 "$checksum")"
}

verify_checksum() {
  local file="$1" expected="$2" actual
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$file" | cut -d' ' -f1)"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$file" | cut -d' ' -f1)"
  else
    echo "No SHA-256 tool found (sha256sum or shasum)." >&2
    exit 1
  fi
  if [ "$expected" != "$actual" ]; then
    echo "SHA-256 mismatch for $file" >&2
    exit 1
  fi
}

if [ "$DEVELOPMENT" -eq 1 ]; then
  install_development
  version="development"
else
  tag="$VERSION"
  if [ -z "$tag" ]; then
    if [ "$CHANNEL" = "stable" ]; then
      feed="$API/repos/$REPO/releases/latest"
    else
      feed="$API/repos/$REPO/releases?per_page=30"
    fi
    feed_file="$tmp/releases.json"
    if ! download "$feed" "$feed_file"; then
      echo "No published release found for the $CHANNEL channel." >&2
      echo "Use --development for a workflow build, or wait for the first official release." >&2
      exit 1
    fi
    tag="$(grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' "$feed_file" | head -n 1 | sed 's/.*"\([^"]*\)"$/\1/')"
  fi
  case "$tag" in
    v*) ;;
    *) tag="v$tag" ;;
  esac
  [ -n "$tag" ] || { echo "Could not determine a release tag." >&2; exit 1; }
  version="${tag#v}"
  asset="agent-nexus_${version}_${os}_${arch}.tar.gz"
  base="https://github.com/$REPO/releases/download/$tag"
  echo "Downloading $asset ($CHANNEL)..."
  download "$base/$asset" "$tmp/$asset"
  download "$base/checksums.txt" "$tmp/checksums.txt"
  expected="$(awk -v name="$asset" '$2 == name { print $1; exit }' "$tmp/checksums.txt")"
  [ -n "$expected" ] || { echo "checksums.txt does not list $asset" >&2; exit 1; }
  verify_checksum "$tmp/$asset" "$expected"

  if command -v cosign >/dev/null 2>&1; then
    if fetch_status "$base/checksums.txt.sigstore.json" "$tmp/checksums.txt.sigstore.json"; then
      case "$HTTP_CODE" in
        200)
          cosign verify-blob \
            --bundle "$tmp/checksums.txt.sigstore.json" \
            --certificate-identity-regexp "^https://github.com/${REPO}/.github/workflows/release.yml@refs/tags/" \
            --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
            "$tmp/checksums.txt" >/dev/null
          echo "Checksum and cosign signature verified."
          ;;
        404)
          if [ "$REQUIRE_SIGNATURE" -eq 1 ]; then
            echo "The release has no cosign bundle and --require-signature was set." >&2
            exit 1
          fi
          echo "Warning: release has no cosign bundle; SHA-256 only." >&2
          ;;
        *)
          echo "Cannot download the cosign bundle (HTTP $HTTP_CODE); refusing to fall back to checksum-only." >&2
          exit 1
          ;;
      esac
    else
      echo "Cannot download the cosign bundle (transport error); refusing to fall back to checksum-only." >&2
      exit 1
    fi
  elif [ "$REQUIRE_SIGNATURE" -eq 1 ]; then
    echo "cosign is required by --require-signature but was not found." >&2
    exit 1
  else
    echo "Warning: cosign is not installed; the archive was verified by SHA-256 only." >&2
  fi

  mkdir -p "$tmp/extract"
  tar -xzf "$tmp/$asset" -C "$tmp/extract" --no-same-owner --no-same-permissions nexusd
  tar -xzf "$tmp/$asset" -C "$tmp/extract" --no-same-owner --no-same-permissions nexus 2>/dev/null || true
  [ -f "$tmp/extract/nexusd" ] || { echo "archive did not contain nexusd" >&2; exit 1; }
  cp "$tmp/extract/nexusd" "$tmp/nexusd"
  if [ -f "$tmp/extract/nexus" ]; then cp "$tmp/extract/nexus" "$tmp/nexus"; fi
fi

chmod 0755 "$tmp/nexusd"
version_output="$("$tmp/nexusd" version 2>/dev/null || true)"
case "$version_output" in
  nexusd\ *) ;;
  *) echo "downloaded daemon failed its version check" >&2; exit 1 ;;
esac

mkdir -p "$dest_dir"
active_runs=0
status_json="$("$tmp/nexusd" status --json 2>/dev/null || true)"
if [ -n "$status_json" ]; then
  active_runs="$(printf '%s' "$status_json" | sed -n 's/.*"active_runs":\([0-9][0-9]*\).*/\1/p')"
  active_runs="${active_runs:-0}"
fi
if [ "$active_runs" -gt 0 ] && [ "$FORCE" -eq 0 ]; then
  echo "Refusing to replace the daemon: $active_runs active agent run(s)." >&2
  echo "Wait until idle, or pass --force after checking the runs." >&2
  exit 2
fi
if [ "$active_runs" -gt 0 ]; then
  printf 'Type FORCE to interrupt %s active agent run(s): ' "$active_runs" >&2
  read -r answer
  if [ "$answer" != "FORCE" ]; then
    echo "Update cancelled; no active run was interrupted." >&2
    exit 2
  fi
fi

stop_service
if [ -f "$dest" ]; then cp "$dest" "$dest.previous"; fi
if [ -f "$cli" ]; then cp "$cli" "$cli.previous"; fi
install -m 0755 "$tmp/nexusd" "$dest"
if [ -f "$tmp/nexus" ]; then install -m 0755 "$tmp/nexus" "$cli"; fi
echo "Installed $dest and $cli"

if [ -n "$CODE" ]; then
  if ! "$dest" pair --hub "$HUB" --code "$CODE" --name "$NAME"; then
    if [ -f "$dest.previous" ]; then
      install -m 0755 "$dest.previous" "$dest"
      [ -f "$cli.previous" ] && install -m 0755 "$cli.previous" "$cli"
      "$dest" service install >/dev/null 2>&1 || true
      echo "Pairing failed; the previous installation was restored." >&2
    fi
    exit 1
  fi
  echo "Compare the printed Pairing SAS with the Hosts page and confirm it there."
else
  echo "Existing host pairing kept; no pairing code was supplied."
fi

if [ -n "$version" ] && [ "$version" != "development" ]; then
  "$dest" update --configure --channel "$CHANNEL" --auto on --install-when-idle on >/dev/null || true
  if [ "$ALLOW_CHECKSUM_ONLY" -eq 1 ]; then
    "$dest" update --configure --checksum-only >/dev/null || true
  elif [ "$REQUIRE_SIGNATURE" -eq 1 ]; then
    "$dest" update --configure --require-signature >/dev/null || true
  fi
fi

if [ "$NO_SERVICE" -eq 0 ]; then
  if ! "$dest" service install; then
    if [ -f "$dest.previous" ]; then
      install -m 0755 "$dest.previous" "$dest"
      [ -f "$cli.previous" ] && install -m 0755 "$cli.previous" "$cli"
      "$dest" service install >/dev/null 2>&1 || true
      echo "Service installation failed; the previous daemon was restored." >&2
    else
      echo "Service installation failed and no rollback binary was available." >&2
    fi
    exit 1
  fi
  "$dest" service status || true
  "$dest" doctor || true
fi

case "$PATH" in
  *"$dest_dir"*) ;;
  *) echo "Note: add $dest_dir to PATH to run nexusd and nexus without the full path." ;;
esac

if [ "$REQUIRE_SIGNATURE" -eq 0 ] && [ "$ALLOW_CHECKSUM_ONLY" -eq 0 ] && ! command -v cosign >/dev/null 2>&1; then
  echo "Note: automatic updates require cosign by default. Install cosign, or set"
  echo "      checksum-only explicitly with: nexusd update --configure --checksum-only"
fi
