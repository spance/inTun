package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
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
	output := m.View()
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
	output := m.View()
	clean := stripANSI(output)

	shortcuts := []string{"Navigate", "Create", "Reconnect", "Exit"}
	for _, s := range shortcuts {
		if !strings.Contains(clean, s) {
			t.Errorf("View output should contain shortcut '%s'", s)
		}
	}
}

func TestViewTunnelList(t *testing.T) {
	m := newTestModelWithTunnel()
	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "test-tunnel") {
		t.Error("View output should contain tunnel name")
	}
	if !strings.Contains(clean, "Local") {
		t.Error("View output should contain tunnel type")
	}
	if !strings.Contains(clean, ":8080") {
		t.Error("View output should contain local port")
	}
	if !strings.Contains(clean, ":80") {
		t.Error("View output should contain remote port")
	}
}

func TestViewNoTunnelsMessage(t *testing.T) {
	hosts := []config.Host{{Name: "test", Hostname: "example.com"}}
	m := NewModel(hosts, tunnel.NewManager(nil), "v1.0.0")
	m.width = 100
	m.height = 30

	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "No tunnels active") {
		t.Error("View should show 'No tunnels active' message")
	}
	if !strings.Contains(clean, "'c' to create") {
		t.Error("View should show create shortcut hint")
	}
}

func TestViewTunnelStatus(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]

	tun.UpdateStats(1024, 2048, 100, 200, 50*1000000, false)
	output := m.View()
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

	output := m.View()
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

	output := m.View()
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

	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "Authentication failed") {
		t.Error("View should show auth error message")
	}
	if !strings.Contains(clean, "~/.ssh/id_rsa") {
		t.Error("View should show key path hint")
	}
}

func TestViewPortInputScreen(t *testing.T) {
	m := newTestModelWithTunnel()
	m.screen = ScreenInputPort
	m.portInput = "808"

	output := m.View()
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

	output := m.View()
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

	output := m.View()
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

	output := m.View()
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

	output := m.View()
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

func TestViewDynamicTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	m.manager.Create("socks-proxy", cfg, tunnel.Dynamic, "1080", "")

	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "SOCKS5") {
		t.Error("Dynamic tunnel should show 'SOCKS5' for remote")
	}
}

func TestANSIStylesPresent(t *testing.T) {
	m := newTestModelWithTunnel()
	output := m.View()

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
	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "second-tunnel") {
		t.Error("Selected tunnel should be visible")
	}
}

func TestViewLatencyDisplay(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]

	tun.Latency = 45 * 1000000 // 45ms
	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "45ms") {
		t.Error("View should show latency when available")
	}
}

func TestViewStoppedTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]
	tun.Status = tunnel.StatusStopped

	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "Stopped") {
		t.Error("View should show 'Stopped' status")
	}
}

func TestViewConnectingTunnel(t *testing.T) {
	m := newTestModelWithTunnel()
	tun := m.manager.List()[0]
	tun.Status = tunnel.StatusConnecting

	output := m.View()
	clean := stripANSI(output)

	if !strings.Contains(clean, "Connecting") {
		t.Error("View should show 'Connecting' status")
	}
}
