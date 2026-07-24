# inTun - AI Context Reference

## Project Identity
- **Purpose**: Interactive SSH Tunnel manager with TUI interface, cross-platform, pure Go implementation
- **Language**: Go 1.25+
- **UI Framework**: Bubble Tea v2 + Bubbles v2 + Lip Gloss v2
- **SSH Implementation**: golang.org/x/crypto/ssh (no external ssh/ssh.exe dependencies)

## Architecture

```
cmd/intun/
  └── main.go                  # Entry point, Version variable injected via ldflags

internal/
  ├── config/
  │   └── config.go            # OpenSSH config resolver: Include, wildcard defaults, identities, ProxyJump, GroupLabels
  ├── platform/
  │   ├── platform.go          # Core interfaces and ForwardSpec direction/protocol model
  │   ├── platform_ssh.go      # SSHExecutor connection and ProxyJump orchestration
  │   ├── ssh_connection.go    # Thread-safe SSH runtime, health checks, stats, forwards, and SFTP access
  │   ├── forward_dispatch.go  # Forwarder selection for TCP and UDP implementations
  │   ├── forward.go           # Local/Remote/Dynamic TCP forwarding
  │   ├── socks5.go            # Stream-exact SOCKS5 parser with IPv4/domain/IPv6 support
  │   ├── auth.go              # Agent, private key/passphrase, password, and keyboard-interactive auth
  │   ├── agent_socket_*.go    # Unix socket / Windows named-pipe SSH agent adapters
  │   ├── udp_forward.go       # Local/Remote UDP composition and remote relay startup handshake
  │   ├── udp_forward_runtime.go # Shared UDP relay process lifecycle and failure propagation
  │   ├── udp_frame_transport.go # UDP payload accounting over the framed transport
  │   ├── bounded_buffer.go    # Bounded relay diagnostics captured from remote sessions
  │   ├── known_hosts.go       # Host key verification, VerifyHostKey() with host:port format
  │   └── counted_conn.go      # Traffic counting wrapper for net.Conn
  ├── testutil/
  │   └── platform.go          # MockConnection + MockExecutor shared only by tests
  ├── tunnel/
  │   ├── tunnel.go            # Domain model and immutable snapshots
  │   ├── manager.go           # CRUD, executor orchestration, and SFTP access
  │   └── runtime.go           # Generation-safe lifecycle and cumulative runtime statistics
  ├── monitor/
  │   └── monitor.go           # Stats polling, ping every 5 ticks (1s interval), synchronous updates
  ├── sftp/
  │   ├── client.go            # Context-aware serialized SFTP operation lifecycle
  │   ├── atomic_transfer.go   # Temporary-file writes and atomic local/remote replacement
  │   └── sync_plan.go         # One-pass immutable recursive synchronization plan
  ├── udprelay/
  │   ├── protocol.go          # Versioned, length-delimited UDP frame codec
  │   ├── transport.go         # Concurrent-safe framed stream transport abstraction
  │   ├── registry.go          # Bounded local peer/association mapping
  │   ├── peer.go              # UDP listener role and peer/association routing
  │   ├── target.go            # Fixed-target role, per-association sockets, limits, idle cleanup
  │   ├── server.go            # Relay options and stream-oriented target entry point
  │   └── command.go           # Hidden intun relay command used over SSH
  └── tui/
      ├── tui.go / state.go    # Root model composed from domain-specific state groups
      ├── components.go        # Bubbles v2 text input, spinner, viewport, and help components
      ├── host_select.go       # Searchable SSH hosts and manual destination input
      ├── modal.go             # Reusable full-screen modal overlay
      ├── tunnel_create_confirm.go # Remote UDP exposure confirmation
      ├── sftp_actions.go      # SFTP key handling and interaction state
      ├── sftp_commands.go     # SFTP session and navigation Tea commands
      ├── sftp_file_commands.go # Preview, rename, and refresh Tea commands
      ├── sftp_sync_commands.go # Preflight, planning, and transfer-result orchestration
      ├── host_view.go         # Host selector and manual destination rendering
      ├── tunnel_form_view.go  # Tunnel type and address form rendering
      ├── overlay_view.go      # Authentication, confirmation, and status overlays
      └── sftp_render.go       # SFTP dual-panel rendering, progress, preview
```

## Key Implementation Details

### Forward Model
- Each Tunnel owns exactly one ForwardSpec with one direction and one protocol: TCP or UDP, never both
- TCP and UDP tunnels are independent and can run concurrently, including on the same numeric port because their socket namespaces are separate
- Local/Remote describe where the listener is created; the opposite side is the target
- A future combined TCP+UDP UX should group two child ForwardSpecs rather than add a `ProtocolBoth` branch throughout the forwarding stack

### SSH Connection Flow
1. Manager.Create()/CreateWithProtocol() builds a ForwardSpec -> executor.Connect()
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
- **SSHConnection**: mu protects client, forwards, exited, lastError; addForward() rejects and closes late forwards after Stop
- **Tunnel**: private mutable runtime state; external consumers receive immutable `Snapshot` values
- **Manager**: generation IDs and per-run contexts reject late Connect results after Stop/Delete/Restart; Delete is terminal even for stale references
- **Monitor**: synchronous updateTunnelStats() (no goroutine per tunnel)
- **KnownHosts**: RWMutex on callback access
- **Auth requests**: channel-based with queue in TUI
- **SFTP Client**: mu serializes operations and Close(); every file operation accepts a context
- **SFTP ProgressInfo**: RWMutex protects transfer progress; TUI reads via Snapshot()
- **SFTP commands**: all TUI disk/network I/O runs in Tea commands; operation and transfer IDs discard stale results
- **SFTP exit**: cancellation is immediate and client close runs asynchronously

### Statistics
- Interval: 1 second (monitor tick)
- Ping frequency: every 5 ticks (5 seconds)
- Speed: ↑/↓ prefix, TX/RX for totals
- Format units: KB, MB only (no B)

### Authentication
- Supports multiple `IdentityFile` entries, encrypted keys, `IdentityAgent`, and `IdentitiesOnly`
- SSH agent uses Unix sockets or Windows OpenSSH named pipes
- Supports `Include`, wildcard defaults, and comma-separated ProxyJump chains
- Host key verification with interactive prompt
- Password and keyboard-interactive auth support with TUI prompts

### UI Layout
- Long tunnel lists use a Bubbles viewport and remain selection-aware
- SFTP uses dual panels on wide terminals and the focused panel on compact terminals
- Selected row: white text, badge rendered separately (no ANSI nesting)
- Status badges: background colors (Running=green, Stopped=gray, Error=red, Connecting=yellow)
- Speed line: left-aligned, 4-space indent, matches IP column above
- Use `lipgloss.Width()` for ANSI-aware width calculations
- Modal dialogs use `ModalSpec` and a full-screen mask that only renders the centered modal rectangle
- SFTP has no separate tab row; focus is shown by a highlighted Local/Remote panel title badge

### SOCKS5 (Dynamic forward)
- Supports no-auth only (method 0x00)
- Address types: IPv4 (0x01), domain (0x03), and IPv6 (0x04)
- Proper SOCKS5 error replies for unsupported commands/address types

### UDP over SSH
- Local and Remote UDP preserve datagram boundaries with a versioned frame protocol over an SSH session/TCP transport
- PeerRelay owns a UDP listener and peer/association map; TargetRelay owns one connected target socket per association
- Local UDP composes local PeerRelay + remote TargetRelay; Remote UDP composes remote PeerRelay + local TargetRelay
- Remote commands use `intun relay udp <target|listen> --address-token ...`; a compatible `intun` must be available in the remote `PATH`
- Both sides cap associations at 1024
- Associations expire after 60 seconds; stale remote close frames cannot remove recently active local mappings
- Relay readiness is part of tunnel startup, so status remains Connecting until the remote relay acknowledges Ready
- UDP payload bytes feed the existing TX/RX counters; protocol framing bytes are excluded
- Remote UDP listeners are sockets owned by the remote `intun` process; OpenSSH `GatewayPorts` and `AllowTcpForwarding` do not govern them
- Remote UDP targets see inTun's relay source address rather than the original remote peer; broadcast and multicast are not supported
- UDP-over-TCP is not suitable for latency-sensitive media, gaming, or QUIC because of TCP head-of-line blocking

### SFTP File Manager
- Entry: press `f` on Running tunnel, reuses SSH connection via SFTPCapable interface
- Dual-panel: left=Local, right=Remote, Tab switches focus
- Navigation: arrows and PgUp/PgDn scroll long lists; Enter opens directory or parent
- Operations: Sync selected file (`s`), recursive directory sync (`r` with confirm modal), rename (`n`), preview (`v`)
- Sync direction follows the active panel: Local uploads to Remote, Remote downloads to Local
- Recursive sync executes the immutable preflight plan, skips links/non-regular or unreadable entries, and only overwrites the exact paths the user approved
- File commits use temporary files and no-replace installation; newly appeared destinations are never overwritten without a fresh confirmation
- Rename accepts basename-only names and rejects path separators, `.` and `..`
- Preview reads the first 4KB of a file
- Transfer progress is shown in a drawer; transfer results stay in a shared modal until the user confirms with Enter/Esc
- Exiting SFTP with `q`/Esc cancels active work, invalidates pending results, and closes the client asynchronously

### Cross-platform
- Username lookup: os/user.Current() with USER/LOGNAME fallback
- Forward input accepts plain ports, hostnames, IPv4, and bracketed IPv6
- Window resize: 500ms polling via golang.org/x/term.GetSize()

## Build Commands
- `make build` - Current platform
- `make all` - All architectures
- `make install` - Build and install to /usr/local/bin
- Version: `git describe --tags` injected via `-ldflags "-X main.Version=$(VERSION)"`

## Testing
- Tests cover config, monitor, sftp, tui, and tunnel packages
- Mock infrastructure is isolated in internal/testutil and is not linked into the application
- `make check` runs formatting, vet, Staticcheck, race tests, a 75% total coverage gate, govulncheck, and Gitleaks scans of both Git history and the current worktree
- CI also runs protocol fuzz smoke tests and five cross-platform builds

## Testing Checklist
- [ ] Host key prompt shows correct hostname (not empty)
- [ ] Accepting unknown host key adds entry to known_hosts
- [ ] Statistics display correctly with KB/MB units, TX/RX labels
- [ ] Window resize updates layout on Windows Terminal
- [ ] Connection lost shows SSH_CONNECTION_LOST message
- [ ] Reconnect (r) works after connection failure
- [ ] Remote tunnel accepts ip:port format for both local target and remote listen
- [ ] Local UDP stays Connecting until remote relay Ready, then relays multiple local clients without mixing replies
- [ ] Remote UDP returns its bound address in Ready, isolates remote peers, and releases the remote socket on Stop
- [ ] Missing or incompatible remote `intun` produces a clear UDP_RELAY_FAILED hint
- [ ] SFTP long file lists scroll with arrows and PgUp/PgDn
- [ ] SFTP sync, recursive sync, rename, and preview work from both panels
- [ ] SFTP rename rejects path separators and parent/current directory names
- [ ] SFTP exit during transfer cancels the operation and returns to the main screen
- [ ] Global modal overlays show only the centered dialog on a full-screen mask
- [ ] Release scan has no credentials, private keys, tokens, local filesystem paths, usernames, machine names, or private host/domain names

## Known Limitations
- SOCKS5 dynamic proxy: no-auth only
- Single auth request at a time (queue-based)
- Host key format in known_hosts must be "host:port" for proper matching
- Remote TCP listen requires `GatewayPorts yes` on the server for non-loopback addresses; Remote UDP has separate process-owned listener semantics described above
- One tunnel owns one protocol; offering TCP and UDP on the same numeric port requires two tunnels
- SFTP preview is intentionally capped at the first 4KB

## Debugging
- Set `INTUN_LOG` env var to a file path for SSH connection diagnostics
- Logs use mode 0600, rotate at 5 MiB, retain one backup, and rate-limit repeated UDP failures
- Without INTUN_LOG, all log output is discarded
