package probe

import (
	"testing"
	"time"
)

func TestCalculateMedian(t *testing.T) {
	tests := []struct {
		name string
		rtts []time.Duration
		want time.Duration
	}{
		{
			name: "empty",
			rtts: []time.Duration{},
			want: 0,
		},
		{
			name: "single value",
			rtts: []time.Duration{10 * time.Millisecond},
			want: 10 * time.Millisecond,
		},
		{
			name: "odd count",
			rtts: []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				30 * time.Millisecond,
			},
			want: 20 * time.Millisecond,
		},
		{
			name: "even count",
			rtts: []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				30 * time.Millisecond,
				40 * time.Millisecond,
			},
			want: 25 * time.Millisecond, // (20+30)/2
		},
		{
			name: "unsorted input",
			rtts: []time.Duration{
				30 * time.Millisecond,
				10 * time.Millisecond,
				20 * time.Millisecond,
			},
			want: 20 * time.Millisecond,
		},
		{
			name: "typical burst (20 pings)",
			rtts: []time.Duration{
				10 * time.Millisecond, 11 * time.Millisecond, 12 * time.Millisecond, 13 * time.Millisecond,
				14 * time.Millisecond, 15 * time.Millisecond, 16 * time.Millisecond, 17 * time.Millisecond,
				18 * time.Millisecond, 19 * time.Millisecond, 20 * time.Millisecond, 21 * time.Millisecond,
				22 * time.Millisecond, 23 * time.Millisecond, 24 * time.Millisecond, 25 * time.Millisecond,
				26 * time.Millisecond, 27 * time.Millisecond, 28 * time.Millisecond, 29 * time.Millisecond,
			},
			want: (19*time.Millisecond + 20*time.Millisecond) / 2, // Average of 10th and 11th values
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateMedian(tt.rtts)
			if got != tt.want {
				t.Errorf("calculateMedian() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewBurstResult(t *testing.T) {
	base := &BaseProbe{
		TargetName: "Test",
		TargetHost: "example.com",
		Timeout:    5 * time.Second,
		Pings:      20,
	}

	tests := []struct {
		name       string
		stats      BurstStats
		wantLossPct float64
		wantSuccess bool
	}{
		{
			name: "all success",
			stats: BurstStats{
				Rtts:        []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
				PacketsSent: 2,
				PacketsRecv: 2,
				MinRtt:      10 * time.Millisecond,
				MaxRtt:      20 * time.Millisecond,
				AvgRtt:      15 * time.Millisecond,
			},
			wantLossPct: 0,
			wantSuccess: true,
		},
		{
			name: "partial loss",
			stats: BurstStats{
				Rtts:        []time.Duration{10 * time.Millisecond},
				PacketsSent: 2,
				PacketsRecv: 1,
				MinRtt:      10 * time.Millisecond,
				MaxRtt:      10 * time.Millisecond,
				AvgRtt:      10 * time.Millisecond,
			},
			wantLossPct: 50,
			wantSuccess: true, // Still success if any packets received
		},
		{
			name: "total loss",
			stats: BurstStats{
				Rtts:        []time.Duration{},
				PacketsSent: 20,
				PacketsRecv: 0,
			},
			wantLossPct: 100,
			wantSuccess: false,
		},
		{
			name: "5% loss (SmokePing style)",
			stats: BurstStats{
				Rtts:        make([]time.Duration, 19), // 19 received
				PacketsSent: 20,
				PacketsRecv: 19,
				MinRtt:      10 * time.Millisecond,
				MaxRtt:      20 * time.Millisecond,
				AvgRtt:      15 * time.Millisecond,
			},
			wantLossPct: 5,
			wantSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := base.NewBurstResult(tt.stats, nil)
			if result.LossPct != tt.wantLossPct {
				t.Errorf("NewBurstResult() LossPct = %v, want %v", result.LossPct, tt.wantLossPct)
			}
			if result.Success != tt.wantSuccess {
				t.Errorf("NewBurstResult() Success = %v, want %v", result.Success, tt.wantSuccess)
			}
		})
	}
}

func TestNewResult(t *testing.T) {
	base := &BaseProbe{
		TargetName: "Test",
		TargetHost: "example.com",
		Timeout:    5 * time.Second,
		Pings:      1,
	}

	t.Run("success", func(t *testing.T) {
		result := base.NewResult(10*time.Millisecond, true, nil)
		if !result.Success {
			t.Error("NewResult() Success should be true")
		}
		if result.LatencyMs != 10.0 {
			t.Errorf("NewResult() LatencyMs = %v, want 10.0", result.LatencyMs)
		}
		if result.LossPct != 0 {
			t.Errorf("NewResult() LossPct = %v, want 0", result.LossPct)
		}
	})

	t.Run("failure", func(t *testing.T) {
		result := base.NewResult(0, false, nil)
		if result.Success {
			t.Error("NewResult() Success should be false")
		}
		if result.LatencyMs != -1 {
			t.Errorf("NewResult() LatencyMs = %v, want -1", result.LatencyMs)
		}
		if result.LossPct != 100 {
			t.Errorf("NewResult() LossPct = %v, want 100", result.LossPct)
		}
	})
}
