package discovery

import (
	"sync"
	"time"
)

// Device represents a discovered device on the network
type Device struct {
	Instance  string
	Service   string
	Domain    string
	Addr      string
	Port      int
	HostName  string
	SharedDir string
	LastSeen  time.Time
	Connected bool
}

// DeviceRegistry manages discovered devices
type DeviceRegistry struct {
	mu      sync.RWMutex
	devices map[string]*Device
}

// NewDeviceRegistry creates a new device registry
func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{
		devices: make(map[string]*Device),
	}
}

// AddOrUpdate adds a device to the registry or updates an existing one
func (r *DeviceRegistry) AddOrUpdate(device *Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device.LastSeen = time.Now()
	r.devices[device.Instance] = device
}

// Get returns a device by instance name
func (r *DeviceRegistry) Get(instance string) (*Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	device, ok := r.devices[instance]
	if !ok {
		return nil, false
	}
	return device, ok
}

// GetAll returns all discovered devices
func (r *DeviceRegistry) GetAll() []*Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	devices := make([]*Device, 0, len(r.devices))
	for _, d := range r.devices {
		devices = append(devices, d)
	}
	return devices
}

// Remove removes a device from the registry
func (r *DeviceRegistry) Remove(instance string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.devices, instance)
}

// CleanExpired removes devices that haven't been seen recently
func (r *DeviceRegistry) CleanExpired(timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for key, device := range r.devices {
		if now.Sub(device.LastSeen) > timeout {
			delete(r.devices, key)
		}
	}
}
