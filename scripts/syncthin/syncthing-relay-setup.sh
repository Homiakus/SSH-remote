#!/usr/bin/env bash
set -euo pipefail

# Syncthing private relay setup for Debian/Ubuntu-like systems.
# Assumes strelaysrv binary is already installed and available in PATH
# or at /usr/local/bin/strelaysrv.
#
# Usage:
#   sudo ./syncthing-relay-setup.sh          # interactive menu
#   sudo ./syncthing-relay-setup.sh install  # install or update service
#   ./syncthing-relay-setup.sh status        # show service status and relay URL
#   sudo ./syncthing-relay-setup.sh remove   # remove service/firewall rules
#
# Optional env vars:
#   RELAY_USER=strelaysrv
#   RELAY_HOME=/var/lib/strelaysrv
#   RELAY_LISTEN=:22067
#   RELAY_STATUS=:22070
#   RELAY_EXT_ADDRESS=relay.example.com:443
#   RELAY_HOST=relay.example.com
#   RELAY_PORT=443
#   RELAY_PROVIDED_BY="My private Syncthing relay"
#   RELAY_POOL_MODE=private   # private | public
#   RELAY_TOKEN="sharedSecret"
#   PUBLIC_INTERFACE=eth0
#   ENABLE_443_REDIRECT=yes   # yes | no
#
# Syncthing client field:
#   Actions -> Settings -> Connections -> Sync Protocol Listen Addresses

SERVICE_NAME="strelaysrv.service"
SERVICE_PATH="/etc/systemd/system/$SERVICE_NAME"
PUBLIC_POOL_ENDPOINT="https://relays.syncthing.net/endpoint"

RELAY_USER="${RELAY_USER:-strelaysrv}"
RELAY_HOME="${RELAY_HOME:-/var/lib/strelaysrv}"
RELAY_LISTEN="${RELAY_LISTEN:-:22067}"
RELAY_STATUS="${RELAY_STATUS:-:22070}"
RELAY_EXT_ADDRESS="${RELAY_EXT_ADDRESS:-}"
RELAY_HOST="${RELAY_HOST:-}"
RELAY_PORT="${RELAY_PORT:-}"
RELAY_PROVIDED_BY="${RELAY_PROVIDED_BY:-Private Syncthing Relay}"
RELAY_POOL_MODE="${RELAY_POOL_MODE:-private}"
RELAY_TOKEN="${RELAY_TOKEN:-}"
PUBLIC_INTERFACE="${PUBLIC_INTERFACE:-eth0}"
ENABLE_443_REDIRECT="${ENABLE_443_REDIRECT:-yes}"

RELAY_BIN=""

say() {
  printf '%s\n' "$*"
}

die() {
  say "Error: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

need_root() {
  [[ "$(id -u)" == "0" ]] || die "Run this action as root, for example with sudo."
}

normalize_yes_no() {
  case "${1,,}" in
    y|yes|true|1|on) say "yes" ;;
    n|no|false|0|off) say "no" ;;
    *) say "$1" ;;
  esac
}

listen_port() {
  local value="${1:-}"

  [[ -n "$value" ]] || return 1
  value="${value%%/*}"
  value="${value%%\?*}"
  value="${value%/}"
  value="${value##*:}"

  [[ "$value" =~ ^[0-9]+$ ]] || return 1
  say "$value"
}

default_client_port() {
  if [[ -n "$RELAY_PORT" ]]; then
    say "$RELAY_PORT"
    return
  fi

  if [[ "$(normalize_yes_no "$ENABLE_443_REDIRECT")" == "yes" ]]; then
    say "443"
    return
  fi

  listen_port "$RELAY_LISTEN" || say "22067"
}

detect_relay_bin() {
  if command -v strelaysrv >/dev/null 2>&1; then
    RELAY_BIN="$(command -v strelaysrv)"
  elif [[ -x /usr/local/bin/strelaysrv ]]; then
    RELAY_BIN="/usr/local/bin/strelaysrv"
  else
    die "strelaysrv not found in PATH or /usr/local/bin/strelaysrv. Install syncthing-relaysrv first."
  fi
}

require_linux_tools() {
  need_cmd systemctl
  need_cmd getent
  need_cmd id
  need_cmd install
  need_cmd groupadd
  need_cmd useradd
  need_cmd iptables
}

service_exists() {
  systemctl list-unit-files "$SERVICE_NAME" --no-legend 2>/dev/null | grep -q "$SERVICE_NAME" \
    || [[ -f "$SERVICE_PATH" ]]
}

unit_value() {
  local key="$1"

  [[ -f "$SERVICE_PATH" ]] || return 1
  awk -F= -v key="$key" '$1 == key { print substr($0, length(key) + 2); exit }' "$SERVICE_PATH"
}

service_exec_start() {
  unit_value "ExecStart" || true
}

extract_device_id_from_text() {
  grep -Eo 'id=[A-Z2-7-]+' | tail -n 1 | sed 's/^id=//' || true
}

relay_device_id_from_logs() {
  if command -v journalctl >/dev/null 2>&1; then
    journalctl -u "$SERVICE_NAME" -b --no-pager 2>/dev/null | extract_device_id_from_text
  fi

  return 0
}

relay_device_id_from_status() {
  local port

  port="$(listen_port "$RELAY_STATUS" 2>/dev/null || true)"
  [[ -n "$port" ]] || return 0
  command -v curl >/dev/null 2>&1 || return 0

  curl -fsS "http://127.0.0.1:$port/status" 2>/dev/null | extract_device_id_from_text || true
}

relay_device_id() {
  local id

  id="$(relay_device_id_from_logs || true)"
  if [[ -n "$id" ]]; then
    say "$id"
    return
  fi

  relay_device_id_from_status || true
}

relay_base_from_ext_address() {
  local address="$RELAY_EXT_ADDRESS"
  local host

  [[ -n "$address" ]] || return 1
  address="${address%%\?*}"
  address="${address%/}"
  address="${address#relay://}"

  if [[ "$address" == :* ]]; then
    host="${RELAY_HOST#relay://}"
    host="${host%%/*}"
    host="${host%%\?*}"

    if [[ -n "$host" && "$host" != *:* ]]; then
      say "relay://$host$address/"
    elif [[ -n "$host" ]]; then
      say "relay://$host/"
    else
      say "relay://YOUR_SERVER_OR_DNS$address/"
    fi
    return
  fi

  say "relay://$address/"
}

relay_base_from_logs() {
  local uri base

  command -v journalctl >/dev/null 2>&1 || return 1
  uri="$(
    journalctl -u "$SERVICE_NAME" -b --no-pager 2>/dev/null \
      | grep -Eo 'relay://[^[:space:]]+' \
      | tail -n 1 \
      || true
  )"
  [[ -n "$uri" ]] || return 1

  base="${uri%%\?*}"
  base="${base%/}"
  if [[ "$base" =~ ^relay://:[0-9]+$ ]]; then
    base="relay://YOUR_SERVER_OR_DNS:${base##*:}"
  fi
  say "$base/"
}

relay_base_from_host() {
  local host="$RELAY_HOST"
  local port

  [[ -n "$host" ]] || return 1
  host="${host#relay://}"
  host="${host%%/*}"
  port="$(default_client_port)"

  if [[ "$host" == *:* ]]; then
    say "relay://$host/"
  else
    say "relay://$host:$port/"
  fi
}

relay_base() {
  relay_base_from_ext_address && return
  relay_base_from_host && return
  relay_base_from_logs && return

  say "relay://YOUR_SERVER_OR_DNS:$(default_client_port)/"
}

relay_client_url() {
  local base id sep

  base="$(relay_base)"
  id="$(relay_device_id)"
  sep="?"

  if [[ "$base" == *\?* ]]; then
    sep="&"
  fi

  if [[ -n "$id" ]]; then
    printf '%s%sid=%s' "$base" "$sep" "$id"
  else
    printf '%s%sid=RELAY_DEVICE_ID' "$base" "$sep"
  fi

  if [[ -n "$RELAY_TOKEN" ]]; then
    printf '&token=%s' "$RELAY_TOKEN"
  fi

  printf '\n'
}

print_client_url() {
  local url

  url="$(relay_client_url)"

  cat <<EOF_URL

Syncthing URL to paste:
  $url

Paste it here:
  Actions -> Settings -> Connections -> Sync Protocol Listen Addresses
EOF_URL
}

ensure_relay_user() {
  if ! getent group "$RELAY_USER" >/dev/null; then
    groupadd --system "$RELAY_USER"
  fi

  if ! id "$RELAY_USER" >/dev/null 2>&1; then
    useradd --system --gid "$RELAY_USER" --home-dir "$RELAY_HOME" \
      --create-home --shell /usr/sbin/nologin "$RELAY_USER"
  fi
}

effective_ext_address() {
  local address="$RELAY_EXT_ADDRESS"
  local host

  if [[ -n "$address" ]]; then
    address="${address%%\?*}"
    address="${address%/}"
    address="${address#relay://}"
    say "$address"
    return
  fi

  [[ -n "$RELAY_HOST" ]] || return 0
  host="${RELAY_HOST#relay://}"
  host="${host%%/*}"
  host="${host%%\?*}"

  if [[ "$host" == *:* ]]; then
    say "$host"
  else
    say "$host:$(default_client_port)"
  fi
}

service_args() {
  local pool_arg
  local args
  local ext_address

  case "$RELAY_POOL_MODE" in
    public) pool_arg="-pools=$PUBLIC_POOL_ENDPOINT" ;;
    private|"") pool_arg="-pools=" ;;
    *) die "RELAY_POOL_MODE must be private or public." ;;
  esac

  args="-keys $RELAY_HOME/keys -listen $RELAY_LISTEN -status-srv $RELAY_STATUS $pool_arg -provided-by \"$RELAY_PROVIDED_BY\""

  ext_address="$(effective_ext_address)"
  if [[ -n "$ext_address" ]]; then
    args="$args -ext-address=$ext_address"
  fi

  if [[ -n "$RELAY_TOKEN" ]]; then
    args="$args -token=$RELAY_TOKEN"
  fi

  say "$args"
}

write_service() {
  local args

  args="$(service_args)"

  cat >"$SERVICE_PATH" <<EOF_SERVICE
[Unit]
Description=Syncthing Relay Server
After=network-online.target
Wants=network-online.target

[Service]
User=$RELAY_USER
Group=$RELAY_USER
WorkingDirectory=$RELAY_HOME
ExecStart=$RELAY_BIN $args
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$RELAY_HOME
AmbientCapabilities=
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
EOF_SERVICE
}

add_iptables_rule() {
  iptables -C "$@" 2>/dev/null || iptables -A "$@"
}

add_nat_redirect_rule() {
  iptables -t nat -C PREROUTING -i "$PUBLIC_INTERFACE" -p tcp --dport 443 -j REDIRECT --to-ports "$(listen_port "$RELAY_LISTEN")" 2>/dev/null \
    || iptables -t nat -A PREROUTING -i "$PUBLIC_INTERFACE" -p tcp --dport 443 -j REDIRECT --to-ports "$(listen_port "$RELAY_LISTEN")"
}

open_firewall() {
  local relay_port status_port

  relay_port="$(listen_port "$RELAY_LISTEN")"
  status_port="$(listen_port "$RELAY_STATUS" 2>/dev/null || true)"

  add_iptables_rule INPUT -p tcp --dport "$relay_port" -j ACCEPT
  if [[ -n "$status_port" ]]; then
    add_iptables_rule INPUT -p tcp --dport "$status_port" -j ACCEPT
  fi

  if [[ "$(normalize_yes_no "$ENABLE_443_REDIRECT")" == "yes" ]]; then
    add_nat_redirect_rule
    add_iptables_rule INPUT -p tcp --dport 443 -j ACCEPT
  fi
}

wait_for_device_id() {
  local i id

  for _ in {1..15}; do
    id="$(relay_device_id)"
    if [[ -n "$id" ]]; then
      return 0
    fi
    sleep 1
  done

  return 1
}

install_relay() {
  need_root
  require_linux_tools
  detect_relay_bin

  install -d -o root -g root -m 0755 "$(dirname "$SERVICE_PATH")"
  ensure_relay_user
  install -d -o "$RELAY_USER" -g "$RELAY_USER" -m 0750 "$RELAY_HOME"
  install -d -o "$RELAY_USER" -g "$RELAY_USER" -m 0750 "$RELAY_HOME/keys"

  write_service

  systemctl daemon-reload
  systemctl enable --now "$SERVICE_NAME"
  systemctl restart "$SERVICE_NAME"
  open_firewall

  wait_for_device_id || true

  say
  say "Installed or updated $SERVICE_NAME."
  print_client_url
  say
  say "Service status:"
  systemctl --no-pager --lines=8 status "$SERVICE_NAME" || true
}

delete_iptables_rule() {
  while iptables -C "$@" 2>/dev/null; do
    iptables -D "$@" || break
  done
}

delete_nat_redirect_rule() {
  local relay_port

  relay_port="$(listen_port "$RELAY_LISTEN" 2>/dev/null || true)"
  [[ -n "$relay_port" ]] || relay_port="22067"

  while iptables -t nat -C PREROUTING -i "$PUBLIC_INTERFACE" -p tcp --dport 443 -j REDIRECT --to-ports "$relay_port" 2>/dev/null; do
    iptables -t nat -D PREROUTING -i "$PUBLIC_INTERFACE" -p tcp --dport 443 -j REDIRECT --to-ports "$relay_port" || break
  done
}

remove_firewall() {
  local relay_port status_port

  relay_port="$(listen_port "$RELAY_LISTEN" 2>/dev/null || true)"
  status_port="$(listen_port "$RELAY_STATUS" 2>/dev/null || true)"

  [[ -n "$relay_port" ]] || relay_port="22067"

  delete_iptables_rule INPUT -p tcp --dport "$relay_port" -j ACCEPT
  if [[ -n "$status_port" ]]; then
    delete_iptables_rule INPUT -p tcp --dport "$status_port" -j ACCEPT
  else
    delete_iptables_rule INPUT -p tcp --dport 22070 -j ACCEPT
  fi

  delete_nat_redirect_rule
  delete_iptables_rule INPUT -p tcp --dport 443 -j ACCEPT
}

safe_remove_home() {
  if [[ -z "$RELAY_HOME" || "$RELAY_HOME" == "/" || "$RELAY_HOME" == "/var" || "$RELAY_HOME" == "/var/lib" ]]; then
    die "Refusing to remove unsafe RELAY_HOME: $RELAY_HOME"
  fi

  rm -rf -- "$RELAY_HOME"
}

remove_relay() {
  local remove_data="${1:-ask}"

  need_root
  need_cmd systemctl
  need_cmd iptables

  if service_exists; then
    systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
  fi

  rm -f -- "$SERVICE_PATH"
  systemctl daemon-reload
  systemctl reset-failed "$SERVICE_NAME" >/dev/null 2>&1 || true
  remove_firewall

  if [[ "$remove_data" == "ask" && -t 0 ]]; then
    read -r -p "Remove relay data and keys at $RELAY_HOME? [y/N]: " remove_data
    remove_data="$(normalize_yes_no "$remove_data")"
  fi

  if [[ "$remove_data" == "yes" ]]; then
    safe_remove_home
    if id "$RELAY_USER" >/dev/null 2>&1; then
      userdel "$RELAY_USER" >/dev/null 2>&1 || true
    fi
    if getent group "$RELAY_USER" >/dev/null 2>&1; then
      groupdel "$RELAY_USER" >/dev/null 2>&1 || true
    fi
  fi

  say "Removed $SERVICE_NAME."
}

show_status() {
  need_cmd systemctl

  say "Service:"
  if service_exists; then
    systemctl --no-pager --lines=12 status "$SERVICE_NAME" || true
  else
    say "  $SERVICE_NAME is not installed."
  fi

  say
  say "Configuration:"
  say "  Relay home: $RELAY_HOME"
  say "  Listen:     $RELAY_LISTEN"
  say "  Status:     $RELAY_STATUS"
  say "  ExecStart:  $(service_exec_start)"

  print_client_url
}

prompt_default() {
  local var_name="$1"
  local prompt="$2"
  local current="$3"
  local value

  read -r -p "$prompt [$current]: " value
  value="${value:-$current}"
  printf -v "$var_name" '%s' "$value"
}

prompt_install_settings() {
  local answer

  say
  say "Install/update settings. Press Enter to keep defaults."
  prompt_default RELAY_HOST "Public DNS/IP for Syncthing URL" "$RELAY_HOST"
  prompt_default ENABLE_443_REDIRECT "Redirect public TCP/443 to relay port? yes/no" "$ENABLE_443_REDIRECT"
  ENABLE_443_REDIRECT="$(normalize_yes_no "$ENABLE_443_REDIRECT")"

  if [[ "$ENABLE_443_REDIRECT" == "yes" ]]; then
    prompt_default PUBLIC_INTERFACE "Public network interface for iptables redirect" "$PUBLIC_INTERFACE"
    RELAY_PORT="${RELAY_PORT:-443}"
  else
    RELAY_PORT="${RELAY_PORT:-$(listen_port "$RELAY_LISTEN")}"
  fi

  read -r -p "Require relay token? Leave empty for no token [$RELAY_TOKEN]: " answer
  RELAY_TOKEN="${answer:-$RELAY_TOKEN}"
}

interactive_menu() {
  local choice

  while true; do
    cat <<EOF_MENU

Syncthing private relay

1) Install/update private relay
2) Show status and Syncthing URL
3) Remove private relay
4) Exit

EOF_MENU
    read -r -p "Choose action [1-4]: " choice

    case "$choice" in
      1)
        prompt_install_settings
        install_relay
        ;;
      2)
        show_status
        ;;
      3)
        remove_relay ask
        ;;
      4|q|Q|exit)
        exit 0
        ;;
      *)
        say "Unknown choice: $choice"
        ;;
    esac
  done
}

usage() {
  cat <<EOF_USAGE
Usage:
  $0 [menu|install|status|remove|purge|url|help]

Commands:
  menu     Show interactive menu (default)
  install  Install or update the private relay service
  status   Show service state and the Syncthing URL
  remove   Remove service and firewall rules, ask before deleting keys
  purge    Remove service, firewall rules, relay data, user, and group
  url      Print only the Syncthing relay URL
  help     Show this help
EOF_USAGE
}

main() {
  local action="${1:-menu}"

  case "$action" in
    menu)
      interactive_menu
      ;;
    install|--install)
      install_relay
      ;;
    status|--status)
      show_status
      ;;
    remove|uninstall|--remove|--uninstall)
      remove_relay ask
      ;;
    purge|--purge)
      remove_relay yes
      ;;
    url|--url)
      relay_client_url
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
}

main "$@"
