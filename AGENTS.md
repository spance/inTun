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
  │   ├── platform.go          # Core interfaces: Connection, SFTPCapable, Executor, AuthContext
  │   ├── platform_ssh.go      # SSHExecutor.connect() - SSH handshake, tunnel creation, keepalive, NewSFTPClient
  │   ├── mock.go              # MockConnection + MockExecutor for testing
  │   ├── known_hosts.go       # Host key verification, VerifyHostKey() with host:port format
  │   └── counted_conn.go      # Traffic counting wrapper for net.Conn
  ├── tunnel/
  │   └── tunnel.go            # Manager CRUD, Tunnel struct with stats, thread-safe setStatus, GetSFTPClient
  ├── monitor/
  │   └── monitor.go           # Stats polling, ping every 5 ticks (1s interval), synchronous updates
  ├── sftp/
  │   └── client.go            # Context-aware SFTP wrapper: read, sync, preview, rename, remote path helpers
  └── tui/
      ├── tui.go               # Bubbletea model, auth prompt queue, main screens, shared styles
      ├── modal.go             # Reusable full-screen modal overlay
      ├── sftp_actions.go      # SFTP key handling, context/cancel, transfer orchestration
      └── sftp_render.go       # SFTP dual-panel rendering, progress, preview
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
- **SFTP Client**: mu serializes operations and Close(); every file operation accepts a context
- **SFTP ProgressInfo**: RWMutex protects transfer progress; TUI reads via Snapshot()
- **SFTP transfers**: started with cancelable context; stale done messages are ignored; exiting SFTP cancels transfer and synchronously closes the client

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
- Modal dialogs use `ModalSpec` and a full-screen mask that only renders the centered modal rectangle
- SFTP has no separate tab row; focus is shown by a highlighted Local/Remote panel title badge

### SOCKS5 (Dynamic forward)
- Supports no-auth only (method 0x00)
- Address types: IPv4 (0x01) and domain (0x03)
- Proper SOCKS5 error replies for unsupported commands/address types

### SFTP File Manager
- Entry: press `f` on Running tunnel, reuses SSH connection via SFTPCapable interface
- Dual-panel: left=Local, right=Remote, Tab switches focus
- Navigation: arrows and PgUp/PgDn scroll long lists; Enter opens directory or parent
- Operations: Sync selected file (`s`), recursive directory sync (`r` with confirm modal), rename (`n`), preview (`v`)
- Sync direction follows the active panel: Local uploads to Remote, Remote downloads to Local
- Rename accepts basename-only names and rejects path separators, `.` and `..`
- Preview reads the first 4KB of a file
- Transfer progress is shown in a drawer; transfer results stay in a shared modal until the user confirms with Enter/Esc
- Exiting SFTP with `q`/Esc cancels active transfer and synchronously closes the SFTP client

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
- Tests cover config, monitor, sftp, tui, and tunnel packages
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
- [ ] SFTP long file lists scroll with arrows and PgUp/PgDn
- [ ] SFTP sync, recursive sync, rename, and preview work from both panels
- [ ] SFTP rename rejects path separators and parent/current directory names
- [ ] SFTP exit during transfer cancels the operation and returns to the main screen
- [ ] Global modal overlays show only the centered dialog on a full-screen mask
- [ ] Release scan has no credentials, private keys, tokens, local filesystem paths, usernames, machine names, or private host/domain names

## Known Limitations
- SOCKS5 dynamic proxy: no-auth only, no IPv6 address type
- Single auth request at a time (queue-based)
- Host key format in known_hosts must be "host:port" for proper matching
- Remote tunnel listen requires GatewayPorts yes on server for non-localhost
- SFTP preview is intentionally capped at the first 4KB
- SFTP exit may block briefly while cancel/close completes

## Debugging
- Set `INTUN_LOG` env var to a file path for SSH connection diagnostics
- Without INTUN_LOG, all log output is discarded
