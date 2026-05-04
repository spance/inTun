# inTun

[English](README.md) | [简体中文](README_CN.md)

Interactive SSH Tunnel - A cross-platform SSH tunnel manager with a rich TUI interface, written in pure Go.

[![Go Version](https://img.shields.io/badge/go-1.21%2B-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## Features

- **Three Tunnel Modes**: Local port forwarding (-L), Remote port forwarding (-R), Dynamic SOCKS proxy (-D)
- **Pure Go SSH**: No external dependencies on ssh/plink, fully cross-platform
- **Real-time Monitoring**: Live upload/download statistics (TX/RX), transfer speeds, and latency
- **Auto-configuration**: Parses `~/.ssh/config` for host discovery
- **GroupLabels Support**: Parse `#!! GroupLabels` comments for host tagging and filtering
- **Interactive Host Key Verification**: Accept or reject unknown host keys with visual feedback
- **Password Authentication**: Interactive password and keyboard-interactive auth via TUI prompts
- **Connection Health**: SSH/TCP keepalive with automatic reconnection prompts on connection loss
- **Remote Tunnel LAN Targets**: Supports `ip:port` format for both local target and remote listen address
- **Keyboard-driven Interface**: Efficient navigation and control via shortcuts

## Installation

### From Source

```bash
git clone https://github.com/spance/intun.git
cd intun
make build

# Or cross-compile for all platforms
make all
```

### Install to System

```bash
make install    # Builds and copies to /usr/local/bin/
```

### Prebuilt Binaries

Download the latest release from the [releases page](https://github.com/spance/intun/releases).

## Usage

Launch intun:

```bash
./intun
```

### Creating Tunnels

1. Press `c` to create a new tunnel
2. Select a host from your `~/.ssh/config`
3. Choose tunnel type:
   - **Local**: Forward local port to remote service
   - **Remote**: Forward remote port to local service (supports LAN targets)
   - **Dynamic**: Create a SOCKS proxy
4. Enter port numbers as prompted

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `c` | Create new tunnel |
| `r` | Reconnect failed tunnel |
| `s` | Stop/Start selected tunnel |
| `d` | Delete selected tunnel |
| `↑↓` | Navigate tunnel list |
| `e` | Exit |

### Requirements

- Go 1.21+ (for building)
- SSH key in `~/.ssh/` (id_rsa, id_ed25519, or id_ecdsa) or password auth
- SSH config file at `~/.ssh/config` (optional, for host discovery)

## Configuration

intun reads your existing `~/.ssh/config`:

```ssh
Host myserver
    Hostname example.com
    User root
    Port 2222
    IdentityFile ~/.ssh/my_key
    #!! GroupLabels production web
```

Supported fields:
- `Host` - Alias
- `Hostname` - Actual host address
- `User` - Username
- `Port` - Port (default 22)
- `IdentityFile` - Private key path
- `#!! GroupLabels` - Tags for filtering (displayed as gold labels)

## Technical Details

- **UI Framework**: [bubbletea](https://github.com/charmbracelet/bubbletea) (Charm TUI)
- **SSH Library**: [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh)
- **Styling**: [lipgloss](https://github.com/charmbracelet/lipgloss)
- **Statistics**: 1s interval sampling, 5s interval ping, TX/RX totals with ↑↓ speed indicators

## Development

```bash
# Build for current platform
make build

# Run tests
make test

# Run with version injection
VERSION=$(git describe --tags)
go build -ldflags "-X main.Version=$VERSION" ./cmd/intun

# Cross-compile
make all    # All architectures
```

### Debugging

Set `INTUN_LOG` environment variable to enable SSH connection diagnostics:

```bash
INTUN_LOG=/tmp/intun.log ./intun
```

## License

MIT License - see [LICENSE](LICENSE) for details.
