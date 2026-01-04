package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Column defines a table column
type Column struct {
	Title string
	Width int
	Align lipgloss.Position
}

// Table renders a simple table
type Table struct {
	Columns     []Column
	HeaderStyle lipgloss.Style
	RowStyle    lipgloss.Style
	SelectedStyle lipgloss.Style
}

// NewTable creates a new table with the given columns
func NewTable(columns []Column) *Table {
	return &Table{
		Columns: columns,
		HeaderStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#06B6D4")).
			Padding(0, 1),
		RowStyle: lipgloss.NewStyle().
			Padding(0, 1),
		SelectedStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("#374151")).
			Foreground(lipgloss.Color("#F9FAFB")).
			Padding(0, 1),
	}
}

// RenderHeader renders the table header
func (t *Table) RenderHeader() string {
	var cells []string
	for _, col := range t.Columns {
		cell := lipgloss.NewStyle().
			Width(col.Width).
			Align(col.Align).
			Render(col.Title)
		cells = append(cells, t.HeaderStyle.Render(cell))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// RenderRow renders a single row
func (t *Table) RenderRow(values []string, selected bool) string {
	style := t.RowStyle
	if selected {
		style = t.SelectedStyle
	}

	var cells []string
	for i, col := range t.Columns {
		value := ""
		if i < len(values) {
			value = values[i]
		}

		// Calculate visible width (accounting for ANSI codes)
		visibleWidth := lipgloss.Width(value)

		var cell string
		if visibleWidth >= col.Width {
			// Value already wide enough or has formatting
			cell = value
		} else {
			// Need to pad
			padding := col.Width - visibleWidth
			switch col.Align {
			case lipgloss.Right:
				cell = strings.Repeat(" ", padding) + value
			case lipgloss.Center:
				left := padding / 2
				right := padding - left
				cell = strings.Repeat(" ", left) + value + strings.Repeat(" ", right)
			default: // Left
				cell = value + strings.Repeat(" ", padding)
			}
		}

		cells = append(cells, style.Render(cell))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// RenderSeparator renders a separator line
func (t *Table) RenderSeparator() string {
	totalWidth := 0
	for _, col := range t.Columns {
		totalWidth += col.Width + 2 // +2 for padding
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Render(strings.Repeat("─", totalWidth))
}

// DefaultColumns returns the default columns for the target list
func DefaultColumns() []Column {
	return []Column{
		{Title: "Target", Width: 16, Align: lipgloss.Left},
		{Title: "Last", Width: 8, Align: lipgloss.Right},
		{Title: "Avg", Width: 8, Align: lipgloss.Right},
		{Title: "Loss", Width: 6, Align: lipgloss.Right},
		{Title: "Sparkline", Width: 20, Align: lipgloss.Left},
	}
}

// AdaptiveColumns returns columns adapted to the terminal width
func AdaptiveColumns(width int) []Column {
	// Minimum widths
	minSparkline := 10
	minTarget := 12

	// Fixed widths
	lastWidth := 8
	avgWidth := 8
	lossWidth := 6

	// Calculate remaining space for target and sparkline
	fixedWidth := lastWidth + avgWidth + lossWidth + 8 // 8 for padding
	remaining := width - fixedWidth

	// Split remaining between target and sparkline
	targetWidth := remaining / 3
	if targetWidth < minTarget {
		targetWidth = minTarget
	}
	if targetWidth > 20 {
		targetWidth = 20
	}

	sparklineWidth := remaining - targetWidth
	if sparklineWidth < minSparkline {
		sparklineWidth = minSparkline
	}

	return []Column{
		{Title: "Target", Width: targetWidth, Align: lipgloss.Left},
		{Title: "Last", Width: lastWidth, Align: lipgloss.Right},
		{Title: "Avg", Width: avgWidth, Align: lipgloss.Right},
		{Title: "Loss", Width: lossWidth, Align: lipgloss.Right},
		{Title: "Sparkline", Width: sparklineWidth, Align: lipgloss.Left},
	}
}
