# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dpty ./cmd/dpty

FROM alpine:3.20
RUN apk add --no-cache bash ca-certificates tini
COPY --from=builder /out/dpty /usr/local/bin/dpty
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/dpty"]
