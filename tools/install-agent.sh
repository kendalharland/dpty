#!/bin/sh
# install-agent.sh — install dpty broker or server as a user-level startup agent.
#
# Uses launchd on macOS and systemd --user on Linux (Ubuntu, Raspberry Pi OS).
# The installed agent invokes tools/dpty-agent.sh, which reads its config from
#
#   ${DPTY_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/dpty}/<role>.conf
#
# at every start. Edit the config and restart the agent to apply changes:
#   macOS:  launchctl kickstart -k gui/$UID/dev.kjh.dpty-<role>
#   Linux:  systemctl --user restart dpty-<role>
#
# Usage:
#   tools/install-agent.sh {broker|server} [extra dpty args...]
#
# Extra args after the role are baked into the agent unit and override values
# from the config file (Go's flag package keeps the last value for repeats).
#
# Binary discovery (at install time, baked into the unit as DPTY_BIN):
#   $DPTY_BIN, then `dpty` in $PATH, then ../bin/dpty relative to this script.

set -eu

if [ $# -lt 1 ]; then
    echo "Usage: $0 {broker|server} [extra dpty args...]" >&2
    exit 2
fi

ROLE="$1"
shift
case "$ROLE" in
    broker|server) ;;
    *) echo "Unknown role: $ROLE (expected 'broker' or 'server')" >&2; exit 2 ;;
esac

script_dir=$(cd "$(dirname "$0")" && pwd)
project_root=$(cd "$script_dir/.." && pwd)
wrapper="$script_dir/dpty-agent.sh"

if [ ! -x "$wrapper" ]; then
    echo "Wrapper not found or not executable: $wrapper" >&2
    exit 1
fi

if [ -n "${DPTY_BIN:-}" ] && [ -x "$DPTY_BIN" ]; then
    BIN="$DPTY_BIN"
elif command -v dpty >/dev/null 2>&1; then
    BIN=$(command -v dpty)
elif [ -x "$project_root/bin/dpty" ]; then
    BIN="$project_root/bin/dpty"
else
    echo "dpty binary not found." >&2
    echo "Build it (go build -o bin/dpty ./cmd/dpty) or set DPTY_BIN." >&2
    exit 1
fi

case "$BIN" in
    /*) ;;
    *) BIN=$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN") ;;
esac

config_dir="${DPTY_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/dpty}"
config_file="$config_dir/$ROLE.conf"

LABEL="dpty-$ROLE"
OS=$(uname -s)

case "$OS" in
    Darwin)
        plist_dir="$HOME/Library/LaunchAgents"
        plist="$plist_dir/dev.kjh.${LABEL}.plist"
        log_dir="$HOME/Library/Logs"
        mkdir -p "$plist_dir" "$log_dir"

        {
            printf '<?xml version="1.0" encoding="UTF-8"?>\n'
            printf '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n'
            printf '<plist version="1.0">\n<dict>\n'
            printf '  <key>Label</key><string>dev.kjh.%s</string>\n' "$LABEL"
            printf '  <key>ProgramArguments</key>\n  <array>\n'
            printf '    <string>%s</string>\n' "$wrapper"
            printf '    <string>%s</string>\n' "$ROLE"
            for arg in "$@"; do
                printf '    <string>%s</string>\n' "$arg"
            done
            printf '  </array>\n'
            printf '  <key>EnvironmentVariables</key>\n  <dict>\n'
            printf '    <key>DPTY_BIN</key><string>%s</string>\n' "$BIN"
            if [ -n "${DPTY_CONFIG_DIR:-}" ]; then
                printf '    <key>DPTY_CONFIG_DIR</key><string>%s</string>\n' "$DPTY_CONFIG_DIR"
            fi
            printf '  </dict>\n'
            printf '  <key>RunAtLoad</key><true/>\n'
            printf '  <key>KeepAlive</key><true/>\n'
            printf '  <key>StandardOutPath</key><string>%s/%s.log</string>\n' "$log_dir" "$LABEL"
            printf '  <key>StandardErrorPath</key><string>%s/%s.log</string>\n' "$log_dir" "$LABEL"
            printf '</dict>\n</plist>\n'
        } > "$plist"

        target="gui/$(id -u)/dev.kjh.$LABEL"
        launchctl bootout "$target" 2>/dev/null || true
        launchctl bootstrap "gui/$(id -u)" "$plist"
        launchctl kickstart -k "$target" >/dev/null 2>&1 || true

        echo "Installed launchd agent: $plist"
        echo "Config:  $config_file (edit, then 'launchctl kickstart -k $target')"
        echo "Logs:    $log_dir/$LABEL.log"
        ;;
    Linux)
        if ! command -v systemctl >/dev/null 2>&1; then
            echo "systemctl not found; this script requires systemd on Linux." >&2
            exit 1
        fi
        unit_dir="$HOME/.config/systemd/user"
        unit="$unit_dir/$LABEL.service"
        mkdir -p "$unit_dir"

        sd_quote() {
            printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
        }

        exec_line="\"$(sd_quote "$wrapper")\" $ROLE"
        for arg in "$@"; do
            exec_line="$exec_line \"$(sd_quote "$arg")\""
        done

        {
            printf '[Unit]\n'
            printf 'Description=dpty %s\n' "$ROLE"
            printf 'After=network.target\n\n'
            printf '[Service]\n'
            printf 'Environment="DPTY_BIN=%s"\n' "$(sd_quote "$BIN")"
            if [ -n "${DPTY_CONFIG_DIR:-}" ]; then
                printf 'Environment="DPTY_CONFIG_DIR=%s"\n' "$(sd_quote "$DPTY_CONFIG_DIR")"
            fi
            printf 'ExecStart=%s\n' "$exec_line"
            printf 'Restart=on-failure\n'
            printf 'RestartSec=5\n\n'
            printf '[Install]\n'
            printf 'WantedBy=default.target\n'
        } > "$unit"

        systemctl --user daemon-reload
        systemctl --user enable --now "$LABEL.service"

        if command -v loginctl >/dev/null 2>&1; then
            if ! loginctl show-user "$USER" 2>/dev/null | grep -q '^Linger=yes'; then
                echo
                echo "To keep the agent running when you're not logged in, enable lingering:"
                echo "  sudo loginctl enable-linger \"$USER\""
            fi
        fi

        echo "Installed systemd user unit: $unit"
        echo "Config:  $config_file (edit, then 'systemctl --user restart $LABEL.service')"
        echo "Logs:    journalctl --user -u $LABEL.service"
        ;;
    *)
        echo "Unsupported OS: $OS (need Darwin or Linux)" >&2
        exit 1
        ;;
esac
