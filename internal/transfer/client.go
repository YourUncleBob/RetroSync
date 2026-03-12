package transfer

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"retrosync/internal/index"
)

// Client fetches index metadata and files from a remote peer.
type Client struct {
	http *http.Client
}

// NewClient creates a Client with sensible timeouts.
func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

// FetchIndex retrieves the remote node's file index.
func (c *Client) FetchIndex(addr string, port int) (index.Index, error) {
	u := &url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", addr, port), Path: "/index"}
	resp, err := c.http.Get(u.String())
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

// FetchFile downloads a single file from a remote peer and writes it atomically
// to the path returned by resolver(virtualPath), creating intermediate dirs as needed.
func (c *Client) FetchFile(addr string, port int, virtualPath string, resolver func(string) (string, error)) error {
	u := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", addr, port),
		Path:   "/files/" + virtualPath,
	}

	resp, err := c.http.Get(u.String())
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
