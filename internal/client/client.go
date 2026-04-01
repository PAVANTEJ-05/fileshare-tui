package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fileshare/internal/server"
)

// DeviceClient is a client for connecting to remote file share devices
type DeviceClient struct {
	baseURL    string
	httpClient *http.Client
	deviceName string
}

// NewDeviceClient creates a new client for a device
func NewDeviceClient(addr string, port int) *DeviceClient {
	baseURL := fmt.Sprintf("http://%s:%d", addr, port)
	return &DeviceClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ListFiles lists files in a directory on the remote device
func (c *DeviceClient) ListFiles(path string) ([]server.FileInfo, error) {
	reqURL := fmt.Sprintf("%s/list?path=%s", c.baseURL, url.QueryEscape(path))
	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	var files []server.FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return files, nil
}

// DownloadFile downloads a file from the remote device
// progressChan receives progress updates (bytes downloaded)
func (c *DeviceClient) DownloadFile(ctx context.Context, remotePath, localPath string, progressChan chan<- int64) error {
	// URL-encode each path segment to handle special characters while preserving path separators
	segments := strings.Split(filepath.ToSlash(remotePath), "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	reqURL := fmt.Sprintf("%s/files/%s", c.baseURL, strings.Join(segments, "/"))
	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to start download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	// Create parent directories if needed
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create the file
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Download with progress tracking
	var total int64
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			written, werr := file.Write(buf[:n])
			if werr != nil {
				return fmt.Errorf("failed to write: %w", werr)
			}
			total += int64(written)
			if progressChan != nil {
				progressChan <- total
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("download error: %w", err)
		}
	}

	return nil
}

// GetInfo gets information about the remote device
func (c *DeviceClient) GetInfo(ctx context.Context) (map[string]string, error) {
	url := fmt.Sprintf("%s/info", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get info: %w", err)
	}
	defer resp.Body.Close()

	var info map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return info, nil
}

// CheckConnection checks if the device is reachable
func (c *DeviceClient) CheckConnection(ctx context.Context) bool {
	url := fmt.Sprintf("%s/info", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
