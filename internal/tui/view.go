package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/wellsgz/pulse/internal/storage"
	"github.com/wellsgz/pulse/internal/tui/components"
)

// View renders the current view
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	switch m.currentView {
	case ListView:
		return m.renderListView()
	case DetailView:
		return m.renderDetailView()
	default:
		return m.renderListView()
	}
}

// renderListView renders the main list view
func (m Model) renderListView() string {
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Error (if any)
	if m.err != nil {
		b.WriteString(m.renderError())
		b.WriteString("\n")
	}

	// Table
	b.WriteString(m.renderTable())

	// Help
	b.WriteString("\n")
	b.WriteString(m.renderHelp())

	return b.String()
}

// renderError renders an error message
func (m Model) renderError() string {
	errorBox := lipgloss.NewStyle().
		Foreground(ColorDanger).
		Background(lipgloss.Color("#3F1F1F")).
		Padding(0, 1).
		Width(m.width - 2).
		Render("Error: " + m.err.Error())
	return errorBox
}

// renderHeader renders the application header
func (m Model) renderHeader() string {
	title := TitleStyle.Render(" pulse ")
	subtitle := SubtitleStyle.Render("Network Latency Monitor")
	apiInfo := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Render(fmt.Sprintf("API: %s", m.apiAddr))

	left := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", subtitle)

	// Calculate spacing
	spacing := m.width - lipgloss.Width(left) - lipgloss.Width(apiInfo) - 2
	if spacing < 1 {
		spacing = 1
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		left,
		strings.Repeat(" ", spacing),
		apiInfo,
	)
}

// renderTable renders the targets table
func (m Model) renderTable() string {
	columns := components.AdaptiveColumns(m.width)
	table := components.NewTable(columns)

	var rows []string

	// Header
	rows = append(rows, table.RenderHeader())
	rows = append(rows, table.RenderSeparator())

	// Rows
	for i, target := range m.targets {
		row := m.renderTargetRow(target, columns[4].Width)
		rows = append(rows, table.RenderRow(row, i == m.selectedIdx))
	}

	return strings.Join(rows, "\n")
}

// renderTargetRow renders a single target row
func (m Model) renderTargetRow(target TargetState, sparklineWidth int) []string {
	// Name
	name := target.Config.Name
	if len(name) > 16 {
		name = name[:15] + "…"
	}

	// Get stats
	var lastMs, avgMs, lossPct float64
	if target.Stats != nil {
		lastMs = target.Stats.LastMs
		avgMs = target.Stats.AvgMs
		lossPct = target.Stats.LossPct
	}

	// Format values with colors
	last := FormatLatency(lastMs)
	avg := FormatLatency(avgMs)
	loss := FormatLoss(lossPct)

	// Sparkline
	sparkline := components.Sparkline(target.History, sparklineWidth)

	return []string{name, last, avg, loss, sparkline}
}

// renderDetailView renders the detail view for a selected target
func (m Model) renderDetailView() string {
	target := m.SelectedTarget()
	if target == nil {
		return "No target selected"
	}

	var b strings.Builder

	// Header
	headerText := fmt.Sprintf(" %s (%s) - %s ",
		target.Config.Name,
		target.Config.Host,
		strings.ToUpper(target.Config.Probe))
	header := TitleStyle.Render(headerText)
	backHint := lipgloss.NewStyle().Foreground(ColorMuted).Render("[Esc] back")

	spacing := m.width - lipgloss.Width(header) - lipgloss.Width(backHint) - 2
	if spacing < 1 {
		spacing = 1
	}

	b.WriteString(lipgloss.JoinHorizontal(
		lipgloss.Center,
		header,
		strings.Repeat(" ", spacing),
		backHint,
	))
	b.WriteString("\n\n")

	// Error (if any)
	if m.err != nil {
		b.WriteString(m.renderError())
		b.WriteString("\n\n")
	}

	// Stats section - show appropriate stats based on time range
	b.WriteString(m.renderStatsSection(target))
	b.WriteString("\n")

	// Graph section with time range tabs
	b.WriteString(m.renderGraphSection(target))
	b.WriteString("\n")

	// Historical summary
	b.WriteString(m.renderHistoricalSummary(target))
	b.WriteString("\n")

	// Help
	b.WriteString(m.renderDetailHelp())

	return b.String()
}

// renderGraphSection renders the graph with time range tabs
func (m Model) renderGraphSection(target *TargetState) string {
	var b strings.Builder

	// Section header with tabs
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
	b.WriteString(sectionStyle.Render("Latency Graph"))
	b.WriteString("  ")
	b.WriteString(m.renderTimeRangeTabs(target.TimeRange))
	b.WriteString("\n")

	// Calculate graph dimensions
	graphWidth := m.width - 4
	if graphWidth > 80 {
		graphWidth = 80
	}
	graphHeight := 8

	// Loading indicator
	if target.LoadingHistory {
		loadingStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
		b.WriteString("  ")
		b.WriteString(loadingStyle.Render("Loading..."))
		b.WriteString("\n")
		return b.String()
	}

	// Determine which data to display
	var graphPoints []components.GraphPoint
	var from, to time.Time

	if target.TimeRange == TimeRangeRealtime {
		// Use memory buffer history
		graphPoints = components.GraphFromFloats(target.History, 10*time.Second)
		if len(graphPoints) > 0 {
			from = graphPoints[0].Timestamp
			to = graphPoints[len(graphPoints)-1].Timestamp
		}
	} else {
		// Use historical data from storage
		if len(target.HistoricalData) > 0 {
			graphPoints = make([]components.GraphPoint, len(target.HistoricalData))
			for i, dp := range target.HistoricalData {
				graphPoints[i] = components.GraphPoint{
					Timestamp: dp.Timestamp,
					Value:     dp.Value,
				}
			}
			from = target.HistoricalData[0].Timestamp
			to = target.HistoricalData[len(target.HistoricalData)-1].Timestamp
		}
	}

	// Render graph
	config := components.GraphConfig{
		Width:      graphWidth,
		Height:     graphHeight,
		ShowYAxis:  true,
		ShowXAxis:  true,
		YAxisWidth: 8,
	}

	if len(graphPoints) > 0 {
		b.WriteString(components.GraphWithRange(graphPoints, from, to, config))
	} else {
		b.WriteString(components.Graph(nil, config))
	}

	return b.String()
}

// renderTimeRangeTabs renders the time range selector tabs
func (m Model) renderTimeRangeTabs(selected TimeRange) string {
	ranges := []TimeRange{TimeRangeRealtime, TimeRange1Hour, TimeRange1Day, TimeRange1Week}

	selectedStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorText).
		Background(ColorPrimary).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Padding(0, 1)

	var tabs []string
	for _, tr := range ranges {
		if tr == selected {
			tabs = append(tabs, selectedStyle.Render(tr.String()))
		} else {
			tabs = append(tabs, normalStyle.Render(tr.String()))
		}
	}

	return "[" + strings.Join(tabs, "|") + "]"
}

// renderHistoricalSummary renders the historical statistics summary
func (m Model) renderHistoricalSummary(target *TargetState) string {
	var b strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted).Width(12)

	b.WriteString(sectionStyle.Render("Historical Summary"))
	b.WriteString("\n")

	if target.HistoricalStats == nil {
		b.WriteString("  ")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("Loading historical data..."))
		b.WriteString("\n")
		return b.String()
	}

	// Last Hour
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Last Hour:"))
	if target.HistoricalStats.Hour != nil {
		b.WriteString(m.formatPeriodStats(target.HistoricalStats.Hour))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("No data"))
	}
	b.WriteString("\n")

	// Last Day
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Last Day:"))
	if target.HistoricalStats.Day != nil {
		b.WriteString(m.formatPeriodStats(target.HistoricalStats.Day))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("No data"))
	}
	b.WriteString("\n")

	// Last Week
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Last Week:"))
	if target.HistoricalStats.Week != nil {
		b.WriteString(m.formatPeriodStats(target.HistoricalStats.Week))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("No data"))
	}
	b.WriteString("\n")

	return b.String()
}

// formatPeriodStats formats period statistics
func (m Model) formatPeriodStats(stats *PeriodStats) string {
	avgStyle := LatencyStyle(stats.AvgMs)
	lossStyle := LossPercentStyle(stats.LossPct)

	return fmt.Sprintf("Avg %s, Loss %s  (%d samples)",
		avgStyle.Render(fmt.Sprintf("%.1fms", stats.AvgMs)),
		lossStyle.Render(fmt.Sprintf("%.1f%%", stats.LossPct)),
		stats.Samples,
	)
}

// renderStatsSection renders statistics based on the current time range
func (m Model) renderStatsSection(target *TargetState) string {
	if target.TimeRange == TimeRangeRealtime {
		// Show real-time stats from memory buffer
		if target.Stats != nil {
			return m.renderStats(target.Stats, "current run")
		}
		return ""
	}

	// Show historical stats for selected time range
	if target.HistoricalStats == nil {
		sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
		return sectionStyle.Render("Statistics") + " (loading...)\n"
	}

	var periodStats *PeriodStats
	var label string

	switch target.TimeRange {
	case TimeRange1Hour:
		periodStats = target.HistoricalStats.Hour
		label = "last hour"
	case TimeRange1Day:
		periodStats = target.HistoricalStats.Day
		label = "last day"
	case TimeRange1Week:
		periodStats = target.HistoricalStats.Week
		label = "last week"
	}

	if periodStats != nil {
		return m.renderPeriodStatsSection(periodStats, label)
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
	return sectionStyle.Render("Statistics") + " (no data for " + label + ")\n"
}

// renderStats renders the statistics section from storage.Stats
func (m Model) renderStats(stats *storage.Stats, label string) string {
	var b strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted).Width(10)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)

	b.WriteString(sectionStyle.Render("Statistics"))
	b.WriteString(fmt.Sprintf(" (%s, %d samples)\n", label, stats.SampleCount))

	// Row 1: Min, Max, Avg
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Min:"))
	b.WriteString(FormatLatency(stats.MinMs))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Max:"))
	b.WriteString(FormatLatency(stats.MaxMs))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Avg:"))
	b.WriteString(FormatLatency(stats.AvgMs))
	b.WriteString("\n")

	// Row 2: Median, P95, StdDev
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Median:"))
	b.WriteString(FormatLatency(stats.MedianMs))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("P95:"))
	b.WriteString(FormatLatency(stats.P95Ms))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("StdDev:"))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%.2fms", stats.StdDevMs)))
	b.WriteString("\n")

	// Row 3: Loss
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Loss:"))
	b.WriteString(FormatLoss(stats.LossPct))
	b.WriteString("\n")

	return b.String()
}

// renderPeriodStatsSection renders the statistics section from PeriodStats
func (m Model) renderPeriodStatsSection(stats *PeriodStats, label string) string {
	var b strings.Builder

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted).Width(10)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)

	b.WriteString(sectionStyle.Render("Statistics"))
	b.WriteString(fmt.Sprintf(" (%s, %d samples)\n", label, stats.Samples))

	// Row 1: Min, Max, Avg
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Min:"))
	b.WriteString(FormatLatency(stats.MinMs))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Max:"))
	b.WriteString(FormatLatency(stats.MaxMs))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Avg:"))
	b.WriteString(FormatLatency(stats.AvgMs))
	b.WriteString("\n")

	// Row 2: Median, P95, StdDev
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Median:"))
	b.WriteString(FormatLatency(stats.MedianMs))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("P95:"))
	b.WriteString(FormatLatency(stats.P95Ms))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("StdDev:"))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%.2fms", stats.StdDevMs)))
	b.WriteString("\n")

	// Row 3: Loss
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("Loss:"))
	b.WriteString(FormatLoss(stats.LossPct))
	b.WriteString("\n")

	return b.String()
}

// renderHelp renders the help footer for list view
func (m Model) renderHelp() string {
	keys := []struct {
		key  string
		desc string
	}{
		{"↑/↓", "navigate"},
		{"Enter", "details"},
		{"r", "refresh"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts,
			HelpKeyStyle.Render(k.key)+
				HelpStyle.Render(" "+k.desc))
	}

	return HelpStyle.Render(strings.Join(parts, "  "))
}

// renderDetailHelp renders the help footer for detail view
func (m Model) renderDetailHelp() string {
	keys := []struct {
		key  string
		desc string
	}{
		{"Esc", "back"},
		{"↑/↓", "targets"},
		{"0-3", "range"},
		{"Tab", "cycle"},
		{"r", "refresh"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts,
			HelpKeyStyle.Render(k.key)+
				HelpStyle.Render(" "+k.desc))
	}

	return HelpStyle.Render(strings.Join(parts, "  "))
}
