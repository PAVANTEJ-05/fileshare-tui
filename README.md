# FileShare

A terminal UI application for sharing files across devices on the same local network.

## Features

- **Automatic Device Discovery**: Uses mDNS (multicast DNS) to automatically discover other devices running FileShare on your local network
- **File Browser**: Browse shared directories on remote devices
- **File Download**: Download files from connected devices
- **Real-time Updates**: Device list refreshes automatically

## Installation

```bash
# Build from source
go build ./cmd/fileshare

# Or install globally
go install ./cmd/fileshare
```

## Usage

### Basic Usage

Start FileShare on a device (this will both share files and allow browsing others):

```bash
./fileshare -dir /path/to/share -port 8765
```

### Command Line Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-dir` | Directory to share | Current directory |
| `-port` | Port to listen on | 8765 |
| `-client-only` | Don't start server, only browse other devices | false |

### Examples

```bash
# Share current directory
./fileshare

# Share a specific directory on a custom port
./fileshare -dir ~/Documents -port 9000

# Client-only mode (don't share, only browse)
./fileshare -client-only
```

## How It Works

1. **Device Discovery**: When you start FileShare, it:
   - Registers your device on the local network using mDNS
   - Starts browsing for other FileShare devices
   - Devices appear in the list within a few seconds

2. **File Sharing**: Each device runs an HTTP server that:
   - Serves files from the shared directory
   - Provides directory listings via `/list?path=<dir>`
   - Serves file downloads via `/files/<path>`

3. **Browsing & Downloading**:
   - Select a device from the list with arrow keys
   - Press Enter to connect and browse files
   - Navigate directories with Enter (down) and Backspace (up)
   - Press 'd' to download a file

## Keyboard Controls

| Key | Action |
|-----|--------|
| `↑/↓` | Navigate list |
| `Enter` | Select device / Open directory |
| `Backspace` | Go up one directory |
| `d` | Download selected file |
| `r` | Refresh |
| `q` | Quit |

## Network Requirements

- Devices must be on the same local network (LAN)
- mDNS (UDP port 5353) must be allowed through firewalls
- The configured port (default 8765) must be open for incoming connections

## Architecture

```
fileshare/
├── cmd/fileshare/    # Main application entry point
├── internal/
│   ├── discovery/    # mDNS device discovery
│   ├── server/       # HTTP file server
│   ├── client/       # HTTP client for remote files
│   └── tui/          # Terminal user interface
└── go.mod
```

## Troubleshooting

**No devices found:**
- Ensure all devices are on the same network
- Check firewall settings allow mDNS (UDP 5353)
- Press 'r' to manually refresh the device list

**Cannot connect to device:**
- Verify the device is still running FileShare
- Check that the port is not blocked by a firewall
- The device may have disconnected (refresh the list)

**Download fails:**
- Ensure you have write permissions in the current directory
- Set `FILESHARE_DOWNLOAD_DIR` environment variable to change download location

## License

MIT
