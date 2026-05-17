package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
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

func keyDown() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
}

func keyUp() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
}

func keyBackspace() tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace})
}

func updateModel(m Model, msg tea.Msg) Model {
	result, _ := m.Update(msg)
	return result.(Model)
}

func newTestModel(hosts []config.Host) Model {
	mockExec := platform.NewMockExecutor()
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
	m = updateModel(m, keyMsg("10.0.0.15:5551"))
	if m.portInput != "10.0.0.15:5551" {
		t.Fatalf("remote target paste portInput = %q, want %q", m.portInput, "10.0.0.15:5551")
	}
}

func TestModelDynamicPortInputPasteFiltersAddress(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())

	for i := 0; i < 2; i++ {
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

func TestValidPortInput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		allowAddr bool
		want      bool
	}{
		{name: "plain port", input: "5555", allowAddr: true, want: true},
		{name: "host port", input: "10.0.0.15:5551", allowAddr: true, want: true},
		{name: "empty", input: "", allowAddr: true, want: false},
		{name: "missing port", input: "127.0.0.1:", allowAddr: true, want: false},
		{name: "out of range", input: "70000", allowAddr: true, want: false},
		{name: "dynamic rejects host", input: "127.0.0.1:1080", allowAddr: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validPortInput(tt.input, tt.allowAddr); got != tt.want {
				t.Fatalf("validPortInput(%q, %v) = %v, want %v", tt.input, tt.allowAddr, got, tt.want)
			}
		})
	}
}

func TestModelDynamicTunnelPortInput(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com", User: "user", Port: "22"}}
	m := newTestModel(hosts)
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	m = updateModel(m, keyEnter())

	for i := 0; i < 2; i++ {
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

	result, cmd = m.Update(keyMsg("q"))
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
	m.manager.Stop(tun.ID)

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

	mockExec := platform.NewMockExecutor()
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

	m.manager.Restart(tun.ID)
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

func TestModelNoHostsError(t *testing.T) {
	m := newTestModel([]config.Host{})
	m.width = 100
	m.height = 30

	m = updateModel(m, keyMsg("c"))
	if m.err == nil {
		t.Error("'c' with no hosts should set error")
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
