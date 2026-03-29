package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fileshare/internal/discovery"
	"fileshare/internal/server"
	"fileshare/internal/tui"
)

const (
	defaultPort     = 8765
	defaultShareDir = "."
)

func main() {
	// Parse command line flags
	port := flag.Int("port", defaultPort, "Port to run the file server on")
	shareDir := flag.String("dir", defaultShareDir, "Directory to share")
	noServer := flag.Bool("client-only", false, "Don't start a server, only browse other devices")
	flag.Parse()

	// Validate share directory
	if *shareDir != "." && *shareDir != "" {
		if _, err := os.Stat(*shareDir); os.IsNotExist(err) {
			log.Printf("Warning: Share directory does not exist: %s", *shareDir)
			if err := os.MkdirAll(*shareDir, 0755); err != nil {
				log.Fatalf("Failed to create share directory: %v", err)
			}
			log.Printf("Created share directory: %s", *shareDir)
		}
	}

	// Create context that cancels on interrupt
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Create device registry
	registry := discovery.NewDeviceRegistry()

	// Start device discovery
	go func() {
		mdnsDisc, err := discovery.NewMDNSDiscovery(registry, *port, *shareDir)
		if err != nil {
			log.Printf("Warning: Failed to create mDNS discovery: %v", err)
			return
		}

		// Start browsing for other devices
		if err := mdnsDisc.StartDiscovery(ctx); err != nil {
			log.Printf("Warning: Failed to start mDNS discovery: %v", err)
		}
	}()

	// Periodically clean expired devices from registry
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				registry.CleanExpired(60 * time.Second)
			}
		}
	}()

	// Start file server if not client-only
	if !*noServer {
		go func() {
			fileServer := server.NewFileServer(*shareDir, *port)
			if err := fileServer.Start(ctx); err != nil {
				log.Printf("Server error: %v", err)
			}
		}()

		// Register our service with mDNS
		go func() {
			mdnsDisc, err := discovery.NewMDNSDiscovery(registry, *port, *shareDir)
			if err != nil {
				log.Printf("Warning: Failed to create mDNS service: %v", err)
				return
			}
			if err := mdnsDisc.Register(ctx); err != nil {
				log.Printf("Warning: Failed to register mDNS service: %v", err)
			}
		}()

		log.Printf("FileShare server starting on port %d", *port)
		log.Printf("Sharing directory: %s", *shareDir)
	} else {
		log.Println("Running in client-only mode")
	}

	// Create and run TUI
	log.Println("Starting TUI...")
	app := tui.NewApp(registry)
	if err := app.Run(); err != nil {
		log.Fatalf("TUI error: %v", err)
	}

	fmt.Println("\nGoodbye!")
}
