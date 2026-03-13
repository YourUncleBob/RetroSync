package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"retrosync/internal/config"
	"retrosync/internal/index"
)

// Client fetches index metadata and files from a remote peer.
type Client struct {
	http *http.Client
	id   string
	name string
	port int
}

// NewClient creates a Client with sensible timeouts. id, name, and port
// identify this node to remote servers so they can register it as a connected
// client without relying on UDP discovery.
func NewClient(id, name string, port int) *Client {
	return &Client{
		http: &http.Client{Timeout: 60 * time.Second},
		id:   id,
		name: name,
		port: port,
	}
}

// addIdentity stamps outgoing requests with this node's identity so the remote
// server can register it as a connected client.
func (c *Client) addIdentity(req *http.Request) {
	req.Header.Set("X-RetroSync-ID", c.id)
	req.Header.Set("X-RetroSync-Name", c.name)
	req.Header.Set("X-RetroSync-Port", strconv.Itoa(c.port))
}

// FetchIndex retrieves the remote node's file index.
func (c *Client) FetchIndex(addr string, port int) (index.Index, error) {
	u := &url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", addr, port), Path: "/index"}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.addIdentity(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return index.FromJSON(data)
}

// FetchStatus retrieves the remote node's name from its /api/status endpoint.
func (c *Client) FetchStatus(addr string, port int) (string, error) {
	u := &url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", addr, port), Path: "/api/status"}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	c.addIdentity(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %s", resp.Status)
	}
	var s struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return "", err
	}
	return s.Name, nil
}

// FetchSyncs retrieves the remote node's sync group configuration.
func (c *Client) FetchSyncs(addr string, port int) ([]config.SyncGroup, error) {
	u := &url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", addr, port), Path: "/api/config"}
	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	c.addIdentity(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	var syncs []config.SyncGroup
	return syncs, json.NewDecoder(resp.Body).Decode(&syncs)
}

// FetchFile downloads a single file from a remote peer and writes it atomically
// to the path returned by resolver(virtualPath), creating intermediate dirs as needed.
func (c *Client) FetchFile(addr string, port int, virtualPath string, resolver func(string) (string, error)) error {
	u := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", addr, port),
		Path:   "/files/" + virtualPath,
	}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	c.addIdentity(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s for %s", resp.Status, virtualPath)
	}

	destPath, err := resolver(virtualPath)
	if err != nil {
		return fmt.Errorf("route %s: %w", virtualPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Write to a temp file then rename for an atomic update.
	tmp, err := os.CreateTemp(filepath.Dir(destPath), ".retrosync-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	tmp.Close()

	return os.Rename(tmpName, destPath)
}

// PushFile uploads a local file to the remote server via PUT /files/<virtualPath>.
func (c *Client) PushFile(addr string, port int, virtualPath, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	u := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", addr, port),
		Path:   "/files/" + virtualPath,
	}
	req, err := http.NewRequest(http.MethodPut, u.String(), f)
	if err != nil {
		return err
	}
	c.addIdentity(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s for PUT %s", resp.Status, virtualPath)
	}
	return nil
}
