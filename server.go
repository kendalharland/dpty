package dpty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Default values for [ServerConfig].
const (
	DefaultPTYReadBufSize = 4096
	DefaultPTYCols        = 100
	DefaultPTYRows        = 35
)

// ServerConfig configures a [Server].
type ServerConfig struct {
	// Addr is the listen address (e.g., ":5137").
	Addr string

	// ID is the identity reported to the [Broker]. Empty means
	// "<hostname>:<port-from-Addr>" (best-effort).
	ID string

	// AdvertiseURL is the URL the Broker (and clients) will use to reach
	// this Server. Empty means "http://localhost<Addr>".
	AdvertiseURL string

	// DefaultShell is used when a CreateOptions request omits Shell.
	DefaultShell string

	// DefaultArgs are appended when a CreateOptions request omits Args.
	DefaultArgs []string

	// DefaultEnv are appended to the spawned process's environment when
	// CreateOptions omits Env.
	DefaultEnv []string

	// SessionBufferMax caps per-session scrollback bytes.
	// Zero means [DefaultSessionBufferMax].
	SessionBufferMax int

	// PTYReadBufSize is the read buffer for the PTY -> client pump.
	// Zero means [DefaultPTYReadBufSize].
	PTYReadBufSize int

	// PTYCols/PTYRows are the initial PTY dimensions. Zero means
	// [DefaultPTYCols] / [DefaultPTYRows].
	PTYCols, PTYRows uint16

	// Logger used for diagnostics. Nil means log.Default().
	Logger *log.Logger
}

// Server hosts PTY sessions and exposes the dpty HTTP API:
//
//	GET  /health         - liveness probe.
//	GET  /status         - {"running":true,"load":N}.
//	GET  /sessions       - list of [SessionInfo].
//	POST /pty            - create a session ([CreateOptions]) -> [CreateResponse].
//	GET  /:alias         - WebSocket attach to an existing session.
type Server struct {
	cfg ServerConfig
	log *log.Logger

	mu       sync.Mutex
	sessions map[string]*Session

	load atomic.Int64

	upgrader websocket.Upgrader
}

// NewServer returns a Server initialized from cfg, applying defaults.
func NewServer(cfg ServerConfig) *Server {
	if cfg.SessionBufferMax <= 0 {
		cfg.SessionBufferMax = DefaultSessionBufferMax
	}
	if cfg.PTYReadBufSize <= 0 {
		cfg.PTYReadBufSize = DefaultPTYReadBufSize
	}
	if cfg.PTYCols == 0 {
		cfg.PTYCols = DefaultPTYCols
	}
	if cfg.PTYRows == 0 {
		cfg.PTYRows = DefaultPTYRows
	}
	if cfg.ID == "" {
		host, _ := os.Hostname()
		cfg.ID = host + cfg.Addr
	}
	if cfg.AdvertiseURL == "" {
		cfg.AdvertiseURL = "http://localhost" + cfg.Addr
	}
	return &Server{
		cfg:      cfg,
		log:      loggerOrDefault(cfg.Logger),
		sessions: map[string]*Session{},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Start runs the Server until ctx is cancelled or the HTTP server fails.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{Addr: s.cfg.Addr, Handler: s.HTTPHandler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	s.log.Printf("Server %s listening on %s\n", s.cfg.ID, s.cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// HTTPHandler returns the Gin engine wired up with the Server's routes.
func (s *Server) HTTPHandler() http.Handler {
	r := newGinEngine()
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/status", s.handleStatus)
	r.GET("/sessions", s.handleListSessions)
	r.POST("/pty", s.handleCreatePTY)
	r.GET("/:alias", s.handleAttach)
	return r
}

// ID returns the Server's broker-facing identity.
func (s *Server) ID() string { return s.cfg.ID }

// AdvertiseURL returns the URL this Server registers with the Broker.
func (s *Server) AdvertiseURL() string { return s.cfg.AdvertiseURL }

// Load returns the current number of live sessions.
func (s *Server) Load() int { return int(s.load.Load()) }

// Sessions returns a snapshot of all live sessions.
func (s *Server) Sessions() []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

// LookupSession returns the session for alias, if any.
func (s *Server) LookupSession(alias string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[alias]
	return sess, ok
}

func newSessionAlias() string {
	alias := uuid.NewString()[:8]
	fmt.Println(alias)
	return alias
}

// CreateSession spawns a PTY according to opts and registers it. Defaults
// from [ServerConfig] are applied when fields are empty.
//
// Errors:
//   - [ErrInvalidName] if Name is non-empty and fails [IsValidSessionName].
//   - [ErrSessionExists] if Name collides with an existing session.
func (s *Server) CreateSession(opts CreateOptions) (*Session, error) {
	if opts.Name != "" && !IsValidSessionName(opts.Name) {
		return nil, ErrInvalidName
	}

	shell := opts.Shell
	if shell == "" {
		shell = s.cfg.DefaultShell
	}
	if shell == "" {
		return nil, fmt.Errorf("dpty: shell is required")
	}
	args := opts.Args
	if len(args) == 0 {
		args = s.cfg.DefaultArgs
	}
	envs := opts.Env
	if len(envs) == 0 {
		envs = s.cfg.DefaultEnv
	}

	ptmx, proc, err := s.spawn(shell, args, envs)
	if err != nil {
		return nil, err
	}

	alias := opts.Name
	if alias == "" {
		alias = newSessionAlias()
	}

	sess := &Session{
		Alias:     alias,
		Shell:     shell,
		Args:      append([]string{}, args...),
		CreatedAt: time.Now(),
		pty:       ptmx,
		process:   proc,
		bufMax:    s.cfg.SessionBufferMax,
	}

	s.mu.Lock()
	if _, exists := s.sessions[alias]; exists {
		s.mu.Unlock()
		_ = ptmx.Close()
		_ = proc.Kill()
		_, _ = proc.Wait()
		return nil, ErrSessionExists
	}
	s.sessions[alias] = sess
	s.mu.Unlock()

	load := s.load.Add(1)
	s.log.Printf("Created session %s (PID=%d, load=%d)\n", alias, proc.Pid, load)

	go s.runReader(sess)
	go s.runWaiter(sess)

	return sess, nil
}

func (s *Server) spawn(shell string, args, envs []string) (*os.File, *os.Process, error) {
	cmd := exec.Command(shell, append([]string{}, args...)...)
	cmd.Env = append(os.Environ(), envs...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: s.cfg.PTYCols,
		Rows: s.cfg.PTYRows,
	})
	if err != nil {
		return nil, nil, err
	}
	return ptmx, cmd.Process, nil
}

// runReader continuously drains the session PTY into the session buffer.
func (s *Server) runReader(sess *Session) {
	buf := make([]byte, s.cfg.PTYReadBufSize)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			sess.appendOutput(buf[:n])
		}
		if err != nil {
			sess.closeOutput()
			return
		}
	}
}

// runWaiter waits for the session's process to exit, then cleans up.
func (s *Server) runWaiter(sess *Session) {
	_, _ = sess.process.Wait()

	s.mu.Lock()
	delete(s.sessions, sess.Alias)
	s.mu.Unlock()

	_ = sess.pty.Close()
	sess.closeOutput()
	load := s.load.Add(-1)
	s.log.Printf("Session %s (PID=%d) closed (load=%d)\n", sess.Alias, sess.process.Pid, load)
}

// ---- HTTP handlers ----

func (s *Server) handleStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"running": true, "load": s.Load()})
}

func (s *Server) handleListSessions(c *gin.Context) {
	sessions := s.Sessions()
	out := make([]SessionInfo, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sess.Info())
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) handleCreatePTY(c *gin.Context) {
	var opts CreateOptions
	if err := c.ShouldBindJSON(&opts); err != nil && err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	sess, err := s.CreateSession(opts)
	switch {
	case errors.Is(err, ErrInvalidName):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid name: use 1-64 chars from [A-Za-z0-9._-]",
		})
		return
	case errors.Is(err, ErrSessionExists):
		c.JSON(http.StatusConflict, gin.H{
			"error": fmt.Sprintf("session %q already exists on this server", opts.Name),
		})
		return
	case err != nil:
		s.log.Printf("CreateSession error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start shell"})
		return
	}

	c.JSON(http.StatusOK, CreateResponse{Alias: sess.Alias})
}

func (s *Server) handleAttach(c *gin.Context) {
	alias := c.Param("alias")
	sess, ok := s.LookupSession(alias)
	if !ok {
		c.String(http.StatusNotFound, "no such session\n")
		return
	}
	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.log.Printf("websocket upgrade error: %v\n", err)
		return
	}
	defer conn.Close()
	s.attachConn(sess, conn)
}

// ---- WebSocket attach loop ----

// attachConn wires a WebSocket to an existing session's PTY. Only one
// active attachment is allowed at a time; subsequent attaches receive a
// short text message and are closed.
//
// Wire protocol:
//   - BINARY frames carry raw PTY bytes in both directions.
//   - TEXT frames are JSON control messages from the client. Currently
//     the only message is {"type":"resize","cols":N,"rows":N}.
func (s *Server) attachConn(sess *Session, conn *websocket.Conn) {
	if !sess.tryAcquireAttach() {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("session is already attached\n"))
		return
	}
	defer sess.setAttached(false)

	s.log.Printf("Attaching to session %s (PID=%d)\n", sess.Alias, sess.process.Pid)

	snapshot, sub, alreadyClosed := sess.subscribe()
	if len(snapshot) > 0 {
		if err := conn.WriteMessage(websocket.BinaryMessage, snapshot); err != nil {
			if sub != nil {
				sess.unsubscribe(sub)
			}
			return
		}
	}
	if alreadyClosed {
		s.log.Printf("Session %s already closed; sent snapshot only\n", sess.Alias)
		return
	}
	defer sess.unsubscribe(sub)

	done := make(chan struct{}, 2)

	go s.pumpClientToPTY(conn, sess, done)
	go s.pumpPTYToClient(conn, sub, done)

	<-done
	s.log.Printf("Detached from session %s (PID=%d)\n", sess.Alias, sess.process.Pid)
}

// resizeMessage is the JSON shape of the only client control message.
type resizeMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func (s *Server) pumpClientToPTY(conn *websocket.Conn, sess *Session, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.TextMessage:
			s.handleControlMessage(sess, msg)
		case websocket.BinaryMessage:
			if !writeAll(sess.pty, msg) {
				return
			}
		}
	}
}

func (s *Server) handleControlMessage(sess *Session, msg []byte) {
	var ctrl resizeMessage
	if err := json.Unmarshal(bytes.TrimSpace(msg), &ctrl); err != nil {
		return
	}
	if ctrl.Type == "resize" && ctrl.Cols > 0 && ctrl.Rows > 0 {
		_ = pty.Setsize(sess.pty, &pty.Winsize{Cols: ctrl.Cols, Rows: ctrl.Rows})
	}
}

func (s *Server) pumpPTYToClient(conn *websocket.Conn, sub <-chan []byte, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	for chunk := range sub {
		if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
			return
		}
	}
}

// writeAll loops until every byte of msg has been written to w. Returns
// false on any short / failed write.
func writeAll(w io.Writer, msg []byte) bool {
	total := 0
	for total < len(msg) {
		n, err := w.Write(msg[total:])
		if err != nil || n <= 0 {
			return false
		}
		total += n
	}
	return true
}

// ---- Broker registration ----

// RegisterWith registers this Server with the Broker at brokerURL,
// retrying a few times on transient failures. It returns the last error
// if all attempts fail.
func (s *Server) RegisterWith(ctx context.Context, brokerURL string) error {
	body, _ := json.Marshal(RegisterRequest{
		ID:      s.cfg.ID,
		Address: s.cfg.AdvertiseURL,
	})
	url := strings.TrimRight(brokerURL, "/") + "/register"

	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for i := 0; i < 5; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				s.log.Printf("Registered with broker as %s @ %s\n", s.cfg.ID, s.cfg.AdvertiseURL)
				return nil
			}
			lastErr = fmt.Errorf("broker returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return lastErr
}
