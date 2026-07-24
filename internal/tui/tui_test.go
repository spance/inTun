package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/sftp"
	"github.com/spance/intun/internal/testutil"
	"github.com/spance/intun/internal/tunnel"
)

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Text: s, Code: []rune(s)[0]})
}

func keyEnter() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
}

func keyEsc() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
}

func keyTab() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
}

func keyDown() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
}

func keyUp() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
}

func keyBackspace() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace})
}

func keyPgDown() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
}

func keyPgUp() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp})
}

func updateModel(m Model, msg tea.Msg) Model {
	result, _ := m.Update(msg)
	return result.(Model)
}

func runCommandChain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		result, next := m.Update(msg)
		m = result.(Model)
		cmd = next
	}
	return m
}

func newTestModel(hosts []config.Host) Model {
	mockExec := testutil.NewMockExecutor()
	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)
	return NewModel(hosts, m, "v1.0.0-test")
}

func TestModelInit(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a command")
	}
}

func TestModelScreenTransition(t *testing.T) {
	hosts := []config.Host{
		{Name: "host1", Hostname: "host1.example.com", User: "user1", Port: "22"},
		{Name: "host2", Hostname: "host2.example.com", User: "user2", Port: "22"},
	}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	if m.screen != ScreenMain {
		t.Errorf("initial screen = %v, want %v", m.screen, ScreenMain)
	}

	m = updateModel(m, keyMsg("c"))
	if m.screen != ScreenSelectHost {
		t.Errorf("after 'c' key, screen = %v, want %v", m.screen, ScreenSelectHost)
	}

	m = updateModel(m, keyEnter())
	if m.screen != ScreenSelectType {
		t.Errorf("after Enter in host select, screen = %v, want %v", m.screen, ScreenSelectType)
	}

	m = updateModel(m, keyEnter())
	if m.screen != ScreenInputPort {
		t.Errorf("after Enter in type select, screen = %v, want %v", m.screen, ScreenInputPort)
	}
}

func TestModelBackNavigation(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	if m.screen != ScreenSelectHost {
		t.Errorf("'c' should go to ScreenSelectHost, got %v", m.screen)
	}

	m = updateModel(m, keyEsc())
	if m.screen != ScreenMain {
		t.Errorf("Esc from host select should return to main screen, got %v", m.screen)
	}

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())
	if m.screen != ScreenSelectType {
		t.Errorf("Enter from host select should go to ScreenSelectType, got %v", m.screen)
	}

	m = updateModel(m, keyEsc())
	if m.screen != ScreenSelectHost {
		t.Errorf("Esc from type select should return to host select, got %v", m.screen)
	}

	m = updateModel(m, keyEnter())
	if m.screen != ScreenSelectType {
		t.Errorf("Enter should go to ScreenSelectType again, got %v", m.screen)
	}

	m = updateModel(m, keyEnter())
	if m.screen != ScreenInputPort {
		t.Errorf("Enter from type select should go to ScreenInputPort, got %v", m.screen)
	}

	m = updateModel(m, keyEsc())
	if m.screen != ScreenSelectType {
		t.Errorf("Esc from port input should return to type select, got %v", m.screen)
	}
}

func TestHostSelectScrollsLongList(t *testing.T) {
	hosts := make([]config.Host, 40)
	for i := range hosts {
		hosts[i] = config.Host{
			Name:     fmt.Sprintf("host-%02d", i),
			Hostname: fmt.Sprintf("host-%02d.example.com", i),
			User:     "user",
			Port:     "22",
		}
	}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 20
	m.screen = ScreenSelectHost

	visible := hostSelectVisibleItems(m.height)
	if visible >= len(hosts) {
		t.Fatalf("visible items = %d, want fewer than host count", visible)
	}

	m = updateModel(m, keyPgDown())
	if m.hostCursor != visible {
		t.Fatalf("hostCursor = %d, want %d after PgDown", m.hostCursor, visible)
	}
	if m.hostScroll == 0 {
		t.Fatal("hostScroll should move after PgDown")
	}

	clean := stripANSI(m.renderHostSelect())
	if !strings.Contains(clean, fmt.Sprintf("%d-", m.hostScroll+1)) {
		t.Fatalf("host list should render scroll range, got:\n%s", clean)
	}

	m = updateModel(m, keyPgUp())
	if m.hostCursor != 0 {
		t.Fatalf("hostCursor = %d, want 0 after PgUp", m.hostCursor)
	}
}

func TestModelPortInput(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyEnter())

	m = updateModel(m, keyMsg("8"))
	m = updateModel(m, keyMsg("0"))
	m = updateModel(m, keyMsg("8"))
	m = updateModel(m, keyMsg("0"))

	if m.portInput != "8080" {
		t.Errorf("portInput = %q, want %q", m.portInput, "8080")
	}

	m = updateModel(m, keyBackspace())
	if m.portInput != "808" {
		t.Errorf("after backspace, portInput = %q, want %q", m.portInput, "808")
	}
}

func TestModelPortInputPasteAddress(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyEnter())

	m = updateModel(m, keyMsg("127.0.0.1:5555"))
	if m.portInput != "127.0.0.1:5555" {
		t.Fatalf("local listen paste portInput = %q, want %q", m.portInput, "127.0.0.1:5555")
	}

	m = updateModel(m, keyEnter())
	m = updateModel(m, keyMsg("192.0.2.15:5551"))
	if m.portInput != "192.0.2.15:5551" {
		t.Fatalf("remote target paste portInput = %q, want %q", m.portInput, "192.0.2.15:5551")
	}
}

func TestModelDynamicPortInputPasteFiltersAddress(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())

	for i := 0; i < 4; i++ {
		m = updateModel(m, keyDown())
	}
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyMsg("127.0.0.1:1080"))

	if m.portInput != "1270011080" {
		t.Fatalf("dynamic paste portInput = %q, want only digits", m.portInput)
	}
}

func TestModelRejectsEmptyPortInput(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyEnter())

	if m.screen != ScreenInputPort {
		t.Errorf("screen = %v, want ScreenInputPort", m.screen)
	}
	if m.err == nil {
		t.Fatal("empty port input should set an error")
	}
	if got := len(m.manager.List()); got != 0 {
		t.Fatalf("tunnel count = %d, want 0", got)
	}
}

func TestModelDynamicTunnelPortInput(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())

	for i := 0; i < 4; i++ {
		m = updateModel(m, keyDown())
	}

	m = updateModel(m, keyEnter())
	if m.selectedType != tunnel.Dynamic {
		t.Errorf("selectedType = %v, want %v", m.selectedType, tunnel.Dynamic)
	}

	m = updateModel(m, keyMsg("1"))
	m = updateModel(m, keyMsg("0"))
	m = updateModel(m, keyMsg("8"))
	m = updateModel(m, keyMsg("0"))

	if m.portInput != "1080" {
		t.Errorf("portInput = %q, want %q", m.portInput, "1080")
	}
}

func TestModelCreatesLocalUDPTunnel(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyDown())
	m = updateModel(m, keyEnter())
	if m.selectedType != tunnel.Local || m.selectedProtocol != tunnel.UDP {
		t.Fatalf("selection = %s/%s, want Local/UDP", m.selectedType, m.selectedProtocol)
	}
	m = updateModel(m, keyMsg("5353"))
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyMsg("53"))
	m = updateModel(m, keyEnter())

	tunnels := m.manager.List()
	if len(tunnels) != 1 {
		t.Fatalf("tunnel count = %d, want 1", len(tunnels))
	}
	if tunnels[0].Forward.Protocol != tunnel.UDP || tunnels[0].Forward.LocalAddr != "127.0.0.1:5353" || tunnels[0].Forward.RemoteAddr != "127.0.0.1:53" {
		t.Fatalf("UDP tunnel = %#v", tunnels[0])
	}
}

func TestModelCreatesRemoteUDPTunnel(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())
	for i := 0; i < 3; i++ {
		m = updateModel(m, keyDown())
	}
	m = updateModel(m, keyEnter())
	if m.selectedType != tunnel.Remote || m.selectedProtocol != tunnel.UDP {
		t.Fatalf("selection = %s/%s, want Remote/UDP", m.selectedType, m.selectedProtocol)
	}
	m = updateModel(m, keyMsg("53"))
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyMsg("5353"))
	m = updateModel(m, keyEnter())

	tunnels := m.manager.List()
	if len(tunnels) != 1 {
		t.Fatalf("tunnel count = %d, want 1", len(tunnels))
	}
	forward := tunnels[0].Forward
	if forward.Type != tunnel.Remote || forward.Protocol != tunnel.UDP || forward.LocalAddr != "127.0.0.1:53" || forward.RemoteAddr != "127.0.0.1:5353" {
		t.Fatalf("Remote UDP forward = %#v", forward)
	}
}

func TestModelConfirmsNonLoopbackRemoteUDPBind(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())
	for i := 0; i < 3; i++ {
		m = updateModel(m, keyDown())
	}
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyMsg("53"))
	m = updateModel(m, keyEnter())
	m = updateModel(m, keyMsg("0.0.0.0:5353"))
	m = updateModel(m, keyEnter())

	if m.pendingTunnelCreate == nil {
		t.Fatal("non-loopback Remote UDP bind should require confirmation")
	}
	if len(m.manager.List()) != 0 {
		t.Fatal("Remote UDP tunnel should not be created before confirmation")
	}
	clean := stripANSI(m.View().Content)
	if !strings.Contains(clean, "Expose Remote UDP Port?") || !strings.Contains(clean, "0.0.0.0:5353") || !strings.Contains(clean, "127.0.0.1:53") {
		t.Fatalf("confirmation modal does not show operation direction and endpoints:\n%s", clean)
	}
	m = updateModel(m, keyMsg("y"))
	if m.pendingTunnelCreate != nil || m.screen != ScreenMain || len(m.manager.List()) != 1 {
		t.Fatalf("confirmed Remote UDP creation state = pending:%v screen:%v tunnels:%d", m.pendingTunnelCreate != nil, m.screen, len(m.manager.List()))
	}
}

func TestRemoteUDPBindConfirmationPolicy(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:5353", "[::1]:5353"} {
		if remoteUDPBindRequiresConfirmation(addr) {
			t.Fatalf("loopback bind %q should not require exposure confirmation", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:5353", "192.0.2.10:5353", "bad-address"} {
		if !remoteUDPBindRequiresConfirmation(addr) {
			t.Fatalf("non-loopback bind %q should require exposure confirmation", addr)
		}
	}
}

func TestModelQuit(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)

	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Error("'q' key should trigger quit command")
	}
}

func TestModelQuitConfirmsWithLiveTunnel(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	m.manager.Create("live-tunnel", cfg, tunnel.Local, "8080", "80")

	result, cmd := m.Update(keyMsg("q"))
	m = result.(Model)
	if cmd != nil {
		t.Fatal("'q' with a live tunnel should open confirmation instead of quitting immediately")
	}
	if !m.confirmQuit {
		t.Fatal("confirmQuit should be true with a live tunnel")
	}

	result, cmd = m.Update(keyEsc())
	m = result.(Model)
	if cmd != nil {
		t.Fatal("Esc should cancel quit confirmation, not quit")
	}
	if m.confirmQuit {
		t.Fatal("confirmQuit should be false after Esc")
	}

	result, _ = m.Update(keyMsg("q"))
	m = result.(Model)
	if !m.confirmQuit {
		t.Fatal("confirmQuit should reopen on q")
	}

	_, cmd = m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("second q should confirm quit")
	}
}

func TestModelQuitDoesNotConfirmStoppedTunnel(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, _ := m.manager.Create("stopped-tunnel", cfg, tunnel.Local, "8080", "80")
	m.manager.Stop(tun.ID())

	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("'q' with only stopped tunnels should quit immediately")
	}
}

func TestModelNavigateTunnels(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	mockExec := testutil.NewMockExecutor()
	m.manager.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	m.manager.Create("tunnel1", cfg, tunnel.Local, "8080", "80")
	m.manager.Create("tunnel2", cfg, tunnel.Local, "9090", "90")
	m.manager.Create("tunnel3", cfg, tunnel.Local, "7070", "70")

	m.selectedIndex = 1

	m = updateModel(m, keyUp())
	if m.selectedIndex != 0 {
		t.Errorf("after up, selectedIndex = %d, want 0", m.selectedIndex)
	}

	m = updateModel(m, keyDown())
	if m.selectedIndex != 1 {
		t.Errorf("after down, selectedIndex = %d, want 1", m.selectedIndex)
	}

	m = updateModel(m, keyDown())
	if m.selectedIndex != 2 {
		t.Errorf("after down again, selectedIndex = %d, want 2", m.selectedIndex)
	}
}

func TestModelStopStartTunnel(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, _ := m.manager.Create("test-tunnel", cfg, tunnel.Local, "8080", "80")
	m.selectedIndex = 0

	m = updateModel(m, keyMsg("s"))
	if tun.GetStatus() != tunnel.StatusStopped {
		t.Errorf("after 's', tunnel status = %v, want %v", tun.GetStatus(), tunnel.StatusStopped)
	}

	m.manager.Restart(tun.ID())
	m = updateModel(m, keyMsg("s"))
	if tun.GetStatus() != tunnel.StatusStopped {
		t.Errorf("'s' on running tunnel should stop it")
	}
}

func TestModelDeleteTunnel(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	m.manager.Create("t1", cfg, tunnel.Local, "8080", "80")
	m.manager.Create("t2", cfg, tunnel.Local, "9090", "90")
	m.selectedIndex = 1

	m = updateModel(m, keyMsg("d"))

	tunnels := m.manager.List()
	if len(tunnels) != 1 {
		t.Errorf("after delete, tunnels count = %d, want 1", len(tunnels))
	}
}

func TestModelNoHostsOpensManualConnection(t *testing.T) {
	m := newTestModel([]config.Host{})
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	if m.err != nil || m.screen != ScreenInputHost || !m.manualHostInput.Focused() {
		t.Fatalf("'c' with no hosts should open manual input, screen=%v err=%v focused=%v", m.screen, m.err, m.manualHostInput.Focused())
	}
}

func TestAuthPromptQueueBasic(t *testing.T) {
	q := NewAuthPromptQueue()

	respChan := make(chan platform.AuthResponse, 1)
	req := platform.AuthRequest{
		ID:          1,
		Type:        platform.AuthRequestHostKey,
		Host:        "test@example.com",
		Fingerprint: "SHA256:test",
		Response:    respChan,
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		q.requestChan <- req
	}()

	time.Sleep(20 * time.Millisecond)

	polled := q.Poll()
	if polled.Response == nil {
		t.Error("Poll should return the queued request")
	}

	if polled.Host != "test@example.com" {
		t.Errorf("poll.Host = %q, want %q", polled.Host, "test@example.com")
	}

	q.Complete(platform.AuthResponse{Accept: true})

	select {
	case resp := <-respChan:
		if !resp.Accept {
			t.Error("response should have Accept = true")
		}
	default:
		t.Error("response should be sent to response channel")
	}
}

func TestAuthPromptQueuePollDoesNotRepeatCurrent(t *testing.T) {
	q := NewAuthPromptQueue()

	req := platform.AuthRequest{
		ID:       1,
		Type:     platform.AuthRequestPassword,
		Host:     "user@example.com",
		Response: make(chan platform.AuthResponse, 1),
	}

	q.requestChan <- req
	first := q.Poll()
	if first.Response == nil {
		t.Fatal("first Poll should return request")
	}

	second := q.Poll()
	if second.Response != nil {
		t.Fatal("second Poll should not return the same request again")
	}
}

func TestAuthPromptQueueQueuesWhileCurrentActive(t *testing.T) {
	q := NewAuthPromptQueue()

	req1 := platform.AuthRequest{ID: 1, Host: "one@example.com", Response: make(chan platform.AuthResponse, 1)}
	req2 := platform.AuthRequest{ID: 2, Host: "two@example.com", Response: make(chan platform.AuthResponse, 1)}

	q.requestChan <- req1
	if got := q.Poll(); got.Host != req1.Host {
		t.Fatalf("first Poll host = %q, want %q", got.Host, req1.Host)
	}

	q.requestChan <- req2
	if got := q.Poll(); got.Response != nil {
		t.Fatal("Poll should not surface a second request while current is active")
	}
	if pending := q.PendingCount(); pending != 1 {
		t.Fatalf("pending count = %d, want 1", pending)
	}

	q.Complete(platform.AuthResponse{Accept: true})
	if got := q.Poll(); got.Host != req2.Host {
		t.Fatalf("queued Poll host = %q, want %q", got.Host, req2.Host)
	}
}

func TestAuthPromptQueueCancelAll(t *testing.T) {
	q := NewAuthPromptQueue()

	respChan1 := make(chan platform.AuthResponse, 1)
	respChan2 := make(chan platform.AuthResponse, 1)

	req1 := platform.AuthRequest{ID: 1, Response: respChan1}
	req2 := platform.AuthRequest{ID: 1, Response: respChan2}

	q.requestChan <- req1
	q.Poll()

	q.requestChan <- req2

	q.CancelAll(1)

	current := q.Current()
	if current != nil {
		t.Error("current should be nil after CancelAll")
	}

	select {
	case resp := <-respChan1:
		if resp.Accept {
			t.Error("cancelled response should have Accept = false")
		}
	default:
		t.Error("current request should receive cancel response")
	}

	select {
	case resp := <-respChan2:
		if resp.Accept {
			t.Error("queued cancelled response should have Accept = false")
		}
	default:
		t.Error("queued request should receive cancel response")
	}
}

func TestWindowSizeMsg(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)

	wsMsg := tea.WindowSizeMsg{Width: 80, Height: 24}
	m = updateModel(m, wsMsg)

	if m.width != 80 {
		t.Errorf("width = %d, want 80", m.width)
	}
	if m.height != 24 {
		t.Errorf("height = %d, want 24", m.height)
	}
}

func TestPromptModeHostKey(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	respChan := make(chan platform.AuthResponse, 1)
	req := platform.AuthRequest{
		Type:        platform.AuthRequestHostKey,
		Host:        "test@example.com",
		Fingerprint: "SHA256:test",
		Response:    respChan,
	}

	m.authQueue.requestChan <- req
	m.authQueue.Poll()

	authMsg := authRequestMsg{request: req}
	m = updateModel(m, authMsg)

	if !m.promptMode {
		t.Error("promptMode should be true after auth request")
	}

	m = updateModel(m, keyMsg("a"))

	select {
	case resp := <-respChan:
		if !resp.Accept {
			t.Error("'a' key should accept host key")
		}
	default:
		t.Error("response should be sent")
	}
}

func TestPromptModePassword(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	respChan := make(chan platform.AuthResponse, 1)
	req := platform.AuthRequest{
		Type:       platform.AuthRequestPassword,
		Host:       "user@example.com",
		RetryCount: 0,
		Response:   respChan,
	}

	m.authQueue.requestChan <- req
	m.authQueue.Poll()

	authMsg := authRequestMsg{request: req}
	m = updateModel(m, authMsg)

	m = updateModel(m, keyMsg("t"))
	m = updateModel(m, keyMsg("e"))
	m = updateModel(m, keyMsg("s"))
	m = updateModel(m, keyMsg("t"))

	if m.promptInput != "test" {
		t.Errorf("promptInput = %q, want %q", m.promptInput, "test")
	}

	m = updateModel(m, keyEnter())

	select {
	case resp := <-respChan:
		if !resp.Accept {
			t.Error("Enter should submit password")
		}
		if resp.Password != "test" {
			t.Errorf("Password = %q, want %q", resp.Password, "test")
		}
	default:
		t.Error("response should be sent")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{500, "0.49 KB"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1572864, "1.50 MB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.bytes)
		if diff := cmp.Diff(got, tt.want); diff != "" {
			t.Errorf("formatBytes(%d) mismatch (-want +got):\n%s", tt.bytes, diff)
		}
	}
}

func TestFormatTransferSpeed(t *testing.T) {
	tests := []struct {
		bytesPerSec int64
		want        string
	}{
		{512, "0.5KB/s"},
		{1536, "1.5KB/s"},
		{2 * 1024 * 1024, "2.0MB/s"},
	}

	for _, tt := range tests {
		if got := formatTransferSpeed(tt.bytesPerSec); got != tt.want {
			t.Fatalf("formatTransferSpeed(%d) = %q, want %q", tt.bytesPerSec, got, tt.want)
		}
	}
}

func TestBuildSSHConfigUsesSelectedHost(t *testing.T) {
	m := newTestModel(nil)
	m.selectedHost = config.Host{
		Hostname:     "example.com",
		Port:         "2222",
		User:         "alice",
		IdentityFile: "~/.ssh/custom",
	}

	cfg := m.buildSSHConfig()
	if cfg.Host != "example.com" || cfg.Port != "2222" || cfg.User != "alice" || cfg.IdentityFile != "~/.ssh/custom" {
		t.Fatalf("buildSSHConfig = %#v", cfg)
	}
}

func TestSFTPTransferErrorSetsStatus(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.screen = ScreenSFTP
	m.sftpTransferring = true
	m.sftpProgress = sftp.NewProgressInfo("file.txt", 100)

	m = updateModel(m, sftpTransferResult{err: fmt.Errorf("permission denied")})

	if m.sftpTransferring {
		t.Fatal("transfer should be marked complete after result")
	}
	if m.statusMsg == "" || !strings.Contains(m.statusMsg, "permission denied") {
		t.Fatalf("statusMsg = %q, want transfer error", m.statusMsg)
	}
	if !m.statusConfirm {
		t.Fatal("transfer result notice should wait for user confirmation")
	}
	if !strings.Contains(m.statusMsg, "FROM:") || !strings.Contains(m.statusMsg, "TO:") {
		t.Fatalf("statusMsg = %q, want source and target context", m.statusMsg)
	}
	if m.sftpProgress.Snapshot().Active {
		t.Fatal("progress should be inactive after result")
	}
}

func TestStatusConfirmWaitsForUserOK(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.setStatusConfirm("SFTP upload complete")

	m = updateModel(m, tickMsg{})
	m = updateModel(m, tickMsg{})
	m = updateModel(m, tickMsg{})

	if m.statusMsg == "" || !m.statusConfirm {
		t.Fatalf("confirmed status should not auto-dismiss, msg=%q confirm=%v", m.statusMsg, m.statusConfirm)
	}

	m = updateModel(m, keyEnter())
	if m.statusMsg != "" || m.statusConfirm {
		t.Fatalf("Enter should clear confirmed status, msg=%q confirm=%v", m.statusMsg, m.statusConfirm)
	}
}

func TestAutoStatusStillExpires(t *testing.T) {
	m := newTestModel(nil)
	m.setStatusMsg("short notice")

	m = updateModel(m, tickMsg{})
	m = updateModel(m, tickMsg{})
	m = updateModel(m, tickMsg{})

	if m.statusMsg != "" || m.statusConfirm {
		t.Fatalf("auto status should expire, msg=%q confirm=%v", m.statusMsg, m.statusConfirm)
	}
}

func TestStaleSFTPTransferResultIsIgnored(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.screen = ScreenSFTP
	m.sftpTransferID = 2
	m.sftpTransferring = true
	m.sftpProgress = sftp.NewProgressInfo("file.txt", 100)

	m = updateModel(m, sftpTransferResult{id: 1, err: fmt.Errorf("old failure"), direction: "upload"})

	if !m.sftpTransferring {
		t.Fatal("stale transfer result should not stop the current transfer")
	}
	if m.statusMsg != "" {
		t.Fatalf("stale transfer result set statusMsg = %q", m.statusMsg)
	}
}

func TestFormatSFTPTransferMessages(t *testing.T) {
	result := sftpTransferResult{
		err:       context.Canceled,
		source:    "/local/file.txt",
		target:    "/remote/file.txt",
		direction: "upload",
	}
	if got := formatSFTPTransferError(result); got != "SFTP upload cancelled" {
		t.Fatalf("cancel message = %q", got)
	}

	result.err = nil
	got := formatSFTPTransferSuccess(result)
	if !strings.Contains(got, "SFTP upload complete") || !strings.Contains(got, "FROM: /local/file.txt") || !strings.Contains(got, "TO: /remote/file.txt") {
		t.Fatalf("success message = %q", got)
	}

	result.report = sftp.TransferReport{
		SkippedCount: 2,
		Skipped: []sftp.SkippedItem{
			{Path: "/remote/link", Reason: "symbolic link"},
			{Path: "/remote/private", Reason: "permission denied"},
		},
	}
	got = formatSFTPTransferSuccess(result)
	if !strings.Contains(got, "Skipped 2 item(s)") || !strings.Contains(got, "/remote/link: symbolic link") {
		t.Fatalf("success message should include skipped summary: %q", got)
	}
}

func TestFormatDirectorySyncConfirmMessageShowsDirection(t *testing.T) {
	upload := formatDirectorySyncConfirmMessage(0, "/local/project", "/remote/project", sftp.OverwriteReport{})
	if !strings.Contains(upload, "LOCAL -> REMOTE  UPLOAD") ||
		!strings.Contains(upload, "SOURCE: LOCAL  /local/project") ||
		!strings.Contains(upload, "DESTINATION: REMOTE  /remote/project") {
		t.Fatalf("upload confirm message is unclear: %q", upload)
	}

	downloadReport := sftp.OverwriteReport{
		Count: 1,
		Items: []sftp.ExistingItem{{Path: "/local/project/existing.txt", Kind: "file"}},
	}
	download := formatDirectorySyncConfirmMessage(1, "/remote/project", "/local/project", downloadReport)
	if !strings.Contains(download, "REMOTE -> LOCAL  DOWNLOAD") ||
		!strings.Contains(download, "OVERWRITE: 1 existing target item(s)") ||
		!strings.Contains(download, "/local/project/existing.txt: file") ||
		!strings.Contains(download, "SOURCE: REMOTE  /remote/project") ||
		!strings.Contains(download, "DESTINATION: LOCAL  /local/project") {
		t.Fatalf("download confirm message is unclear: %q", download)
	}

	body, fields := modalMessageParts(download)
	if len(body) < 3 || body[0] != "REMOTE -> LOCAL  DOWNLOAD" || !strings.Contains(body[1], "OVERWRITE") {
		t.Fatalf("direction should be the primary modal body, got %#v", body)
	}
	if len(fields) != 2 || fields[0].Label != "SOURCE" || fields[1].Label != "DESTINATION" {
		t.Fatalf("source/destination fields not parsed: %#v", fields)
	}
}

func TestFormatFileOverwriteConfirmMessageShowsRisk(t *testing.T) {
	report := sftp.OverwriteReport{
		Count: 1,
		Items: []sftp.ExistingItem{{Path: "/remote/file.txt", Kind: "file"}},
	}
	msg := formatFileOverwriteConfirmMessage(0, "/local/file.txt", "/remote/file.txt", report)
	if !strings.Contains(msg, "LOCAL -> REMOTE  UPLOAD") ||
		!strings.Contains(msg, "OVERWRITE FILE") ||
		!strings.Contains(msg, "/remote/file.txt: file") ||
		!strings.Contains(msg, "SOURCE: LOCAL  /local/file.txt") ||
		!strings.Contains(msg, "DESTINATION: REMOTE  /remote/file.txt") {
		t.Fatalf("file overwrite message is unclear: %q", msg)
	}
}

func TestValidateSingleSyncSourceRejectsLocalSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(target, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	m := newTestModel(nil)
	m.sftpLocalDir = tmpDir
	pending := sftpPendingSync{
		focus:  0,
		source: link,
		target: "/remote/link.txt",
		name:   "link.txt",
		size:   1,
	}
	updated, cmd := m.preflightSingleSync(pending)
	m = runCommandChain(t, updated, cmd)
	if !strings.Contains(m.statusMsg, "symbolic link") {
		t.Fatalf("statusMsg = %q, want symbolic link skip reason", m.statusMsg)
	}
}

func TestSFTPKeyFlowSwitchesFocusAndMovesOnlyActivePanel(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "local.txt"}}
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "remote.txt"}}
	m.sftpFocus = 0

	m = updateModel(m, keyTab())
	if m.sftpFocus != 1 {
		t.Fatalf("Tab should switch focus to remote panel, got %d", m.sftpFocus)
	}

	m = updateModel(m, keyDown())
	if m.sftpCursor[1] != 1 {
		t.Fatalf("remote cursor = %d, want 1", m.sftpCursor[1])
	}
	if m.sftpCursor[0] != 0 {
		t.Fatalf("local cursor should not move while remote is focused, got %d", m.sftpCursor[0])
	}
}

func TestSFTPEnterDirNavigatesLocalDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	childDir := filepath.Join(tmpDir, "child")
	if err := os.Mkdir(childDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childDir, "inside.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0
	m.sftpLocalDir = tmpDir
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "child", IsDir: true}}
	m.sftpCursor[0] = 1

	updated, cmd := m.sftpEnterDir()
	m = updated.(Model)
	m = runCommandChain(t, m, cmd)

	if m.sftpLocalDir != childDir {
		t.Fatalf("sftpLocalDir = %q, want %q", m.sftpLocalDir, childDir)
	}
	if len(m.sftpLocalFiles) != 1 || m.sftpLocalFiles[0].Name != "inside.txt" {
		t.Fatalf("local files = %#v, want inside.txt", m.sftpLocalFiles)
	}
	if m.sftpCursor[0] != 0 || m.sftpScroll[0] != 0 {
		t.Fatalf("cursor/scroll = %d/%d, want reset", m.sftpCursor[0], m.sftpScroll[0])
	}
}

func TestSFTPEnterDirRejectsFileSelection(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "file.txt"}}
	m.sftpCursor[0] = 1

	updated, _ := m.sftpEnterDir()
	m = updated.(Model)

	if !strings.Contains(m.statusMsg, "preview") {
		t.Fatalf("statusMsg = %q, want preview hint", m.statusMsg)
	}
}

func TestSFTPNavigateToCancelledContext(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.cancelCtx = ctx

	updated, cmd := m.sftpNavigateTo(t.TempDir())
	m = updated.(Model)
	m = runCommandChain(t, m, cmd)

	if !strings.Contains(m.statusMsg, "cancelled") {
		t.Fatalf("statusMsg = %q, want cancelled", m.statusMsg)
	}
}

func TestSFTPKeyFlowBlocksActionsDuringTransfer(t *testing.T) {
	tests := []struct {
		key       tea.KeyMsg
		fieldName string
	}{
		{key: keyMsg("s"), fieldName: "sync"},
		{key: keyMsg("r"), fieldName: "sync-dir"},
		{key: keyMsg("v"), fieldName: "preview"},
		{key: keyMsg("n"), fieldName: "rename"},
	}

	for _, tt := range tests {
		t.Run(tt.fieldName, func(t *testing.T) {
			m := newTestModel(nil)
			m.screen = ScreenSFTP
			m.sftpTransferring = true
			m.sftpLocalFiles = []sftp.FileEntry{{Name: "file.txt", Size: 1}}
			m.sftpCursor[0] = 1

			m = updateModel(m, tt.key)

			if !strings.Contains(m.statusMsg, "Wait for transfer") {
				t.Fatalf("statusMsg = %q, want transfer guard", m.statusMsg)
			}
			if m.sftpRenaming {
				t.Fatal("rename mode should not start during transfer")
			}
			if m.sftpPreviewing {
				t.Fatal("preview should not open during transfer")
			}
		})
	}
}

func TestSFTPConfirmCancelClearsPendingState(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpOverwriteConfirm = true
	m.sftpOverwriteConfirmMsg = "overwrite?"
	m.sftpPendingSync = sftpPendingSync{source: "/tmp/source", target: "/tmp/target"}

	m = updateModel(m, keyEsc())

	if m.sftpOverwriteConfirm || m.sftpOverwriteConfirmMsg != "" || m.sftpPendingSync != (sftpPendingSync{}) {
		t.Fatalf("overwrite cancel should clear modal state: %#v", m)
	}

	m.sftpSyncConfirm = true
	m.sftpSyncConfirmMsg = "sync?"
	m = updateModel(m, keyEsc())
	if m.sftpSyncConfirm || m.sftpSyncConfirmMsg != "" {
		t.Fatalf("sync cancel should clear modal state: confirm=%v msg=%q", m.sftpSyncConfirm, m.sftpSyncConfirmMsg)
	}
}

func TestSFTPDirectoryConfirmEnterClearsModalWithoutSelection(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpSyncConfirm = true
	m.sftpSyncConfirmMsg = "confirm"
	m.sftpFocus = 0
	m.sftpCursor[0] = 0

	m = updateModel(m, keyEnter())

	if m.sftpSyncConfirm || m.sftpSyncConfirmMsg != "" {
		t.Fatalf("confirm enter should clear modal state: confirm=%v msg=%q", m.sftpSyncConfirm, m.sftpSyncConfirmMsg)
	}
	if m.sftpTransferring {
		t.Fatal("empty directory selection should not start transfer")
	}
}

func TestSFTPRenameInputEditsAndEscCancels(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpRenaming = true
	m.sftpRenameInput = "old"

	m = updateModel(m, keyMsg("x"))
	if m.sftpRenameInput != "oldx" {
		t.Fatalf("rename input = %q, want oldx", m.sftpRenameInput)
	}

	m = updateModel(m, keyBackspace())
	if m.sftpRenameInput != "old" {
		t.Fatalf("rename input after backspace = %q, want old", m.sftpRenameInput)
	}

	m = updateModel(m, keyEsc())
	if m.sftpRenaming || m.sftpRenameInput != "" {
		t.Fatalf("Esc should cancel rename input, renaming=%v input=%q", m.sftpRenaming, m.sftpRenameInput)
	}
}

func TestSFTPStartSyncRejectsDirectoryBeforeClientUse(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0
	m.sftpCursor[0] = 1
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "dir", IsDir: true}}

	m = updateModel(m, keyMsg("s"))

	if !strings.Contains(m.statusMsg, "Use [r] for directory sync") {
		t.Fatalf("statusMsg = %q, want directory sync hint", m.statusMsg)
	}
	if m.sftpTransferring {
		t.Fatal("directory selected with file sync should not start transfer")
	}
}

func TestSFTPStartSyncRejectsRemoteDirectoryBeforeClientUse(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 1
	m.sftpCursor[1] = 1
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "dir", IsDir: true}}

	updated, cmd := m.sftpStartSync()
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("directory selected with remote file sync should not return a command")
	}
	if !strings.Contains(m.statusMsg, "Use [r] for directory sync") {
		t.Fatalf("statusMsg = %q, want directory sync hint", m.statusMsg)
	}
	if m.sftpTransferring {
		t.Fatal("remote directory selected with file sync should not start transfer")
	}
}

func TestSFTPDoSingleSyncRejectsEmptyPending(t *testing.T) {
	m := newTestModel(nil)

	updated, cmd := m.sftpDoSingleSync(sftpPendingSync{})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("empty pending sync should not return a command")
	}
	if m.sftpTransferring {
		t.Fatal("empty pending sync should not enter transferring state")
	}
	if !strings.Contains(m.statusMsg, "No file selected") {
		t.Fatalf("statusMsg = %q, want no file selected", m.statusMsg)
	}
}

func TestValidateSingleSyncSourceRejectsLocalDirectoryAndMissing(t *testing.T) {
	tmpDir := t.TempDir()
	childDir := filepath.Join(tmpDir, "child")
	if err := os.Mkdir(childDir, 0755); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(nil)
	updated, cmd := m.preflightSingleSync(sftpPendingSync{focus: 0, source: filepath.Join(tmpDir, "missing")})
	m = runCommandChain(t, updated, cmd)
	if !strings.Contains(m.statusMsg, "Source no longer exists") {
		t.Fatalf("missing source status=%q, want rejection", m.statusMsg)
	}

	updated, cmd = m.preflightSingleSync(sftpPendingSync{focus: 0, source: childDir})
	m = runCommandChain(t, updated, cmd)
	if !strings.Contains(m.statusMsg, "Skipped non-regular file: directory") {
		t.Fatalf("directory source status=%q, want non-regular rejection", m.statusMsg)
	}
}

func TestSFTPRecursiveConfirmRejectsInvalidSelection(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0

	updated, cmd := m.sftpStartRecursiveConfirm()
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("missing recursive selection should not return a command")
	}
	if !strings.Contains(m.statusMsg, "No file selected") {
		t.Fatalf("statusMsg = %q, want no file selected", m.statusMsg)
	}

	m.statusMsg = ""
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "file.txt"}}
	m.sftpCursor[0] = 1
	updated, cmd = m.sftpStartRecursiveConfirm()
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("file recursive sync should not return a command")
	}
	if !strings.Contains(m.statusMsg, "Use [s] for file sync") {
		t.Fatalf("statusMsg = %q, want file sync hint", m.statusMsg)
	}
}

func TestSFTPDoRecursiveRejectsInvalidSelection(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0

	updated, cmd := m.sftpDoRecursive()
	m = updated.(Model)
	if cmd != nil || m.sftpTransferring {
		t.Fatal("missing local recursive selection should not start transfer")
	}

	m.sftpLocalFiles = []sftp.FileEntry{{Name: "file.txt"}}
	m.sftpCursor[0] = 1
	updated, cmd = m.sftpDoRecursive()
	m = updated.(Model)
	if cmd != nil || m.sftpTransferring {
		t.Fatal("local file recursive selection should not start transfer")
	}

	m.sftpFocus = 1
	m.sftpRemoteFiles = []sftp.FileEntry{{Name: "file.txt"}}
	m.sftpCursor[1] = 1
	updated, cmd = m.sftpDoRecursive()
	m = updated.(Model)
	if cmd != nil || m.sftpTransferring {
		t.Fatal("remote file recursive selection should not start transfer")
	}
}

func TestRenderSFTPPreviewTruncatesAndCapsLines(t *testing.T) {
	m := newTestModel(nil)
	m.sftpPreview = "abcdefghijklmnopqrstuvwxyz\nline-2\nline-3"

	rendered := stripANSI(m.renderSFTPPreview(10, 2))
	if !strings.Contains(rendered, "Preview") {
		t.Fatalf("preview render missing title:\n%s", rendered)
	}
	if strings.Contains(rendered, "line-3") {
		t.Fatalf("preview should cap rendered lines:\n%s", rendered)
	}
	if strings.Contains(rendered, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("preview should truncate long lines:\n%s", rendered)
	}
}

func TestSFTPPreviewLocalTextAndBinary(t *testing.T) {
	tmpDir := t.TempDir()
	textFile := filepath.Join(tmpDir, "text.txt")
	if err := os.WriteFile(textFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	binaryFile := filepath.Join(tmpDir, "binary.bin")
	if err := os.WriteFile(binaryFile, []byte{'a', 0, 'b'}, 0644); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0
	m.sftpLocalDir = tmpDir
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "text.txt"}, {Name: "binary.bin"}}
	m.sftpCursor[0] = 1

	updated, cmd := m.sftpPreviewFile()
	m = updated.(Model)
	m = runCommandChain(t, m, cmd)
	if !m.sftpPreviewing || m.sftpPreview != "hello" {
		t.Fatalf("text preview = %q previewing=%v, want hello true", m.sftpPreview, m.sftpPreviewing)
	}

	m.sftpCursor[0] = 2
	updated, cmd = m.sftpPreviewFile()
	m = updated.(Model)
	m = runCommandChain(t, m, cmd)
	if m.sftpPreview != "[binary file]" {
		t.Fatalf("binary preview = %q, want binary marker", m.sftpPreview)
	}
}

func TestSFTPPreviewRejectsDirectoryAndMissingSelection(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "dir", IsDir: true}}

	updated, _ := m.sftpPreviewFile()
	m = updated.(Model)
	if !strings.Contains(m.statusMsg, "No file selected") {
		t.Fatalf("statusMsg = %q, want no selection", m.statusMsg)
	}

	m.statusMsg = ""
	m.sftpCursor[0] = 1
	updated, _ = m.sftpPreviewFile()
	m = updated.(Model)
	if !strings.Contains(m.statusMsg, "Cannot preview a directory") {
		t.Fatalf("statusMsg = %q, want directory rejection", m.statusMsg)
	}
}

func TestSFTPConfirmRenameLocalSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := filepath.Join(tmpDir, "old.txt")
	if err := os.WriteFile(oldPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpFocus = 0
	m.sftpLocalDir = tmpDir
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "old.txt"}}
	m.sftpCursor[0] = 1
	m.sftpRenaming = true
	m.sftpRenameInput = "new.txt"

	updated, cmd := m.sftpConfirmRename()
	m = updated.(Model)
	m = runCommandChain(t, m, cmd)

	if _, err := os.Stat(filepath.Join(tmpDir, "new.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if m.sftpRenaming || m.sftpRenameInput != "" {
		t.Fatalf("rename state not cleared: renaming=%v input=%q", m.sftpRenaming, m.sftpRenameInput)
	}
}

func TestSFTPContextAndPreviewHeightFallbacks(t *testing.T) {
	m := Model{}
	if m.sftpContext() == nil {
		t.Fatal("sftpContext should return a background context when cancelCtx is nil")
	}
	m.height = 8
	if got := m.sftpPreviewHeight(); got != 4 {
		t.Fatalf("small preview height = %d, want 4", got)
	}
	m.height = 80
	if got := m.sftpPreviewHeight(); got != 8 {
		t.Fatalf("large preview height = %d, want 8", got)
	}
}

func TestSFTPScrollStateTracksCursor(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.height = 18
	for i := 0; i < 30; i++ {
		m.sftpLocalFiles = append(m.sftpLocalFiles, sftp.FileEntry{Name: fmt.Sprintf("file-%02d.txt", i)})
	}

	for i := 0; i < 15; i++ {
		m = updateModel(m, keyDown())
	}

	visible := m.sftpListVisibleItems()
	if m.sftpScroll[0] == 0 {
		t.Fatal("scroll should advance when cursor moves below the visible list")
	}
	if m.sftpCursor[0] < m.sftpScroll[0] || m.sftpCursor[0] >= m.sftpScroll[0]+visible {
		t.Fatalf("cursor %d should be visible in scroll window [%d,%d)", m.sftpCursor[0], m.sftpScroll[0], m.sftpScroll[0]+visible)
	}
}

func TestSFTPPanelHeightMatchesVisibleRows(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.height = 24

	if got, want := m.sftpPanelHeight()-sftpPanelRowsAroundList, m.sftpListVisibleItems(); got != want {
		t.Fatalf("panel list rows = %d, want %d", got, want)
	}

	m.sftpTransferring = true
	if got, want := m.sftpPanelHeight()-sftpPanelRowsAroundList, m.sftpListVisibleItems(); got != want {
		t.Fatalf("panel list rows with drawer = %d, want %d", got, want)
	}
}

func TestReadPreviewBytesLimitsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 8192)), 0644); err != nil {
		t.Fatal(err)
	}

	data, err := readPreviewBytes(path, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 4096 {
		t.Fatalf("preview length = %d, want 4096", len(data))
	}
}

func TestValidateRenameNameRejectsPaths(t *testing.T) {
	for _, name := range []string{"../x", "dir/file", `dir\file`, ".", "..", " padded"} {
		if err := validateRenameName(name); err == nil {
			t.Fatalf("validateRenameName(%q) should reject invalid file names", name)
		}
	}
	if err := validateRenameName("renamed.txt"); err != nil {
		t.Fatalf("validateRenameName valid name returned %v", err)
	}
}

func TestSFTPConfirmRenameRejectsPathInput(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := filepath.Join(tmpDir, "old.txt")
	if err := os.WriteFile(oldPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpLocalDir = tmpDir
	m.sftpLocalFiles = []sftp.FileEntry{{Name: "old.txt"}}
	m.sftpCursor[0] = 1
	m.sftpFocus = 0
	m.sftpRenaming = true
	m.sftpRenameInput = "../moved.txt"

	updated, _ := m.sftpConfirmRename()
	m = updated.(Model)

	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("original file should remain after invalid rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "..", "moved.txt")); err == nil {
		t.Fatal("invalid rename should not move file outside current directory")
	}
	if !strings.Contains(m.statusMsg, "path separators") {
		t.Fatalf("statusMsg = %q, want path separator error", m.statusMsg)
	}
}

func TestSFTPExitDuringTransferCancelsAndLeavesSFTP(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.sftpTransferring = true
	cancelled := false
	m.sftpCancel = func() { cancelled = true }

	m = updateModel(m, keyMsg("q"))

	if !cancelled {
		t.Fatal("exit should cancel the active transfer")
	}
	if m.sftpTransferring {
		t.Fatal("exit should clear transferring state after synchronous cancellation")
	}
	if m.screen != ScreenMain {
		t.Fatal("exit should return to main screen")
	}
}

func TestStaleSFTPTransferResultKeepsTickAlive(t *testing.T) {
	m := newTestModel(nil)
	m.screen = ScreenSFTP
	m.statusMsg = "visible"
	m.statusTicks = 1
	m.sftpTransferID = 2
	m.sftpTransferring = true
	m.sftpProgress = sftp.NewProgressInfo("file.txt", 1)

	m = updateModel(m, sftpTransferResult{id: 1, err: context.Canceled, direction: "download"})
	updated, cmd := m.Update(tickMsg{})
	m = updated.(Model)

	if !m.sftpTransferring {
		t.Fatal("stale transfer result should not stop the current transfer")
	}
	if m.statusMsg != "" {
		t.Fatal("tick should continue processing status countdown after stale result")
	}
	if cmd == nil {
		t.Fatal("tick should continue scheduling the next tick after stale result")
	}
}

func TestSampleTunnelTrafficCapsHistoryAndDropsStaleIDs(t *testing.T) {
	m := newTestModel(nil)
	conn := testutil.NewMockConnection()
	exec := testutil.NewMockExecutor()
	exec.ConnectFn = func(cfg *platform.SSHConfig, spec platform.ForwardSpec) (*testutil.MockConnection, error) {
		return conn, nil
	}
	m.manager.SetExecutor(exec)
	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, err := m.manager.Create("flow", cfg, tunnel.Local, "8080", "80")
	if err != nil {
		t.Fatal(err)
	}
	m.trafficHist = map[int][]int64{999: []int64{1, 2, 3}}

	var total int64
	for i := int64(0); i < 125; i++ {
		total += i
		conn.SetStats(total, total)
		m.manager.Refresh(false, time.Second)
		m.sampleTunnelTraffic()
	}

	if _, ok := m.trafficHist[999]; ok {
		t.Fatal("traffic history should drop stale tunnel IDs")
	}
	history := m.trafficHist[tun.ID()]
	if len(history) != 120 {
		t.Fatalf("history length = %d, want 120", len(history))
	}
	if got := history[len(history)-1]; got != 248 {
		t.Fatalf("last traffic sample = %d, want 248", got)
	}
}

func TestRenderTrafficFlowUsesFixedWidthAndNoLabel(t *testing.T) {
	rendered := stripANSI(renderTrafficFlow([]int64{0, 1024, 64 * 1024, 1024 * 1024}, 12))
	if strings.Contains(rendered, "FLOW") {
		t.Fatalf("traffic flow should not include a label: %q", rendered)
	}
	if got := len([]rune(rendered)); got != 20 {
		t.Fatalf("rendered flow width = %d, want minimum 20: %q", got, rendered)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"very long string", 8, "very ..."},
		{"exact", 5, "exact"},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}
