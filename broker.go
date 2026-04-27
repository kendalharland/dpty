package dpty

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Default ping/eviction timings used when [BrokerConfig] zero-values are
// supplied. Exported so callers can build their own configurations
// relative to the defaults.
const (
	DefaultBrokerPingEvery        = 5 * time.Second
	DefaultBrokerUnavailableAfter = 10 * time.Second
	DefaultBrokerRemoveAfter      = 60 * time.Second
)

// BrokerConfig configures a [Broker].
type BrokerConfig struct {
	// Addr is the listen address (e.g., ":5127").
	Addr string

	// StateDir is the directory for the on-disk state file.
	// Empty means [DefaultBrokerStateDir].
	StateDir string

	// PingEvery controls how often the Broker pings each registered
	// Server's /status endpoint. Zero means [DefaultBrokerPingEvery].
	PingEvery time.Duration

	// UnavailableAfter marks a Server UNAVAILABLE if it's been
	// unreachable longer than this. Zero means
	// [DefaultBrokerUnavailableAfter].
	UnavailableAfter time.Duration

	// RemoveAfter evicts an UNAVAILABLE Server after this much time. Zero
	// means [DefaultBrokerRemoveAfter].
	RemoveAfter time.Duration

	// Logger used for diagnostics. Nil means log.Default().
	Logger *log.Logger
}

// Broker tracks PTY [Server]s and aggregates session state across them.
//
// It exposes the following HTTP endpoints:
//
//	POST /register            - Server registration ([RegisterRequest]).
//	GET  /servers             - list of [ServerStatus].
//	GET  /sessions            - list of [AggregatedSessionInfo].
//	GET  /broker/ping         - liveness probe.
type Broker struct {
	cfg BrokerConfig
	log *log.Logger

	mu      sync.Mutex
	servers map[string]*ServerStatus

	// Pinger uses this client to talk to registered Servers.
	httpClient *http.Client
}

// NewBroker returns a Broker initialized from cfg, applying defaults for
// any zero-valued duration fields and for StateDir / Logger.
func NewBroker(cfg BrokerConfig) *Broker {
	if cfg.PingEvery <= 0 {
		cfg.PingEvery = DefaultBrokerPingEvery
	}
	if cfg.UnavailableAfter <= 0 {
		cfg.UnavailableAfter = DefaultBrokerUnavailableAfter
	}
	if cfg.RemoveAfter <= 0 {
		cfg.RemoveAfter = DefaultBrokerRemoveAfter
	}
	if cfg.StateDir == "" {
		cfg.StateDir = DefaultBrokerStateDir()
	}
	return &Broker{
		cfg:        cfg,
		log:        loggerOrDefault(cfg.Logger),
		servers:    map[string]*ServerStatus{},
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
}

// Start runs the broker until ctx is cancelled or the HTTP server fails.
// On startup it rehydrates known Servers from disk and begins pinging
// them. Returns the underlying HTTP server's error (or nil on graceful
// shutdown).
func (b *Broker) Start(ctx context.Context) error {
	b.loadFromDisk()

	r := b.HTTPHandler()

	srv := &http.Server{Addr: b.cfg.Addr, Handler: r}

	pingCtx, pingCancel := context.WithCancel(ctx)
	defer pingCancel()
	go b.pingLoop(pingCtx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	b.log.Printf("Broker listening on %s\n", b.cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// HTTPHandler returns the Gin engine wired up with the Broker's routes.
// Useful for tests or for embedding the Broker behind another HTTP stack.
func (b *Broker) HTTPHandler() http.Handler {
	r := newGinEngine()
	r.POST("/register", b.handleRegister)
	r.GET("/servers", b.handleListServers)
	r.GET("/sessions", b.handleListSessions)
	r.GET("/broker/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "broker alive")
	})
	return r
}

// Snapshot returns a copy of the Broker's current view of Servers.
func (b *Broker) Snapshot() []ServerStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ServerStatus, 0, len(b.servers))
	for _, s := range b.servers {
		out = append(out, *s)
	}
	return out
}

// ---- HTTP handlers ----

func (b *Broker) handleRegister(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid register payload"})
		return
	}
	if req.ID == "" || req.Address == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id and address required"})
		return
	}

	now := time.Now()
	b.mu.Lock()
	st, ok := b.servers[req.ID]
	addrChanged := false
	if !ok {
		st = &ServerStatus{
			ID:       req.ID,
			Address:  req.Address,
			Status:   StatusUnavailable,
			LastSeen: now,
		}
		b.servers[req.ID] = st
		addrChanged = true
	} else {
		addrChanged = st.Address != req.Address
		st.Address = req.Address
		st.LastSeen = now
	}
	if addrChanged {
		b.persistLocked()
	}
	b.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (b *Broker) handleListServers(c *gin.Context) {
	c.JSON(http.StatusOK, b.Snapshot())
}

func (b *Broker) handleListSessions(c *gin.Context) {
	servers := b.Snapshot()

	out := []AggregatedSessionInfo{}
	for _, s := range servers {
		if s.Status != StatusAvailable {
			continue
		}
		infos, err := b.fetchSessionsFrom(s.Address)
		if err != nil {
			continue
		}
		for _, info := range infos {
			out = append(out, AggregatedSessionInfo{
				SessionInfo:   info,
				ServerID:      s.ID,
				ServerAddress: s.Address,
			})
		}
	}
	c.JSON(http.StatusOK, out)
}

func (b *Broker) fetchSessionsFrom(addr string) ([]SessionInfo, error) {
	url := strings.TrimRight(addr, "/") + "/sessions"
	resp, err := b.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var infos []SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&infos); err != nil {
		return nil, err
	}
	return infos, nil
}

// ---- Pinger ----

// pingLoop periodically polls every registered Server's /status endpoint
// and updates the in-memory ServerStatus entries. Servers that stay
// UNAVAILABLE longer than RemoveAfter are evicted (and the change is
// persisted to disk).
func (b *Broker) pingLoop(ctx context.Context) {
	t := time.NewTicker(b.cfg.PingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.tickOnce()
		}
	}
}

func (b *Broker) tickOnce() {
	now := time.Now()

	b.mu.Lock()
	defer b.mu.Unlock()

	evicted := false
	for id, st := range b.servers {
		if now.Sub(st.LastSeen) > b.cfg.RemoveAfter && st.Status == StatusUnavailable {
			b.log.Printf("Removing server %s (gone too long)\n", id)
			delete(b.servers, id)
			evicted = true
			continue
		}
		b.pingOne(now, st)
	}
	if evicted {
		b.persistLocked()
	}
}

func (b *Broker) pingOne(now time.Time, st *ServerStatus) {
	url := strings.TrimRight(st.Address, "/") + "/status"
	resp, err := b.httpClient.Get(url)
	if err != nil {
		if now.Sub(st.LastSeen) > b.cfg.UnavailableAfter {
			st.Status = StatusUnavailable
		}
		st.LastError = err.Error()
		return
	}
	defer resp.Body.Close()

	var parsed struct {
		Running bool `json:"running"`
		Load    int  `json:"load"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		if now.Sub(st.LastSeen) > b.cfg.UnavailableAfter {
			st.Status = StatusUnavailable
		}
		st.LastError = fmt.Sprintf("decode error: %v", err)
		return
	}

	if parsed.Running {
		st.Status = StatusAvailable
	} else if now.Sub(st.LastSeen) > b.cfg.UnavailableAfter {
		st.Status = StatusUnavailable
	}
	st.Load = parsed.Load
	st.LastSeen = now
	st.LastError = ""
}

// ---- Persistence ----

func (b *Broker) loadFromDisk() {
	entries, err := readBrokerState(b.cfg.StateDir)
	if err != nil {
		b.log.Printf("Failed to read broker state: %v\n", err)
		return
	}
	now := time.Now()
	b.mu.Lock()
	for id, addr := range entries {
		b.servers[id] = &ServerStatus{
			ID:       id,
			Address:  addr,
			Status:   StatusUnavailable,
			LastSeen: now,
		}
	}
	b.mu.Unlock()
	if len(entries) > 0 {
		b.log.Printf("Loaded %d server(s) from %s\n", len(entries), b.cfg.StateDir)
	}
}

// persistLocked writes the current servers map to disk. Caller must hold
// b.mu.
func (b *Broker) persistLocked() {
	entries := make(map[string]string, len(b.servers))
	for id, s := range b.servers {
		entries[id] = s.Address
	}
	if err := writeBrokerState(b.cfg.StateDir, entries); err != nil {
		b.log.Printf("Failed to persist broker state: %v\n", err)
	}
}
