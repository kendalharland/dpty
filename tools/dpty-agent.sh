#!/bin/sh
# dpty-agent.sh — runtime helper invoked by the launchd/systemd unit installed
# by tools/install-agent.sh. Reads the user-level config file (if any) and
# execs dpty with the resulting flags, plus any extra args from the agent unit.
#
# Usage (normally called by the agent, not by hand):
#   dpty-agent.sh {broker|server} [extra args...]
#
# Config search order:
#   $DPTY_CONFIG_DIR/<role>.conf
#   ${XDG_CONFIG_HOME:-$HOME/.config}/dpty/<role>.conf
#
# Config file format: one CLI flag per non-blank, non-comment line. Lines
# starting with '#' are ignored. Repeatable flags appear on multiple lines.
# Example ($HOME/.config/dpty/server.conf):
#
#   -port=5137
#   -broker=http://localhost:5127
#   -arg=--dangerously-skip-permissions
#   -env=PATH=/usr/local/bin:/usr/bin:/bin
#
# Args from the config file come first; any extra args appended after the
# role (either by the agent unit or on the command line) override them
# because Go's flag package keeps the last value for repeated flags.
#
# Binary discovery: $DPTY_BIN (set by the agent unit at install time),
# then `dpty` in $PATH, then ../bin/dpty relative to this script.

set -eu

if [ $# -lt 1 ]; then
    echo "Usage: $0 {broker|server} [extra dpty args...]" >&2
    exit 2
fi

ROLE="$1"
shift
case "$ROLE" in
    broker) SUBCMD="broker" ;;
    server) SUBCMD="serve" ;;
    *) echo "dpty-agent: unknown role: $ROLE" >&2; exit 2 ;;
esac

script_dir=$(cd "$(dirname "$0")" && pwd)
project_root=$(cd "$script_dir/.." && pwd)

if [ -n "${DPTY_BIN:-}" ] && [ -x "$DPTY_BIN" ]; then
    BIN="$DPTY_BIN"
elif command -v dpty >/dev/null 2>&1; then
    BIN=$(command -v dpty)
elif [ -x "$project_root/bin/dpty" ]; then
    BIN="$project_root/bin/dpty"
else
    echo "dpty-agent: dpty binary not found (set DPTY_BIN or put it in PATH)" >&2
    exit 1
fi

config_dir="${DPTY_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/dpty}"
config_file="$config_dir/$ROLE.conf"

# Seed a default (all-commented) config file on first run so users can
# discover where to configure things without consulting docs.
if [ ! -e "$config_file" ]; then
    mkdir -p "$config_dir"
    case "$ROLE" in
        broker)
            cat > "$config_file" <<'EOF'
# dpty broker config — one CLI flag per line, '#' for comments.
# Restart the agent to apply changes:
#   macOS:  launchctl kickstart -k gui/$UID/dev.kjh.dpty-broker
#   Linux:  systemctl --user restart dpty-broker
# See `dpty broker -h` for the full set of flags.

# -port=5127
# -state-dir=/var/lib/dpty/broker
EOF
            ;;
        server)
            cat > "$config_file" <<'EOF'
# dpty server config — one CLI flag per line, '#' for comments.
# Restart the agent to apply changes:
#   macOS:  launchctl kickstart -k gui/$UID/dev.kjh.dpty-server
#   Linux:  systemctl --user restart dpty-server
# See `dpty serve -h` for the full set of flags.

# -port=5137
# -broker=http://localhost:5127
# -advertise=http://your-host-or-ip:5137
# -shell=/bin/bash
# -arg=-l
# -env=PATH=/usr/local/bin:/usr/bin:/bin
EOF
            ;;
    esac
fi

cfg_quoted=""
if [ -f "$config_file" ]; then
    while IFS= read -r line || [ -n "$line" ]; do
        line=$(printf '%s' "$line" | sed 's/\r$//; s/^[[:space:]]*//; s/[[:space:]]*$//')
        case "$line" in ''|\#*) continue ;; esac
        esc=$(printf '%s' "$line" | sed "s/'/'\\\\''/g")
        cfg_quoted="$cfg_quoted '$esc'"
    done < "$config_file"
fi

user_quoted=""
for a in "$@"; do
    esc=$(printf '%s' "$a" | sed "s/'/'\\\\''/g")
    user_quoted="$user_quoted '$esc'"
done

eval "set -- $cfg_quoted $user_quoted"
exec "$BIN" "$SUBCMD" "$@"
