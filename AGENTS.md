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
  │   └── config.go            # ~/.ssh/config parser, returns []Host
  ├── platform/
  │   ├── platform.go          # Core interfaces: Connection, Executor, AuthContext
  │   ├── platform_ssh.go      # SSHExecutor.connect() - SSH handshake, tunnel creation
  │   ├── known_hosts.go       # Host key verification, VerifyHostKey() with host:port format
  │   └── counted_conn.go      # Traffic counting wrapper for net.Conn
  ├── tunnel/
  │   └── tunnel.go            # Manager CRUD, Tunnel struct with stats
  ├── monitor/
  │   └── monitor.go           # Stats polling, ping every 5 ticks (1s interval)
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

### Critical Bug Fixes History
- **Width calculation**: Use `lipgloss.Width()` for ANSI-aware width, not `len()` or `utf8.RuneCountInString()`
- **Windows Terminal resize**: Add 500ms polling via `golang.org/x/term.GetSize()`
- **Host key verification**: MUST pass "host:port" to knownhosts callback, not just hostname

### Statistics
- Interval: 1 second
- Ping frequency: every 5 ticks (5 seconds)
- Connection lost detection: 3 consecutive ping failures
- Format units: KB, MB only (no B)

### Authentication
- Auto-loads: ~/.ssh/id_rsa, ~/.ssh/id_ed25519, ~/.ssh/id_ecdsa
- Supports SSH config IdentityFile
- Host key verification with interactive prompt
- Password and keyboard-interactive auth support with TUI prompts

### Thread Safety
- KnownHosts: RWMutex on callback access
- Tunnel: RWMutex on status/error access
- Manager: RWMutex on tunnel list
- Auth requests: channel-based with queue in TUI

## Build Commands
- `make build` - Current platform
- `make build-all` - All architectures
- Version: `git describe --tags` injected via `-ldflags "-X main.Version=$(VERSION)"`

## Testing Checklist
- [ ] Host key prompt shows correct hostname (not empty)
- [ ] Accepting unknown host key adds entry to known_hosts
- [ ] Statistics display correctly with KB/MB units
- [ ] Window resize updates layout on Windows Terminal
- [ ] Connection lost after 3 failed pings
- [ ] Reconnect (r) works after connection failure

## Known Limitations
- Only key-based SSH auth (no password prompt)
- Single auth request at a time (queue-based)
- Host key format in known_hosts must be "host:port" for proper matching
