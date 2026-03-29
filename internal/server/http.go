package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileServer represents an HTTP file server
type FileServer struct {
	server    *http.Server
	shareDir  string
	port      int
	mu        sync.RWMutex
	downloads map[string]int64 // track progress by file path
}

// FileInfo represents information about a file
type FileInfo struct {
	Name    string  `json:"name"`
	Size    int64   `json:"size"`
	IsDir   bool    `json:"is_dir"`
	ModTime string  `json:"mod_time"`
	Path    string  `json:"path"`
}

// NewFileServer creates a new file server
func NewFileServer(shareDir string, port int) *FileServer {
	fs := &FileServer{
		shareDir:  shareDir,
		port:      port,
		downloads: make(map[string]int64),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/list", fs.handleList)
	mux.HandleFunc("/files/", fs.handleFile)
	mux.HandleFunc("/info", fs.handleInfo)

	fs.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return fs
}

// Start starts the HTTP server
func (fs *FileServer) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		fs.server.Shutdown(context.Background())
	}()

	log.Printf("Starting file server on port %d, sharing: %s", fs.port, fs.shareDir)
	if err := fs.server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// handleList returns a JSON list of files in a directory
func (fs *FileServer) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// Clean and validate path
	cleanPath := filepath.Clean(path)
	fullPath := filepath.Join(fs.shareDir, cleanPath)

	// Ensure path is within shareDir
	if !filepath.IsLocal(fullPath) {
		// Check manually
		absShare, _ := filepath.Abs(fs.shareDir)
		absPath, _ := filepath.Abs(fullPath)
		if len(absPath) < len(absShare) || absPath[:len(absShare)] != absShare {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read directory: %v", err), http.StatusInternalServerError)
		return
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		filePath := filepath.Join(cleanPath, entry.Name())
		files = append(files, FileInfo{
			Name:    entry.Name(),
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime().Format(time.RFC3339),
			Path:    filePath,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// handleFile serves a file for download
func (fs *FileServer) handleFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract file path from URL
	filePath := r.URL.Path[len("/files/"):]
	if filePath == "" {
		http.Error(w, "No file specified", http.StatusBadRequest)
		return
	}

	// Clean and validate path
	cleanPath := filepath.Clean("/" + filePath)[1:] // Remove leading slash
	fullPath := filepath.Join(fs.shareDir, cleanPath)

	// Ensure path is within shareDir
	absShare, _ := filepath.Abs(fs.shareDir)
	absPath, _ := filepath.Abs(fullPath)
	if len(absPath) < len(absShare) || absPath[:len(absShare)] != absShare {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if file exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Error accessing file: %v", err), http.StatusInternalServerError)
		}
		return
	}

	if info.IsDir() {
		http.Error(w, "Cannot download directory", http.StatusBadRequest)
		return
	}

	// Open file for reading
	file, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error opening file: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Set headers for download
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(filePath)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	fs.mu.Lock()
	fs.downloads[filePath] = 0
	fs.mu.Unlock()

	// Stream the file
	buf := make([]byte, 32*1024)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				fs.mu.Lock()
				delete(fs.downloads, filePath)
				fs.mu.Unlock()
				return
			}
			fs.mu.Lock()
			fs.downloads[filePath] += int64(n)
			fs.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				http.Error(w, fmt.Sprintf("Error reading file: %v", err), http.StatusInternalServerError)
			}
			break
		}
	}

	fs.mu.Lock()
	delete(fs.downloads, filePath)
	fs.mu.Unlock()
}

// handleInfo returns server information
func (fs *FileServer) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]string{
		"share_dir": fs.shareDir,
		"hostname":  "fileshare-server",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}
