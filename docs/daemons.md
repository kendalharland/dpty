# Running dpty as a startup agent

`tools/install-agent.sh` installs the broker or a server as a user-level
startup agent. It uses **launchd** on macOS (`~/Library/LaunchAgents/`) and
**systemd `--user`** on Linux (`~/.config/systemd/user/`), so the same script
works on Ubuntu, Raspberry Pi OS, and macOS.

The installed unit invokes `tools/dpty-agent.sh`, which reads a config file at
each start and execs `dpty broker` / `dpty serve`. Edit the config and
restart the agent — no re-install needed.

## Install

```sh
# Build the binary first.
go build -o bin/dpty ./cmd/dpty

# Then install one or both agents.
tools/install-agent.sh broker
tools/install-agent.sh server
```

The script bakes the absolute path of the `dpty` binary into the agent unit
(`DPTY_BIN`). If the binary moves, re-run install. Discovery order: `$DPTY_BIN`,
`dpty` in `$PATH`, `bin/dpty` relative to the repo.

You can also pass extra flags at install time. They are baked into the unit
and override config-file values for repeated flags:

```sh
tools/install-agent.sh server -port 5200
```

### Linux: enable lingering

systemd `--user` services normally stop when the user logs out. To keep the
agents running across logouts and reboots:

```sh
sudo loginctl enable-linger "$USER"
```

The install script prints this hint if lingering is not already enabled.

## Uninstall

```sh
tools/uninstall-agent.sh broker
tools/uninstall-agent.sh server
```

## Config files

Each role reads its own config file. The default search path is:

```
${DPTY_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/dpty}/<role>.conf
```

So by default:

- `~/.config/dpty/broker.conf`
- `~/.config/dpty/server.conf`

Override the directory by setting `DPTY_CONFIG_DIR` in the environment when
running the install script — the value is baked into the unit and used by
the wrapper at runtime.

### Format

One CLI flag per line. Blank lines and lines starting with `#` are ignored.
Leading and trailing whitespace is trimmed. Each non-blank, non-comment line
is passed as a single argument to `dpty broker` / `dpty serve`, so values
may contain spaces — no quoting is required.

Repeatable flags (`-arg`, `-env`) appear on multiple lines.

Args from the config file come first; args baked into the unit at install
time are appended after. Go's `flag` package keeps the **last** value for
repeated flags, so install-time args override config-file values.

### Example: `~/.config/dpty/broker.conf`

```conf
# Listen on a non-default port.
-port=5127

# Override the state directory (default: $HOME/.config/dpty/broker).
-state-dir=/var/lib/dpty/broker
```

Available flags: see `dpty broker -h`.

### Example: `~/.config/dpty/server.conf`

```conf
-port=5137
-broker=http://localhost:5127
-shell=/bin/bash

# Repeatable flags — one per line.
-arg=-l
-env=PATH=/usr/local/bin:/usr/bin:/bin
-env=TERM=xterm-256color
```

Available flags: see `dpty serve -h`.

## Apply a config change

Edit the config file, then restart the agent:

**macOS**

```sh
launchctl kickstart -k gui/$UID/dev.kjh.dpty-broker
launchctl kickstart -k gui/$UID/dev.kjh.dpty-server
```

**Linux**

```sh
systemctl --user restart dpty-broker
systemctl --user restart dpty-server
```

## Logs

**macOS**: `~/Library/Logs/dpty-broker.log`, `~/Library/Logs/dpty-server.log`
(stdout and stderr combined).

**Linux**: `journalctl --user -u dpty-broker -f` (or `dpty-server`).

## Status

**macOS**

```sh
launchctl print gui/$UID/dev.kjh.dpty-broker
```

**Linux**

```sh
systemctl --user status dpty-broker
```
