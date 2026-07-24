package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/logging"
	"github.com/spance/intun/internal/monitor"
	"github.com/spance/intun/internal/tui"
	"github.com/spance/intun/internal/tunnel"
	"github.com/spance/intun/internal/udprelay"
)

var Version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "relay" {
		if err := udprelay.RunCommand(context.Background(), os.Args[2:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			return fmt.Errorf("UDP relay failed: %w", err)
		}
		return nil
	}

	hosts, err := config.ParseSSHConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse ssh config: %v\n", err)
		fmt.Fprintf(os.Stderr, "You can still create tunnels manually.\n\n")
	}

	manager := tunnel.NewManager(nil)
	defer logging.Close()
	defer manager.StopAll()

	mon := monitor.NewMonitor(manager, time.Second)
	mon.Start()
	defer mon.Stop()

	model := tui.NewModel(hosts, manager, Version)
	p := tea.NewProgram(model)

	finalModel, runErr := p.Run()
	var closeErr error
	if closer, ok := finalModel.(interface{ Close() error }); ok {
		closeErr = closer.Close()
	}
	return errors.Join(runErr, closeErr)
}
