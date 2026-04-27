#!/bin/sh
# uninstall-agent.sh — remove a dpty broker or server startup agent.
#
# Counterpart to install-agent.sh. Stops and removes the user-level
# launchd agent (macOS) or systemd --user unit (Linux).
#
# Usage:
#   tools/uninstall-agent.sh {broker|server}

set -eu

if [ $# -lt 1 ]; then
    echo "Usage: $0 {broker|server}" >&2
    exit 2
fi

ROLE="$1"
case "$ROLE" in
    broker|server) ;;
    *) echo "Unknown role: $ROLE (expected 'broker' or 'server')" >&2; exit 2 ;;
esac

LABEL="dpty-$ROLE"
OS=$(uname -s)

case "$OS" in
    Darwin)
        plist="$HOME/Library/LaunchAgents/dev.kjh.${LABEL}.plist"
        target="gui/$(id -u)/dev.kjh.$LABEL"
        launchctl bootout "$target" 2>/dev/null || true
        if [ -f "$plist" ]; then
            rm -f "$plist"
            echo "Removed $plist"
        else
            echo "No launchd agent at $plist"
        fi
        ;;
    Linux)
        unit="$HOME/.config/systemd/user/$LABEL.service"
        if command -v systemctl >/dev/null 2>&1; then
            systemctl --user disable --now "$LABEL.service" 2>/dev/null || true
        fi
        if [ -f "$unit" ]; then
            rm -f "$unit"
            if command -v systemctl >/dev/null 2>&1; then
                systemctl --user daemon-reload 2>/dev/null || true
            fi
            echo "Removed $unit"
        else
            echo "No systemd unit at $unit"
        fi
        ;;
    *)
        echo "Unsupported OS: $OS (need Darwin or Linux)" >&2
        exit 1
        ;;
esac
