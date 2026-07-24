package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/sftp"
	"github.com/spance/intun/internal/tunnel"
)

func TestParseManualSSHHost(t *testing.T) {
	tests := []struct {
		input string
		user  string
		host  string
		port  string
	}{
		{"alice@example.com", "alice", "example.com", "22"},
		{"alice@example.com:2222", "alice", "example.com", "2222"},
		{"alice@[2001:db8::1]:2200", "alice", "2001:db8::1", "2200"},
		{"alice@2001:db8::1", "alice", "2001:db8::1", "22"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			host, err := parseManualSSHHost(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if host.User != test.user || host.Hostname != test.host || host.Port != test.port {
				t.Fatalf("parsed host = %#v", host)
			}
		})
	}
	for _, input := range []string{"", "@example.com", "alice@", "alice@example.com:0", "alice@example.com:70000", "alice@[::1"} {
		if _, err := parseManualSSHHost(input); err == nil {
			t.Fatalf("parseManualSSHHost(%q) should fail", input)
		}
	}
}

func TestHostFilterAndManualHostFlow(t *testing.T) {
	hosts := []config.Host{
		{Name: "prod-api", Hostname: "api.example.com", User: "ops", Port: "22", Labels: []string{"production"}},
		{Name: "dev-db", Hostname: "db.internal", User: "dev", Port: "2222"},
	}
	m := newTestModel(hosts)
	m.screen = ScreenSelectHost
	m = updateModel(m, keyMsg("/"))
	m = updateModel(m, keyMsg("production"))
	filtered := m.filteredHosts()
	if len(filtered) != 1 || filtered[0].Name != "prod-api" {
		t.Fatalf("filtered hosts = %#v", filtered)
	}
	m = updateModel(m, keyEsc())
	if m.hostFiltering || m.hostFilter.Value() != "" {
		t.Fatal("Esc should clear host filtering")
	}

	empty := newTestModel(nil)
	empty = updateModel(empty, keyMsg("c"))
	empty = updateModel(empty, keyMsg("alice@example.com:2222"))
	empty = updateModel(empty, keyEnter())
	if empty.screen != ScreenSelectType || empty.selectedHost.User != "alice" || empty.selectedHost.Port != "2222" {
		t.Fatalf("manual host state = screen:%v host:%#v", empty.screen, empty.selectedHost)
	}
}

func TestPassphrasePromptAcceptsText(t *testing.T) {
	m := newTestModel(nil)
	response := make(chan platform.AuthResponse, 1)
	request := platform.AuthRequest{
		Type:     platform.AuthRequestPassphrase,
		Host:     "alice@example.com key",
		Response: response,
	}
	m.authQueue.requestChan <- request
	m.authQueue.Poll()
	m = updateModel(m, authRequestMsg{request: request})
	m = updateModel(m, keyMsg("s3cret"))
	m = updateModel(m, keyEnter())
	select {
	case got := <-response:
		if !got.Accept || got.Password != "s3cret" {
			t.Fatalf("passphrase response = %#v", got)
		}
	default:
		t.Fatal("passphrase response was not delivered")
	}
}

func TestModelCloseCancelsRootContext(t *testing.T) {
	m := newTestModel(nil)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-m.cancelCtx.Done():
	default:
		t.Fatal("Close did not cancel the model context")
	}
}

func TestResponsiveViewsStayInsideTerminal(t *testing.T) {
	m := newTestModel(nil)
	m.width = 40
	m.height = 12
	for _, line := range strings.Split(m.View().Content, "\n") {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("main line width = %d: %q", width, stripANSI(line))
		}
	}
	_, _ = m.manager.Create(
		"narrow",
		&platform.SSHConfig{Host: "example.com", Port: "22", User: "user"},
		tunnel.Local,
		"127.0.0.1:8080",
		"192.0.2.15:22",
	)
	for _, line := range strings.Split(m.View().Content, "\n") {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("narrow tunnel line width = %d: %q", width, stripANSI(line))
		}
	}

	m.screen = ScreenSFTP
	m.sftpFocus = 0
	m.sftpLocalDir = "/local"
	m.sftpRemoteDir = "/remote"
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "local.txt"}}
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "remote.txt"}}
	rawSFTP := m.renderSFTPScreen()
	rendered := stripANSI(rawSFTP)
	if !strings.Contains(rendered, "LOCAL") || strings.Contains(rendered, "REMOTE  1 items") {
		t.Fatalf("narrow SFTP should render only the focused panel:\n%s", rendered)
	}
	for _, line := range strings.Split(rawSFTP, "\n") {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("SFTP line width = %d: %q", width, stripANSI(line))
		}
	}
}

func TestShortcutBarUsesRenderedHeightAndStaysOnLastRow(t *testing.T) {
	m := newTestModel(nil)
	m.width = 80
	m.height = 18
	m.err = fmt.Errorf("first line\nsecond line")

	output := m.View().Content
	if got := lipgloss.Height(output); got != m.height {
		t.Fatalf("rendered height = %d, want %d", got, m.height)
	}
	lines := strings.Split(stripANSI(output), "\n")
	if !strings.Contains(lines[len(lines)-1], "Quit") {
		t.Fatalf("shortcut bar is not on the last row: %q", lines[len(lines)-1])
	}
}

func TestTunnelViewportKeepsLargeSelectionVisible(t *testing.T) {
	m := newTestModel(nil)
	m.width = 100
	m.height = 16
	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	for index := range 30 {
		m.manager.Create("tunnel", cfg, tunnel.Local, "127.0.0.1:0", "127.0.0.1:22")
		m.selectedIndex = index
	}
	rendered := stripANSI(m.renderMainScreen())
	if !strings.Contains(rendered, "#30") {
		t.Fatalf("selected tunnel should remain visible:\n%s", rendered)
	}
	if strings.Contains(rendered, "#1 ") {
		t.Fatalf("viewport should clip distant rows:\n%s", rendered)
	}
}

func TestSFTPAsyncPreflightAndStaleNavigation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	if err := osWriteFile(source, []byte("data")); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(nil)
	pending := sftpPendingSync{
		focus:  0,
		source: source,
		target: "/remote/source.txt",
		name:   "source.txt",
		size:   4,
	}
	updated, cmd := m.preflightSingleSync(pending)
	m = runCommandChain(t, updated, cmd)
	if !strings.Contains(m.statusMsg, "not connected") {
		t.Fatalf("preflight status = %q", m.statusMsg)
	}

	m.sftpLocalDir = dir
	updated, cmd = m.navigateSFTP(0, dir)
	m = updated
	m.sftpOperationID++
	m = runCommandChain(t, m, cmd)
	if m.sftpLocalDir != dir || !m.sftpLoading {
		t.Fatalf("stale navigation should not replace current operation: dir=%q loading=%v", m.sftpLocalDir, m.sftpLoading)
	}
}

func TestSFTPAsyncResultHandlers(t *testing.T) {
	m := newTestModel(nil)
	m.sftpOperationID = 1
	m.sftpLoading = true
	client := sftp.NewClient(nil)
	t.Cleanup(func() { _ = client.Close() })
	m, cmd := m.handleSFTPOpenResult(sftpOpenResultMsg{
		operationID: 1,
		client:      client,
		localDir:    "/local",
		remoteDir:   "/remote",
		tunnelID:    7,
		hostLabel:   "alice@example.com:22",
	})
	if cmd != nil || m.screen != ScreenSFTP || m.sftpTunnelID != 7 || m.sftpLoading {
		t.Fatalf("open result state = screen:%v tunnel:%d loading:%v", m.screen, m.sftpTunnelID, m.sftpLoading)
	}

	m.sftpOperationID = 2
	m.sftpLoading = true
	pending := sftpPendingSync{focus: 0, source: "/local/a", target: "/remote/a"}
	m, cmd = m.handleSFTPSinglePreflightResult(sftpSinglePreflightResultMsg{
		operationID: 2,
		pending:     pending,
		overwrites: sftp.OverwriteReport{
			Count: 1,
			Items: []sftp.ExistingItem{{Path: "/remote/a", Kind: "file"}},
		},
	})
	if cmd != nil || !m.sftpOverwriteConfirm || m.sftpPendingSync.target != "/remote/a" {
		t.Fatal("overwrite preflight should open the confirmation modal")
	}

	m.sftpOperationID = 3
	m.sftpLoading = true
	plan := sftp.SyncPlan{SourceRoot: "/local/dir", TargetRoot: "/remote/dir", TotalBytes: 10}
	m = m.handleSFTPPlanResult(sftpPlanResultMsg{
		operationID: 3,
		focus:       0,
		source:      plan.SourceRoot,
		target:      plan.TargetRoot,
		plan:        plan,
	})
	if !m.sftpSyncConfirm || m.sftpPendingDirPlan == nil {
		t.Fatal("directory plan should open the sync confirmation modal")
	}

	m.sftpOperationID = 4
	m.sftpLoading = true
	m = m.handleSFTPRefreshResult(sftpRefreshResultMsg{
		operationID: 4,
		localFiles:  []sftp.FileEntry{{Name: "local.txt"}},
		remoteFiles: []sftp.FileEntry{{Name: "remote.txt"}},
	})
	if len(m.sftpLocalFiles) != 1 || len(m.sftpRemoteFiles) != 1 || m.sftpLoading {
		t.Fatalf("refresh result was not applied: local=%v remote=%v", m.sftpLocalFiles, m.sftpRemoteFiles)
	}
}

func TestRenameLocalNoReplacePreservesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(source, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := renameLocalNoReplace(source, target); err == nil {
		t.Fatal("rename should reject an existing target")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "target" {
		t.Fatalf("target content = %q, want target", data)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source should remain after rejected rename: %v", err)
	}
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}
