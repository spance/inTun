package tui

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/config"
)

func (m Model) filteredHosts() []config.Host {
	query := strings.ToLower(strings.TrimSpace(m.hostFilter.Value()))
	if query == "" {
		return m.hosts
	}
	result := make([]config.Host, 0, len(m.hosts))
	for _, host := range m.hosts {
		parts := []string{host.Name, host.Hostname, host.User, strings.Join(host.Labels, " ")}
		if strings.Contains(strings.ToLower(strings.Join(parts, "\x00")), query) {
			result = append(result, host)
		}
	}
	return result
}

func (m Model) beginHostFilter() (Model, tea.Cmd) {
	m.hostFiltering = true
	m.hostFilter.Reset()
	m.hostCursor = 0
	m.hostScroll = 0
	return m, m.hostFilter.Focus()
}

func (m Model) stopHostFilter(clear bool) Model {
	m.hostFiltering = false
	m.hostFilter.Blur()
	if clear {
		m.hostFilter.Reset()
		m.hostCursor = 0
		m.hostScroll = 0
	}
	return m
}

func (m Model) beginManualHostInput() (Model, tea.Cmd) {
	m.err = nil
	m.manualHostInput.Reset()
	m.manualHostInput.Prompt = "SSH  "
	m.screen = ScreenInputHost
	return m, m.manualHostInput.Focus()
}

func (m Model) handleManualHostKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		host, err := parseManualSSHHost(m.manualHostInput.Value())
		if err != nil {
			m.err = err
			return m, nil
		}
		m.err = nil
		m.manualHostInput.Blur()
		m.selectedHost = host
		m.screen = ScreenSelectType
		return m, nil
	case "esc":
		m.err = nil
		m.manualHostInput.Blur()
		if len(m.hosts) > 0 {
			m.screen = ScreenSelectHost
		} else {
			m.screen = ScreenMain
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.manualHostInput, cmd = m.manualHostInput.Update(msg)
	return m, cmd
}

func parseManualSSHHost(value string) (config.Host, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return config.Host{}, fmt.Errorf("SSH destination is required")
	}
	username := ""
	address := value
	if at := strings.LastIndex(address, "@"); at >= 0 {
		username = strings.TrimSpace(address[:at])
		address = strings.TrimSpace(address[at+1:])
		if username == "" {
			return config.Host{}, fmt.Errorf("SSH username is required before @")
		}
	}
	host, port, err := splitSSHAddress(address)
	if err != nil {
		return config.Host{}, err
	}
	if username == "" {
		username = localUsername()
	}
	if username == "" {
		return config.Host{}, fmt.Errorf("SSH username is required")
	}
	return config.Host{
		Name:     host,
		Hostname: host,
		User:     username,
		Port:     port,
	}, nil
}

func splitSSHAddress(address string) (string, string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", "", fmt.Errorf("SSH host is required")
	}
	host := address
	port := "22"
	switch {
	case strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]"):
		host = strings.TrimSuffix(strings.TrimPrefix(address, "["), "]")
	case strings.HasPrefix(address, "["):
		var err error
		host, port, err = net.SplitHostPort(address)
		if err != nil {
			return "", "", fmt.Errorf("invalid SSH address %q: %w", address, err)
		}
	case strings.Count(address, ":") == 1:
		var err error
		host, port, err = net.SplitHostPort(address)
		if err != nil {
			return "", "", fmt.Errorf("invalid SSH address %q: %w", address, err)
		}
	case strings.Count(address, ":") > 1:
		if net.ParseIP(address) == nil {
			return "", "", fmt.Errorf("IPv6 addresses with a port must use [host]:port")
		}
	}
	if strings.TrimSpace(host) == "" {
		return "", "", fmt.Errorf("SSH host is required")
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return "", "", fmt.Errorf("invalid SSH port %q", port)
	}
	return host, strconv.FormatUint(parsedPort, 10), nil
}

func localUsername() string {
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	if value := os.Getenv("USER"); value != "" {
		return value
	}
	return os.Getenv("LOGNAME")
}
