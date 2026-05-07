# Action Feedback & UX Polish

## Problem

Many keypresses in inTun are "silent operations" — when the precondition isn't met, nothing happens and the user gets no feedback. Examples:

- `f` on a stopped tunnel → no response
- `s` on a connecting tunnel → no response
- `q` with active tunnels → immediate quit, no confirmation
- SFTP `d` on local panel → no response
- SFTP `u`/`d` during transfer → no response

Additionally:
- Transfer progress only shows 0% and 100% because progress callbacks are `nil`
- No rename support in SFTP

## Design

### 1. Status Message System

Add to Model:
- `statusMsg string` — temporary hint text
- `statusTicks int` — countdown in seconds (1 tick = 1 second via monitor)

Behavior:
- On invalid operation: set `statusMsg` + `statusTicks = 3`
- On each tick: decrement `statusTicks`; when 0, clear `statusMsg`
- Render above the shortcut bar, yellow foreground, left-aligned
- Coexists with `m.err` (red, connection errors) — `statusMsg` is for operational hints (yellow)

Main screen messages:

| Scenario | Message |
|---|---|
| `f` on non-Running tunnel | `Tunnel must be running to use SFTP` |
| `s` on Connecting tunnel | `Cannot stop: tunnel is connecting` |
| `s` on Error tunnel | `Use [r] to reconnect` |
| `d`/`r` on empty list | `No tunnel selected` |

SFTP messages:

| Scenario | Message |
|---|---|
| Any operation during transfer | `Wait for transfer to complete` |
| Sync with no file selected | `No file selected` |
| Sync a file with `s` on a dir entry | `Use [r] for directory sync` |
| Sync a dir with `r` on a file entry | `Use [s] for file sync` |
| Rename on `..` entry | `Cannot rename parent directory` |

### 2. SFTP: Merge Upload/Download → Sync

Replace `u` (upload) and `d` (download) with a single `s` (sync) key:

- **Local panel** focused + file selected → `s` syncs file to remote (upload)
- **Remote panel** focused + file selected → `s` syncs file to local (download)
- Direction is implicit from focus — no need for separate keys

`r` remains for recursive directory sync (same auto-direction logic).

Updated SFTP shortcut bar:

```
[↑↓]Navigate [Tab]Switch [Enter]Open [s]Sync [r]Sync Dir [n]Rename [v]Preview [q]Back
```

During transfer: `[Esc]Cancel  Syncing filename... 45%`

### 3. SFTP: Rename Support

Key: `n`

Flow:
1. User presses `n` on a selected file/dir (not `..`)
2. TUI enters rename input mode: the file name becomes editable at the status bar position
3. User types new name, `Enter` confirms, `Esc` cancels
4. On confirm:
   - Local panel: `os.Rename(oldPath, newPath)`
   - Remote panel: `sftp.Client.Rename(oldPath, newPath)` via `Client.Rename()`
5. After rename: refresh file listing, keep cursor position
6. If rename fails: show error in `statusMsg`

Model additions:
- `sftpRenaming bool` — rename input mode active
- `sftpRenameInput string` — current input text

New method on `sftp.Client`:
- `Rename(oldPath, newPath string) error` — wraps `sftp.Client.Rename()`

Rendering: when `sftpRenaming` is true, status bar area shows `Rename: [input]_` with cursor, shortcut bar replaced by `[Enter]Confirm [Esc]Cancel`

### 4. Transfer Progress Fix

**Root cause:** All transfer goroutines pass `nil` as the progress callback. `sftpProgress.Done` is never updated during transfer, so progress stays at 0% until the `sftpDone` channel fires, then jumps to 100%.

**Fix:** Add `sftpProgressPtr *sftp.ProgressInfo` to Model — a pointer that the goroutine can update safely (single-writer goroutine, single-reader tick).

For single-file sync (`s`):
- Goroutine passes a `progress func(int64)` callback that updates `sftpProgressPtr.Done` and estimates speed
- Progress bar reads from `sftpProgressPtr` on each tick

For recursive sync (`r`):
- Goroutine passes a `progress func(done, total int64, file string)` callback that updates `sftpProgressPtr.Done`, `sftpProgressPtr.Total`, `sftpProgressPtr.File`
- Progress bar shows file count and overall percentage

Speed calculation:
- Track `sftpProgress.Speed` (bytes/sec) — simple moving average: `(lastDone - prevDone) / tickInterval`
- Display: `↑ filename [████░░░░] 45% 2.3MB/s`

Progress bar rendering improvements:
- Use `m.sftpProgressPtr` instead of value copy
- Show speed when > 0
- For recursive: show `file 3/10` index

### 5. Quit Confirmation

Add to Model:
- `quitConfirm bool`

Behavior:
- First `q`: check for Running tunnels. If any exist, set `quitConfirm = true` and show `Active tunnels running. Press q again to quit.`
- Second `q` (while `quitConfirm == true`): `tea.Quit`
- Any other keypress: reset `quitConfirm = false`
- SFTP `q`/`esc`: not affected — returns to main screen directly

The quit confirmation message replaces the status bar line (same position as `statusMsg`).

### 6. Tick Handling

Status message countdown happens in the existing tick handler (`tickMsg`). On each tick:
1. If `statusTicks > 0`: decrement
2. If `statusTicks == 0` and `statusMsg != ""`: clear `statusMsg`
3. During SFTP transfer: progress is already read from `sftpProgressPtr` on each render

This reuses the monitor's tick interval — no new timer needed.

## Scope

- `internal/tui/tui.go` — status messages, quit confirm, rename UI, progress display, sync merge
- `internal/sftp/client.go` — add `Rename()` method; no other changes needed (existing Download/Upload already accept progress callbacks)

## Not In Scope

- Confirmation for `d` (delete tunnel) — low risk, can add later
- i18n / locale-aware messages — all messages in English
- Sound or bell characters
