#!/usr/bin/env bash
#
# pg-panel.sh — installer/manager for the PasarGuard node-selling control panel.
#
# Installs a PREBUILT binary (per CPU arch) from GitHub Releases and runs it as a
# systemd service — no Docker, no on-server compilation. Web UI + API are embedded
# in the binary.
#
# One-line install:
#   sudo bash -c "$(curl -sL https://raw.githubusercontent.com/loopy-iri/NodePanel/main/scripts/pg-panel.sh)" @ install
#
set -euo pipefail

# ---------- globals ----------
GH_REPO="${GH_REPO:-loopy-iri/NodePanel}"
RAW_SCRIPT_URL="${RAW_SCRIPT_URL:-https://raw.githubusercontent.com/${GH_REPO}/main/scripts/pg-panel.sh}"
BIN_NAME="pg-panel"
# Instance name (overridable with --name) decides paths/service/CLI name.
APP_NAME="pg-panel"

# ---------- helpers ----------
colorized_echo() {
    local color="$1" text="$2" code
    case "$color" in
    red) code=31 ;; green) code=32 ;; yellow) code=33 ;;
    blue) code=34 ;; magenta) code=35 ;; cyan) code=36 ;; *) code=0 ;;
    esac
    printf "\033[1;%sm%s\033[0m\n" "$code" "$text"
}
die() { colorized_echo red "$1"; exit 1; }
check_root() { [ "$(id -u)" -eq 0 ] || die "This command must run as root (use sudo)."; }
require_systemd() { command -v systemctl >/dev/null 2>&1 || die "systemd (systemctl) is required."; }

validate_name() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$ ]]; }

set_paths() {
    APP_DIR="/opt/$APP_NAME"
    DATA_DIR="/var/lib/$APP_NAME"
    ENV_FILE="$APP_DIR/.env"
    SERVICE_UNIT="/etc/systemd/system/$APP_NAME.service"
    BIN_PATH="$APP_DIR/$BIN_NAME"
}

detect_os() {
    if command -v apt-get >/dev/null 2>&1; then PKG="apt";
    elif command -v dnf >/dev/null 2>&1; then PKG="dnf";
    elif command -v yum >/dev/null 2>&1; then PKG="yum";
    elif command -v pacman >/dev/null 2>&1; then PKG="pacman";
    elif command -v zypper >/dev/null 2>&1; then PKG="zypper";
    elif command -v apk >/dev/null 2>&1; then PKG="apk";
    else PKG=""; fi
}
install_package() {
    local pkg="$1"
    case "$PKG" in
    apt) apt-get update -qq && apt-get install -y "$pkg" ;;
    dnf) dnf install -y "$pkg" ;; yum) yum install -y "$pkg" ;;
    pacman) pacman -Sy --noconfirm "$pkg" ;; zypper) zypper install -y "$pkg" ;;
    apk) apk add --no-cache "$pkg" ;; *) die "Install '$pkg' manually." ;;
    esac
}
need() { command -v "$1" >/dev/null 2>&1 || { colorized_echo yellow "Installing ${2:-$1}..."; install_package "${2:-$1}"; }; }
gen_token() { openssl rand -hex 32 2>/dev/null || cat /proc/sys/kernel/random/uuid; }
public_ip() { curl -s -4 --fail --max-time 5 ifconfig.io 2>/dev/null || echo "127.0.0.1"; }

detect_arch() {
    case "$(uname -m)" in
    x86_64|amd64)  ARCH_SUFFIX="amd64" ;;
    aarch64|arm64) ARCH_SUFFIX="arm64" ;;
    armv7l|armv7)  ARCH_SUFFIX="armv7" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
    esac
}
latest_tag() {
    curl -fsSL "https://api.github.com/repos/${GH_REPO}/releases/latest" 2>/dev/null \
        | grep -oE '"tag_name":[[:space:]]*"[^"]+"' | head -n1 | sed -E 's/.*"([^"]+)"$/\1/'
}
is_installed() { [ -f "$BIN_PATH" ] && [ -f "$SERVICE_UNIT" ]; }
require_installed() { is_installed || die "panel is not installed. Run: $APP_NAME install"; }

download_binary() {
    local tag="$1"
    [ -n "$tag" ] || die "No release found for ${GH_REPO}. Create a release first (push a tag like v0.1.0)."
    local asset="${BIN_NAME}_linux_${ARCH_SUFFIX}.tar.gz"
    local url="https://github.com/${GH_REPO}/releases/download/${tag}/${asset}"
    local tmp; tmp="$(mktemp -d)"
    colorized_echo blue "Downloading $asset ($tag)..."
    curl -fsSL "$url" -o "$tmp/$asset" || die "Failed to download $url"
    tar -xzf "$tmp/$asset" -C "$tmp" || die "Failed to extract $asset"
    install -m 755 "$tmp/$BIN_NAME" "$BIN_PATH"
    rm -rf "$tmp"
    colorized_echo green "✓ Binary installed: $BIN_PATH ($tag, $ARCH_SUFFIX)"
}

write_env() {
    local token="$1" port="$2"
    mkdir -p "$APP_DIR"
    umask 077
    cat >"$ENV_FILE" <<EOF
PANEL_HTTP_ADDR=:$port
PANEL_DB_PATH=$DATA_DIR/panel.db
PANEL_API_TOKEN=$token
PANEL_COLLECT_INTERVAL=30s
EOF
    umask 022
    colorized_echo green "✓ Wrote $ENV_FILE"
}

write_service() {
    colorized_echo blue "Creating systemd unit $SERVICE_UNIT"
    cat >"$SERVICE_UNIT" <<EOF
[Unit]
Description=PasarGuard node-selling control panel ($APP_NAME)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=$ENV_FILE
WorkingDirectory=$DATA_DIR
ExecStart=$BIN_PATH
AmbientCapabilities=CAP_NET_BIND_SERVICE
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
}

install_panel_script() {
    local src="${1:-}"
    [ -z "$src" ] && src="$(command -v "$0" 2>/dev/null || true)"
    if [ -n "$src" ] && [ -f "$src" ]; then
        install -m 755 "$src" "/usr/local/bin/$APP_NAME" 2>/dev/null && \
            colorized_echo green "✓ CLI installed: /usr/local/bin/$APP_NAME"
    fi
}

# self_update_script refreshes the installed CLI itself from GitHub so script
# improvements (not just the binary) reach already-installed hosts on `update`.
self_update_script() {
    local dest="/usr/local/bin/$APP_NAME"
    [ -f "$dest" ] || return 0
    local tmp; tmp="$(mktemp)"
    if curl -fsSL "$RAW_SCRIPT_URL" -o "$tmp" 2>/dev/null && [ -s "$tmp" ]; then
        install -m 755 "$tmp" "$dest" && colorized_echo green "✓ CLI script refreshed: $dest"
    fi
    rm -f "$tmp"
}

# ---------- commands ----------
install_command() {
    check_root; require_systemd
    local token="" port="8080" version="latest"
    while [[ $# -gt 0 ]]; do case "$1" in
        --token) token="$2"; shift 2 ;;
        --port) port="$2"; shift 2 ;;
        --version) version="$2"; shift 2 ;;
        -y|--yes) shift ;;
        *) die "Unknown install option: $1" ;;
    esac; done

    if [ -z "$token" ] && [ -f "$ENV_FILE" ]; then
        token="$(grep -E '^PANEL_API_TOKEN=' "$ENV_FILE" | cut -d= -f2-)"
    fi
    [ -z "$token" ] && token="$(gen_token)"

    detect_os; detect_arch
    need curl curl; need openssl openssl; need tar tar

    mkdir -p "$DATA_DIR"
    local tag="$version"; [ "$version" = "latest" ] && tag="$(latest_tag)"
    download_binary "$tag"
    write_env "$token" "$port"
    write_service
    install_panel_script || true
    systemctl enable --now "$APP_NAME"

    local ip; ip="$(public_ip)"
    colorized_echo blue "================================"
    colorized_echo green "PasarGuard control panel is running (systemd: $APP_NAME)."
    colorized_echo magenta "  Panel UI:   http://$ip:$port/"
    colorized_echo magenta "  API docs:   http://$ip:$port/docs"
    colorized_echo magenta "  API token (Bearer) for the sales bot:"
    colorized_echo red "    $token"
    colorized_echo blue "================================"
    colorized_echo yellow "Put the panel behind TLS (reverse proxy) before exposing it publicly."
}

up_command()      { check_root; require_installed; systemctl start "$APP_NAME"; colorized_echo green "started."; }
down_command()    { check_root; require_installed; systemctl stop "$APP_NAME"; colorized_echo green "stopped."; }
restart_command() { check_root; require_installed; systemctl restart "$APP_NAME"; colorized_echo green "restarted."; }
status_command()  { require_installed; systemctl status --no-pager "$APP_NAME"; }
logs_command()    { require_installed; journalctl -u "$APP_NAME" -f --no-pager; }
edit_env_command(){ require_installed; "${EDITOR:-nano}" "$ENV_FILE"; systemctl restart "$APP_NAME"; }

update_command() {
    check_root; require_installed; detect_arch
    self_update_script
    local version="${1:-latest}" tag
    tag="$version"; [ "$version" = "latest" ] && tag="$(latest_tag)"
    download_binary "$tag"
    systemctl restart "$APP_NAME"
    colorized_echo green "panel updated to $tag and restarted."
}

set_token_command() {
    check_root; require_installed
    local token="${1:-$(gen_token)}"
    sed -i "s|^PANEL_API_TOKEN=.*|PANEL_API_TOKEN=$token|" "$ENV_FILE"
    systemctl restart "$APP_NAME"
    colorized_echo magenta "New API token:"; colorized_echo red "  $token"
}

info_command() {
    require_installed
    local port token ip
    port="$(grep -E '^PANEL_HTTP_ADDR=' "$ENV_FILE" | cut -d= -f2- | tr -d ':')"
    token="$(grep -E '^PANEL_API_TOKEN=' "$ENV_FILE" | cut -d= -f2-)"
    ip="$(public_ip)"
    colorized_echo magenta "  Panel UI: http://$ip:$port/"
    colorized_echo magenta "  API docs: http://$ip:$port/docs"
    colorized_echo magenta "  API token: $token"
}

uninstall_command() {
    check_root
    systemctl disable --now "$APP_NAME" 2>/dev/null || true
    rm -f "$SERVICE_UNIT"; systemctl daemon-reload 2>/dev/null || true
    rm -f "$BIN_PATH" "/usr/local/bin/$APP_NAME"
    rm -rf "$APP_DIR"
    colorized_echo green "panel uninstalled. Data kept at $DATA_DIR (remove manually if desired)."
}

completion_command() {
    check_root
    local f="/etc/bash_completion.d/$APP_NAME"
    cat >"$f" <<EOF
_pgpanel(){ COMPREPLY=( \$(compgen -W "install update uninstall up down restart status logs set-token info edit-env completion help" -- "\${COMP_WORDS[COMP_CWORD]}") ); }
complete -F _pgpanel $APP_NAME
EOF
    colorized_echo green "✓ Bash completion installed: $f"
}

usage() {
    colorized_echo cyan "pg-panel — control panel (prebuilt binary + systemd)"
    echo "Usage: $APP_NAME <command> [options]"
    echo
    echo "Commands:"
    echo "  install            Download binary and start the systemd service"
    echo "  update [VER]       Download a newer panel binary and restart (default: latest)"
    echo "  uninstall          Stop and remove the panel"
    echo "  up | down | restart | status | logs"
    echo "  set-token [TOKEN]  Rotate the API bearer token (random if omitted)"
    echo "  info               Show URL/docs/token"
    echo "  edit-env | completion"
    echo
    echo "Install options:"
    echo "  --token TOKEN      API bearer token (auto-generated if omitted)"
    echo "  --port PORT        HTTP port (default 8080)"
    echo "  --version vX.Y.Z   Install a specific release (default: latest)"
    echo
    echo "Global options:"
    echo "  --name NAME        Instance name (paths/service/CLI). Default: pg-panel"
}

# Parse global flags (--name) anywhere on the line; keep the rest in ARGS.
ARGS=()
while [[ $# -gt 0 ]]; do
    case "$1" in
    --name) APP_NAME="$2"; shift 2 ;;
    --name=*) APP_NAME="${1#*=}"; shift ;;
    *) ARGS+=("$1"); shift ;;
    esac
done
set -- "${ARGS[@]+"${ARGS[@]}"}"
validate_name "$APP_NAME" || die "invalid --name '$APP_NAME'."
set_paths

cmd="${1:-help}"; shift || true
case "$cmd" in
    install) install_command "$@" ;;
    update) update_command "$@" ;;
    uninstall) uninstall_command ;;
    up) up_command ;;
    down) down_command ;;
    restart) restart_command ;;
    status) status_command ;;
    logs) logs_command ;;
    set-token) set_token_command "$@" ;;
    info) info_command ;;
    edit-env) edit_env_command ;;
    completion) completion_command ;;
    help|-h|--help) usage ;;
    *) colorized_echo red "Unknown command: $cmd"; usage; exit 1 ;;
esac
