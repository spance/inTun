# inTun - AI Context Reference

## Project Identity
- **Purpose**: Interactive SSH Tunnel manager with TUI interface, cross-platform, pure Go implementation
- **Language**: Go 1.21+
- **UI Framework**: bubbletea (Charm TUI framework) + lipgloss for styling
- **SSH Implementation**: golang.org/x/crypto/ssh (no external ssh/ssh.exe dependencies)

## Architecture

```
cmd/intun/
  └── main.go                  # Entry point, Version variable injected via ldflags

internal/
  ├── config/
  │   └── config.go            # ~/.ssh/config parser, returns []Host; supports #!! GroupLabels
  ├── platform/
  │   ├── platform.go          # Core interfaces: Connection, Executor, AuthContext
  │   ├── platform_ssh.go      # SSHExecutor.connect() - SSH handshake, tunnel creation, keepalive
  │   ├── mock.go              # MockConnection + MockExecutor for testing
  │   ├── known_hosts.go       # Host key verification, VerifyHostKey() with host:port format
  │   └── counted_conn.go      # Traffic counting wrapper for net.Conn
  ├── tunnel/
  │   └── tunnel.go            # Manager CRUD, Tunnel struct with stats, thread-safe setStatus
  ├── monitor/
  │   └── monitor.go           # Stats polling, ping every 5 ticks (1s interval), synchronous updates
  └── tui/
      └── tui.go               # Bubbletea model, auth prompt queue, all UI rendering
```

## Key Implementation Details

### SSH Connection Flow
1. Manager.Create() -> executor.Connect()
2. SSHExecutor.connect() parses SSH config, loads identity files
3. HostKeyCallback wraps VerifyHostKey() with original host:port
4. VerifyHostKey() uses knownhosts callback with "host:port" format (required!)
5. On unknown host: handleUnknownHost() sends AuthRequest to TUI via channel
6. TUI prompts user, response sent back via Response channel
7. On accept: Add() appends to ~/.ssh/known_hosts, reloads callback

### Connection Health
- TCP keepalive: 30s interval via net.Dialer.KeepAlive
- SSH keepalive: goroutine sends keepalive@openssh.org every 10s
- Connection loss: detected via keepalive failure + client.Wait() error
- No ping-fail-count based detection (removed due to false positives)

### Thread Safety
- **SSHConnection**: mu protects client, forwards, exited, lastError; addForward() helper for safe append
- **Tunnel**: mu protects Status, Error, stats; use setStatus() setter (never write directly); GetSnapshot() for atomic reads
- **Manager**: mu protects Tunnels list; Restart() releases mu during sleep to avoid blocking
- **Monitor**: synchronous updateTunnelStats() (no goroutine per tunnel)
- **KnownHosts**: RWMutex on callback access
- **Auth requests**: channel-based with queue in TUI

### Statistics
- Interval: 1 second (monitor tick)
- Ping frequency: every 5 ticks (5 seconds)
- Speed: ↑/↓ prefix, TX/RX for totals
- Format units: KB, MB only (no B)

### Authentication
- Auto-loads: ~/.ssh/id_rsa, ~/.ssh/id_ed25519, ~/.ssh/id_ecdsa
- Supports SSH config IdentityFile
- Host key verification with interactive prompt
- Password and keyboard-interactive auth support with TUI prompts

### UI Layout
- Column widths defined as constants (colIDW, colStatusW, colTypeW, colAddrW, colLatencyW)
- Selected row: white text, badge rendered separately (no ANSI nesting)
- Status badges: background colors (Running=green, Stopped=gray, Error=red, Connecting=yellow)
- Speed line: left-aligned, 4-space indent, matches IP column above
- Use `lipgloss.Width()` for ANSI-aware width calculations

### SOCKS5 (Dynamic forward)
- Supports no-auth only (method 0x00)
- Address types: IPv4 (0x01) and domain (0x03)
- Proper SOCKS5 error replies for unsupported commands/address types

### Cross-platform
- Username lookup: os/user.Current() with USER/LOGNAME fallback
- Port input for Remote tunnels: accepts ip:port or plain port (auto-prefixes 127.0.0.1)
- Window resize: 500ms polling via golang.org/x/term.GetSize()

## Build Commands
- `make build` - Current platform
- `make all` - All architectures
- `make install` - Build and install to /usr/local/bin
- Version: `git describe --tags` injected via `-ldflags "-X main.Version=$(VERSION)"`

## Testing
- 58 tests across config, monitor, tui, tunnel packages
- Mock infrastructure in platform/mock.go
- `make test` / `make vet`

## Testing Checklist
- [ ] Host key prompt shows correct hostname (not empty)
- [ ] Accepting unknown host key adds entry to known_hosts
- [ ] Statistics display correctly with KB/MB units, TX/RX labels
- [ ] Window resize updates layout on Windows Terminal
- [ ] Connection lost shows SSH_CONNECTION_LOST message
- [ ] Reconnect (r) works after connection failure
- [ ] Remote tunnel accepts ip:port format for both local target and remote listen

## Known Limitations
- SOCKS5 dynamic proxy: no-auth only, no IPv6 address type
- Single auth request at a time (queue-based)
- Host key format in known_hosts must be "host:port" for proper matching
- Remote tunnel listen requires GatewayPorts yes on server for non-localhost

## Debugging
- Set `INTUN_LOG` env var to a file path for SSH connection diagnostics
- Without INTUN_LOG, all log output is discarded
