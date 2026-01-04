package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette
var (
	ColorPrimary   = lipgloss.Color("#7C3AED") // Purple
	ColorSecondary = lipgloss.Color("#06B6D4") // Cyan
	ColorSuccess   = lipgloss.Color("#10B981") // Green
	ColorWarning   = lipgloss.Color("#F59E0B") // Yellow
	ColorDanger    = lipgloss.Color("#EF4444") // Red
	ColorMuted     = lipgloss.Color("#6B7280") // Gray
	ColorBg        = lipgloss.Color("#1F2937") // Dark background
	ColorBgLight   = lipgloss.Color("#374151") // Lighter background
	ColorText      = lipgloss.Color("#F9FAFB") // Light text
)

// Base styles
var (
	BaseStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// Header style
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Padding(0, 1)

	// Title style
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText).
			Background(ColorPrimary).
			Padding(0, 1)

	// Subtitle style
	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 1)

	// Table header style
	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorSecondary).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(ColorMuted).
				Padding(0, 1)

	// Table row style
	TableRowStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// Selected row style
	SelectedRowStyle = lipgloss.NewStyle().
				Background(ColorBgLight).
				Foreground(ColorText).
				Padding(0, 1)

	// Status styles
	LatencyGoodStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
	LatencyWarnStyle = lipgloss.NewStyle().Foreground(ColorWarning)
	LatencyBadStyle  = lipgloss.NewStyle().Foreground(ColorDanger)
	LossStyle        = lipgloss.NewStyle().Foreground(ColorDanger)
	SuccessStyle     = lipgloss.NewStyle().Foreground(ColorSuccess)

	// Help style
	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 1)

	// Help key style
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	// Border style
	BorderStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted)

	// Error style
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true)

	// Sparkline styles
	SparklineStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	SparklineLossStyle = lipgloss.NewStyle().
				Foreground(ColorDanger)
)

// LatencyStyle returns the appropriate style based on latency value
func LatencyStyle(ms float64) lipgloss.Style {
	switch {
	case ms < 0:
		return LossStyle
	case ms < 50:
		return LatencyGoodStyle
	case ms < 200:
		return LatencyWarnStyle
	default:
		return LatencyBadStyle
	}
}

// LossPercentStyle returns the appropriate style based on loss percentage
func LossPercentStyle(pct float64) lipgloss.Style {
	switch {
	case pct == 0:
		return SuccessStyle
	case pct < 5:
		return LatencyWarnStyle
	default:
		return LossStyle
	}
}

// FormatLatency formats a latency value with color
func FormatLatency(ms float64) string {
	if ms < 0 {
		return LossStyle.Render("--")
	}
	style := LatencyStyle(ms)
	return style.Render(formatMs(ms))
}

// FormatLoss formats a loss percentage with color
func FormatLoss(pct float64) string {
	style := LossPercentStyle(pct)
	return style.Render(formatPercent(pct))
}

// formatMs formats milliseconds nicely
func formatMs(ms float64) string {
	if ms < 1 {
		return "<1ms"
	}
	if ms < 10 {
		return lipgloss.NewStyle().Width(6).Align(lipgloss.Right).Render(
			sprintf("%.1fms", ms),
		)
	}
	return lipgloss.NewStyle().Width(6).Align(lipgloss.Right).Render(
		sprintf("%dms", int(ms)),
	)
}

// formatPercent formats percentage
func formatPercent(pct float64) string {
	if pct == 0 {
		return "0.0%"
	}
	return sprintf("%.1f%%", pct)
}

// sprintf is a simple wrapper
func sprintf(format string, args ...interface{}) string {
	return lipgloss.NewStyle().Render(
		stringFormat(format, args...),
	)
}

// stringFormat formats strings (avoiding fmt import in hot path)
func stringFormat(format string, args ...interface{}) string {
	// Simple implementation for common cases
	result := format
	for _, arg := range args {
		switch v := arg.(type) {
		case float64:
			if contains(result, "%.1f") {
				result = replaceFirst(result, "%.1f", formatFloat(v, 1))
			}
		case int:
			if contains(result, "%d") {
				result = replaceFirst(result, "%d", formatInt(v))
			}
		}
	}
	return result
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func replaceFirst(s, old, new string) string {
	for i := 0; i <= len(s)-len(old); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}

func formatFloat(f float64, decimals int) string {
	// Simple float formatting
	intPart := int(f)
	fracPart := int((f - float64(intPart)) * 10)
	if fracPart < 0 {
		fracPart = -fracPart
	}
	if decimals == 1 {
		return formatInt(intPart) + "." + formatInt(fracPart)
	}
	return formatInt(intPart)
}

func formatInt(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + formatInt(-i)
	}
	result := ""
	for i > 0 {
		result = string(rune('0'+i%10)) + result
		i /= 10
	}
	return result
}
