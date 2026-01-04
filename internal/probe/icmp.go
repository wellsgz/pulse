package probe

import (
	"context"
	"fmt"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// ICMPProbe implements ICMP ping probing
type ICMPProbe struct {
	BaseProbe
	privileged bool
}

// NewICMPProbe creates a new ICMP probe for the given target
func NewICMPProbe(name, host string, timeout time.Duration, pings int) *ICMPProbe {
	if pings < 1 {
		pings = 1
	}
	return &ICMPProbe{
		BaseProbe: BaseProbe{
			TargetName: name,
			TargetHost: host,
			Timeout:    timeout,
			Pings:      pings,
		},
		privileged: true, // Try privileged mode first
	}
}

// Type returns "icmp"
func (p *ICMPProbe) Type() string {
	return "icmp"
}

// Execute performs ICMP ping burst and returns the result with statistics
func (p *ICMPProbe) Execute(ctx context.Context) ProbeResult {
	pinger, err := probing.NewPinger(p.TargetHost)
	if err != nil {
		return p.NewResult(0, false, fmt.Errorf("failed to create pinger: %w", err))
	}

	// Configure for burst mode
	pinger.Count = p.Pings
	pinger.SetPrivileged(p.privileged)

	// Set interval between outgoing pings (default 1s is too slow for bursts)
	// SmokePing/fping typically uses 10-50ms intervals
	pinger.Interval = 50 * time.Millisecond

	// Calculate burst timeout: allow 250ms max per ping for response collection
	// This ensures adequate time for network latency without exceeding bounds
	perPingAllowance := 250 * time.Millisecond
	burstTimeout := time.Duration(p.Pings) * perPingAllowance
	if burstTimeout < p.Timeout {
		burstTimeout = p.Timeout // Use configured timeout as minimum
	}
	pinger.Timeout = burstTimeout

	// Collect individual RTTs for median calculation
	var rtts []time.Duration
	pinger.OnRecv = func(pkt *probing.Packet) {
		rtts = append(rtts, pkt.Rtt)
	}

	// Run the ping burst
	err = pinger.RunWithContext(ctx)
	if err != nil {
		// If privileged mode fails, try unprivileged mode
		if p.privileged {
			p.privileged = false
			pinger.SetPrivileged(false)
			rtts = nil // Reset RTTs
			err = pinger.RunWithContext(ctx)
		}
		if err != nil {
			return p.NewResult(0, false, fmt.Errorf("ping failed: %w", err))
		}
	}

	stats := pinger.Statistics()

	// Create burst statistics
	burstStats := BurstStats{
		Rtts:        rtts,
		PacketsSent: stats.PacketsSent,
		PacketsRecv: stats.PacketsRecv,
		MinRtt:      stats.MinRtt,
		MaxRtt:      stats.MaxRtt,
		AvgRtt:      stats.AvgRtt,
		StdDevRtt:   stats.StdDevRtt,
	}

	return p.NewBurstResult(burstStats, nil)
}
