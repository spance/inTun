# Action Feedback & UX Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix silent operations with status feedback, merge SFTP upload/download into auto-direction sync, add rename support, fix transfer progress reporting, and add quit confirmation.

**Architecture:** All changes are in `internal/tui/tui.go` and `internal/sftp/client.go`. Status messages use a tick-countdown system on the existing tick. Transfer progress uses a pointer-based approach so goroutines can write progress that the render loop reads. Rename uses inline input mode similar to the existing port input.

**Tech Stack:** Go 1.21+, bubbletea, lipgloss, github.com/pkg/sftp

---

### Task 1: Status Message System (Model + Tick + Render)

**Files:**
- Modify: `internal/tui/tui.go:211-252` (Model struct)
- Modify: `internal/tui/tui.go:468-485` (tickMsg handler)
- Modify: `internal/tui/tui.go:790-820` (View render)
- Modify: `internal/tui/render_test.go`

- [ ] **Step 1: Add fields to Model struct**

In `internal/tui/tui.go`, add after line 227 (`err error` line):

```go
	statusMsg   string
	statusTicks int
	quitConfirm bool
```

- [ ] **Step 2: Add setStatusMsg helper method**

Add after the Model struct definition (around line 252):

```go
func (m *Model) setStatusMsg(msg string) {
	m.statusMsg = msg
	m.statusTicks = 3
}
```

- [ ] **Step 3: Add tick countdown in tickMsg handler**

In the `tickMsg` case (around line 468), add at the top of the handler, before the SFTP done check:

```go
		if m.statusTicks > 0 {
			m.statusTicks--
			if m.statusTicks == 0 {
				m.statusMsg = ""
			}
		}
```

- [ ] **Step 4: Add renderStatusMsg method**

Add before `renderShortcuts`:

```go
func (m Model) renderStatusMsg(width int) string {
	if m.statusMsg == "" && !m.quitConfirm {
		return ""
	}
	msg := m.statusMsg
	if m.quitConfirm {
		msg = "Active tunnels running. Press q again to quit."
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	rendered := style.Render(msg)
	pad := width - lipgloss.Width(rendered)
	if pad > 0 {
		rendered += strings.Repeat(" ", pad)
	}
	return rendered
}
```

- [ ] **Step 5: Render status message in View**

In the `View()` method, find the main screen section. After the title line and before the shortcut bar, add rendering of `renderStatusMsg`. Locate where `renderShortcuts` is called and insert before it:

```go
		if msg := m.renderStatusMsg(width); msg != "" {
			b.WriteString(msg)
			b.WriteString("\n")
		}
```

- [ ] **Step 6: Write test for status message rendering**

Add to `internal/tui/render_test.go`:

```go
func TestViewStatusMessage(t *testing.T) {
	m := newTestModelWithTunnel()
	m.statusMsg = "Tunnel must be running"
	m.statusTicks = 3
	output := m.View()
	clean := stripANSI(output)
	if !strings.Contains(clean, "Tunnel must be running") {
		t.Error("View should show status message")
	}
}
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tui/ -v -run TestViewStatusMessage`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: add status message system with tick countdown"
```

---

### Task 2: Quit Confirmation

**Files:**
- Modify: `internal/tui/tui.go:664-665` (quit handler)
- Modify: `internal/tui/tui.go:504-519` (handleKeyPress — reset quitConfirm)

- [ ] **Step 1: Modify quit handler in handleMainKeys**

Replace the current quit case (line 664):

```go
	case "q", "ctrl+c":
		if m.quitConfirm {
			return m, tea.Quit
		}
		tunnels := m.manager.List()
		for _, t := range tunnels {
			if t.Status == tunnel.StatusRunning {
				m.quitConfirm = true
				return m, nil
			}
		}
		return m, tea.Quit
```

- [ ] **Step 2: Reset quitConfirm on any other key in handleMainKeys**

At the very start of `handleMainKeys` (after `tunnels := m.manager.List()`), add:

```go
	m.quitConfirm = false
```

Wait — this would reset on every keypress including `q`. Need to reset in handleKeyPress instead. In `handleKeyPress`, before the screen switch, when screen is `ScreenMain` and key is not `q`/`ctrl+c`:

Actually, the cleanest approach: reset in the `handleKeyPress` function before dispatching to `handleMainKeys`, then let `handleMainKeys` set it again if needed. In `handleKeyPress` (around line 504), add before the screen switch:

```go
	if m.screen == ScreenMain {
		m.quitConfirm = false
	}
```

But this resets before `handleMainKeys` processes the key, which is fine — `handleMainKeys` will set it again if this is a `q` with running tunnels.

- [ ] **Step 3: Run tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: add quit confirmation when tunnels are running"
```

---

### Task 3: Main Screen Status Feedback for Invalid Operations

**Files:**
- Modify: `internal/tui/tui.go:586-655` (handleMainKeys switch cases)

- [ ] **Step 1: Add feedback to `f` key (SFTP)**

In the `case "f":` block, the `if t.Status == tunnel.StatusRunning` check currently silently drops non-running. Add an else:

```go
	case "f":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				// ... existing SFTP init code unchanged ...
			} else {
				m.setStatusMsg("Tunnel must be running to use SFTP")
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
```

- [ ] **Step 2: Add feedback to `s` key (Stop/Start)**

Replace the `case "s":` block:

```go
	case "s":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				m.manager.Stop(t.ID)
			} else if t.Status == tunnel.StatusStopped {
				m.manager.Restart(t.ID)
			} else if t.Status == tunnel.StatusConnecting {
				m.setStatusMsg("Cannot stop: tunnel is connecting")
			} else if t.Status == tunnel.StatusError {
				m.setStatusMsg("Use [r] to reconnect")
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
```

- [ ] **Step 3: Add feedback to `d` key (Delete)**

```go
	case "d":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Delete(tunnels[m.selectedIndex].ID)
			if m.selectedIndex > 0 {
				m.selectedIndex--
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
```

- [ ] **Step 4: Add feedback to `r` key (Reconnect)**

```go
	case "r":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Restart(tunnels[m.selectedIndex].ID)
		} else {
			m.setStatusMsg("No tunnel selected")
		}
```

- [ ] **Step 5: Run tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: add status feedback for invalid main screen operations"
```

---

### Task 4: SFTP Rename — sftp.Client Method

**Files:**
- Modify: `internal/sftp/client.go` (add Rename method)
- Modify: `internal/sftp/client_test.go` (add test)

- [ ] **Step 1: Add Rename method to Client**

In `internal/sftp/client.go`, add after the `Preview` method:

```go
func (s *Client) Rename(oldPath, newPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client.Rename(oldPath, newPath)
}
```

- [ ] **Step 2: Write test for local rename (tests the integration concept)**

Add to `internal/sftp/client_test.go`:

```go
func TestReadLocalDirRename(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "old.txt"), []byte("data"), 0644)
	os.Rename(filepath.Join(tmpDir, "old.txt"), filepath.Join(tmpDir, "new.txt"))
	entries, err := ReadLocalDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadLocalDir failed: %v", err)
	}
	found := map[string]bool{}
	for _, e := range entries {
		found[e.Name] = true
	}
	if found["old.txt"] {
		t.Error("old.txt should not exist after rename")
	}
	if !found["new.txt"] {
		t.Error("new.txt should exist after rename")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/sftp/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: add Rename method to sftp.Client"
```

---

### Task 5: SFTP Rename — TUI Model Fields + Key Handling

**Files:**
- Modify: `internal/tui/tui.go:211-252` (Model struct — add rename fields)
- Modify: `internal/tui/tui.go:1147-1220` (handleSFTPKeys)

- [ ] **Step 1: Add rename fields to Model struct**

In the SFTP section of the Model struct (after `sftpDirection`):

```go
	sftpRenaming    bool
	sftpRenameInput string
```

- [ ] **Step 2: Add rename input handling in handleSFTPKeys**

At the very top of `handleSFTPKeys`, before the `sftpPreviewing` check, add rename mode handling:

```go
func (m Model) handleSFTPKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.sftpRenaming {
		switch msg.String() {
		case "enter":
			return m.sftpConfirmRename()
		case "esc":
			m.sftpRenaming = false
			m.sftpRenameInput = ""
		case "backspace":
			if len(m.sftpRenameInput) > 0 {
				m.sftpRenameInput = m.sftpRenameInput[:len(m.sftpRenameInput)-1]
			}
		default:
			if len(msg.String()) == 1 && msg.String()[0] >= 32 {
				m.sftpRenameInput += msg.String()
			}
		}
		return m, nil
	}

	if m.sftpPreviewing {
	// ... existing code ...
```

- [ ] **Step 3: Add `n` key case in the SFTP key switch**

In the main switch of `handleSFTPKeys`, add after the `case "v":` block:

```go
	case "n":
		if !m.sftpTransferring {
			files := m.currentSFTPFiles()
			cursor := m.sftpCursor[m.sftpFocus]
			if cursor == 0 || cursor > len(files) {
				m.setStatusMsg("No file selected")
				return m, nil
			}
			m.sftpRenaming = true
			m.sftpRenameInput = files[cursor-1].Name
		} else {
			m.setStatusMsg("Wait for transfer to complete")
		}
```

- [ ] **Step 4: Implement sftpConfirmRename**

Add after `sftpPreviewFile`:

```go
func (m Model) sftpConfirmRename() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	if cursor == 0 || cursor > len(files) {
		m.sftpRenaming = false
		m.sftpRenameInput = ""
		return m, nil
	}

	oldName := files[cursor-1].Name
	newName := m.sftpRenameInput
	m.sftpRenaming = false
	m.sftpRenameInput = ""

	if newName == "" || newName == oldName {
		return m, nil
	}

	if m.sftpFocus == 0 {
		oldPath := filepath.Join(m.sftpLocalDir, oldName)
		newPath := filepath.Join(m.sftpLocalDir, newName)
		if err := os.Rename(oldPath, newPath); err != nil {
			m.setStatusMsg(fmt.Sprintf("Rename failed: %v", err))
			return m, nil
		}
	} else {
		oldPath := m.sftpRemoteDir + "/" + oldName
		newPath := m.sftpRemoteDir + "/" + newName
		if err := m.sftpClient.Rename(oldPath, newPath); err != nil {
			m.setStatusMsg(fmt.Sprintf("Rename failed: %v", err))
			return m, nil
		}
	}

	m = m.refreshSFTPFiles()
	return m, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go build ./... && go test ./... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: add SFTP rename support with inline input"
```

---

### Task 6: SFTP Sync — Merge Upload/Download

**Files:**
- Modify: `internal/tui/tui.go:1187-1210` (replace `u`/`d` cases with `s`)
- Modify: `internal/tui/tui.go:1288-1335` (replace sftpStartUpload/sftpStartDownload with sftpStartSync)
- Modify: `internal/tui/tui.go:1066-1075` (shortcut bar)

- [ ] **Step 1: Replace `u`/`d` key cases with `s`**

In `handleSFTPKeys`, remove `case "u":` and `case "d":` blocks. Add:

```go
	case "s":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpStartSync()
```

- [ ] **Step 2: Implement sftpStartSync**

Add after `sftpStartSync` (replacing `sftpStartUpload` and `sftpStartDownload`):

```go
func (m Model) sftpStartSync() (tea.Model, tea.Cmd) {
	if m.sftpFocus == 0 {
		files := m.sftpLocalFiles
		cursor := m.sftpCursor[0]
		if cursor == 0 || cursor > len(files) {
			m.setStatusMsg("No file selected")
			return m, nil
		}
		entry := files[cursor-1]
		if entry.IsDir {
			m.setStatusMsg("Use [r] for directory sync")
			return m, nil
		}
		localPath := filepath.Join(m.sftpLocalDir, entry.Name)
		remotePath := m.sftpRemoteDir + "/" + entry.Name
		m.sftpTransferring = true
		m.sftpDirection = "↑"
		m.sftpProgress = sftp.ProgressInfo{File: entry.Name, Total: entry.Size, Active: true}
		done := make(chan struct{})
		m.sftpDone = done
		progress := m.sftpProgressPtr()
		client := m.sftpClient
		go func() {
			client.Upload(localPath, remotePath, func(n int64) {
				progress.Done = n
			})
			close(done)
		}()
		return m, nil
	}

	files := m.sftpRemoteFiles
	cursor := m.sftpCursor[1]
	if cursor == 0 || cursor > len(files) {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	entry := files[cursor-1]
	if entry.IsDir {
		m.setStatusMsg("Use [r] for directory sync")
		return m, nil
	}
	remotePath := m.sftpRemoteDir + "/" + entry.Name
	localPath := filepath.Join(m.sftpLocalDir, entry.Name)
	m.sftpTransferring = true
	m.sftpDirection = "↓"
	m.sftpProgress = sftp.ProgressInfo{File: entry.Name, Total: entry.Size, Active: true}
	done := make(chan struct{})
	m.sftpDone = done
	progress := m.sftpProgressPtr()
	client := m.sftpClient
	go func() {
		client.Download(remotePath, localPath, func(n int64) {
			progress.Done = n
		})
		close(done)
	}()
	return m, nil
}
```

- [ ] **Step 3: Add sftpProgressPtr helper**

Add near `sftpVisibleHeight`:

```go
func (m *Model) sftpProgressPtr() *sftp.ProgressInfo {
	return &m.sftpProgress
}
```

- [ ] **Step 4: Update recursive sync to also use progress callback**

In `sftpStartRecursive`, update the two goroutines to pass progress callbacks:

For the upload goroutine (around line 1358):
```go
		go func() {
			progress := &m.sftpProgress
			client.UploadDir(localPath, remotePath, func(done, total int64, file string) {
				progress.Done = done
				progress.Total = total
				progress.File = file
			})
			close(done)
		}()
```

For the download goroutine (around line 1382):
```go
		go func() {
			progress := &m.sftpProgress
			client.DownloadDir(remoteDir, localPath, func(done, total int64, file string) {
				progress.Done = done
				progress.Total = total
				progress.File = file
			})
			close(done)
		}()
```

- [ ] **Step 5: Update SFTP shortcut bar**

In the renderShortcuts SFTP browsing section (around line 1066), replace `Upload`/`Download` with `Sync`:

```go
		items = []string{
			"[" + keyStyle.Render("Tab") + "]Switch",
			"[" + keyStyle.Render("↑↓") + "]Navigate",
			"[" + keyStyle.Render("Enter") + "]Open",
			"[" + keyStyle.Render("s") + "]Sync",
			"[" + keyStyle.Render("r") + "]Sync Dir",
			"[" + keyStyle.Render("n") + "]Rename",
			"[" + keyStyle.Render("v") + "]Preview",
			"[" + keyStyle.Render("q") + "]Back",
		}
```

- [ ] **Step 6: Delete old sftpStartUpload and sftpStartDownload methods**

Remove the `sftpStartUpload` and `sftpStartDownload` functions entirely.

- [ ] **Step 7: Update shortcut test**

In `render_test.go`, the `TestViewContainsShortcuts` test checks for "Navigate", "Create", "Reconnect", "Quit" — this should still pass since those are main screen shortcuts. Verify.

- [ ] **Step 8: Run tests**

Run: `go build ./... && go test ./... -v`
Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add -A && git commit -m "feat: merge SFTP upload/download into auto-direction sync"
```

---

### Task 7: SFTP Status Feedback for Invalid Operations

**Files:**
- Modify: `internal/tui/tui.go` (add statusMsg in SFTP key handler)

- [ ] **Step 1: Add feedback to SFTP `r` key (recursive sync)**

The existing `case "r":` in handleSFTPKeys calls `sftpStartRecursive()`. Add transfer check and statusMsg for no-dir-selected:

```go
	case "r":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpStartRecursive()
```

Update `sftpStartRecursive` to add feedback. At the start of each branch (local focus, remote focus), after checking cursor validity and IsDir:

In local focus branch, after `if !entry.IsDir`:
```go
			m.setStatusMsg("Use [s] for file sync")
			return m, nil
```

In remote focus branch, after `if !entry.IsDir`:
```go
			m.setStatusMsg("Use [s] for file sync")
			return m, nil
```

And for empty cursor in both branches, add `m.setStatusMsg("No file selected")`.

- [ ] **Step 2: Add feedback to SFTP `v` key (preview)**

The existing `case "v":` calls `sftpPreviewFile()`. Add transfer check:

```go
	case "v":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpPreviewFile()
```

- [ ] **Step 3: Run tests**

Run: `go build ./... && go test ./... -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: add status feedback for invalid SFTP operations"
```

---

### Task 8: Render Status Message in SFTP Screen

**Files:**
- Modify: `internal/tui/tui.go:1450-1510` (renderSFTPScreen)
- Modify: `internal/tui/tui.go` (add rename input rendering)

- [ ] **Step 1: Render statusMsg in renderSFTPScreen**

In `renderSFTPScreen`, after the panel rows and before the progress/preview sections, add:

```go
	if !m.sftpTransferring && !m.sftpPreviewing {
		if msg := m.renderStatusMsg(width); msg != "" {
			b.WriteString(msg)
			b.WriteString("\n")
		}
	}
```

- [ ] **Step 2: Render rename input bar**

After the statusMsg rendering (or as part of it), add rename mode rendering. In `renderSFTPScreen`, after the panels and before progress/preview:

```go
	if m.sftpRenaming {
		renameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
		input := m.sftpRenameInput + "_"
		hint := "Rename: " + input
		confirmHint := "  [Enter]Confirm [Esc]Cancel"
		rendered := renameStyle.Render(hint) + shortcutStyle.Render(confirmHint)
		pad := width - lipgloss.Width(rendered)
		if pad > 0 {
			rendered += strings.Repeat(" ", pad)
		}
		b.WriteString(rendered)
		b.WriteString("\n")
	}
```

- [ ] **Step 3: Run tests**

Run: `go build ./... && go test ./... -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: render status messages and rename input in SFTP screen"
```

---

### Task 9: Fix Transfer Progress — Proper Rendering

**Files:**
- Modify: `internal/tui/tui.go:1639-1657` (renderSFTPProgress)

- [ ] **Step 1: Update renderSFTPProgress to show speed and better formatting**

Replace the `renderSFTPProgress` function:

```go
func (m Model) renderSFTPProgress(width int) string {
	p := m.sftpProgress
	var pct float64
	if p.Total > 0 {
		pct = float64(p.Done) / float64(p.Total) * 100
	}
	barWidth := width / 4
	if barWidth < 10 {
		barWidth = 10
	}
	filled := int(pct / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	var speedStr string
	if p.Speed > 0 {
		speedStr = " " + formatSpeed(p.Speed)
	}

	progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	return progressStyle.Render(fmt.Sprintf("%s %s [%s] %.0f%%%s", m.sftpDirection, p.File, bar, pct, speedStr))
}
```

- [ ] **Step 2: Add formatSpeed helper**

Add near `formatBytes`:

```go
func formatSpeed(bytesPerSec int64) string {
	if bytesPerSec >= 1024*1024 {
		return fmt.Sprintf("%.1fMB/s", float64(bytesPerSec)/(1024*1024))
	}
	return fmt.Sprintf("%.1fKB/s", float64(bytesPerSec)/1024)
}
```

- [ ] **Step 3: Calculate speed in tick handler**

In the `tickMsg` handler, inside the SFTP transfer block (where `sftpDone` is checked), calculate speed before checking done. Add before `select`:

```go
		if m.sftpTransferring {
			prevDone := m.sftpProgress.Done
			m.sftpProgress.Speed = prevDone
		}
```

Wait — speed should be `current - previous` per tick. The tick is 500ms, so speed = `(currentDone - prevDone) / 0.5`. But we need to store prevDone. Simplest: store it in a field. Add to Model:

Actually, the simplest approach without extra fields: calculate speed from Done/elapsed. But we don't track start time either.

Simplest correct approach: use a field `sftpPrevDone int64` on Model. On each tick:

```go
		if m.sftpTransferring {
			m.sftpProgress.Speed = (m.sftpProgress.Done - m.sftpPrevDone) * 2
			m.sftpPrevDone = m.sftpProgress.Done
		}
```

Add `sftpPrevDone int64` to Model struct.

Reset it when starting a transfer (in sftpStartSync and sftpStartRecursive): `m.sftpPrevDone = 0`.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "fix: show real transfer progress with speed display"
```

---

### Task 10: Update Tests for New Shortcuts

**Files:**
- Modify: `internal/tui/render_test.go`

- [ ] **Step 1: Add test for SFTP shortcuts**

```go
func TestViewSFTPShortcuts(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSFTP
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "test.txt", IsDir: false}}
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "remote.txt", IsDir: false}}
	output := m.View()
	clean := stripANSI(output)
	for _, s := range []string{"Sync", "Sync Dir", "Rename", "Preview", "Back"} {
		if !strings.Contains(clean, s) {
			t.Errorf("SFTP view should contain shortcut '%s'", s)
		}
	}
}
```

- [ ] **Step 2: Add test for quit confirmation**

```go
func TestQuitConfirmWithRunningTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]
	tun.Status = tunnel.StatusRunning
	output := m.View()
	clean := stripANSI(output)
	if strings.Contains(clean, "Press q again") {
		t.Error("Should not show quit confirmation before pressing q")
	}

	m.quitConfirm = true
	output = m.View()
	clean = stripANSI(output)
	if !strings.Contains(clean, "Press q again to quit") {
		t.Error("Should show quit confirmation when quitConfirm is true")
	}
}
```

- [ ] **Step 3: Add test for rename rendering**

```go
func TestViewSFTPRenameInput(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSFTP
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "old.txt", IsDir: false}}
	m.sftpRemoteFiles = []sftp.FileEntry{}
	m.sftpRenaming = true
	m.sftpRenameInput = "new.txt"
	output := m.View()
	clean := stripANSI(output)
	if !strings.Contains(clean, "Rename: new.txt_") {
		t.Error("Should show rename input")
	}
	if !strings.Contains(clean, "Confirm") {
		t.Error("Should show confirm hint")
	}
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "test: add tests for SFTP sync, rename, quit confirm"
```

---

### Task 11: Final Verification

- [ ] **Step 1: Run full build and test**

Run: `make build && make test && make vet`
Expected: All PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Commit if any fixes needed**

```bash
git add -A && git commit -m "fix: address vet/lint issues from final verification"
```
