package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/sftp"
	"github.com/spance/intun/internal/tunnel"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func newTestModelWithTunnel() Model {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := NewModel(hosts, tunnel.NewManager(nil), "v1.0.0")
	m.width = 120
	m.height = 30

	mockExec := platform.NewMockExecutor()
	m.manager.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	m.manager.Create("test-tunnel", cfg, tunnel.Local, "8080", "80")

	return m
}

func TestViewContainsTitle(t *testing.T) {
	m := newTestModelWithTunnel()
	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "inTun") {
		t.Error("View output should contain 'inTun' title")
	}
	if !strings.Contains(clean, "v1.0.0") {
		t.Error("View output should contain version")
	}
}

func TestViewContainsShortcuts(t *testing.T) {
	m := newTestModelWithTunnel()
	output := m.View().Content
	clean := stripANSI(output)

	shortcuts := []string{"Navigate", "Create", "Reconnect", "Quit"}
	for _, s := range shortcuts {
		if !strings.Contains(clean, s) {
			t.Errorf("View output should contain shortcut '%s'", s)
		}
	}
}

func TestViewTunnelList(t *testing.T) {
	m := newTestModelWithTunnel()
	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "test-tunnel") {
		t.Error("View output should contain tunnel name")
	}
	if !strings.Contains(clean, "LOCAL") {
		t.Error("View output should contain tunnel type")
	}
	if !strings.Contains(clean, ":8080") {
		t.Error("View output should contain local port")
	}
	if !strings.Contains(clean, ":80") {
		t.Error("View output should contain remote port")
	}
}

func TestTunnelRouteTextExplainsEndpointDirection(t *testing.T) {
	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tests := []struct {
		name string
		tun  *tunnel.Tunnel
		want string
	}{
		{
			name: "local forward",
			tun:  &tunnel.Tunnel{SSHConfig: cfg, Type: tunnel.Local, LocalPort: "9090", RemotePort: "22"},
			want: "127.0.0.1:9090 -> REMOTE 127.0.0.1:22",
		},
		{
			name: "remote forward",
			tun:  &tunnel.Tunnel{SSHConfig: cfg, Type: tunnel.Remote, LocalPort: "127.0.0.1:22", RemotePort: "0.0.0.0:9090"},
			want: "0.0.0.0:9090 -> LOCAL 127.0.0.1:22",
		},
		{
			name: "dynamic forward",
			tun:  &tunnel.Tunnel{SSHConfig: cfg, Type: tunnel.Dynamic, LocalPort: "1080"},
			want: "127.0.0.1:1080 -> SOCKS5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tunnelRouteText(tt.tun); got != tt.want {
				t.Fatalf("tunnelRouteText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderTunnelRouteTextHighlightsEndpointLabels(t *testing.T) {
	rendered := renderTunnelRouteText("127.0.0.1:9090 -> REMOTE 127.0.0.1:22")
	if !ansiRegex.MatchString(rendered) {
		t.Fatal("endpoint labels should be highlighted")
	}
	if clean := stripANSI(rendered); clean != "127.0.0.1:9090 -> REMOTE 127.0.0.1:22" {
		t.Fatalf("rendered route changed visible text: %q", clean)
	}
}

func TestViewDoesNotPrefixHostPortAddressWithExtraColon(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]
	tun.LocalPort = "127.0.0.1:5555"
	tun.RemotePort = "10.0.0.15:5551"

	output := m.View().Content
	clean := stripANSI(output)

	if strings.Contains(clean, ":127.0.0.1:5555") {
		t.Error("local address should not get an extra leading colon")
	}
	if strings.Contains(clean, ":10.0.0.15:5551") {
		t.Error("remote address should not get an extra leading colon")
	}
	if !strings.Contains(clean, "127.0.0.1:5555") {
		t.Error("local host:port address should remain visible")
	}
	if !strings.Contains(clean, "10.0.0.15:5551") {
		t.Error("remote host:port address should remain visible")
	}
}

func TestViewTableFitsMinimumWidth(t *testing.T) {
	m := newTestModelWithTunnel()
	m.width = 80
	m.height = 20
	tun := m.manager.List()[0]
	tun.Name = "very-long-production-tunnel-name-that-needs-truncation"
	tun.LocalPort = "127.0.0.1:555555555555"
	tun.RemotePort = "10.0.0.15:555155555555"

	output := m.View().Content
	clean := stripANSI(output)

	for _, line := range strings.Split(clean, "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Fatalf("line width = %d, want <= 80: %q", width, line)
		}
	}
}

func TestViewNoTunnelsMessage(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com"}}
	m := NewModel(hosts, tunnel.NewManager(nil), "v1.0.0")
	m.width = 100
	m.height = 30

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "No tunnels active") {
		t.Error("View should show 'No tunnels active' message")
	}
	if !strings.Contains(clean, "'c' to create") {
		t.Error("View should show create shortcut hint")
	}
}

func TestViewDisplaysModelError(t *testing.T) {
	m := newTestModelWithTunnel()
	m.err = fmt.Errorf("no hosts found in ~/.ssh/config")

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Error: no hosts found") {
		t.Error("View should render model errors")
	}
}

func TestViewTunnelStatus(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]

	tun.UpdateStats(1024, 2048, 100, 200, 50*1000000, false)
	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Running") {
		t.Error("View should show Running status")
	}
	if !strings.Contains(clean, "1.00 KB") {
		t.Error("View should show traffic stats")
	}
}

func TestViewErrorState(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]

	tun.Status = tunnel.StatusError
	tun.Error = "SSH_CONNECTION_FAILED: connection refused"

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Error") {
		t.Error("View should show Error status")
	}
	if !strings.Contains(clean, "connection refused") {
		t.Error("View should show error message")
	}
}

func TestViewHostKeyError(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]

	tun.Status = tunnel.StatusError
	tun.Error = "HOST_KEY_NOT_CACHED: unknown host"

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Host key not cached") {
		t.Error("View should show host key hint")
	}
}

func TestViewAuthError(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]

	tun.Status = tunnel.StatusError
	tun.Error = "SSH_AUTH_FAILED: no valid key"

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Authentication failed") {
		t.Error("View should show auth error message")
	}
	if !strings.Contains(clean, "~/.ssh/id_rsa") {
		t.Error("View should show key path hint")
	}
}

func TestViewConnectionLostShowsDetail(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]

	tun.Status = tunnel.StatusError
	tun.Error = "SSH_CONNECTION_LOST: EOF"

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "SSH connection lost") {
		t.Error("View should show connection lost message")
	}
	if !strings.Contains(clean, "EOF") {
		t.Error("View should show connection lost detail")
	}
}

func TestViewPortInputScreen(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenInputPort
	m.portInput = "808"

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Port") {
		t.Error("Port input screen should show 'Port'")
	}
	if !strings.Contains(clean, "808") {
		t.Error("Port input screen should show current input")
	}
}

func TestViewHostSelectScreen(t *testing.T) {
	hosts := []config.Host{
		{Name: "host1", Hostname: "host1.com", User: "user1"},
		{Name: "host2", Hostname: "host2.com", User: "user2"},
	}
	m := NewModel(hosts, tunnel.NewManager(nil), "v1.0.0")
	m.width = 100
	m.height = 30
	m.screen = ScreenSelectHost

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Select Host") {
		t.Error("Host select screen should show 'Select Host'")
	}
	if !strings.Contains(clean, "host1") {
		t.Error("Host select screen should show host name")
	}
}

func TestViewTypeSelectScreen(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSelectType

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Select Tunnel Type") {
		t.Error("Type select screen should show title")
	}
	if !strings.Contains(clean, "Local") {
		t.Error("Type select screen should show Local option")
	}
	if !strings.Contains(clean, "Remote") {
		t.Error("Type select screen should show Remote option")
	}
	if !strings.Contains(clean, "Dynamic") {
		t.Error("Type select screen should show Dynamic option")
	}
}

func TestViewPromptModeHostKey(t *testing.T) {
	m := newTestModelWithTunnel()
	normal := stripANSI(m.View().Content)
	m.promptMode = true

	respChan := make(chan platform.AuthResponse, 1)
	req := platform.AuthRequest{
		Type:        platform.AuthRequestHostKey,
		Host:        "test@example.com",
		Fingerprint: "SHA256:abc123",
		Response:    respChan,
	}
	m.authQueue.requestChan <- req
	m.authQueue.Poll()

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Auth Required") {
		t.Error("Prompt mode should show 'Auth Required'")
	}
	if !strings.Contains(clean, "test@example.com") {
		t.Error("Prompt mode should show host")
	}
	if !strings.Contains(clean, "SHA256:abc123") {
		t.Error("Prompt mode should show fingerprint")
	}
	if !strings.Contains(clean, "Accept") {
		t.Error("Prompt mode should show Accept option")
	}
	if lineIndex(clean, "Name") != lineIndex(normal, "Name") {
		t.Error("Prompt should overlay the main screen without pushing the table down")
	}
}

func TestViewPromptModePassword(t *testing.T) {
	m := newTestModelWithTunnel()
	m.promptMode = true
	m.promptInput = "testpassword"

	respChan := make(chan platform.AuthResponse, 1)
	req := platform.AuthRequest{
		Type:       platform.AuthRequestPassword,
		Host:       "user@example.com",
		RetryCount: 1,
		Response:   respChan,
	}
	m.authQueue.requestChan <- req
	m.authQueue.Poll()

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Password") {
		t.Error("Password prompt should show 'Password'")
	}
	if !strings.Contains(clean, "attempt") {
		t.Error("Password prompt should show attempt count")
	}
	maskLen := strings.Count(clean, "*")
	if maskLen < 5 {
		t.Error("Password prompt should mask input with asterisks")
	}
}

func TestViewPromptOverlayFitsMinimumWidth(t *testing.T) {
	m := newTestModelWithTunnel()
	m.width = 80
	m.height = 20
	m.promptMode = true
	m.promptInput = strings.Repeat("x", 120)

	respChan := make(chan platform.AuthResponse, 1)
	req := platform.AuthRequest{
		Type:       platform.AuthRequestPassword,
		Host:       "very-long-user-name@very-long-host-name.example.internal",
		RetryCount: 1,
		Response:   respChan,
	}
	m.authQueue.requestChan <- req
	m.authQueue.Poll()

	output := m.View().Content
	clean := stripANSI(output)

	for _, line := range strings.Split(clean, "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Fatalf("line width = %d, want <= 80: %q", width, line)
		}
	}
}

func TestViewQuitConfirmOverlay(t *testing.T) {
	m := newTestModelWithTunnel()
	normal := stripANSI(m.View().Content)
	m.confirmQuit = true

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Confirm Exit") {
		t.Error("quit confirmation should show title")
	}
	if !strings.Contains(clean, "Active tunnels are still running") {
		t.Error("quit confirmation should explain active tunnels")
	}
	if !strings.Contains(clean, "Cancel") {
		t.Error("quit confirmation should show cancel action")
	}
	if lineIndex(clean, "Name") != lineIndex(normal, "Name") {
		t.Error("quit confirmation should overlay the main screen without pushing the table down")
	}
}

func TestRenderModalKeepsBorderAligned(t *testing.T) {
	body := strings.Join([]string{
		lipgloss.NewStyle().Background(lipgloss.Color("#D29922")).Render("Confirm Exit"),
		"",
		"Active tunnels are still running",
		"this line is intentionally much longer than the modal content width and should be clipped",
	}, "\n")

	clean := stripANSI(renderModal(80, body, 56).String())
	lines := strings.Split(clean, "\n")

	for _, line := range lines {
		if width := lipgloss.Width(line); width != 56 {
			t.Fatalf("modal line width = %d, want 56: %q", width, line)
		}
		if !strings.HasPrefix(line, "╭") && !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "╰") {
			t.Fatalf("modal line missing left border: %q", line)
		}
		if !strings.HasSuffix(line, "╮") && !strings.HasSuffix(line, "│") && !strings.HasSuffix(line, "╯") {
			t.Fatalf("modal line missing right border: %q", line)
		}
	}
}

func TestOverlayCentersModalAsSingleBlock(t *testing.T) {
	modal := renderModal(120, strings.Join([]string{
		lipgloss.NewStyle().Background(lipgloss.Color("#D29922")).Render("Confirm Exit"),
		"Active tunnels are still running",
		"[Enter/Y/Q] Exit    [Esc/N] Cancel",
	}, "\n"), 56)

	clean := stripANSI(overlayCentered(strings.Repeat("\n", 30), modal, 120, 30))
	positions := make([]int, 0)
	for _, line := range strings.Split(clean, "\n") {
		if idx := strings.IndexAny(line, "╭│╰"); idx >= 0 {
			positions = append(positions, idx)
		}
	}
	if len(positions) == 0 {
		t.Fatal("expected overlay border lines")
	}
	for _, pos := range positions {
		if pos != positions[0] {
			t.Fatalf("modal line starts are misaligned: %v", positions)
		}
	}
}

func TestOverlayMasksBaseOutsideModal(t *testing.T) {
	baseLines := make([]string, 12)
	for i := range baseLines {
		baseLines[i] = fmt.Sprintf("base-row-%02d", i)
	}
	base := strings.Join(baseLines, "\n")
	modal := renderModal(80, "Confirm Exit", 32)

	clean := stripANSI(overlayCentered(base, modal, 80, 12))
	lines := strings.Split(clean, "\n")
	if strings.Contains(clean, "base-row") {
		t.Fatalf("full-screen mask should hide base content:\n%s", clean)
	}
	if strings.TrimSpace(lines[0]) != "" {
		t.Fatalf("top mask row should be blank: %q", lines[0])
	}
	if strings.HasPrefix(strings.TrimLeft(lines[0], " "), "+") || strings.Contains(lines[0], "Confirm Exit") {
		t.Fatalf("modal should not be placed at top row: %q", lines[0])
	}
}

func TestOverlayMasksModalRows(t *testing.T) {
	baseLines := make([]string, 12)
	for i := range baseLines {
		baseLines[i] = "background-list-item-border"
	}
	base := strings.Join(baseLines, "\n")
	modal := renderModal(80, "Confirm Exit", 32)

	clean := stripANSI(overlayCentered(base, modal, 80, 12))
	for _, line := range strings.Split(clean, "\n") {
		if strings.Contains(line, "Confirm Exit") {
			if strings.Contains(line, "background-list-item-border") {
				t.Fatalf("modal row should mask background content: %q", line)
			}
			if strings.Contains(line, "...") {
				t.Fatalf("modal row should not contain truncation dots: %q", line)
			}
			return
		}
	}
	t.Fatal("expected modal content")
}

func lineIndex(s, needle string) int {
	for i, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func TestViewDynamicTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	m.manager.Create("socks-proxy", cfg, tunnel.Dynamic, "1080", "")

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "SOCKS5") {
		t.Error("Dynamic tunnel should show 'SOCKS5' for remote")
	}
}

func TestANSIStylesPresent(t *testing.T) {
	m := newTestModelWithTunnel()
	output := m.View().Content

	hasStyling := ansiRegex.MatchString(output) || strings.Contains(output, "Running")
	if !hasStyling {
		t.Error("View output should contain visual styling indicators")
	}
}

func TestViewSelectedTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	m.manager.Create("second-tunnel", cfg, tunnel.Local, "9090", "90")

	m.selectedIndex = 1
	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "second-tunnel") {
		t.Error("Selected tunnel should be visible")
	}
}

func TestViewLatencyDisplay(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]

	tun.Latency = 45 * 1000000 // 45ms
	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "45ms") {
		t.Error("View should show latency when available")
	}
}

func TestViewStoppedTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]
	tun.Status = tunnel.StatusStopped

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Stopped") {
		t.Error("View should show 'Stopped' status")
	}
}

func TestViewStatusMessage(t *testing.T) {
	m := newTestModelWithTunnel()
	m.statusMsg = "Tunnel must be running"
	m.statusTicks = 3
	output := m.View().Content
	clean := stripANSI(output)
	if !strings.Contains(clean, "Tunnel must be running") {
		t.Error("View should show status message")
	}
}

func TestViewSFTPShortcuts(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSFTP
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "test.txt", IsDir: false}}
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "remote.txt", IsDir: false}}
	output := m.View().Content
	clean := stripANSI(output)
	for _, s := range []string{"Open", "Sync", "Sync Dir", "Rename", "Preview", "Close"} {
		if !strings.Contains(clean, s) {
			t.Errorf("SFTP view should contain shortcut '%s'", s)
		}
	}
}

func TestViewSFTPNoTabBar(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSFTP
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "test.txt", IsDir: false}}
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "remote.txt", IsDir: false}}

	clean := stripANSI(m.View().Content)
	lines := strings.Split(clean, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected SFTP view lines, got %d", len(lines))
	}
	if strings.Contains(lines[1], "LOCAL") || strings.Contains(lines[1], "REMOTE") {
		t.Fatalf("SFTP should not render a standalone tab row: %q", lines[1])
	}
	if !strings.Contains(clean, "LOCAL") || !strings.Contains(clean, "REMOTE") {
		t.Fatal("SFTP panel labels should remain visible")
	}
}

func TestViewSFTPTopBarKeepsSingleModeTag(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSFTP
	m.sftpHostLabel = "user@example.com:22"

	firstLine := strings.Split(stripANSI(m.View().Content), "\n")[0]
	if strings.Count(firstLine, "SFTP") != 1 {
		t.Fatalf("SFTP top bar should not duplicate the mode label: %q", firstLine)
	}
	if !strings.Contains(firstLine, "v1.0.0") {
		t.Fatalf("version tag should remain visible: %q", firstLine)
	}
}

func TestRenderSFTPPanelSplitsTitleAndPath(t *testing.T) {
	m := newTestModelWithTunnel()
	m.sftpLocalDir = "/Users/example/projects/intun"
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "file.txt", Size: 1024}}

	clean := stripANSI(m.renderSFTPPanel("LOCAL", m.sftpLocalDir, m.sftpLocalFiles, 0, 48, 12, true))
	lines := strings.Split(clean, "\n")

	labelLine := -1
	pathLine := -1
	for i, line := range lines {
		if strings.Contains(line, "LOCAL") && strings.Contains(line, "1 items") {
			labelLine = i
		}
		if strings.Contains(line, "Users") && strings.Contains(line, "intun") {
			pathLine = i
		}
	}
	if labelLine < 0 {
		t.Fatalf("panel should render label and item count on the title line:\n%s", clean)
	}
	if pathLine < 0 {
		t.Fatalf("panel should render the current path:\n%s", clean)
	}
	if labelLine == pathLine {
		t.Fatalf("panel path should be on a dedicated line, got:\n%s", clean)
	}
	if !strings.Contains(clean, "1-1/1") {
		t.Fatalf("panel range should count real entries, not parent row:\n%s", clean)
	}
}

func TestRenderSFTPEntryLineUsesSubtleCursorAndParentPath(t *testing.T) {
	m := newTestModelWithTunnel()

	clean := stripANSI(m.renderSFTPEntryLine(nil, 0, 0, 40, true))
	if !strings.Contains(clean, "› ../") {
		t.Fatalf("parent entry should use subtle cursor and ../ label, got %q", clean)
	}
	if strings.Contains(clean, "❯") {
		t.Fatalf("entry line should not use old heavy cursor marker: %q", clean)
	}
	if strings.Contains(clean, "/ ..") {
		t.Fatalf("entry line should not use old directory marker column: %q", clean)
	}
}

func TestRenderSFTPEntryLineShowsColumnsAndFileKinds(t *testing.T) {
	m := newTestModelWithTunnel()
	modTime := time.Date(2026, 6, 25, 23, 9, 0, 0, time.UTC)
	files := []sftp.FileEntry{
		{Name: "config", IsDir: true, Mode: os.ModeDir | 0755, ModTime: modTime},
		{Name: "current", Mode: os.ModeSymlink | 0777, ModTime: modTime},
	}

	dirLine := stripANSI(m.renderSFTPEntryLine(files, 1, 0, 72, true))
	if !strings.Contains(dirLine, "config/") ||
		!strings.Contains(dirLine, "drwxr-xr-x") ||
		!strings.Contains(dirLine, "DIR") ||
		!strings.Contains(dirLine, "06-25 23:09") {
		t.Fatalf("directory line should show name, type and modified column: %q", dirLine)
	}
	linkLine := stripANSI(m.renderSFTPEntryLine(files, 2, 0, 72, true))
	if !strings.Contains(linkLine, "current") || !strings.Contains(linkLine, "Lrwxrwxrwx") || !strings.Contains(linkLine, "LINK") {
		t.Fatalf("symlink line should show link type token: %q", linkLine)
	}
}

func TestRenderSFTPPanelShowsInternalScrollMarker(t *testing.T) {
	m := newTestModelWithTunnel()
	files := make([]sftp.FileEntry, 20)
	for i := range files {
		files[i] = sftp.FileEntry{Name: fmt.Sprintf("file-%02d.txt", i), Size: int64(i + 1), Mode: 0644}
	}
	m.sftpScroll[0] = 6
	m.sftpCursor[0] = 7

	clean := stripANSI(m.renderSFTPPanel("LOCAL", "/tmp", files, 0, 52, 10, true))
	if !strings.Contains(clean, "┃") {
		t.Fatalf("long SFTP panel should render an internal scroll marker:\n%s", clean)
	}
	for _, line := range strings.Split(clean, "\n") {
		if got := lipgloss.Width(line); got > 52 {
			t.Fatalf("panel line width = %d, want <= 52: %q", got, line)
		}
	}
}

func TestRenderSFTPSelectedDetailShowsFocusedEntry(t *testing.T) {
	m := newTestModelWithTunnel()
	m.sftpFocus = 1
	m.sftpRemoteDir = "/home/example"
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "remote.txt", Size: 2048, Mode: 0644}}
	m.sftpCursor[1] = 1

	clean := stripANSI(m.renderSFTPSelectedDetail(90))
	for _, want := range []string{"REMOTE", "selected", "remote.txt", "file", "-rw-r--r--", "2.00 KB"} {
		if !strings.Contains(clean, want) {
			t.Fatalf("selected detail missing %q: %q", want, clean)
		}
	}
}

func TestRenderShortcutsDoesNotLeaveTrailingSeparator(t *testing.T) {
	m := newTestModelWithTunnel()
	m.width = 120
	m.screen = ScreenSFTP

	clean := strings.TrimSpace(stripANSI(m.renderShortcuts()))
	if strings.HasSuffix(clean, "·") {
		t.Fatalf("shortcut bar should not leave a trailing separator: %q", clean)
	}
}

func TestRenderMainShortcutsFollowSelectedTunnelState(t *testing.T) {
	m := newTestModelWithTunnel()
	m.width = 120
	tun := m.manager.List()[0]

	running := stripANSI(m.renderShortcuts())
	if !strings.Contains(running, "SFTP") || !strings.Contains(running, "Stop") {
		t.Fatalf("running tunnel shortcuts should prioritize SFTP and Stop: %q", running)
	}

	tun.Status = tunnel.StatusError
	errored := stripANSI(m.renderShortcuts())
	if !strings.Contains(errored, "Reconnect") {
		t.Fatalf("error tunnel shortcuts should prioritize reconnect: %q", errored)
	}
	if strings.Contains(errored, "SFTP") || strings.Contains(errored, "Stop") {
		t.Fatalf("error tunnel shortcuts should hide running-only actions: %q", errored)
	}

	tun.Status = tunnel.StatusStopped
	stopped := stripANSI(m.renderShortcuts())
	if !strings.Contains(stopped, "Start") {
		t.Fatalf("stopped tunnel shortcuts should show Start: %q", stopped)
	}
}

func TestViewSFTPStatusUsesModalMask(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSFTP
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "panel-local.bin", IsDir: false}}
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "panel-remote.bin", IsDir: false}}
	m.statusMsg = "SFTP upload complete\nFROM: /local/test.txt\nTO: /remote/test.txt"
	m.statusTicks = 3

	clean := stripANSI(m.View().Content)
	if !strings.Contains(clean, "Notice") || !strings.Contains(clean, "SFTP upload complete") {
		t.Fatalf("expected SFTP status modal, got:\n%s", clean)
	}
	if strings.Contains(clean, "panel-local.bin") || strings.Contains(clean, "panel-remote.bin") {
		t.Fatalf("SFTP modal mask should hide panel content:\n%s", clean)
	}
}

func TestQuitConfirmWithRunningTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]
	tun.Status = tunnel.StatusRunning
	output := m.View().Content
	clean := stripANSI(output)
	if strings.Contains(clean, "Active tunnels are still running") {
		t.Error("Should not show quit confirmation before pressing q")
	}

	m.confirmQuit = true
	output = m.View().Content
	clean = stripANSI(output)
	if !strings.Contains(clean, "Active tunnels are still running") {
		t.Error("Should show quit confirmation when confirmQuit is true")
	}
}

func TestViewSFTPRenameInput(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSFTP
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "old.txt", IsDir: false}}
	m.sftpRemoteFiles = []sftp.FileEntry{}
	m.sftpRenaming = true
	m.sftpRenameInput = "new.txt"
	output := m.View().Content
	clean := stripANSI(output)
	if !strings.Contains(clean, "Rename: new.txt_") {
		t.Error("Should show rename input")
	}
	if !strings.Contains(clean, "Confirm") {
		t.Error("Should show confirm hint")
	}
}

func TestRenderSFTPEntryLineFitsWidthWithLargeSize(t *testing.T) {
	m := newTestModelWithTunnel()
	width := 24
	line := m.renderSFTPEntryLine(
		[]sftp.FileEntry{{Name: "very-long-file-name-that-needs-truncation.bin", Size: 123456789}},
		1,
		1,
		width,
		true,
	)
	clean := stripANSI(line)

	if got, wantMax := lipgloss.Width(clean), width-4; got > wantMax {
		t.Fatalf("entry line width = %d, want <= %d: %q", got, wantMax, clean)
	}
	if !strings.Contains(clean, "MB") {
		t.Fatalf("entry line should keep size visible, got %q", clean)
	}
}

func TestRenderSFTPProgressClampsPercent(t *testing.T) {
	m := newTestModelWithTunnel()
	m.sftpDirection = "↑"
	m.sftpProgress = sftp.NewProgressInfo("file.txt", 10)
	m.sftpProgress.SetDone(20)

	clean := stripANSI(m.renderSFTPProgress(20))
	if !strings.Contains(clean, "100%") {
		t.Fatalf("progress should clamp over-complete transfer to 100%%, got %q", clean)
	}
}

func TestRenderSFTPProgressFitsNarrowWidthWithLongSpeed(t *testing.T) {
	m := newTestModelWithTunnel()
	m.sftpDirection = "↓"
	m.sftpProgress = sftp.NewProgressInfo("very-long-file-name-that-needs-middle-truncation.tar.gz", 100)
	m.sftpProgress.SetDone(50)
	m.sftpProgress.SetSpeed(123456789)

	const width = 20
	clean := stripANSI(m.renderSFTPProgress(width))
	for _, line := range strings.Split(clean, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("progress line width = %d, want <= %d: %q", got, width, line)
		}
	}
}

func TestViewSFTPLayoutFitsNarrowWidth(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenSFTP
	m.width = 80
	m.height = 20
	m.sftpLocalDir = "/Users/example/projects/a-very-long-local-directory-name"
	m.sftpRemoteDir = "/home/example/a-very-long-remote-directory-name"
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "very-long-local-file-name-that-needs-truncation.bin", Size: 987654321}}
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "very-long-remote-file-name-that-needs-truncation.bin", Size: 123456789}}

	clean := stripANSI(m.View().Content)
	for _, line := range strings.Split(clean, "\n") {
		if got := lipgloss.Width(line); got > 80 {
			t.Fatalf("SFTP view line width = %d, want <= 80: %q", got, line)
		}
	}
}

func TestRenderTunnelSummaryFitsWidth(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]
	tun.UpdateStats(1024, 2048, 100, 200, 50*time.Millisecond, true)

	clean := stripANSI(m.renderTunnelSummary(80, 1))
	for _, line := range strings.Split(clean, "\n") {
		if got := lipgloss.Width(line); got > 80 {
			t.Fatalf("summary line width = %d, want <= 80: %q", got, line)
		}
	}
}

func TestRenderTunnelFlowLineSuspendsWhenNotRunning(t *testing.T) {
	tun := &tunnel.Tunnel{Status: tunnel.StatusError, Error: "failed"}
	clean := stripANSI(renderTunnelFlowLine(tun, []int64{1, 2, 3}, 40))
	if !strings.Contains(clean, "flow paused") {
		t.Fatalf("error tunnel should show paused flow state: %q", clean)
	}
	if strings.Contains(clean, "▁") || strings.Contains(clean, "·") {
		t.Fatalf("error tunnel should not render a live flow graph: %q", clean)
	}
}

func TestViewConnectingTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]
	tun.Status = tunnel.StatusConnecting

	output := m.View().Content
	clean := stripANSI(output)

	if !strings.Contains(clean, "Connecting") {
		t.Error("View should show 'Connecting' status")
	}
}
