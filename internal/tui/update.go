package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wellsgz/pulse/internal/collector"
	"github.com/wellsgz/pulse/internal/ipc"
	"github.com/wellsgz/pulse/internal/probe"
	"github.com/wellsgz/pulse/internal/storage"
)

// Message types
type (
	// ProbeResultMsg is sent when a probe result is received
	ProbeResultMsg probe.ProbeResult

	// IPCProbeResultMsg is sent when a probe result is received via IPC
	IPCProbeResultMsg ipc.ProbeResultData

	// TickMsg is sent periodically for refresh
	TickMsg struct{}

	// ErrMsg is sent when an error occurs
	ErrMsg struct{ Err error }

	// HistoricalDataMsg carries fetched historical data
	HistoricalDataMsg struct {
		TargetName string
		TimeRange  TimeRange
		Data       []storage.DataPoint
		Stats      *PeriodStats
		Err        error
	}

	// IPCStatsMsg carries stats fetched via IPC
	IPCStatsMsg struct {
		TargetName string
		Stats      *storage.Stats
		Err        error
	}
)

// Update handles messages and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case ProbeResultMsg:
		m.updateTargetStats(probe.ProbeResult(msg))
		return m, waitForResult(m.resultsChan)

	case IPCProbeResultMsg:
		m.updateTargetStatsFromIPC(ipc.ProbeResultData(msg))
		return m, waitForIPCResult(m.ipcResults)

	case IPCStatsMsg:
		if msg.Err == nil && msg.Stats != nil {
			for i := range m.targets {
				if m.targets[i].Config.Name == msg.TargetName {
					m.targets[i].Stats = msg.Stats
					break
				}
			}
		}
		return m, nil

	case TickMsg:
		m.refreshAllStats()
		return m, nil

	case ErrMsg:
		m.err = msg.Err
		return m, nil

	case HistoricalDataMsg:
		return m.handleHistoricalData(msg)
	}

	return m, nil
}

// handleHistoricalData processes fetched historical data
func (m Model) handleHistoricalData(msg HistoricalDataMsg) (tea.Model, tea.Cmd) {
	for i := range m.targets {
		if m.targets[i].Config.Name == msg.TargetName {
			m.targets[i].LoadingHistory = false

			if msg.Err != nil {
				m.err = msg.Err
				return m, nil
			}

			// Store the data if it's for the current time range
			if msg.TimeRange == m.targets[i].TimeRange {
				m.targets[i].HistoricalData = msg.Data
			}

			// Update historical stats based on time range
			if m.targets[i].HistoricalStats == nil {
				m.targets[i].HistoricalStats = &HistoricalStats{}
			}

			switch msg.TimeRange {
			case TimeRange1Hour:
				m.targets[i].HistoricalStats.Hour = msg.Stats
			case TimeRange1Day:
				m.targets[i].HistoricalStats.Day = msg.Stats
			case TimeRange1Week:
				m.targets[i].HistoricalStats.Week = msg.Stats
			}

			// In IPC mode, chain the next fetch sequentially to avoid race condition
			if m.IsIPCMode() {
				switch msg.TimeRange {
				case TimeRange1Hour:
					return m, m.fetchHistoricalDataCmd(msg.TargetName, TimeRange1Day)
				case TimeRange1Day:
					return m, m.fetchHistoricalDataCmd(msg.TargetName, TimeRange1Week)
				}
			}
			break
		}
	}
	return m, nil
}

// handleKeyPress handles keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.currentView {
	case ListView:
		return m.handleListViewKeys(msg)
	case DetailView:
		return m.handleDetailViewKeys(msg)
	}
	return m, nil
}

// handleListViewKeys handles keys in list view
func (m Model) handleListViewKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.selectedIdx > 0 {
			m.selectedIdx--
		}

	case "down", "j":
		if m.selectedIdx < len(m.targets)-1 {
			m.selectedIdx++
		}

	case "enter", " ":
		m.currentView = DetailView
		// Fetch historical data for summary
		target := m.SelectedTarget()
		if target != nil {
			return m, m.fetchAllHistorical(target.Config.Name)
		}

	case "home":
		m.selectedIdx = 0

	case "end":
		m.selectedIdx = len(m.targets) - 1

	case "r":
		// Refresh all stats
		m.refreshAllStats()
	}

	return m, nil
}

// handleDetailViewKeys handles keys in detail view
func (m Model) handleDetailViewKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc", "backspace":
		m.currentView = ListView

	case "up", "k":
		if m.selectedIdx > 0 {
			m.selectedIdx--
			// Fetch historical data for new target
			target := m.SelectedTarget()
			if target != nil {
				return m, m.fetchAllHistorical(target.Config.Name)
			}
		}

	case "down", "j":
		if m.selectedIdx < len(m.targets)-1 {
			m.selectedIdx++
			// Fetch historical data for new target
			target := m.SelectedTarget()
			if target != nil {
				return m, m.fetchAllHistorical(target.Config.Name)
			}
		}

	case "r":
		// Refresh stats and historical data
		m.refreshAllStats()
		target := m.SelectedTarget()
		if target != nil {
			return m, m.fetchAllHistorical(target.Config.Name)
		}

	case "0":
		// Realtime view
		return m.setTimeRange(TimeRangeRealtime)

	case "1":
		// 1 hour view
		return m.setTimeRange(TimeRange1Hour)

	case "2":
		// 1 day view
		return m.setTimeRange(TimeRange1Day)

	case "3":
		// 1 week view
		return m.setTimeRange(TimeRange1Week)

	case "tab":
		// Cycle through time ranges
		target := m.SelectedTarget()
		if target != nil {
			return m.setTimeRange(target.TimeRange.Next())
		}
	}

	return m, nil
}

// setTimeRange sets the time range for the selected target and fetches data
func (m Model) setTimeRange(tr TimeRange) (tea.Model, tea.Cmd) {
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.targets) {
		m.targets[m.selectedIdx].TimeRange = tr
		m.targets[m.selectedIdx].LoadingHistory = true

		if tr == TimeRangeRealtime {
			// No need to fetch for realtime
			m.targets[m.selectedIdx].LoadingHistory = false
			return m, nil
		}

		return m, m.fetchHistoricalDataCmd(m.targets[m.selectedIdx].Config.Name, tr)
	}
	return m, nil
}

// fetchAllHistorical returns commands to fetch all historical periods for a target
func (m Model) fetchAllHistorical(targetName string) tea.Cmd {
	// Both direct mode and IPC mode can now fetch in parallel
	// (IPC race condition fixed by holding lock during channel send)
	return tea.Batch(
		m.fetchHistoricalDataCmd(targetName, TimeRange1Hour),
		m.fetchHistoricalDataCmd(targetName, TimeRange1Day),
		m.fetchHistoricalDataCmd(targetName, TimeRange1Week),
	)
}

// fetchHistoricalDataCmd returns the appropriate command based on mode (direct or IPC)
func (m Model) fetchHistoricalDataCmd(targetName string, tr TimeRange) tea.Cmd {
	if m.IsIPCMode() {
		return fetchHistoricalDataIPC(m.ipcClient, targetName, tr)
	}
	return fetchHistoricalData(m.collector, targetName, tr)
}

// waitForResult creates a command that waits for a probe result
func waitForResult(ch <-chan probe.ProbeResult) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return ErrMsg{Err: nil} // Channel closed
		}
		return ProbeResultMsg(result)
	}
}

// fetchHistoricalData creates a command to fetch historical data from storage
func fetchHistoricalData(coll *collector.Collector, targetName string, tr TimeRange) tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		from := now.Add(-tr.Duration())

		data, err := coll.FetchHistory(targetName, from, now)
		if err != nil {
			return HistoricalDataMsg{
				TargetName: targetName,
				TimeRange:  tr,
				Err:        err,
			}
		}

		// Calculate stats from data (pass base step for time-span-based sample count)
		stats := calculatePeriodStats(data, 10*time.Second)

		return HistoricalDataMsg{
			TargetName: targetName,
			TimeRange:  tr,
			Data:       data,
			Stats:      stats,
		}
	}
}

// waitForIPCResult creates a command that waits for a probe result from IPC
func waitForIPCResult(ch <-chan ipc.ProbeResultData) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return ErrMsg{Err: nil} // Channel closed
		}
		return IPCProbeResultMsg(result)
	}
}

// fetchHistoricalDataIPC creates a command to fetch historical data via IPC
func fetchHistoricalDataIPC(client *ipc.Client, targetName string, tr TimeRange) tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		from := now.Add(-tr.Duration())

		data, err := client.GetHistory(targetName, from, now)
		if err != nil {
			return HistoricalDataMsg{
				TargetName: targetName,
				TimeRange:  tr,
				Err:        err,
			}
		}

		// Calculate stats from data (pass base step for time-span-based sample count)
		stats := calculatePeriodStats(data, 10*time.Second)

		return HistoricalDataMsg{
			TargetName: targetName,
			TimeRange:  tr,
			Data:       data,
			Stats:      stats,
		}
	}
}

// fetchStatsIPC creates a command to fetch stats via IPC
func fetchStatsIPC(client *ipc.Client, targetName string) tea.Cmd {
	return func() tea.Msg {
		stats, err := client.GetStats(targetName)
		return IPCStatsMsg{
			TargetName: targetName,
			Stats:      stats,
			Err:        err,
		}
	}
}
