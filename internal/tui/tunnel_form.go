package tui

import (
	"fmt"

	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

type tunnelInputTarget int

const (
	tunnelInputLocal tunnelInputTarget = iota
	tunnelInputRemote
)

type tunnelInputStep struct {
	label     string
	errorName string
	allowHost bool
	target    tunnelInputTarget
}

func (m Model) tunnelInputSteps() []tunnelInputStep {
	switch m.selectedType {
	case tunnel.Dynamic:
		return []tunnelInputStep{{
			label:     "SOCKS Proxy Port",
			errorName: "SOCKS proxy port",
			target:    tunnelInputLocal,
		}}
	case tunnel.Remote:
		return []tunnelInputStep{
			{label: "Local Target (host:port or port)", errorName: "local target", allowHost: true, target: tunnelInputLocal},
			{label: "Remote Listen (host:port or port)", errorName: "remote listen", allowHost: true, target: tunnelInputRemote},
		}
	default:
		return []tunnelInputStep{
			{label: "Local Listen (host:port or port)", errorName: "local listen", allowHost: true, target: tunnelInputLocal},
			{label: "Remote Target (host:port or port)", errorName: "remote target", allowHost: true, target: tunnelInputRemote},
		}
	}
}

func (m Model) currentTunnelInputStep() (tunnelInputStep, bool) {
	steps := m.tunnelInputSteps()
	if m.inputMode < 0 || m.inputMode >= len(steps) {
		return tunnelInputStep{}, false
	}
	return steps[m.inputMode], true
}

func (m Model) acceptTunnelInput() (Model, bool) {
	step, ok := m.currentTunnelInputStep()
	if !ok {
		m.err = fmt.Errorf("invalid tunnel input step")
		return m, false
	}
	normalized, err := platform.ParseForwardAddress(m.portInput, platform.ForwardAddressOptions{AllowHost: step.allowHost})
	if err != nil {
		m.err = fmt.Errorf("invalid %s: %s", step.errorName, m.portInput)
		return m, false
	}
	m.err = nil
	if step.target == tunnelInputLocal {
		m.localPort = normalized
	} else {
		m.remotePort = normalized
	}

	steps := m.tunnelInputSteps()
	if m.inputMode+1 < len(steps) {
		m.inputMode++
		m.portInput = ""
		return m, false
	}
	return m, true
}
