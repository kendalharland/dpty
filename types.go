package dpty

import (
	"errors"
	"time"
)

// Server status values. A Server is AVAILABLE if the Broker has recently
// gotten a successful /status reply from it; otherwise UNAVAILABLE.
const (
	StatusAvailable   = "AVAILABLE"
	StatusUnavailable = "UNAVAILABLE"
)

// ServerStatus describes one PTY [Server] as known to the [Broker].
type ServerStatus struct {
	ID        string    `json:"id"`
	Address   string    `json:"address"`
	Status    string    `json:"status"`
	Load      int       `json:"load"`
	LastSeen  time.Time `json:"lastSeen"`
	LastError string    `json:"lastError,omitempty"`
}

// SessionInfo describes one live PTY session on a [Server].
type SessionInfo struct {
	Alias     string    `json:"alias"`
	Shell     string    `json:"shell"`
	Args      []string  `json:"args"`
	CreatedAt time.Time `json:"createdAt"`
	InUse     bool      `json:"inUse"`
}

// AggregatedSessionInfo is what the [Broker]'s /sessions endpoint
// returns: a [SessionInfo] plus the address of the owning [Server].
type AggregatedSessionInfo struct {
	SessionInfo
	ServerID      string `json:"serverID"`
	ServerAddress string `json:"serverAddress"`
}

// RegisterRequest is the body a [Server] sends to the [Broker]'s
// /register endpoint when it starts up.
type RegisterRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// CreateOptions are the parameters for creating a new PTY session via
// POST /pty on a [Server].
type CreateOptions struct {
	// Name is the desired session alias. Must be unique on the chosen
	// Server (collisions across different Servers are allowed). If empty,
	// the Server generates a UUID.
	Name string `json:"name,omitempty"`

	// Shell is the program to run.
	Shell string `json:"shell"`

	// Args are command-line arguments passed to Shell.
	Args []string `json:"args"`

	// Env entries (KEY=VALUE) appended to the PTY process environment.
	Env []string `json:"env"`
}

// CreateResponse is the body of a successful POST /pty.
type CreateResponse struct {
	Alias string `json:"alias"`
}

// Common errors returned by [Client] and the Server's POST /pty handler.
var (
	// ErrSessionExists indicates a name collision on the targeted Server.
	ErrSessionExists = errors.New("dpty: session name already exists")

	// ErrInvalidName indicates a session name that fails
	// [IsValidSessionName].
	ErrInvalidName = errors.New("dpty: invalid session name")

	// ErrNoServers indicates that no AVAILABLE Servers are registered
	// with the Broker.
	ErrNoServers = errors.New("dpty: no AVAILABLE servers")

	// ErrSessionNotFound indicates the requested alias is unknown to the
	// targeted Server.
	ErrSessionNotFound = errors.New("dpty: session not found")
)
