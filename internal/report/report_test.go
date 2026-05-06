package report

import (
	"testing"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

func TestLossCalculation(t *testing.T) {
	tests := []struct {
		name     string
		sent     int
		received int
		expected float64
	}{
		{"0% loss", 100, 100, 0.0},
		{"50% loss", 100, 50, 50.0},
		{"100% loss", 100, 0, 100.0},
		{"5% loss", 100, 95, 5.0},
		{"exact 5% threshold", 20, 19, 5.0},
		{"just below 5% threshold", 20, 19, 5.0}, // 1/20 = 5.0%
		{"zero sent", 0, 0, 0.0},
		{"partial 33% loss", 300, 200, 33.33333333333333},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &TestResult{
				Sent:     tc.sent,
				Received: tc.received,
				Lost:     tc.sent - tc.received,
			}
			got := r.LossPercent()
			if got != tc.expected {
				t.Errorf("LossPercent() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestCriticalSizeFirstAtOrAboveThreshold(t *testing.T) {
	threshold := ThresholdPercent

	tests := []struct {
		name        string
		results     []*TestResult
		expectNil   bool
		expectValue *int
	}{
		{
			name: "first size at threshold",
			results: []*TestResult{
				{PayloadSize: 1000, Sent: 100, Received: 95, Lost: 5},
				{PayloadSize: 1200, Sent: 100, Received: 50, Lost: 50},
			},
			expectNil:   false,
			expectValue: intPtr(1000),
		},
		{
			name: "second size at threshold",
			results: []*TestResult{
				{PayloadSize: 1000, Sent: 100, Received: 100, Lost: 0},
				{PayloadSize: 1200, Sent: 100, Received: 94, Lost: 6},
				{PayloadSize: 1400, Sent: 100, Received: 50, Lost: 50},
			},
			expectNil:   false,
			expectValue: intPtr(1200),
		},
		{
			name: "no size reaches threshold",
			results: []*TestResult{
				{PayloadSize: 1000, Sent: 100, Received: 100, Lost: 0},
				{PayloadSize: 1200, Sent: 100, Received: 99, Lost: 1},
				{PayloadSize: 1400, Sent: 100, Received: 98, Lost: 2},
			},
			expectNil:   true,
			expectValue: nil,
		},
		{
			name: "100% loss at first size",
			results: []*TestResult{
				{PayloadSize: 1000, Sent: 100, Received: 0, Lost: 100},
				{PayloadSize: 1200, Sent: 100, Received: 0, Lost: 100},
			},
			expectNil:   false,
			expectValue: intPtr(1000),
		},
		{
			name: "threshold reached at size 1472",
			results: []*TestResult{
				{PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
				{PayloadSize: 1336, Sent: 100, Received: 100, Lost: 0},
				{PayloadSize: 1472, Sent: 100, Received: 94, Lost: 6},
			},
			expectNil:   false,
			expectValue: intPtr(1472),
		},
		{
			name:        "empty results",
			results:     []*TestResult{},
			expectNil:   true,
			expectValue: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FindCriticalSize(tc.results, threshold)

			if tc.expectNil && got != nil {
				t.Errorf("FindCriticalSize() = %v, want nil", *got)
			}
			if !tc.expectNil && got == nil {
				t.Errorf("FindCriticalSize() = nil, want %v", *tc.expectValue)
			}
			if !tc.expectNil && got != nil && *got != *tc.expectValue {
				t.Errorf("FindCriticalSize() = %v, want %v", *got, *tc.expectValue)
			}
		})
	}
}

func TestReportFormatting(t *testing.T) {
	results := []*TestResult{
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1336, Sent: 100, Received: 95, Lost: 5},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1472, Sent: 100, Received: 50, Lost: 50},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionDownlink, UDPPort: 40000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionDownlink, UDPPort: 40000, PayloadSize: 1336, Sent: 100, Received: 100, Lost: 0},
	}

	r := BuildReport(results, "1s")

	output := FormatReport(r)

	if output == "" {
		t.Error("FormatReport() returned empty string")
	}

	if !contains(output, "critical_size=") {
		t.Error("FormatReport() missing critical_size in output")
	}

	if !contains(output, "loss=") {
		t.Error("FormatReport() missing loss percentage in output")
	}

	if !contains(output, "sent=") {
		t.Error("FormatReport() missing sent count in output")
	}

	if !contains(output, "received=") {
		t.Error("FormatReport() missing received count in output")
	}

	if !contains(output, "family: ipv4") {
		t.Error("FormatReport() missing family in output")
	}

	if !contains(output, "direction: uplink") && !contains(output, "direction: downlink") {
		t.Error("FormatReport() missing direction in output")
	}

	if !contains(output, "port: 40000") {
		t.Error("FormatReport() missing port in output")
	}

	if !contains(output, "size=1200") && !contains(output, "size=1336") && !contains(output, "size=1472") {
		t.Error("FormatReport() missing payload sizes in output")
	}

	if !contains(output, "target: 127.0.0.1") {
		t.Error("FormatReport() missing target address in output")
	}
}

func TestCriticalSizeNoneWhenAllBelowThreshold(t *testing.T) {
	results := []*TestResult{
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1336, Sent: 100, Received: 99, Lost: 1},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1472, Sent: 100, Received: 98, Lost: 2},
	}

	r := BuildReport(results, "500ms")

	output := FormatReport(r)

	if !contains(output, "critical_size=none") {
		t.Errorf("Expected critical_size=none in output, got:\n%s", output)
	}

	if !contains(output, "observed threshold") {
		t.Errorf("Expected 'observed threshold' semantics in output, got:\n%s", output)
	}
}

func TestGroupingAndSorting(t *testing.T) {
	results := []*TestResult{
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv6, Direction: protocol.DirectionDownlink, UDPPort: 50000, PayloadSize: 1472, Sent: 100, Received: 50, Lost: 50},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1472, Sent: 100, Received: 50, Lost: 50},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv6, Direction: protocol.DirectionDownlink, UDPPort: 50000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1336, Sent: 100, Received: 90, Lost: 10},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv6, Direction: protocol.DirectionDownlink, UDPPort: 50000, PayloadSize: 1336, Sent: 100, Received: 80, Lost: 20},
	}

	groups := AggregateResults(results)

	if len(groups) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(groups))
	}

	for _, g := range groups {
		for i := 1; i < len(g.Results); i++ {
			if g.Results[i].PayloadSize < g.Results[i-1].PayloadSize {
				t.Errorf("Group not sorted by payload size: %d then %d",
					g.Results[i-1].PayloadSize, g.Results[i].PayloadSize)
			}
		}
	}

	for _, g := range groups {
		if g.Key.Family == protocol.FamilyIPv4 && g.Key.Port == 40000 && g.Key.Direction == protocol.DirectionUplink {
			if len(g.Results) != 3 {
				t.Errorf("Expected 3 results for ipv4:uplink:40000, got %d", len(g.Results))
			}
			if g.Results[0].PayloadSize != 1200 {
				t.Errorf("Expected first size 1200, got %d", g.Results[0].PayloadSize)
			}
			if g.Results[1].PayloadSize != 1336 {
				t.Errorf("Expected second size 1336, got %d", g.Results[1].PayloadSize)
			}
			if g.Results[2].PayloadSize != 1472 {
				t.Errorf("Expected third size 1472, got %d", g.Results[2].PayloadSize)
			}
		}
	}
}

func TestDeterministicGroupOrdering(t *testing.T) {
	results := []*TestResult{
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv6, Direction: protocol.DirectionDownlink, UDPPort: 50000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv6, Direction: protocol.DirectionDownlink, UDPPort: 50000, PayloadSize: 1336, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1336, Sent: 100, Received: 100, Lost: 0},
	}

	groups := AggregateResults(results)

	if len(groups) != 2 {
		t.Fatalf("Expected 2 groups, got %d", len(groups))
	}

	if groups[0].Key.Family != protocol.FamilyIPv4 {
		t.Errorf("Expected first group to be IPv4, got %v", groups[0].Key.Family)
	}
	if groups[1].Key.Family != protocol.FamilyIPv6 {
		t.Errorf("Expected second group to be IPv6, got %v", groups[1].Key.Family)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func intPtr(i int) *int {
	return &i
}
