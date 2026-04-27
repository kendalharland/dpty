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
python3 -m http.server 8080
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

## Library

```go
import "kjh.dev/dpty"
```

See `doc.go` for the package overview. Key APIs:

- `dpty.NewBroker(BrokerConfig).Start(ctx)`
- `dpty.NewServer(ServerConfig).Start(ctx)` + `.RegisterWith(ctx, brokerURL)`
- `dpty.NewClient(brokerURL)` with `ListServers`, `ListSessions`,
  `PickAvailableServer`, `CreatePTY`
- `dpty.AttachWebSocketURL(serverAddress, alias)`
