package probe

import (
	"context"
	"fmt"
	"math"
	"net"
	"time"
)

// TCPProbe implements TCP connection probing
type TCPProbe struct {
	BaseProbe
	Port int
}

// NewTCPProbe creates a new TCP probe for the given target
func NewTCPProbe(name, host string, port int, timeout time.Duration, pings int) *TCPProbe {
	if pings < 1 {
		pings = 1
	}
	return &TCPProbe{
		BaseProbe: BaseProbe{
			TargetName: name,
			TargetHost: host,
			Timeout:    timeout,
			Pings:      pings,
		},
		Port: port,
	}
}

// Type returns "tcp"
func (p *TCPProbe) Type() string {
	return "tcp"
}

// Execute performs TCP connection burst and returns the result with statistics
func (p *TCPProbe) Execute(ctx context.Context) ProbeResult {
	address := fmt.Sprintf("%s:%d", p.TargetHost, p.Port)

	// Create a dialer with per-ping timeout
	dialer := &net.Dialer{
		Timeout: p.Timeout / time.Duration(p.Pings), // Divide timeout among pings
	}

	// Ensure minimum 1 second timeout per ping
	if dialer.Timeout < time.Second {
		dialer.Timeout = time.Second
	}

	var rtts []time.Duration
	var minRtt, maxRtt time.Duration
	var totalRtt time.Duration
	packetsSent := 0
	packetsRecv := 0

	// Perform burst of TCP connections
	for i := 0; i < p.Pings; i++ {
		// Check context before each ping
		select {
		case <-ctx.Done():
			break
		default:
		}

		packetsSent++
		start := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", address)
		latency := time.Since(start)

		if err != nil {
			// Connection failed - count as lost packet
			continue
		}

		// Close the connection immediately
		conn.Close()

		// Record successful ping
		packetsRecv++
		rtts = append(rtts, latency)
		totalRtt += latency

		if minRtt == 0 || latency < minRtt {
			minRtt = latency
		}
		if latency > maxRtt {
			maxRtt = latency
		}

		// Small delay between pings to avoid overwhelming the target
		if i < p.Pings-1 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Calculate statistics
	var avgRtt, stdDevRtt time.Duration
	if packetsRecv > 0 {
		avgRtt = totalRtt / time.Duration(packetsRecv)

		// Calculate standard deviation
		if packetsRecv > 1 {
			var sumSquares float64
			avgNs := float64(avgRtt.Nanoseconds())
			for _, rtt := range rtts {
				diff := float64(rtt.Nanoseconds()) - avgNs
				sumSquares += diff * diff
			}
			stdDevRtt = time.Duration(math.Sqrt(sumSquares / float64(packetsRecv)))
		}
	}

	burstStats := BurstStats{
		Rtts:        rtts,
		PacketsSent: packetsSent,
		PacketsRecv: packetsRecv,
		MinRtt:      minRtt,
		MaxRtt:      maxRtt,
		AvgRtt:      avgRtt,
		StdDevRtt:   stdDevRtt,
	}

	return p.NewBurstResult(burstStats, nil)
}
