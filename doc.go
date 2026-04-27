// Package dpty implements a small distributed PTY broker, server, and
// client.
//
// # Overview
//
// dpty splits PTY management into three pieces that talk to each other
// over HTTP:
//
//   - A [Broker] (one per cluster) keeps a directory of registered
//     [Server]s and aggregates session state.
//   - One or more [Server]s each spawn and own PTY processes.
//   - A [Client] (this package's HTTP-level helper, the bundled CLI, or
//     a browser via WebSocket) discovers Servers through the Broker, then
//     creates or attaches to sessions on a chosen Server.
//
// Broker and Server are independent processes: a Server registers itself
// with the Broker on startup, and the Broker periodically pings the
// Server's /status endpoint to track availability and load.
//
// # Wire types
//
// All HTTP responses are JSON. The most important types are:
//   - [ServerStatus]: published by the Broker for each known Server.
//   - [SessionInfo]: published by a Server for each of its live sessions.
//   - [AggregatedSessionInfo]: published by the Broker, combining
//     [SessionInfo] with the address of the owning Server.
//   - [CreateOptions]: passed in the body of POST /pty.
//
// # Quick start (server side)
//
//	ctx := context.Background()
//
//	broker := dpty.NewBroker(dpty.BrokerConfig{Addr: ":5127"})
//	go broker.Start(ctx)
//
//	srv := dpty.NewServer(dpty.ServerConfig{
//	    Addr:  ":5137",
//	    Shell: "/bin/bash",
//	})
//	go srv.Start(ctx)
//	srv.RegisterWith(ctx, "http://localhost:5127")
//
// # Quick start (client side)
//
//	c := dpty.NewClient("http://localhost:5127")
//	target, _ := c.PickAvailableServer(ctx)
//	alias, _  := c.CreatePTY(ctx, target.Address, dpty.CreateOptions{
//	    Shell: "claude", Args: []string{"hello"},
//	})
//	wsURL := dpty.AttachWebSocketURL(target.Address, alias)
//
// # WebSocket attach protocol
//
// The /<alias> endpoint speaks a tiny protocol over the WebSocket:
//   - BINARY frames carry raw PTY input (client -> server) and output
//     (server -> client) bytes.
//   - TEXT frames are JSON control messages from the client. Currently
//     the only message is {"type":"resize","cols":N,"rows":N}.
package dpty
