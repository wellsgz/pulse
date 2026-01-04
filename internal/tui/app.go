package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wellsgz/pulse/internal/collector"
	"github.com/wellsgz/pulse/internal/ipc"
)

// Init initializes the model and returns initial commands
func (m Model) Init() tea.Cmd {
	if m.IsIPCMode() {
		return tea.Batch(
			// Wait for first IPC probe result
			waitForIPCResult(m.ipcResults),
		)
	}
	return tea.Batch(
		// Wait for first probe result
		waitForResult(m.resultsChan),
		// Initial refresh of stats
		func() tea.Msg {
			m.refreshAllStats()
			return TickMsg{}
		},
	)
}

// Run starts the TUI application in standalone mode
func Run(coll *collector.Collector, apiAddr string) error {
	model := NewModel(coll, apiAddr)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen buffer
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	return nil
}

// RunWithIPC starts the TUI application connected to a daemon via IPC
func RunWithIPC(client *ipc.Client, apiAddr string) error {
	// Get targets from daemon
	targets, err := client.GetTargets()
	if err != nil {
		return fmt.Errorf("failed to get targets from daemon: %w", err)
	}

	// Subscribe to probe results
	if err := client.Subscribe(); err != nil {
		return fmt.Errorf("failed to subscribe to probe results: %w", err)
	}

	model := NewModelWithIPC(client, targets, apiAddr)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen buffer
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	_, err = p.Run()
	if err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	return nil
}
