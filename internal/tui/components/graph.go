package components

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// GraphPoint represents a single data point for graphing
type GraphPoint struct {
	Timestamp time.Time
	Value     float64 // Latency in ms, -1 for packet loss
}

// GraphConfig configures graph rendering
type GraphConfig struct {
	Width      int     // Total width including Y-axis labels
	Height     int     // Graph area height (excluding X-axis)
	ShowYAxis  bool    // Show Y-axis with labels
	ShowXAxis  bool    // Show X-axis with time labels
	MinY       float64 // Minimum Y value (auto if both 0)
	MaxY       float64 // Maximum Y value (auto if both 0)
	YAxisWidth int     // Width of Y-axis label area
}

// DefaultGraphConfig returns sensible defaults
func DefaultGraphConfig() GraphConfig {
	return GraphConfig{
		Width:      60,
		Height:     8,
		ShowYAxis:  true,
		ShowXAxis:  true,
		MinY:       0,
		MaxY:       0, // Auto-scale
		YAxisWidth: 8,
	}
}

// Graph colors
var (
	graphAxisStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	graphLineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#06B6D4"))
	graphLossStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
)

// Graph renders an ASCII line graph from data points
func Graph(points []GraphPoint, config GraphConfig) string {
	if len(points) == 0 {
		return renderEmptyGraph(config)
	}

	// Determine time range from data
	from := points[0].Timestamp
	to := points[len(points)-1].Timestamp
	if to.Before(from) {
		from, to = to, from
	}

	return GraphWithRange(points, from, to, config)
}

// GraphWithRange renders with explicit time range
func GraphWithRange(points []GraphPoint, from, to time.Time, config GraphConfig) string {
	if len(points) == 0 {
		return renderEmptyGraph(config)
	}

	// Calculate graph dimensions
	graphWidth := config.Width
	if config.ShowYAxis {
		graphWidth -= config.YAxisWidth
	}
	if graphWidth < 10 {
		graphWidth = 10
	}

	// Calculate Y-axis range
	minY, maxY, ticks := calculateYRange(points, config.MinY, config.MaxY, config.Height)

	// Create canvas (2x vertical resolution using half-blocks)
	canvasHeight := config.Height * 2 // Each row has upper and lower half
	canvas := make([][]rune, config.Height)
	for i := range canvas {
		canvas[i] = make([]rune, graphWidth)
		for j := range canvas[i] {
			canvas[i][j] = ' '
		}
	}

	// Track packet loss positions
	lossPositions := make([]bool, graphWidth)

	// Plot points
	timeRange := to.Sub(from)
	if timeRange == 0 {
		timeRange = time.Second // Avoid division by zero
	}

	prevX, prevY := -1, -1
	for _, point := range points {
		// Calculate X position
		xRatio := float64(point.Timestamp.Sub(from)) / float64(timeRange)
		x := int(xRatio * float64(graphWidth-1))
		if x < 0 {
			x = 0
		}
		if x >= graphWidth {
			x = graphWidth - 1
		}

		if point.Value < 0 || math.IsNaN(point.Value) {
			// Packet loss
			lossPositions[x] = true
			prevX, prevY = -1, -1
			continue
		}

		// Calculate Y position (0 = top, canvasHeight-1 = bottom)
		yRatio := (point.Value - minY) / (maxY - minY)
		if yRatio < 0 {
			yRatio = 0
		}
		if yRatio > 1 {
			yRatio = 1
		}
		y := canvasHeight - 1 - int(yRatio*float64(canvasHeight-1))

		// Draw point and connect to previous
		if prevX >= 0 && prevY >= 0 {
			drawLine(canvas, prevX, prevY, x, y, canvasHeight)
		} else {
			drawPoint(canvas, x, y, canvasHeight)
		}

		prevX, prevY = x, y
	}

	// Render canvas to string
	var result strings.Builder

	for row := 0; row < config.Height; row++ {
		// Y-axis label
		if config.ShowYAxis {
			label := formatYLabel(ticks, row, config.Height, config.YAxisWidth)
			result.WriteString(graphAxisStyle.Render(label))
		}

		// Graph row with colors
		for col := 0; col < graphWidth; col++ {
			ch := canvas[row][col]
			if ch == ' ' {
				result.WriteRune(' ')
			} else {
				result.WriteString(graphLineStyle.Render(string(ch)))
			}
		}
		result.WriteString("\n")
	}

	// Draw packet loss markers at bottom
	if hasLoss(lossPositions) {
		if config.ShowYAxis {
			result.WriteString(strings.Repeat(" ", config.YAxisWidth))
		}
		for col := 0; col < graphWidth; col++ {
			if lossPositions[col] {
				result.WriteString(graphLossStyle.Render("×"))
			} else {
				result.WriteRune(' ')
			}
		}
		result.WriteString("\n")
	}

	// X-axis with time labels
	if config.ShowXAxis {
		result.WriteString(renderXAxis(from, to, graphWidth, config.YAxisWidth, config.ShowYAxis))
	}

	return result.String()
}

// calculateYRange determines min/max Y with nice tick marks
func calculateYRange(points []GraphPoint, forcedMin, forcedMax float64, numTicks int) (min, max float64, ticks []float64) {
	// Find data range (excluding packet loss)
	dataMin, dataMax := math.MaxFloat64, -math.MaxFloat64
	hasData := false
	for _, p := range points {
		if p.Value >= 0 && !math.IsNaN(p.Value) {
			if p.Value < dataMin {
				dataMin = p.Value
			}
			if p.Value > dataMax {
				dataMax = p.Value
			}
			hasData = true
		}
	}

	if !hasData {
		return 0, 100, []float64{0, 50, 100}
	}

	// Use forced values if provided
	if forcedMin != 0 || forcedMax != 0 {
		min, max = forcedMin, forcedMax
	} else {
		// Add 10% padding
		padding := (dataMax - dataMin) * 0.1
		if padding < 1 {
			padding = 1
		}
		min = 0 // Always start from 0 for latency
		max = dataMax + padding
	}

	// Generate nice tick values
	tickSpacing := niceNum((max-min)/float64(numTicks-1), true)
	niceMin := math.Floor(min/tickSpacing) * tickSpacing
	niceMax := math.Ceil(max/tickSpacing) * tickSpacing

	min, max = niceMin, niceMax
	if min < 0 {
		min = 0
	}

	// Generate tick marks
	ticks = make([]float64, 0, numTicks+1)
	for tick := min; tick <= max+tickSpacing*0.5; tick += tickSpacing {
		ticks = append(ticks, tick)
	}

	return min, max, ticks
}

// niceNum finds a "nice" number approximately equal to x
func niceNum(x float64, round bool) float64 {
	if x <= 0 {
		return 1
	}
	exp := math.Floor(math.Log10(x))
	f := x / math.Pow(10, exp)
	var nf float64
	if round {
		if f < 1.5 {
			nf = 1
		} else if f < 3 {
			nf = 2
		} else if f < 7 {
			nf = 5
		} else {
			nf = 10
		}
	} else {
		if f <= 1 {
			nf = 1
		} else if f <= 2 {
			nf = 2
		} else if f <= 5 {
			nf = 5
		} else {
			nf = 10
		}
	}
	return nf * math.Pow(10, exp)
}

// drawPoint draws a single point on the canvas
func drawPoint(canvas [][]rune, x, y, canvasHeight int) {
	row := y / 2
	isUpper := y%2 == 0

	if row < 0 || row >= len(canvas) || x < 0 || x >= len(canvas[0]) {
		return
	}

	existing := canvas[row][x]
	if isUpper {
		if existing == '▄' || existing == '█' {
			canvas[row][x] = '█'
		} else {
			canvas[row][x] = '▀'
		}
	} else {
		if existing == '▀' || existing == '█' {
			canvas[row][x] = '█'
		} else {
			canvas[row][x] = '▄'
		}
	}
}

// drawLine draws a line between two points
func drawLine(canvas [][]rune, x1, y1, x2, y2, canvasHeight int) {
	// Bresenham's line algorithm
	dx := abs(x2 - x1)
	dy := abs(y2 - y1)
	sx := 1
	if x1 > x2 {
		sx = -1
	}
	sy := 1
	if y1 > y2 {
		sy = -1
	}
	err := dx - dy

	for {
		drawPoint(canvas, x1, y1, canvasHeight)
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x1 += sx
		}
		if e2 < dx {
			err += dx
			y1 += sy
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// formatYLabel formats the Y-axis label for a given row
func formatYLabel(ticks []float64, row, height, width int) string {
	// Find the tick value closest to this row
	if len(ticks) == 0 {
		return strings.Repeat(" ", width)
	}

	// Map row to value (row 0 = top = max, row height-1 = bottom = min)
	maxTick := ticks[len(ticks)-1]
	minTick := ticks[0]

	// Check if there's a tick near this row
	for _, tick := range ticks {
		tickRow := int((maxTick - tick) / (maxTick - minTick) * float64(height-1))
		if tickRow == row {
			label := formatLatencyShort(tick)
			// Right-align with axis character
			padded := strings.Repeat(" ", width-len(label)-1) + label + "┤"
			return padded
		}
	}

	// No tick at this row, just draw axis
	return strings.Repeat(" ", width-1) + "│"
}

// formatLatencyShort formats latency for Y-axis labels
func formatLatencyShort(ms float64) string {
	if ms >= 1000 {
		return strings.TrimRight(strings.TrimRight(
			strings.Replace(formatFloat(ms/1000, 1), ".0", "", 1), "0"), ".") + "s"
	}
	if ms >= 100 {
		return formatInt(int(ms)) + "ms"
	}
	if ms >= 10 {
		return formatInt(int(ms)) + "ms"
	}
	if ms >= 1 {
		return formatFloat(ms, 0) + "ms"
	}
	return "<1ms"
}

func formatFloat(f float64, decimals int) string {
	intPart := int(f)
	if decimals == 0 {
		return formatInt(intPart)
	}
	fracPart := int((f - float64(intPart)) * math.Pow(10, float64(decimals)))
	if fracPart < 0 {
		fracPart = -fracPart
	}
	return formatInt(intPart) + "." + formatInt(fracPart)
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

// renderXAxis renders the X-axis with time labels
func renderXAxis(from, to time.Time, width, yAxisWidth int, showYAxis bool) string {
	var result strings.Builder

	// Axis line
	if showYAxis {
		result.WriteString(strings.Repeat(" ", yAxisWidth-1))
		result.WriteString("└")
	}
	result.WriteString(strings.Repeat("─", width))
	result.WriteString("\n")

	// Time labels
	if showYAxis {
		result.WriteString(strings.Repeat(" ", yAxisWidth))
	}

	duration := to.Sub(from)
	fromLabel := formatTimeLabel(from, duration)
	toLabel := formatTimeLabel(to, duration)

	// Left label, padding, right label
	padding := width - len(fromLabel) - len(toLabel)
	if padding < 1 {
		padding = 1
	}

	result.WriteString(graphAxisStyle.Render(fromLabel))
	result.WriteString(strings.Repeat(" ", padding))
	result.WriteString(graphAxisStyle.Render(toLabel))

	return result.String()
}

// formatTimeLabel formats a time based on the duration being displayed
func formatTimeLabel(t time.Time, duration time.Duration) string {
	switch {
	case duration <= time.Hour:
		return t.Format("15:04:05")
	case duration <= 24*time.Hour:
		return t.Format("15:04")
	default:
		return t.Format("Jan 02 15:04")
	}
}

// renderEmptyGraph renders an empty graph placeholder
func renderEmptyGraph(config GraphConfig) string {
	var result strings.Builder

	graphWidth := config.Width
	if config.ShowYAxis {
		graphWidth -= config.YAxisWidth
	}

	for row := 0; row < config.Height; row++ {
		if config.ShowYAxis {
			result.WriteString(strings.Repeat(" ", config.YAxisWidth-1))
			if row == config.Height-1 {
				result.WriteString("└")
			} else {
				result.WriteString("│")
			}
		}
		if row == config.Height/2 {
			msg := "No data"
			padding := (graphWidth - len(msg)) / 2
			result.WriteString(strings.Repeat(" ", padding))
			result.WriteString(graphAxisStyle.Render(msg))
			result.WriteString(strings.Repeat(" ", graphWidth-padding-len(msg)))
		} else {
			result.WriteString(strings.Repeat(" ", graphWidth))
		}
		result.WriteString("\n")
	}

	if config.ShowXAxis {
		if config.ShowYAxis {
			result.WriteString(strings.Repeat(" ", config.YAxisWidth-1))
			result.WriteString("└")
		}
		result.WriteString(strings.Repeat("─", graphWidth))
	}

	return result.String()
}

// hasLoss checks if there are any packet loss positions
func hasLoss(positions []bool) bool {
	for _, p := range positions {
		if p {
			return true
		}
	}
	return false
}

// GraphFromFloats creates GraphPoints from a slice of latencies (for realtime data)
func GraphFromFloats(values []float64, interval time.Duration) []GraphPoint {
	now := time.Now()
	points := make([]GraphPoint, len(values))
	for i, v := range values {
		points[i] = GraphPoint{
			Timestamp: now.Add(-time.Duration(len(values)-1-i) * interval),
			Value:     v,
		}
	}
	return points
}
