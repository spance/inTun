package main

import (
	"context"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/monitor"
	"github.com/spance/intun/internal/tui"
	"github.com/spance/intun/internal/tunnel"
	"github.com/spance/intun/internal/udprelay"
)

var Version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "relay" {
		if err := udprelay.RunCommand(context.Background(), os.Args[2:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "UDP relay failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

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
	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
