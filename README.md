# dpty

A small distributed PTY broker, server, and client. Usable as a library
(`import "kjh.dev/dpty"`) or via the bundled CLI.

## Build

```
go build -o bin/dpty ./cmd/dpty
```

## Run

```
# Tab 1 - broker
./bin/dpty broker

# Tab 2 - one (or more) PTY servers
./bin/dpty serve

# Tab 3 - serve the static HTML
cd demo; python3 -m http.server 8080
```

Visit http://localhost:8080.

The page (`index.html` / `session.html`) talks to the broker (default
`http://localhost:5127`) and to dpty servers directly. Override the broker URL
by appending `?broker=http://host:port` to the page URL.

## CLI

```
dpty broker                       # run the broker
dpty serve [-shell ...]           # run a PTY server (registers with broker)
dpty list  [servers|sessions]     # list state via the broker (default: sessions)
dpty create [-name N] ...         # create a new PTY through the broker
```

## Run as a startup agent

`tools/install-agent.sh` installs the broker or a server as a user-level
startup agent — launchd on macOS, systemd `--user` on Linux (Ubuntu,
Raspberry Pi OS).

```sh
tools/install-agent.sh broker
tools/install-agent.sh server
```

Each role reads its config from
`${XDG_CONFIG_HOME:-$HOME/.config}/dpty/<role>.conf` at every start. One CLI
flag per line; blank lines and `#` comments ignored. Restart the agent to
apply changes:

```sh
# macOS
launchctl kickstart -k gui/$UID/dev.kjh.dpty-broker
# Linux
systemctl --user restart dpty-broker
```

Uninstall: `tools/uninstall-agent.sh {broker|server}`.

See [docs/daemons.md](docs/daemons.md) for the full reference (install
options, config format, logs, status, and how to enable lingering on Linux
so agents survive logout).

## Library

### Go

```go
import "kjh.dev/dpty"
```

See `doc.go` for the package overview. Key APIs:

- `dpty.NewBroker(BrokerConfig).Start(ctx)`
- `dpty.NewServer(ServerConfig).Start(ctx)` + `.RegisterWith(ctx, brokerURL)`
- `dpty.NewClient(brokerURL)` with `ListServers`, `ListSessions`,
  `PickAvailableServer`, `CreatePTY`
- `dpty.AttachWebSocketURL(serverAddress, alias)`

### JavaScript (browser, ES module)

```js
import { Client, Attachment, attachWebSocketUrl } from './dpty.js';

const c = new Client('http://localhost:5127');
const target = await c.pickAvailableServer();
const { alias } = await c.createPTY(target.address, {
  shell: 'claude', args: ['hello'],
});

const att = new Attachment(target.address, alias, {
  onOutput: (text) => term.write(text),
  onClose:  ()    => console.log('closed'),
});
att.resize(80, 24);
att.send('ls\r');
```

`demo/dpty.js` mirrors the Go library: a `Client` for broker / server
HTTP calls, an `Attachment` that wraps the WebSocket attach protocol, and
typed errors (`SessionExistsError`, `InvalidNameError`, `NoServersError`).
