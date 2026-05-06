package config

import (
	"testing"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

func TestParsePorts(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    []int
		wantErr bool
	}{
		{
			name:    "single port",
			spec:    "40000",
			want:    []int{40000},
			wantErr: false,
		},
		{
			name:    "comma separated ports",
			spec:    "40000,40001,40002",
			want:    []int{40000, 40001, 40002},
			wantErr: false,
		},
		{
			name:    "range ports",
			spec:    "40000-40002",
			want:    []int{40000, 40001, 40002},
			wantErr: false,
		},
		{
			name:    "mixed range and single",
			spec:    "40000,40005-40007,40010",
			want:    []int{40000, 40005, 40006, 40007, 40010},
			wantErr: false,
		},
		{
			name:    "mixed with spaces",
			spec:    "40000, 40001, 40002",
			want:    []int{40000, 40001, 40002},
			wantErr: false,
		},
		{
			name:    "duplicate ports deduplicated",
			spec:    "40000,40000,40001",
			want:    []int{40000, 40001},
			wantErr: false,
		},
		{
			name:    "range with duplicate single port",
			spec:    "40000-40002,40000",
			want:    []int{40000, 40001, 40002},
			wantErr: false,
		},
		{
			name:    "out of range port",
			spec:    "70000",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "port zero",
			spec:    "0",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "negative port",
			spec:    "-1",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid port string",
			spec:    "abc",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty string",
			spec:    "",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "range start greater than end",
			spec:    "40002-40000",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty in comma list",
			spec:    "40000,,40001",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "port too high",
			spec:    "65536",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePorts(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePorts(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("ParsePorts(%q) = %v, want %v", tt.spec, got, tt.want)
					return
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("ParsePorts(%q) = %v, want %v", tt.spec, got, tt.want)
						return
					}
				}
			}
		})
	}
}

func TestBuildSweepPlan(t *testing.T) {
	tests := []struct {
		name           string
		target4        string
		target6        string
		direction      protocol.Direction
		ports          []int
		minSize        int
		maxSize        int
		step           int
		count          int
		interval       time.Duration
		maxEstDuration time.Duration
		wantPlanCount  int
		wantErr        bool
	}{
		{
			name:           "ipv4 only uplink single port",
			target4:        "127.0.0.1",
			target6:        "",
			direction:      protocol.DirectionUplink,
			ports:          []int{40000},
			minSize:        1200,
			maxSize:        1200,
			step:           100,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantPlanCount:  1,
			wantErr:        false,
		},
		{
			name:           "ipv4 only both directions single port",
			target4:        "127.0.0.1",
			target6:        "",
			direction:      protocol.DirectionBoth,
			ports:          []int{40000},
			minSize:        1200,
			maxSize:        1200,
			step:           100,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantPlanCount:  2, // uplink + downlink
			wantErr:        false,
		},
		{
			name:           "ipv4 and ipv6 both directions two ports",
			target4:        "127.0.0.1",
			target6:        "::1",
			direction:      protocol.DirectionBoth,
			ports:          []int{40000, 40001},
			minSize:        1200,
			maxSize:        1200,
			step:           100,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantPlanCount:  8, // 2 families × 2 ports × 2 directions
			wantErr:        false,
		},
		{
			name:           "size sweep three sizes",
			target4:        "127.0.0.1",
			target6:        "",
			direction:      protocol.DirectionUplink,
			ports:          []int{40000},
			minSize:        1200,
			maxSize:        1400,
			step:           100,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantPlanCount:  3, // 1200, 1300, 1400
			wantErr:        false,
		},
		{
			name:           "min greater than max",
			target4:        "127.0.0.1",
			target6:        "",
			direction:      protocol.DirectionUplink,
			ports:          []int{40000},
			minSize:        1400,
			maxSize:        1200,
			step:           100,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantPlanCount:  0,
			wantErr:        true,
		},
		{
			name:           "step zero",
			target4:        "127.0.0.1",
			target6:        "",
			direction:      protocol.DirectionUplink,
			ports:          []int{40000},
			minSize:        1200,
			maxSize:        1400,
			step:           0,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantPlanCount:  0,
			wantErr:        true,
		},
		{
			name:           "no targets",
			target4:        "",
			target6:        "",
			direction:      protocol.DirectionUplink,
			ports:          []int{40000},
			minSize:        1200,
			maxSize:        1400,
			step:           100,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantPlanCount:  0,
			wantErr:        true,
		},
		{
			name:           "no ports",
			target4:        "127.0.0.1",
			target6:        "",
			direction:      protocol.DirectionUplink,
			ports:          []int{},
			minSize:        1200,
			maxSize:        1400,
			step:           100,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantPlanCount:  0,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ClientConfig{
				Target4:              tt.target4,
				Target6:              tt.target6,
				ControlPort:          18080,
				Direction:            tt.direction,
				Ports:                tt.ports,
				MinSize:              tt.minSize,
				MaxSize:              tt.maxSize,
				Step:                 tt.step,
				Count:                tt.count,
				Interval:             tt.interval,
				MaxEstimatedDuration: tt.maxEstDuration,
			}

			plans, err := cfg.BuildSweepPlan()
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildSweepPlan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(plans) != tt.wantPlanCount {
					t.Errorf("BuildSweepPlan() returned %d plans, want %d", len(plans), tt.wantPlanCount)
				}

				// Verify deterministic order - IPv4 first, then IPv6
				// Ports in ascending order
				// Direction: uplink before downlink when both
				// Size in ascending order
				for i := 1; i < len(plans); i++ {
					prev := plans[i-1]
					curr := plans[i]

					// Check size is non-decreasing
					if curr.PayloadSize < prev.PayloadSize {
						t.Errorf("Plan[%d] size %d < Plan[%d] size %d", i, curr.PayloadSize, i-1, prev.PayloadSize)
					}

					// If same family and port and direction, size should increase
					if curr.Family == prev.Family && curr.Port == prev.Port && curr.Direction == prev.Direction {
						if curr.PayloadSize <= prev.PayloadSize {
							t.Errorf("Expected increasing size for same family/port/direction")
						}
					}
				}
			}
		})
	}
}

func TestRejectOverlongSweep(t *testing.T) {
	tests := []struct {
		name           string
		target4        string
		target6        string
		direction      protocol.Direction
		ports          []int
		minSize        int
		maxSize        int
		step           int
		count          int
		interval       time.Duration
		maxEstDuration time.Duration
		wantErr        bool
	}{
		{
			name:           "short sweep passes",
			target4:        "127.0.0.1",
			target6:        "",
			direction:      protocol.DirectionUplink,
			ports:          []int{40000},
			minSize:        1200,
			maxSize:        1200,
			step:           100,
			count:          10,
			interval:       10 * time.Millisecond,
			maxEstDuration: 5 * time.Minute,
			wantErr:        false,
		},
		{
			name:           "overlong sweep rejected",
			target4:        "127.0.0.1",
			target6:        "",
			direction:      protocol.DirectionBoth,
			ports:          []int{40000, 40001, 40002, 40003, 40004},
			minSize:        1200,
			maxSize:        1472,
			step:           136,
			count:          1000,
			interval:       1 * time.Millisecond,
			maxEstDuration: 1 * time.Second, // very short limit
			wantErr:        true,
		},
		{
			name:           "overlong sweep with very short max duration",
			target4:        "127.0.0.1",
			target6:        "::1",
			direction:      protocol.DirectionBoth,
			ports:          []int{40000},
			minSize:        1200,
			maxSize:        1472,
			step:           136,
			count:          100,
			interval:       10 * time.Millisecond,
			maxEstDuration: 1 * time.Millisecond, // impossible limit
			wantErr:        true,
		},
		{
			name:           "large sweep with generous limit passes",
			target4:        "127.0.0.1",
			target6:        "::1",
			direction:      protocol.DirectionBoth,
			ports:          []int{40000, 40001, 40002},
			minSize:        1200,
			maxSize:        1472,
			step:           136,
			count:          100,
			interval:       10 * time.Millisecond,
			maxEstDuration: 1 * time.Hour, // generous limit
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ClientConfig{
				Target4:              tt.target4,
				Target6:              tt.target6,
				ControlPort:          18080,
				Direction:            tt.direction,
				Ports:                tt.ports,
				MinSize:              tt.minSize,
				MaxSize:              tt.maxSize,
				Step:                 tt.step,
				Count:                tt.count,
				Interval:             tt.interval,
				MaxEstimatedDuration: tt.maxEstDuration,
			}

			plans, err := cfg.BuildSweepPlan()
			if err != nil {
				// BuildSweepPlan may fail first for validation errors
				if tt.wantErr {
					return // expected error
				}
				t.Errorf("BuildSweepPlan() unexpected error = %v", err)
				return
			}

			err = cfg.CheckDurationLimit(plans)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckDurationLimit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSweepPlanDeterministicOrder(t *testing.T) {
	// This test verifies that the sweep plan is generated in deterministic order
	// regardless of how Ports slice is ordered.
	cfg := &ClientConfig{
		Target4:              "127.0.0.1",
		Target6:              "::1",
		ControlPort:          18080,
		Direction:            protocol.DirectionBoth,
		Ports:                []int{40003, 40001, 40002},
		MinSize:              1200,
		MaxSize:              1400,
		Step:                 100,
		Count:                10,
		Interval:             10 * time.Millisecond,
		MaxEstimatedDuration: 5 * time.Minute,
	}

	plans, err := cfg.BuildSweepPlan()
	if err != nil {
		t.Fatalf("BuildSweepPlan() error = %v", err)
	}

	// Expected order (ports must be sorted to 40001, 40002, 40003):
	// IPv4, port 40001, uplink, 1200
	// IPv4, port 40001, uplink, 1300
	// IPv4, port 40001, uplink, 1400
	// IPv4, port 40001, downlink, 1200
	// ...
	// IPv4, port 40002, uplink, 1200
	// ...
	// IPv4, port 40003, uplink, 1200
	// ...
	// IPv6, port 40001, uplink, 1200
	// ...

	// Verify first plan is IPv4
	if plans[0].Family != protocol.FamilyIPv4 {
		t.Errorf("First plan family = %v, want %v", plans[0].Family, protocol.FamilyIPv4)
	}

	// Verify last plan is IPv6
	if plans[len(plans)-1].Family != protocol.FamilyIPv6 {
		t.Errorf("Last plan family = %v, want %v", plans[len(plans)-1].Family, protocol.FamilyIPv6)
	}

	// Verify ports are in ascending order across the entire plan for each family
	// This catches the bug where unsorted input would produce unsorted output
	var prevPort int = 0
	for i, plan := range plans {
		if plan.Family == protocol.FamilyIPv4 {
			if i > 0 && plans[i-1].Family == protocol.FamilyIPv4 {
				if plan.Port < prevPort {
					t.Errorf("IPv4 port decreased at index %d: port %d < previous %d", i, plan.Port, prevPort)
				}
			}
			prevPort = plan.Port
		}
	}

	// Verify for same family/port/direction, sizes increase
	for i := 1; i < len(plans); i++ {
		prev := plans[i-1]
		curr := plans[i]
		if prev.Family == curr.Family && prev.Port == curr.Port && prev.Direction == curr.Direction {
			if curr.PayloadSize <= prev.PayloadSize {
				t.Errorf("Size not increasing: prev=%d, curr=%d", prev.PayloadSize, curr.PayloadSize)
			}
		}
	}
}

// TestSweepPlanPortsAlwaysSorted verifies that ports are always sorted in output
// even when input Ports slice is unsorted.
func TestSweepPlanPortsAlwaysSorted(t *testing.T) {
	// Create config with deliberately unsorted ports
	// Use direction=up only to simplify port counting
	cfg := &ClientConfig{
		Target4:              "127.0.0.1",
		Target6:              "",
		ControlPort:          18080,
		Direction:            protocol.DirectionUplink,
		Ports:                []int{50000, 40000, 60000, 40001},
		MinSize:              1200,
		MaxSize:              1200,
		Step:                 100,
		Count:                10,
		Interval:             10 * time.Millisecond,
		MaxEstimatedDuration: 5 * time.Minute,
	}

	plans, err := cfg.BuildSweepPlan()
	if err != nil {
		t.Fatalf("BuildSweepPlan() error = %v", err)
	}

	// Extract all ports in order for IPv4
	var ipv4Ports []int
	for _, p := range plans {
		if p.Family == protocol.FamilyIPv4 {
			ipv4Ports = append(ipv4Ports, p.Port)
		}
	}

	// With direction=Uplink only and single size, we get 4 plans for 4 ports
	if len(ipv4Ports) != 4 {
		t.Fatalf("Expected 4 ports, got %d", len(ipv4Ports))
	}

	// Verify ports are sorted
	for i := 1; i < len(ipv4Ports); i++ {
		if ipv4Ports[i] < ipv4Ports[i-1] {
			t.Errorf("Ports not sorted: ipv4Ports[%d]=%d < ipv4Ports[%d]=%d",
				i, ipv4Ports[i], i-1, ipv4Ports[i-1])
		}
	}

	// Verify they are 40000, 40001, 50000, 60000 (sorted)
	expected := []int{40000, 40001, 50000, 60000}
	for i := range expected {
		if ipv4Ports[i] != expected[i] {
			t.Errorf("ipv4Ports[%d] = %d, want %d", i, ipv4Ports[i], expected[i])
		}
	}
}

func TestNewClientConfig(t *testing.T) {
	tests := []struct {
		name        string
		target4     string
		target6     string
		controlPort int
		direction   protocol.Direction
		portsSpec   string
		minSize     int
		maxSize     int
		step        int
		count       int
		interval    time.Duration
		maxEstDur   time.Duration
		wantErr     bool
	}{
		{
			name:        "valid config",
			target4:     "127.0.0.1",
			target6:     "",
			controlPort: 18080,
			direction:   protocol.DirectionBoth,
			portsSpec:   "40000-40002",
			minSize:     1200,
			maxSize:     1472,
			step:        136,
			count:       100,
			interval:    10 * time.Millisecond,
			maxEstDur:   5 * time.Minute,
			wantErr:     false,
		},
		{
			name:        "invalid ports",
			target4:     "127.0.0.1",
			target6:     "",
			controlPort: 18080,
			direction:   protocol.DirectionBoth,
			portsSpec:   "invalid",
			minSize:     1200,
			maxSize:     1472,
			step:        136,
			count:       100,
			interval:    10 * time.Millisecond,
			maxEstDur:   5 * time.Minute,
			wantErr:     true,
		},
		{
			name:        "min greater than max",
			target4:     "127.0.0.1",
			target6:     "",
			controlPort: 18080,
			direction:   protocol.DirectionBoth,
			portsSpec:   "40000",
			minSize:     1472,
			maxSize:     1200,
			step:        136,
			count:       100,
			interval:    10 * time.Millisecond,
			maxEstDur:   5 * time.Minute,
			wantErr:     true,
		},
		{
			name:        "step zero",
			target4:     "127.0.0.1",
			target6:     "",
			controlPort: 18080,
			direction:   protocol.DirectionBoth,
			portsSpec:   "40000",
			minSize:     1200,
			maxSize:     1472,
			step:        0,
			count:       100,
			interval:    10 * time.Millisecond,
			maxEstDur:   5 * time.Minute,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := NewClientConfig(
				tt.target4, tt.target6,
				tt.controlPort,
				tt.direction,
				tt.portsSpec,
				tt.minSize, tt.maxSize, tt.step, tt.count,
				tt.interval, tt.maxEstDur,
			)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClientConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg == nil {
				t.Error("NewClientConfig() returned nil without error")
			}
		})
	}
}

func TestValidateAddresses(t *testing.T) {
	tests := []struct {
		name    string
		target4 string
		target6 string
		wantErr bool
	}{
		{
			name:    "valid IPv4",
			target4: "127.0.0.1",
			target6: "",
			wantErr: false,
		},
		{
			name:    "valid IPv6",
			target4: "",
			target6: "::1",
			wantErr: false,
		},
		{
			name:    "both valid",
			target4: "127.0.0.1",
			target6: "::1",
			wantErr: false,
		},
		{
			name:    "invalid IPv4",
			target4: "not-an-ip",
			target6: "",
			wantErr: true,
		},
		{
			name:    "invalid IPv6",
			target4: "",
			target6: "not-an-ip",
			wantErr: true,
		},
		{
			name:    "IPv4 address in IPv6 field",
			target4: "",
			target6: "127.0.0.1",
			wantErr: true,
		},
		{
			name:    "IPv6 address in IPv4 field",
			target4: "::1",
			target6: "",
			wantErr: true,
		},
		{
			name:    "empty both",
			target4: "",
			target6: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ClientConfig{
				Target4:              tt.target4,
				Target6:              tt.target6,
				ControlPort:          18080,
				Direction:            protocol.DirectionUplink,
				Ports:                []int{40000},
				MinSize:              1200,
				MaxSize:              1400,
				Step:                 100,
				Count:                10,
				Interval:             10 * time.Millisecond,
				MaxEstimatedDuration: 5 * time.Minute,
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
