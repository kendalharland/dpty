package dpty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultClientTimeout is the default per-request timeout used by [Client].
const DefaultClientTimeout = 5 * time.Second

// Client is a thin HTTP client for the dpty [Broker] and [Server] APIs.
//
// All methods take a context.Context. Use [NewClient] to construct one.
// Methods are safe for concurrent use.
type Client struct {
	brokerURL string
	http      *http.Client
}

// NewClient returns a Client that talks to the [Broker] at brokerURL.
func NewClient(brokerURL string) *Client {
	return &Client{
		brokerURL: strings.TrimRight(brokerURL, "/"),
		http:      &http.Client{Timeout: DefaultClientTimeout},
	}
}

// BrokerURL returns the broker URL the client is configured for.
func (c *Client) BrokerURL() string { return c.brokerURL }

// ListServers returns the broker's view of registered Servers.
func (c *Client) ListServers(ctx context.Context) ([]ServerStatus, error) {
	var out []ServerStatus
	if err := c.getJSON(ctx, c.brokerURL+"/servers", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListSessions returns the broker's aggregated view of live sessions
// across all AVAILABLE Servers.
func (c *Client) ListSessions(ctx context.Context) ([]AggregatedSessionInfo, error) {
	var out []AggregatedSessionInfo
	if err := c.getJSON(ctx, c.brokerURL+"/sessions", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PickAvailableServer returns the AVAILABLE Server with the lowest load.
// Returns [ErrNoServers] if none are available.
func (c *Client) PickAvailableServer(ctx context.Context) (*ServerStatus, error) {
	servers, err := c.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	var best *ServerStatus
	for i := range servers {
		s := &servers[i]
		if s.Status != StatusAvailable {
			continue
		}
		if best == nil || s.Load < best.Load {
			best = s
		}
	}
	if best == nil {
		return nil, ErrNoServers
	}
	return best, nil
}

// CreatePTY creates a new session on the Server at serverAddress and
// returns its alias. On a name collision it returns [ErrSessionExists];
// on an invalid name [ErrInvalidName].
func (c *Client) CreatePTY(ctx context.Context, serverAddress string, opts CreateOptions) (string, error) {
	body, err := json.Marshal(opts)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(serverAddress, "/") + "/pty"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var cr CreateResponse
		if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
			return "", err
		}
		return cr.Alias, nil
	case http.StatusConflict:
		return "", ErrSessionExists
	case http.StatusBadRequest:
		return "", ErrInvalidName
	default:
		return "", fmt.Errorf("dpty: server returned %d", resp.StatusCode)
	}
}

// AttachWebSocketURL builds the ws:// (or wss://) URL a client should
// open to attach to alias on the Server at serverAddress.
//
// It does not contact the network.
func AttachWebSocketURL(serverAddress, alias string) string {
	host := strings.TrimRight(serverAddress, "/")
	switch {
	case strings.HasPrefix(host, "https://"):
		host = "wss://" + strings.TrimPrefix(host, "https://")
	case strings.HasPrefix(host, "http://"):
		host = "ws://" + strings.TrimPrefix(host, "http://")
	}
	return host + "/" + alias
}

func (c *Client) getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dpty: GET %s returned %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
