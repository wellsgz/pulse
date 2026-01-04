package storage

import (
	"testing"
	"time"
)

func TestParseRRAs(t *testing.T) {
	tests := []struct {
		name       string
		retention  string
		baseStep   time.Duration
		wantLen    int
		wantErr    bool
		checkFirst *rraConfig // Optional: check first RRA config
	}{
		{
			name:      "single retention",
			retention: "10s:1d",
			baseStep:  10 * time.Second,
			wantLen:   1,
			wantErr:   false,
			checkFirst: &rraConfig{
				steps: 1,    // 10s / 10s = 1
				rows:  8640, // 1 day / 10s = 8640
			},
		},
		{
			name:      "multiple retentions",
			retention: "10s:1d,1m:7d,1h:90d",
			baseStep:  10 * time.Second,
			wantLen:   3,
			wantErr:   false,
		},
		{
			name:      "with spaces",
			retention: "10s:1d, 1m:7d, 1h:90d",
			baseStep:  10 * time.Second,
			wantLen:   3,
			wantErr:   false,
		},
		{
			name:      "empty retention",
			retention: "",
			baseStep:  10 * time.Second,
			wantLen:   0,
			wantErr:   true,
		},
		{
			name:      "invalid format - missing duration",
			retention: "10s",
			baseStep:  10 * time.Second,
			wantLen:   0,
			wantErr:   true,
		},
		{
			name:      "invalid format - bad resolution",
			retention: "abc:1d",
			baseStep:  10 * time.Second,
			wantLen:   0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rras, err := parseRRAs(tt.retention, tt.baseStep)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRRAs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(rras) != tt.wantLen {
				t.Errorf("parseRRAs() returned %d RRAs, want %d", len(rras), tt.wantLen)
			}
			if tt.checkFirst != nil && len(rras) > 0 {
				if rras[0].steps != tt.checkFirst.steps {
					t.Errorf("parseRRAs() first RRA steps = %d, want %d", rras[0].steps, tt.checkFirst.steps)
				}
				if rras[0].rows != tt.checkFirst.rows {
					t.Errorf("parseRRAs() first RRA rows = %d, want %d", rras[0].rows, tt.checkFirst.rows)
				}
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"10s", 10 * time.Second, false},
		{"1m", 1 * time.Minute, false},
		{"1h", 1 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"90d", 90 * 24 * time.Hour, false},
		{"", 0, true},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCalculateStep(t *testing.T) {
	s := &RRDStorage{step: 10 * time.Second}

	tests := []struct {
		name     string
		duration time.Duration
		want     time.Duration
	}{
		{"1 hour - use base step", 1 * time.Hour, 10 * time.Second},
		{"12 hours - use base step", 12 * time.Hour, 10 * time.Second},
		{"24 hours - use base step", 24 * time.Hour, 10 * time.Second},
		{"25 hours - use 1 minute", 25 * time.Hour, time.Minute},
		{"3 days - use 1 minute", 3 * 24 * time.Hour, time.Minute},
		{"7 days - use 1 minute", 7 * 24 * time.Hour, time.Minute},
		{"8 days - use 1 hour", 8 * 24 * time.Hour, time.Hour},
		{"30 days - use 1 hour", 30 * 24 * time.Hour, time.Hour},
		{"90 days - use 1 hour", 90 * 24 * time.Hour, time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.calculateStep(tt.duration)
			if got != tt.want {
				t.Errorf("calculateStep(%v) = %v, want %v", tt.duration, got, tt.want)
			}
		})
	}
}

func TestGetFilename(t *testing.T) {
	s := &RRDStorage{dataDir: "/data"}

	tests := []struct {
		name     string
		target   string
		wantFile string
	}{
		{"simple", "Google DNS", "/data/google_dns.rrd"},
		{"with slash", "Server/Main", "/data/server_main.rrd"},
		{"with backslash", "Server\\Main", "/data/server_main.rrd"},
		{"with special chars", "Test<>:\"?*|", "/data/test.rrd"},
		{"empty after sanitize", "???", "/data/unnamed.rrd"},
		{"long name", string(make([]byte, 300)), "/data/" + string(make([]byte, 200)) + ".rrd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.getFilename(tt.target)
			// For the long name test, just check length
			if tt.name == "long name" {
				// Base path + / + 200 chars + .rrd = should be under limit
				if len(got) > 250 {
					t.Errorf("getFilename() path too long: %d chars", len(got))
				}
				return
			}
			if got != tt.wantFile {
				t.Errorf("getFilename(%q) = %q, want %q", tt.target, got, tt.wantFile)
			}
		})
	}
}
