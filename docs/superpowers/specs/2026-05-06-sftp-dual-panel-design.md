# inTun SFTP - Dual-Panel File Manager Design

## Overview

Add an integrated SFTP file manager to inTun. Users select a running tunnel and press `f` to open a dual-panel file browser (local left, remote right) for upload, download, recursive directory sync, and quick file preview — all over the tunnel's existing SSH connection.

## Architecture

### New Files

```
internal/sftp/client.go    # SFTPClient wrapper: dir read, upload, download, recursive sync, preview
```

### Modified Files

| File | Change |
|------|--------|
| `platform/platform.go` | Add `SFTPCapable` interface |
| `platform/platform_ssh.go` | `SSHConnection.NewSFTPClient()` implementation |
| `tunnel/tunnel.go` | `Manager.GetSFTPClient(id)` method |
| `tui/tui.go` | New `ScreenSFTP`, dual-panel rendering, key bindings, transfer progress |

### New Dependency

- `github.com/pkg/sftp`

### Data Flow

```
User presses f on Running tunnel
  → Manager.GetSFTPClient(id)
    → Type-assert tunnel.Conn to SFTPCapable
      → SSHConnection.NewSFTPClient()
        → sftp.NewClient(sshClient)   // reuses existing SSH connection
  → Initialize dual-panel state (local cwd, remote cwd, file lists)
  → Switch to ScreenSFTP
```

## Platform Layer

### SFTPCapable Interface

Defined in `platform.go`, separate from `Connection` to keep it optional:

```go
type SFTPCapable interface {
    NewSFTPClient() (*sftp.Client, error)
}
```

TUI accesses it via type assertion:

```go
if sc, ok := tunnel.Conn.(platform.SFTPCapable); ok {
    client, _ := sc.NewSFTPClient()
}
```

### SSHConnection Extension

In `platform_ssh.go`, follows existing RLock pattern:

```go
func (c *SSHConnection) NewSFTPClient() (*sftp.Client, error) {
    c.mu.RLock()
    client := c.client
    exited := c.exited
    c.mu.RUnlock()
    if exited || client == nil {
        return nil, fmt.Errorf("SSH_CONNECTION_LOST: connection not available")
    }
    return sftp.NewClient(client)
}
```

When `SSHConnection.Stop()` closes the underlying `*ssh.Client`, the SFTP session closes with it.

## SFTP Client Wrapper

`internal/sftp/client.go` isolates TUI from `github.com/pkg/sftp`:

### Types

```go
type FileEntry struct {
    Name    string
    Size    int64
    Mode    fs.FileMode
    ModTime time.Time
    IsDir   bool
}

type ProgressInfo struct {
    Done      int64   // bytes transferred
    Total     int64   // total bytes
    File      string  // current filename
    FileIndex int     // current file number (for recursive)
    FileCount int     // total file count (for recursive)
    Speed     int64   // bytes/sec
    Active    bool    // transfer in progress
}
```

### Methods

```go
func NewSFTPClient(c *sftp.Client) *SFTPClient
func (s *SFTPClient) Close() error

// Directory browsing
func (s *SFTPClient) ReadDir(path string) ([]FileEntry, error)

// Single file transfer
func (s *SFTPClient) Download(remotePath, localPath string, progress func(int64)) error
func (s *SFTPClient) Upload(localPath, remotePath string, progress func(int64)) error

// Recursive directory sync
func (s *SFTPClient) DownloadDir(remoteDir, localDir string, progress func(done, total int64, file string)) error
func (s *SFTPClient) UploadDir(localDir, remoteDir string, progress func(done, total int64, file string)) error

// Quick preview (first 4KB of text)
func (s *SFTPClient) Preview(path string) (string, error)

// File management
func (s *SFTPClient) Mkdir(path string) error
func (s *SFTPClient) Remove(path string) error
```

### Design Notes

- `SFTPClient.mu` serializes all operations — `sftp.Client` is not concurrency-safe
- Recursive sync: first pass counts total files/bytes (`filepath.Walk` for local, `sftp.Walk` for remote), second pass transfers and calls progress for each file
- `progress` callbacks allow TUI to drive status updates without the transfer logic depending on bubbletea

## Manager Extension

New method in `tunnel/tunnel.go`:

```go
func (m *Manager) GetSFTPClient(id int) (*sftp.Client, error)
```

- Acquires `m.mu.RLock()`
- Finds tunnel by ID
- Verifies `t.Status == StatusRunning`
- Type-asserts `t.Conn` to `SFTPCapable`
- Calls `NewSFTPClient()`
- Returns the wrapper

## TUI Design

### New Screen State

```go
ScreenSFTP  // added to Screen enum
```

### Model Fields

```go
sftpClient      *sftp.Client
sftpLocalDir    string
sftpRemoteDir   string
sftpLocalFiles  []sftp.FileEntry
sftpRemoteFiles []sftp.FileEntry
sftpFocus       int           // 0=left (Local), 1=right (Remote)
sftpCursor      [2]int        // cursor per panel
sftpScroll      [2]int        // scroll offset per panel
sftpTransferring bool
sftpProgress    sftp.ProgressInfo
sftpPreview     string
sftpPreviewing  bool
sftpCancel      context.CancelFunc  // cancel active transfer
```

### Layout

```
┌──────────── inTun SFTP ─ user@host:22 ────────────────────┐
│                                                             │
│  Local: /home/user/downloads    Remote: /var/www/html      │
│  ────────────────────────────    ─────────────────────────  │
│  ..                              ..                         │
│  ▸ config/                       logs/                      │
│    readme.txt                    index.html                 │
│    data.json                     app.js                     │
│                                                             │
│                                                             │
│  ↓ app.js  45% [████░░░░░░] 1.2MB/s  3/12                 │
│  [Tab]Switch [↑↓]Nav [Enter]Open [u]Upload [d]Download ... │
└─────────────────────────────────────────────────────────────┘
```

- Each panel occupies `(width / 2 - 2)` characters with a gap between them
- Focused panel has highlighted header, non-focused panel is dimmed
- Directories displayed with trailing `/`, sorted: `..` first, then dirs, then files
- Cursor item rendered with `▸` prefix and selected style

### Key Bindings

| Key | Action | Condition |
|-----|--------|-----------|
| `Tab` | Switch focus panel | Always |
| `↑/k` | Move cursor up | Not transferring |
| `↓/j` | Move cursor down | Not transferring |
| `Enter` | Enter directory / go up (`..`) | Not transferring |
| `u` | Upload selected item to opposite panel's directory | File or dir selected, not transferring |
| `d` | Download selected item to opposite panel's directory | File or dir selected, not transferring |
| `r` | Recursive sync selected directory | Dir selected, not transferring |
| `v` | Preview file content (first 4KB) | File selected, not transferring |
| `Esc/q` | Close preview / Exit SFTP screen | Always |

### Shortcut Bar

Rendered identically to main screen's `renderShortcuts()` — `keyStyle` for keys, `shortcutStyle` for descriptions, spaced across full width. Content changes by state:

- **Browsing**: `[Tab]Switch  [↑↓]Nav  [Enter]Open  [u]Upload  [d]Download  [r]Recurse  [v]View  [q]Back`
- **Transferring**: `[Tab]Switch  [↑↓]Nav  [Enter]Open  ···传输中···  [q]Back`
- **Preview**: `[Esc/q]关闭预览`

### Transfer Progress (Bottom Status Line)

Shown above the shortcut bar during active transfers:

```
↓ filename.txt  67% [████████░░░░░] 2.1MB/s  5/12
```

- `↓` = download, `↑` = upload, `↕` = recursive sync
- Percentage bar uses block characters
- Speed formatted same as tunnel stats (KB/MB)
- File count shown for recursive transfers

Progress updates via channel: transfer goroutine sends `sftpProgressMsg`, TUI consumes in `Update()` and re-renders.

### Transfer Mechanics

1. User presses `u`/`d`/`r` → spawn goroutine with context
2. Goroutine calls `SFTPClient` method with progress callback
3. Progress callback sends `sftpProgressMsg` to TUI via channel
4. TUI renders progress in bottom status line on each `tickMsg`
5. On completion: goroutine sends `sftpDoneMsg`, TUI refreshes both file lists
6. On cancel (user presses `q`): context cancel → goroutine exits → cleanup

### Entry/Exit Flow

**Entry** (`f` key on Running tunnel):
1. `handleMainKeys` receives `f`
2. Call `manager.GetSFTPClient(selectedTunnel.ID)`
3. Set `sftpLocalDir` to `os.Getwd()`, `sftpRemoteDir` to `~/`
4. Load both directory listings
5. Switch to `ScreenSFTP`

**Exit** (`q` or `Esc`):
1. Cancel active transfer if any, wait for goroutine
2. Call `sftpClient.Close()`
3. Switch to `ScreenMain`

**Tunnel disconnection while in SFTP**:
1. `Monitor.CheckStatus()` detects connection loss
2. TUI checks: if `screen == ScreenSFTP`, auto-close SFTP and return to main screen
3. Show error message on the tunnel row as usual

## Thread Safety

| Resource | Protection |
|----------|-----------|
| `sftp.Client` operations | `SFTPClient.mu` — serialized |
| Active transfer goroutine | `context.CancelFunc` for cancellation |
| `sftpProgress` state | Written by goroutine via channel, read by TUI in Update() |
| Panel file lists | Only modified in TUI Update() — single-threaded by bubbletea |

## Directory Listing Format

Each entry rendered as:
```
  ▸ dirname/              (cursor, focused panel, directory)
    filename.txt    12KB  (regular file with size)
    ..                     (parent directory, always first)
```

- Directories: name + `/`, no size
- Files: name + size (KB/MB, same format as tunnel stats)
- Symlinks: name + `@`
- Hidden files (starting with `.`): shown, including `..`

## Edge Cases

| Case | Behavior |
|------|----------|
| Permission denied on ReadDir | Show error in panel, keep current directory |
| Target file exists on upload/download | Overwrite silently |
| Empty directory | Show only `..` |
| Very large directory (>1000 entries) | Paginate or limit display with scroll |
| Binary file preview | Show "[binary file]" instead of content |
| Transfer of file with special chars | Pass paths as-is, sftp library handles escaping |
| SSH connection lost during transfer | Context cancel triggers, TUI shows SSH_CONNECTION_LOST |
| User presses `f` on non-Running tunnel | Ignore, only Running tunnels eligible |

## Future Considerations (Not in MVP)

- Multi-select (space to mark, batch transfer)
- Create new directory (`n`)
- Delete file/dir (`x`)
- Rename (`e`)
- Permission display (ls -la style)
- Sort options (name, size, date)
- Bookmark directories
- Drag-and-drop style (copy from one panel to other with visual feedback)
