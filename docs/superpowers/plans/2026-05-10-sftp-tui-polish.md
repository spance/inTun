# SFTP TUI Visual Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade SFTP dual-panel visuals to Modern Minimal style with Catppuccin Mocha colors and bubbles/progress component.

**Architecture:** Replace hand-drawn progress bar with `bubbles/progress` component using `ViewAs()` static rendering. Update all SFTP rendering colors to Catppuccin Mocha palette. Replace `▸` cursor marker with `▎` left-bar marker.

**Tech Stack:** Go 1.21+, bubbletea v0.25.0, bubbles v0.17.1 (progress), lipgloss v0.9.1

---

### Task 1: Add bubbles/progress import and model field

**Files:**
- Modify: `internal/tui/tui.go:1-15` (imports)
- Modify: `internal/tui/tui.go:240-260` (Model struct)

- [ ] **Step 1: Add progress import**

In `internal/tui/tui.go`, add to the import block:

```go
"github.com/charmbracelet/bubbles/progress"
```

- [ ] **Step 2: Add progress model field to Model struct**

After the `sftpPrevDone int64` field (line 255), add:

```go
	sftpProgressBar progress.Model
```

- [ ] **Step 3: Initialize progress bar in NewModel**

In `NewModel()` function, after the existing field initializations (before the return), add:

```go
	m.sftpProgressBar = progress.New(progress.WithScaledGradient("#7C3AED", "#F59E0B"), progress.WithoutPercentage(), progress.WithWidth(40))
```

Note: `progress` is referenced as `m.sftpProgressBar` since NewModel returns `Model` not `*Model`. If NewModel initializes via `m := Model{...}`, add the field after the literal or use `m.sftpProgressBar = ...` before return.

- [ ] **Step 4: Run go mod tidy to fetch harmonica dependency**

Run: `go mod tidy`
Expected: Downloads `charmbracelet/harmonica` and any other missing transitive deps.

- [ ] **Step 5: Build to verify compilation**

Run: `make build`
Expected: Builds without errors.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/tui.go go.mod go.sum
git commit -m "feat(sftp): add bubbles/progress import and model field"
```

---

### Task 2: Update SFTP color constants

**Files:**
- Modify: `internal/tui/tui.go:51-115` (style variables)

- [ ] **Step 1: Update `shortcutStyle` color**

Change line 107 from:
```go
		Foreground(lipgloss.Color("#6B7280"))
```
to:
```go
		Foreground(lipgloss.Color("#CDD6F4"))
```

- [ ] **Step 2: Run tests to see what breaks**

Run: `make test`
Expected: Some tests may fail due to color changes in output. This is expected — we'll fix tests after all visual changes are complete.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/tui.go
git commit -m "style(sftp): update file row color to Catppuccin Mocha Text"
```

---

### Task 3: Update panel header colors

**Files:**
- Modify: `internal/tui/tui.go:1865-1868` (renderSFTPPanel header styles)

- [ ] **Step 1: Update inactive panel header color**

In `renderSFTPPanel`, change the inactive header style (line 1865) from:
```go
	hdrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Bold(true)
```
to:
```go
	hdrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70")).Bold(true)
```

The active header style (`#7C3AED`) stays the same.

- [ ] **Step 2: Build and verify**

Run: `make build`
Expected: Builds without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/tui.go
git commit -m "style(sftp): update inactive panel header to Catppuccin Overlay0"
```

---

### Task 4: Update cursor marker and non-active cursor row color

**Files:**
- Modify: `internal/tui/tui.go:1921-1953` (renderSFTPPanel cursor and row rendering)

- [ ] **Step 1: Replace `▸ ` with `▎ ` cursor marker**

In `renderSFTPPanel`, change line 1923 from:
```go
			prefix = "▸ "
```
to:
```go
			prefix = "▎ "
```

- [ ] **Step 2: Update non-active cursor row color**

Change line 1953 from:
```go
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Render(line))
```
to:
```go
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70")).Render(line))
```

- [ ] **Step 3: Run tests**

Run: `make test`
Expected: Tests that check for `▸` will fail. We'll fix them in Task 8.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/tui.go
git commit -m "style(sftp): use left-bar cursor marker and Mocha Overlay0 for inactive"
```

---

### Task 5: Update directory name color

**Files:**
- Modify: `internal/tui/tui.go:1916-1960` (renderSFTPPanel entry rendering)

- [ ] **Step 1: Add directory-specific styling**

After the `displayName` assignment (line 1919, after the `if isDir && i > 0` block), add directory name coloring logic. Change the line rendering section so that directory entries get `#89B4FA` (Blue) color while regular entries keep the row's base color.

The current code renders each line with one of three styles (selected, inactive cursor, normal). We need to apply directory color on top of the base style.

Replace the entire styling block (lines 1942-1961) with:

```go
		if i == cursor && focused {
			linePad := width - lipgloss.Width(line)
			if linePad > 0 {
				line += strings.Repeat(" ", linePad)
			}
			b.WriteString(selectedStyle.Render(line))
		} else if i == cursor {
			linePad := width - lipgloss.Width(line)
			if linePad > 0 {
				line += strings.Repeat(" ", linePad)
			}
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#585B70"))
			if isDir {
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("#89B4FA"))
			}
			b.WriteString(style.Render(line))
		} else {
			linePad := width - lipgloss.Width(line)
			if linePad > 0 {
				line += strings.Repeat(" ", linePad)
			}
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#CDD6F4"))
			if isDir {
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("#89B4FA"))
			}
			b.WriteString(style.Render(line))
		}
		b.WriteString("\n")
```

- [ ] **Step 2: Build and verify**

Run: `make build`
Expected: Builds without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/tui.go
git commit -m "style(sftp): color directory names in Catppuccin Blue"
```

---

### Task 6: Update preview content color

**Files:**
- Modify: `internal/tui/tui.go:1979` (renderSFTPPreview)

- [ ] **Step 1: Update preview text color**

Change line 1979 from:
```go
	previewStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Width(width - 4)
```
to:
```go
	previewStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CDD6F4")).Width(width - 4)
```

- [ ] **Step 2: Commit**

```bash
git add internal/tui/tui.go
git commit -m "style(sftp): update preview text to Catppuccin Mocha Text"
```

---

### Task 7: Replace hand-drawn progress bar with bubbles/progress

**Files:**
- Modify: `internal/tui/tui.go:1989-2016` (renderSFTPProgress)
- Modify: `internal/tui/tui.go:1849-1852` (progress section in renderSFTPScreen)

- [ ] **Step 1: Rewrite renderSFTPProgress to use bubbles/progress**

Replace the entire `renderSFTPProgress` function with:

```go
func (m Model) renderSFTPProgress(width int) string {
	p := m.sftpProgress
	if p == nil {
		return ""
	}
	var pct float64
	if p.Total > 0 {
		pct = float64(p.Done) / float64(p.Total)
	}

	barWidth := width - 6
	if barWidth < 20 {
		barWidth = 20
	}

	progressInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	var speedStr string
	if p.Speed > 0 {
		speedStr = " " + formatTransferSpeed(p.Speed)
	}
	infoLine := progressInfo.Render(fmt.Sprintf("%s %s  %.0f%%%s", m.sftpDirection, p.File, pct*100, speedStr))

	barModel := m.sftpProgressBar
	barModel.Width = barWidth
	barLine := barModel.ViewAs(pct)

	return infoLine + "\n" + barLine
}
```

- [ ] **Step 2: Update progress rendering call in renderSFTPScreen**

In `renderSFTPScreen`, change lines 1849-1852 from:
```go
	if m.sftpTransferring {
		b.WriteString(m.renderSFTPProgress(panelWidth))
		b.WriteString("\n")
	}
```
to:
```go
	if m.sftpTransferring {
		b.WriteString(m.renderSFTPProgress(width))
		b.WriteString("\n")
	}
```

Note: passing full `width` instead of `panelWidth` so the progress bar spans the full screen width.

- [ ] **Step 3: Build and verify**

Run: `make build`
Expected: Builds without errors.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/tui.go
git commit -m "feat(sftp): replace hand-drawn progress bar with bubbles/progress"
```

---

### Task 8: Fix broken tests

**Files:**
- Modify: `internal/tui/render_test.go` (all SFTP-related tests)

After all visual changes, run tests to see what breaks and fix assertions.

- [ ] **Step 1: Run tests to identify failures**

Run: `make test 2>&1`
Expected: Some tests fail due to color and cursor marker changes.

- [ ] **Step 2: Fix any test assertions**

Tests that check for specific text content (like `TestViewSFTPShortcuts`, `TestViewSFTPRenameInput`) should still pass since we didn't change shortcut or rename text. Tests that check for `▸` in output would need updating to check for `▎` instead.

If `TestViewSFTPRenameInput` or similar tests check for specific rendered content with old colors, update the expected strings to match new output. Run each failing test individually to see the diff.

- [ ] **Step 3: Run full test suite**

Run: `make test && make vet`
Expected: All tests pass, vet clean.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/render_test.go
git commit -m "test: update assertions for SFTP visual changes"
```

---

### Task 9: Final verification and cleanup

- [ ] **Step 1: Run full build and test**

Run: `make build && make test && make vet`
Expected: All pass.

- [ ] **Step 2: Manual smoke test**

Build and run: `./intun` → connect to a host → press `f` to enter SFTP → verify:
- Panel headers show correct colors (active=purple, inactive=dim gray)
- File list rows are brighter (`#CDD6F4`)
- Directories show in blue (`#89B4FA`)
- Cursor marker shows `▎` instead of `▸`
- Inactive cursor row is dimmer (`#585B70`)
- Transfer progress shows two lines (info + gradient bar)

- [ ] **Step 3: Final commit if any fixes needed**
