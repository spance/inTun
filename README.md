# inTun

[English](README.md) | [简体中文](README_CN.md)

Interactive SSH Tunnel - A cross-platform SSH tunnel manager with a rich TUI interface, written in pure Go.

[![Go Version](https://img.shields.io/badge/go-1.21%2B-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## Preview

![inTun interactive SSH tunnel TUI preview](docs/assets/intun-preview.webp)

## Features

- **TCP and UDP Forwarding**: Local and Remote TCP/UDP forwarding, plus a Dynamic SOCKS5 TCP proxy (-D)
- **Pure Go SSH**: No external dependencies on ssh/plink, fully cross-platform
- **Real-time Monitoring**: Live upload/download statistics (TX/RX), transfer speeds, and latency
- **Auto-configuration**: Parses `~/.ssh/config` for host discovery
- **GroupLabels Support**: Parse `#!! GroupLabels` comments for host tagging and filtering
- **Interactive Host Key Verification**: Accept or reject unknown host keys with visual feedback
- **Password Authentication**: Interactive password and keyboard-interactive auth via TUI prompts
- **Connection Health**: SSH/TCP keepalive with automatic reconnection prompts on connection loss
- **Remote Tunnel LAN Targets**: Supports `ip:port` format for both local target and remote listen address
- **Integrated SFTP Manager**: Dual-panel local/remote browser with file sync, directory sync, rename, preview, and acknowledged transfer results
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
   - **Local TCP**: Forward a local TCP port to a remote service
   - **Local UDP**: Relay local UDP datagrams to a remote UDP service through SSH
   - **Remote TCP**: Forward a remote TCP port to a local service (supports LAN targets)
   - **Remote UDP**: Expose a remote UDP port and relay datagrams to a local UDP service
   - **Dynamic TCP**: Create a SOCKS5 TCP proxy
4. Enter port numbers as prompted

### Protocol Model

Each tunnel has one direction and one protocol. TCP and UDP tunnels are independent and can run at the same time. Separate TCP and UDP tunnels may use the same numeric port because the operating system maintains distinct TCP and UDP socket namespaces.

| Tunnel Type | Listener / Peer Side | Target Side |
|-------------|----------------------|-------------|
| Local TCP | Local TCP listener | Remote TCP service |
| Local UDP | Local UDP peers | Remote UDP service |
| Remote TCP | Remote TCP listener | Local TCP service |
| Remote UDP | Remote UDP peers | Local UDP service |
| Dynamic TCP | Local SOCKS5 listener | Remote TCP destinations |

UDP forwarding uses a framed datagram protocol over an SSH session. A compatible `intun` binary must be installed in `PATH` on the remote host. Because the SSH transport runs over TCP, it preserves datagram boundaries but is subject to TCP head-of-line blocking; it is intended for DNS, monitoring, and control traffic rather than latency-sensitive media, gaming, or QUIC.

Remote UDP ports entered without an address bind to `127.0.0.1`. An explicit non-loopback bind such as `0.0.0.0:5353` exposes the relay according to the remote account permissions and host firewall. The UDP listener is owned by the remote `intun` process, so OpenSSH `GatewayPorts` and `AllowTcpForwarding` do not control that socket.

Remote UDP behaves like a stateful relay: each remote source gets an isolated local UDP socket, but the local target sees inTun's local source address rather than the original remote peer address. Broadcast and multicast forwarding are not supported.

### Keyboard Shortcuts

Main screen:

| Key | Action |
|-----|--------|
| `c` | Create new tunnel |
| `r` | Reconnect failed tunnel |
| `s` | Stop/Start selected tunnel |
| `d` | Delete selected tunnel |
| `f` | Open SFTP file manager for a running tunnel |
| `↑↓` | Navigate tunnel list |
| `e` | Exit |

SFTP screen:

| Key | Action |
|-----|--------|
| `Tab` | Switch local/remote panel |
| `↑↓` / `PgUp` / `PgDn` | Navigate and scroll files |
| `Enter` | Open directory or parent |
| `s` | Sync selected file in the active panel's direction |
| `r` | Recursively sync selected directory after confirmation |
| `n` | Rename selected item |
| `v` | Preview selected file |
| `q` / `Esc` | Close preview or exit SFTP |
| `Enter` / `Esc` | Acknowledge transfer result notices |

### Requirements

- Go 1.21+ (for building)
- SSH key in `~/.ssh/` (id_rsa, id_ed25519, or id_ecdsa) or password auth
- SSH config file at `~/.ssh/config` (optional, for host discovery)
- SSH server with SFTP subsystem enabled (only required for the SFTP manager)
- Compatible `intun` binary in the remote `PATH` (required for UDP forwarding)

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
- **SFTP Library**: [github.com/pkg/sftp](https://github.com/pkg/sftp)
- **Styling**: [lipgloss](https://github.com/charmbracelet/lipgloss)
- **Forward Model**: One direction/protocol specification per tunnel, dispatched to independent TCP or UDP implementations
- **UDP Relay**: Versioned, length-delimited UDP frames multiplexed through an SSH session, with per-client associations and idle cleanup
- **UDP Roles**: `PeerRelay` owns the listener and peer associations; `TargetRelay` owns one target socket per association. Local and Remote modes swap which side runs each role
- **Statistics**: 1s interval sampling, 5s interval ping, TX/RX totals with ↑↓ speed indicators
- **TUI Safety**: Shared modal overlays, acknowledged SFTP transfer results, cancelable SFTP operations, and serialized SFTP client access

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

### Release hygiene

Before publishing a release, run the test suite and scan tracked files for sensitive material. Treat credentials, private keys, tokens, local filesystem paths, usernames, machine names, and private host/domain names as release blockers. Keep examples anonymized with placeholders such as `example.com`, `/path/to/project`, and `user`.

### Debugging

Set `INTUN_LOG` environment variable to enable SSH connection diagnostics:

```bash
INTUN_LOG=/tmp/intun.log ./intun
```

## License

MIT License - see [LICENSE](LICENSE) for details.
