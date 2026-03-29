package discovery

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/libp2p/zeroconf/v2"
)

const (
	ServiceType = "_fileshare._tcp"
	Domain      = "local"
)

// MDNSDiscovery handles mDNS service registration and discovery
type MDNSDiscovery struct {
	server   *zeroconf.Server
	registry *DeviceRegistry
	port     int
	hostname string
	shareDir string
}

// NewMDNSDiscovery creates a new mDNS discovery instance
func NewMDNSDiscovery(registry *DeviceRegistry, port int, shareDir string) (*MDNSDiscovery, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-device"
	}

	return &MDNSDiscovery{
		registry: registry,
		port:     port,
		hostname: hostname,
		shareDir: shareDir,
	}, nil
}

// Register starts advertising this device on the network
func (m *MDNSDiscovery) Register(ctx context.Context) error {
	// Encode shared directory in TXT record
	encodedDir := base64.StdEncoding.EncodeToString([]byte(m.shareDir))

	txt := []string{
		fmt.Sprintf("device_name=%s", m.hostname),
		fmt.Sprintf("shared_path=%s", encodedDir),
		fmt.Sprintf("port=%d", m.port),
	}

	server, err := zeroconf.Register(
		m.hostname,
		ServiceType,
		Domain,
		m.port,
		txt,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register mDNS service: %w", err)
	}

	m.server = server
	log.Printf("Registered mDNS service: %s.%s.%s on port %d", m.hostname, ServiceType, Domain, m.port)

	// Keep running until context is cancelled
	<-ctx.Done()
	return nil
}

// Stop stops the mDNS server
func (m *MDNSDiscovery) Stop() {
	if m.server != nil {
		m.server.Shutdown()
		log.Println("mDNS server stopped")
	}
}

// StartDiscovery starts browsing for other devices
func (m *MDNSDiscovery) StartDiscovery(ctx context.Context) error {
	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			device := parseServiceEntry(entry)
			if device != nil {
				m.registry.AddOrUpdate(device)
				log.Printf("Discovered device: %s at %s:%d", device.Instance, device.Addr, device.Port)
			}
		}
	}()

	err := zeroconf.Browse(ctx, ServiceType, Domain, entries)
	if err != nil {
		return fmt.Errorf("failed to browse services: %w", err)
	}

	log.Printf("Started mDNS discovery for %s.%s.%s", ServiceType, Domain, ServiceType)
	return nil
}

// parseServiceEntry converts a zeroconf ServiceEntry to our Device struct
func parseServiceEntry(entry *zeroconf.ServiceEntry) *Device {
	if entry == nil {
		return nil
	}

	device := &Device{
		Instance: entry.Instance,
		Service:  entry.Service,
		Domain:   entry.Domain,
		Port:     entry.Port,
		HostName: entry.HostName,
	}

	// Use first IPv4 address if available
	if len(entry.AddrIPv4) > 0 {
		device.Addr = entry.AddrIPv4[0].String()
	}

	// Parse TXT records
	for _, txt := range entry.Text {
		if len(txt) == 0 {
			continue
		}
		// Handle both "key=value" and raw TXT records
		var key, value string
		if idx := indexOf(txt, '='); idx > 0 {
			key = txt[:idx]
			value = txt[idx+1:]
		} else {
			continue
		}

		switch key {
		case "device_name":
			if device.HostName == "" {
				device.HostName = value
			}
		case "shared_path":
			if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
				device.SharedDir = string(decoded)
			}
		case "port":
			if port, err := strconv.Atoi(value); err == nil {
				device.Port = port
			}
		}
	}

	return device
}

// indexOf returns the index of the first occurrence of a byte in a string
func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// GetLocalIP returns the local IP address
func GetLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no valid IP address found")
}
