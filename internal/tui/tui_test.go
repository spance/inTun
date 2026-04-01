package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyEnter() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

func keyEsc() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEsc}
}

func keyDown() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyDown}
}

func keyUp() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyUp}
}

func keyBackspace() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyBackspace}
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
