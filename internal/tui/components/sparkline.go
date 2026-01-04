package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Sparkline block characters from lowest to highest
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Colors for sparkline
var (
	sparkNormalColor = lipgloss.Color("#06B6D4") // Cyan
	sparkLossColor   = lipgloss.Color("#EF4444") // Red
)

// Sparkline generates a sparkline string from latency values
// Values less than 0 are treated as packet loss
func Sparkline(values []float64, width int) string {
	if len(values) == 0 {
		return strings.Repeat(" ", width)
	}

	// Take last 'width' values
	start := 0
	if len(values) > width {
		start = len(values) - width
	}
	values = values[start:]

	// Find min/max for scaling (excluding packet loss)
	min, max := -1.0, -1.0
	for _, v := range values {
		if v >= 0 {
			if min < 0 || v < min {
				min = v
			}
			if max < 0 || v > max {
				max = v
			}
		}
	}

	// If all values are packet loss, show all loss markers
	if min < 0 {
		lossStyle := lipgloss.NewStyle().Foreground(sparkLossColor)
		return lossStyle.Render(strings.Repeat("×", len(values))) + strings.Repeat(" ", width-len(values))
	}

	// Ensure we have a range to scale
	if max == min {
		max = min + 1
	}

	normalStyle := lipgloss.NewStyle().Foreground(sparkNormalColor)
	lossStyle := lipgloss.NewStyle().Foreground(sparkLossColor)

	var result strings.Builder
	for _, v := range values {
		if v < 0 {
			// Packet loss
			result.WriteString(lossStyle.Render("×"))
		} else {
			// Scale to 0-7 range
			scaled := (v - min) / (max - min)
			idx := int(scaled * 7)
			if idx > 7 {
				idx = 7
			}
			if idx < 0 {
				idx = 0
			}
			result.WriteString(normalStyle.Render(string(sparkBlocks[idx])))
		}
	}

	// Pad if needed
	padding := width - len(values)
	if padding > 0 {
		result.WriteString(strings.Repeat(" ", padding))
	}

	return result.String()
}

// SparklineWithRange generates a sparkline with a fixed min/max range
func SparklineWithRange(values []float64, width int, min, max float64) string {
	if len(values) == 0 {
		return strings.Repeat(" ", width)
	}

	// Take last 'width' values
	start := 0
	if len(values) > width {
		start = len(values) - width
	}
	values = values[start:]

	// Ensure we have a range to scale
	if max <= min {
		max = min + 1
	}

	normalStyle := lipgloss.NewStyle().Foreground(sparkNormalColor)
	lossStyle := lipgloss.NewStyle().Foreground(sparkLossColor)

	var result strings.Builder
	for _, v := range values {
		if v < 0 {
			// Packet loss
			result.WriteString(lossStyle.Render("×"))
		} else {
			// Scale to 0-7 range
			scaled := (v - min) / (max - min)
			if scaled > 1 {
				scaled = 1
			}
			if scaled < 0 {
				scaled = 0
			}
			idx := int(scaled * 7)
			if idx > 7 {
				idx = 7
			}
			result.WriteString(normalStyle.Render(string(sparkBlocks[idx])))
		}
	}

	// Pad if needed
	padding := width - len(values)
	if padding > 0 {
		result.WriteString(strings.Repeat(" ", padding))
	}

	return result.String()
}
