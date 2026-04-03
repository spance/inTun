package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/monitor"
	"github.com/spance/intun/internal/tui"
	"github.com/spance/intun/internal/tunnel"
)

var Version = "dev"

func main() {
	hosts, err := config.ParseSSHConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse ssh config: %v\n", err)
		fmt.Fprintf(os.Stderr, "You can still create tunnels manually.\n\n")
	}

	manager := tunnel.NewManager(nil)
	mon := monitor.NewMonitor(manager, 1000000000)
	mon.Start()
	defer mon.Stop()

	model := tui.NewModel(hosts, manager, Version)
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
