# Running dpty in Docker

The repo ships a multi-stage `Dockerfile` that produces a single image
runnable as either the broker or a server — pick which by overriding the
command. Use it from your own `compose.yaml`; this repo does not ship one
of its own.

## Build the image

From a clone of this repo:

```sh
docker build -t dpty:latest .
```

Or build it from compose by pointing `build:` at the path to the dpty
checkout (see snippets below).

## Compose snippet

Drop these services into your existing `compose.yaml`. Adjust the `build:`
context (or replace it with `image: dpty:latest`) to match where the dpty
checkout lives in your project.

```yaml
services:
  broker:
    build: ./vendor/dpty           # or: image: dpty:latest
    command: ["broker", "-port=5127", "-state-dir=/data"]
    ports:
      - "5127:5127"
    volumes:
      - dpty-broker-state:/data
    restart: unless-stopped

  dpty-server:
    build: ./vendor/dpty           # or: image: dpty:latest
    command:
      - serve
      - -port=5137
      - -broker=http://broker:5127
      - -advertise=${DPTY_SERVER_ADVERTISE:-http://localhost:5137}
    ports:
      - "5137:5137"
    depends_on:
      - broker
    restart: unless-stopped

volumes:
  dpty-broker-state:
```

You only need the services you actually want — drop `broker` if you point
the server at an external broker, or drop `dpty-server` if you only want
the broker.

### About `-advertise`

The broker hands clients the URL they should use to attach via WebSocket.
That URL must be reachable from wherever the client is — typically a
browser running on the docker host. The default
`http://localhost:5137` is correct when the published port `5137:5137`
forwards to the host's localhost. If clients live elsewhere, override:

```sh
DPTY_SERVER_ADVERTISE=http://my-host.lan:5137 docker compose up
```

### Using `include:` instead of copy-paste

If your compose CLI supports `include:` (Compose Spec, late-2023+), you
can keep the snippet in a separate file under your project and pull it in:

```yaml
include:
  - path: ./vendor/dpty/compose.dpty.yaml

services:
  app:
    # ...
```

This repo does not ship `compose.dpty.yaml` itself — author it once in
your own project from the snippet above.

## State

The broker persists its state under `-state-dir`. The snippet mounts a
named volume at `/data` so it survives container restarts. The server is
stateless.

## PTY support

The container runs unprivileged. Default Docker settings expose
`/dev/ptmx` and a private `/dev/pts`, which is what the server needs to
allocate PTYs. No `--privileged` or device mounts required.

The runtime image is `alpine` with `bash` installed, so `dpty serve`
defaults work. If you need a different shell or extra binaries inside
sessions (for example `claude`), bake them into a derived image:

```dockerfile
FROM dpty:latest
RUN apk add --no-cache zsh
```
