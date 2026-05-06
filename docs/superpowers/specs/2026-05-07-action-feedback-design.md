# Action Feedback & UX Polish

## Problem

Many keypresses in inTun are "silent operations" — when the precondition isn't met, nothing happens and the user gets no feedback. Examples:

- `f` on a stopped tunnel → no response
- `s` on a connecting tunnel → no response
- `q` with active tunnels → immediate quit, no confirmation
- SFTP `d` on local panel → no response
- SFTP `u`/`d` during transfer → no response

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

### 2. SFTP: Merge Upload/Download → Sync

Replace `u` (upload) and `d` (download) with a single `s` (sync) key:

- **Local panel** focused + file selected → `s` syncs file to remote (upload)
- **Remote panel** focused + file selected → `s` syncs file to local (download)
- Direction is implicit from focus — no need for separate keys

`r` remains for recursive directory sync (same auto-direction logic).

Updated SFTP shortcut bar:

```
[↑↓]Navigate [Tab]Switch [Enter]Open [s]Sync [r]Sync Dir [v]Preview [q]Back
```

During transfer: `[Esc]Cancel  Syncing filename... 45%`

### 3. Quit Confirmation

Add to Model:
- `quitConfirm bool`

Behavior:
- First `q`: check for Running tunnels. If any exist, set `quitConfirm = true` and show `Active tunnels running. Press q again to quit.`
- Second `q` (while `quitConfirm == true`): `tea.Quit`
- Any other keypress: reset `quitConfirm = false`
- SFTP `q`/`esc`: not affected — returns to main screen directly

The quit confirmation message replaces the status bar line (same position as `statusMsg`).

### 4. Tick Handling

Status message countdown happens in the existing 1-second tick handler (`tickMsg`). On each tick:
1. If `statusTicks > 0`: decrement
2. If `statusTicks == 0`: clear `statusMsg`

This reuses the monitor's tick interval — no new timer needed.

## Scope

- Only `internal/tui/tui.go` changes
- No changes to tunnel, platform, monitor, sftp, or config packages
- No new dependencies

## Not In Scope

- Confirmation for `d` (delete tunnel) — low risk, can add later
- i18n / locale-aware messages — all messages in English
- Sound or bell characters
