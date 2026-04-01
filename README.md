# inTun

[English](README.md) | [简体中文](README_CN.md)

Interactive SSH Tunnel - A cross-platform SSH tunnel manager with a rich TUI interface, written in pure Go.

[![Go Version](https://img.shields.io/badge/go-1.21%2B-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## Features

- **Three Tunnel Modes**: Local port forwarding (-L), Remote port forwarding (-R), Dynamic SOCKS proxy (-D)
- **Pure Go SSH**: No external dependencies on ssh/plink, fully cross-platform
- **Real-time Monitoring**: Live upload/download statistics, transfer speeds, and latency
- **Auto-configuration**: Parses `~/.ssh/config` for host discovery
- **Interactive Host Key Verification**: Accept or reject unknown host keys with visual feedback
- **Connection Health**: Automatic ping-based connection monitoring with automatic reconnection prompts
- **Keyboard-driven Interface**: Efficient navigation and control via shortcuts

## Installation

### From Source

```bash
git clone https://github.com/spance/intun.git
cd intun
make build

# Or cross-compile for all platforms
make build-all
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
   - **Remote**: Forward remote port to local service
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
- SSH private key in `~/.ssh/` (id_rsa, id_ed25519, or id_ecdsa)
- SSH config file at `~/.ssh/config` (optional, for host discovery)

## Configuration

intun reads your existing `~/.ssh/config`:

```ssh
Host myserver
    Hostname example.com
    User root
    Port 2222
    IdentityFile ~/.ssh/my_key
```

## Technical Details

- **UI Framework**: [bubbletea](https://github.com/charmbracelet/bubbletea) (Charm TUI)
- **SSH Library**: [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh)
- **Styling**: [lipgloss](https://github.com/charmbracelet/lipgloss)

## Development

```bash
# Build for current platform
go build ./cmd/intun

# Run with version injection
VERSION=$(git describe --tags)
go build -ldflags "-X main.Version=$VERSION" ./cmd/intun

# Cross-compile
make build-all  # All architectures
```

## License

MIT License - see [LICENSE](LICENSE) for details.
