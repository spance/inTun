# SFTP TUI Visual Polish — Modern Minimal

## Goal

Upgrade the SFTP dual-panel file manager and transfer progress UI from functional to modern-minimal, using Catppuccin Mocha color system and the `bubbles/progress` component.

## Design Decisions

- **Style**: Modern Minimal — clean, purposeful use of color, no borders
- **Progress bar**: `bubbles/progress` with `ViewAs()` static rendering (no spring animation)
- **Layout**: No structural changes to dual-panel architecture; only colors, cursor markers, and progress rendering change

## Changes

### 1. Progress Bar — bubbles/progress Component

**Current** (single line, hand-drawn):
```
↑ filename.txt [████░░░░░░░░] 45% 1.2MB/s
```

**New** (two lines):
```
↑ filename.txt  45% 1.2MB/s
████████████████░░░░░░░░░░░░
```

**Implementation**:
- Add `sftpProgressBar progress.Model` to TUI model
- Initialize: `progress.New(progress.WithScaledGradient("#7C3AED", "#F59E0B"), progress.WithoutPercentage())`
- Width: `panelWidth - 6` (minimum 20), set in `renderSFTPScreen` or tick handler
- Render: `m.sftpProgressBar.ViewAs(p.Done / p.Total)` — static, no FrameMsg needed
- Remove `renderSFTPProgress()` hand-drawn bar code
- Remove `sftpPrevDone` speed delta pattern (keep for speed calc, remove bar logic)
- Progress info line: `direction + truncatedFilename + "  " + pct + " " + speed`
- Progress bar line: `m.sftpProgressBar.ViewAs(percent)` below the info line
- Both lines rendered in the progress section of `renderSFTPScreen()`, replacing the single-line approach

### 2. Color System — Catppuccin Mocha

| Element | Current | New |
|---------|---------|-----|
| Active panel header | `#7C3AED` purple bold | Keep |
| Inactive panel header | `#6B7280` gray bold | `#585B70` (Overlay0) bold |
| Cursor marker (active) | `▸` white `#FFFFFF` | `▎` purple `#7C3AED` |
| Cursor marker (inactive) | `▸` gray `#9CA3AF` | `▎` gray `#585B70` |
| Normal file rows | `#6B7280` gray | `#CDD6F4` (Text) |
| Directory names | `#6B7280` + `/` suffix | `#89B4FA` (Blue) + `/` suffix |
| Panel separator `│` | `#6B7280` | Keep |
| Selected row text (active) | `#FFFFFF` white bold | Keep white bold |
| Selected row text (inactive) | `#9CA3AF` gray | `#585B70` gray |
| Preview header | `#7C3AED` `── Preview ──` | Keep |
| Preview content | `#D1D5DB` light gray | `#CDD6F4` (Text) |
| Progress info line | `#F59E0B` amber | `#F59E0B` amber (keep) |
| Progress bar fill | hand-drawn `█` amber | bubbles/progress gradient `#7C3AED→#F59E0B` |

### 3. Cursor Marker Change

Replace `▸ ` (2 chars: triangle + space) with `▎` (1 char: left bar) + ` ` (1 space).

This means:
- Cursor line prefix: `▎ ` instead of `▸ `
- Non-cursor line prefix: `  ` (2 spaces, unchanged)
- Display width unchanged (2 cells)
- `plainW` calculations in `renderSFTPPanel` remain the same

The `▎` character is a left-half block, display width 1 in most terminals.

### 4. What Does NOT Change

- Dual-panel layout (left=LOCAL, right=REMOTE, `│` separator)
- LOCAL/REMOTE labels in panel headers
- File list scroll logic, `..` parent entry, directory `/` suffix
- Right-aligned file sizes
- Tab switching, keyboard handling
- Sync confirmation dialog (`renderDialogBox`)
- Status message popup
- Shortcuts bar
- Rename input mode
- Preview mode

## Files to Modify

- `internal/tui/tui.go`: All rendering changes (colors, cursor marker, progress bar integration)
- `internal/tui/render_test.go`: Update any snapshot tests affected by color/marker changes

## Risk Areas

- `▎` display width: on some terminals this may render as width 2 (East Asian Ambiguous). Fallback to `│` (pipe) if width issues arise.
- `bubbles/progress` adds dependencies on `harmonica`, `go-colorful`, `termenv` — all already in go.sum via existing bubbles/lipgloss dependencies.
