#!/usr/bin/env bash
#
# pg-panel.sh — installer/manager for the PasarGuard node-selling control panel.
#
# Self-contained (no external shared libs). Manages the panel via Docker Compose.
#
# One-line install (after hosting this repo):
#   sudo bash -c "$(curl -sL https://raw.githubusercontent.com/loopy-iri/NodePanel/main/scripts/pg-panel.sh)" @ install
#
set -euo pipefail

# ---------- globals ----------
APP_NAME="${APP_NAME:-pg-panel}"
INSTALL_DIR="/opt"
APP_DIR="${APP_DIR:-$INSTALL_DIR/$APP_NAME}"
DATA_DIR="${DATA_DIR:-/var/lib/$APP_NAME}"
ENV_FILE="$APP_DIR/.env"
COMPOSE_FILE="$APP_DIR/docker-compose.yml"
REPO_URL="${REPO_URL:-https://github.com/loopy-iri/NodePanel.git}"   # this panel's repo
COMPOSE=""

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
    dnf) dnf install -y "$pkg" ;;
    yum) yum install -y "$pkg" ;;
    pacman) pacman -Sy --noconfirm "$pkg" ;;
    zypper) zypper install -y "$pkg" ;;
    apk) apk add --no-cache "$pkg" ;;
    *) die "Unknown package manager; install '$pkg' manually." ;;
    esac
}
need() { command -v "$1" >/dev/null 2>&1 || { colorized_echo yellow "Installing ${2:-$1}..."; install_package "${2:-$1}"; }; }

install_docker() {
    command -v docker >/dev/null 2>&1 && return
    colorized_echo blue "Installing Docker..."
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker 2>/dev/null || true
}
detect_compose() {
    if docker compose version >/dev/null 2>&1; then COMPOSE="docker compose";
    elif command -v docker-compose >/dev/null 2>&1; then COMPOSE="docker-compose";
    else die "docker compose not found."; fi
}
gen_token() { openssl rand -hex 32 2>/dev/null || cat /proc/sys/kernel/random/uuid; }
public_ip() { curl -s -4 --fail --max-time 5 ifconfig.io 2>/dev/null || echo "127.0.0.1"; }

compose_cmd() { ( cd "$APP_DIR" && $COMPOSE --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@" ); }
is_installed() { [ -d "$APP_DIR" ] && [ -f "$COMPOSE_FILE" ]; }
require_installed() { is_installed || die "panel is not installed. Run: $APP_NAME install"; }

fetch_sources() {
    if [ -f "Dockerfile" ] && [ -f "docker-compose.yml" ] && [ -d "cmd/panel" ]; then
        colorized_echo blue "Using local sources -> $APP_DIR"
        local here; here="$(pwd)"
        mkdir -p "$APP_DIR"
        tar -cf - --exclude=.git -C "$here" . | tar -xf - -C "$APP_DIR"
    else
        need git git
        colorized_echo blue "Cloning $REPO_URL -> $APP_DIR"
        rm -rf "$APP_DIR"
        git clone --depth 1 "$REPO_URL" "$APP_DIR"
    fi
    SRC_DIR="$APP_DIR"
}

write_env() {
    local token="$1" port="$2"
    umask 077
    cat >"$ENV_FILE" <<EOF
APP_NAME=$APP_NAME
DATA_DIR=$DATA_DIR
PANEL_PORT=$port
PANEL_HTTP_ADDR=:8080
PANEL_DB_PATH=/var/lib/pg-panel/panel.db
PANEL_API_TOKEN=$token
PANEL_COLLECT_INTERVAL=30s
EOF
    umask 022
    colorized_echo green "✓ Wrote $ENV_FILE"
}

# ---------- commands ----------
install_command() {
    check_root
    local token="" port="8080"
    while [[ $# -gt 0 ]]; do case "$1" in
        --token) token="$2"; shift 2 ;;
        --port) port="$2"; shift 2 ;;
        --repo) REPO_URL="$2"; shift 2 ;;
        -y|--yes) shift ;;
        *) die "Unknown install option: $1" ;;
    esac; done

    detect_os; need curl curl; need openssl openssl
    install_docker; detect_compose
    if [ -z "$token" ] && [ -f "$ENV_FILE" ]; then
        token="$(grep -E '^PANEL_API_TOKEN=' "$ENV_FILE" | cut -d= -f2-)"
    fi
    [ -z "$token" ] && token="$(gen_token)"

    mkdir -p "$DATA_DIR"
    fetch_sources
    write_env "$token" "$port"

    colorized_echo blue "Building and starting the panel..."
    ( cd "$APP_DIR" && $COMPOSE --env-file "$ENV_FILE" -f docker-compose.yml up -d --build )
    install_panel_script || true

    local ip; ip="$(public_ip)"
    colorized_echo blue "================================"
    colorized_echo green "PasarGuard control panel is up."
    colorized_echo magenta "  Panel UI:   http://$ip:$port/"
    colorized_echo magenta "  API docs:   http://$ip:$port/docs"
    colorized_echo magenta "  API token (Bearer) for the sales bot:"
    colorized_echo red "    $token"
    colorized_echo blue "================================"
    colorized_echo yellow "Put the panel behind TLS (reverse proxy) before exposing it publicly."
}

install_panel_script() {
    local target="/usr/local/bin/$APP_NAME"
    [ -f "$SRC_DIR/scripts/pg-panel.sh" ] && install -m 755 "$SRC_DIR/scripts/pg-panel.sh" "$target" 2>/dev/null && \
        colorized_echo green "✓ CLI installed: $target"
}
uninstall_panel_script() { rm -f "/usr/local/bin/$APP_NAME"; }

up_command()      { require_installed; detect_compose; compose_cmd up -d; colorized_echo green "panel started."; }
down_command()    { require_installed; detect_compose; compose_cmd down; colorized_echo green "panel stopped."; }
restart_command() { require_installed; detect_compose; compose_cmd down; compose_cmd up -d; colorized_echo green "panel restarted."; }
status_command()  { require_installed; detect_compose; compose_cmd ps; }
logs_command()    { require_installed; detect_compose; compose_cmd logs -f --tail=200; }
edit_command()    { require_installed; "${EDITOR:-nano}" "$COMPOSE_FILE"; }
edit_env_command(){ require_installed; "${EDITOR:-nano}" "$ENV_FILE"; }

update_command() {
    check_root; require_installed; detect_compose
    [ -d "$APP_DIR/.git" ] && ( cd "$APP_DIR" && git pull --ff-only || true )
    ( cd "$APP_DIR" && $COMPOSE --env-file "$ENV_FILE" -f docker-compose.yml up -d --build )
    colorized_echo green "panel updated."
}

set_token_command() {
    check_root; require_installed
    local token="${1:-$(gen_token)}"
    sed -i "s|^PANEL_API_TOKEN=.*|PANEL_API_TOKEN=$token|" "$ENV_FILE"
    detect_compose; restart_command
    colorized_echo magenta "New API token:"; colorized_echo red "  $token"
}

info_command() {
    require_installed
    local port token ip
    port="$(grep -E '^PANEL_PORT=' "$ENV_FILE" | cut -d= -f2)"
    token="$(grep -E '^PANEL_API_TOKEN=' "$ENV_FILE" | cut -d= -f2)"
    ip="$(public_ip)"
    colorized_echo magenta "  Panel UI: http://$ip:$port/"
    colorized_echo magenta "  API docs: http://$ip:$port/docs"
    colorized_echo magenta "  API token: $token"
}

uninstall_command() {
    check_root; detect_compose
    is_installed && compose_cmd down 2>/dev/null || true
    uninstall_panel_script
    rm -rf "$APP_DIR"
    colorized_echo green "panel uninstalled. Data kept at $DATA_DIR (remove manually if desired)."
}

completion_command() {
    check_root
    local f="/etc/bash_completion.d/$APP_NAME"
    cat >"$f" <<EOF
_pgpanel(){ COMPREPLY=( \$(compgen -W "install update uninstall up down restart status logs set-token info edit edit-env completion help" -- "\${COMP_WORDS[COMP_CWORD]}") ); }
complete -F _pgpanel $APP_NAME
EOF
    colorized_echo green "✓ Bash completion installed: $f"
}

usage() {
    colorized_echo cyan "pg-panel — node-selling control panel CLI"
    echo "Usage: $APP_NAME <command> [options]"
    echo
    echo "Commands:"
    echo "  install            Install & start the panel"
    echo "  update             Rebuild & restart from latest sources"
    echo "  uninstall          Stop and remove the panel"
    echo "  up | down | restart | status | logs"
    echo "  set-token [TOKEN]  Rotate the API bearer token (random if omitted)"
    echo "  info               Show URL/docs/token"
    echo "  edit | edit-env    Edit compose / .env"
    echo "  completion         Install bash completion"
    echo
    echo "Install options:"
    echo "  --token TOKEN      API bearer token (auto-generated if omitted)"
    echo "  --port PORT        Host port for the panel UI/API (default 8080)"
    echo "  --repo URL         Source repo to clone (default $REPO_URL)"
}

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
    edit) edit_command ;;
    edit-env) edit_env_command ;;
    completion) completion_command ;;
    help|-h|--help) usage ;;
    *) colorized_echo red "Unknown command: $cmd"; usage; exit 1 ;;
esac
