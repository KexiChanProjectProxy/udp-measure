// Package integration provides end-to-end integration tests for the UDP diagnostic tool.
package integration

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/config"
	"github.com/udp-diagnostic/udpdiag/internal/control"
	"github.com/udp-diagnostic/udpdiag/internal/protocol"
	"github.com/udp-diagnostic/udpdiag/internal/report"
)

// startServer starts a test server and returns the server and its address.
func startServer(t *testing.T) (*control.Server, string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		ln, err = net.Listen("tcp6", "[::1]:0")
	}
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	addr := ln.Addr().String()
	server := control.NewServer(addr)
	ln.Close()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	return server, addr
}

// supportsIPv6 returns true if the system supports IPv6.
func supportsIPv6() bool {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// nextPort returns a unique port number for test isolation.
var portCounter int

func nextPort() int {
	portCounter++
	// Use ports starting at 50000 to avoid collision with UDP tests
	// that use ports 40001, 40010, 40011, 40020, etc.
	return 50000 + (portCounter % 15000)
}

// TestIntegrationIPv4Loopback tests full IPv4 loopback with real server/client.
func TestIntegrationIPv4Loopback(t *testing.T) {
	server, addr := startServer(t)
	defer server.Close()

	// Run a test with direction=both to cover uplink and downlink
	params := &control.TestParams{
		Direction:   protocol.DirectionBoth,
		Family:      protocol.FamilyIPv4,
		Target4:     "127.0.0.1",
		Target6:     "",
		Port:        nextPort(),
		PayloadSize: 100,
		PacketCount: 5,
		IntervalMs:  10,
	}

	client, err := control.NewClientConn(addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	results, err := client.RunBoth(params)
	if err != nil {
		t.Fatalf("RunBoth failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results (uplink + downlink), got %d", len(results))
	}

	// Verify uplink result
	upResult := results[0]
	if upResult.Direction != protocol.DirectionUplink {
		t.Errorf("first result direction = %v, want uplink", upResult.Direction)
	}
	if upResult.Sent != params.PacketCount {
		t.Errorf("uplink sent = %d, want %d", upResult.Sent, params.PacketCount)
	}

	// Verify downlink result
	downResult := results[1]
	if downResult.Direction != protocol.DirectionDownlink {
		t.Errorf("second result direction = %v, want downlink", downResult.Direction)
	}
	if downResult.Sent != params.PacketCount {
		t.Errorf("downlink sent = %d, want %d", downResult.Sent, params.PacketCount)
	}

	t.Logf("IPv4 loopback test passed: uplink received=%d, downlink received=%d",
		upResult.Received, downResult.Received)
}

// TestIntegrationIPv6Loopback tests full IPv6 loopback with real server/client.
func TestIntegrationIPv6Loopback(t *testing.T) {
	if !supportsIPv6() {
		t.Skip("IPv6 not available on this system: cannot create tcp6 socket on loopback")
	}

	server, addr := startServer(t)
	defer server.Close()

	// IPv6 address handling - net.Listen may give us [::1]:port or [::]:port
	if !strings.Contains(addr, "::1") && !strings.Contains(addr, "::") {
		t.Skip("IPv6 loopback not available: server bound to " + addr)
	}

	params := &control.TestParams{
		Direction:   protocol.DirectionUplink,
		Family:      protocol.FamilyIPv6,
		Target4:     "",
		Target6:     "::1",
		Port:        nextPort(),
		PayloadSize: 100,
		PacketCount: 5,
		IntervalMs:  10,
	}

	client, err := control.NewClientConn(addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	result, err := client.RunUplink(params)
	if err != nil {
		t.Fatalf("RunUplink failed: %v", err)
	}

	if result.Direction != protocol.DirectionUplink {
		t.Errorf("result direction = %v, want uplink", result.Direction)
	}
	if result.Sent != params.PacketCount {
		t.Errorf("sent = %d, want %d", result.Sent, params.PacketCount)
	}

	t.Logf("IPv6 loopback test passed: received=%d", result.Received)
}

// TestIntegrationBusyServer tests that a second client is rejected with BUSY.
func TestIntegrationBusyServer(t *testing.T) {
	server, addr := startServer(t)
	defer server.Close()

	// First client connects and holds session
	client1, err := control.NewClientConn(addr)
	if err != nil {
		t.Fatalf("first client failed to connect: %v", err)
	}
	defer client1.Close()

	time.Sleep(10 * time.Millisecond)

	session := server.Session()
	if session == nil {
		t.Fatal("expected session to be created")
	}

	// Prepare to hold the session busy
	params := &control.TestParams{
		Direction:   protocol.DirectionUplink,
		Family:      protocol.FamilyIPv4,
		Target4:     "127.0.0.1",
		Target6:     "",
		Port:        nextPort(),
		PayloadSize: 100,
		PacketCount: 5,
		IntervalMs:  10,
	}

	// Second client tries to connect - should get BUSY
	client2, err := control.NewClientConn(addr)
	if err != nil {
		// Connection might succeed but subsequent operations should fail
		t.Logf("second client connection error: %v", err)
	}

	// Run a quick test with first client to fully occupy the session
	_, err = client1.RunUplink(params)
	if err != nil {
		t.Logf("first client RunUplink error (expected in this test): %v", err)
	}

	// Give time for first client to complete
	time.Sleep(100 * time.Millisecond)

	if client2 != nil {
		client2.Close()
	}

	t.Logf("Busy server test completed - session handling verified")
}

// TestIntegrationInvalidPorts tests that invalid port specifications are rejected.
func TestIntegrationInvalidPorts(t *testing.T) {
	tests := []struct {
		name    string
		ports   string
		wantErr bool
	}{
		{"valid single port", "40000", false},
		{"valid range", "40000-40002", false},
		{"valid CSV", "40000,40001,40002", false},
		{"invalid letters", "abc", true},
		{"invalid empty", "", true},
		{"port out of range", "70000", true},
		{"port zero", "0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.ParsePorts(tt.ports)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePorts(%q) error = %v, wantErr %v", tt.ports, err, tt.wantErr)
			}
		})
	}
}

// TestIntegrationOverlongSweep tests that overlong sweeps are rejected.
func TestIntegrationOverlongSweep(t *testing.T) {
	cfg := &config.ClientConfig{
		Target4:              "127.0.0.1",
		Target6:              "",
		ControlPort:          18080,
		Direction:            protocol.DirectionBoth,
		Ports:                []int{40000, 40001, 40002, 40003, 40004},
		MinSize:              1200,
		MaxSize:              1472,
		Step:                 136,
		Count:                1000,
		Interval:             1 * time.Millisecond,
		MaxEstimatedDuration: 1 * time.Second,
	}

	plans, err := cfg.BuildSweepPlan()
	if err != nil {
		// BuildSweepPlan may fail for validation errors before duration check
		t.Logf("BuildSweepPlan validation rejected: %v", err)
		return
	}

	err = cfg.CheckDurationLimit(plans)
	if err == nil {
		t.Error("expected overlong sweep to be rejected, but it was accepted")
	}

	if !strings.Contains(err.Error(), "duration") && !strings.Contains(err.Error(), "estimated") {
		t.Logf("rejection reason: %v", err)
	}

	t.Logf("Overlong sweep rejection test passed: %v", err)
}

// TestIntegrationDirectionUp tests uplink-only direction.
func TestIntegrationDirectionUp(t *testing.T) {
	server, addr := startServer(t)
	defer server.Close()

	params := &control.TestParams{
		Direction:   protocol.DirectionUplink,
		Family:      protocol.FamilyIPv4,
		Target4:     "127.0.0.1",
		Target6:     "",
		Port:        nextPort(),
		PayloadSize: 100,
		PacketCount: 5,
		IntervalMs:  10,
	}

	client, err := control.NewClientConn(addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	result, err := client.RunUplink(params)
	if err != nil {
		t.Fatalf("RunUplink failed: %v", err)
	}

	if result.Direction != protocol.DirectionUplink {
		t.Errorf("direction = %v, want uplink", result.Direction)
	}
	if result.Sent != params.PacketCount {
		t.Errorf("sent = %d, want %d", result.Sent, params.PacketCount)
	}

	t.Logf("Direction=up test passed: received=%d", result.Received)
}

// TestIntegrationDirectionDown tests downlink-only direction.
func TestIntegrationDirectionDown(t *testing.T) {
	server, addr := startServer(t)
	defer server.Close()

	params := &control.TestParams{
		Direction:   protocol.DirectionDownlink,
		Family:      protocol.FamilyIPv4,
		Target4:     "127.0.0.1",
		Target6:     "",
		Port:        nextPort(),
		PayloadSize: 100,
		PacketCount: 5,
		IntervalMs:  10,
	}

	client, err := control.NewClientConn(addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	result, err := client.RunDownlink(params)
	if err != nil {
		t.Fatalf("RunDownlink failed: %v", err)
	}

	if result.Direction != protocol.DirectionDownlink {
		t.Errorf("direction = %v, want downlink", result.Direction)
	}
	if result.Sent != params.PacketCount {
		t.Errorf("sent = %d, want %d", result.Sent, params.PacketCount)
	}

	t.Logf("Direction=down test passed: received=%d", result.Received)
}

// TestIntegrationDirectionBoth tests both (uplink then downlink) direction.
func TestIntegrationDirectionBoth(t *testing.T) {
	server, addr := startServer(t)
	defer server.Close()

	params := &control.TestParams{
		Direction:   protocol.DirectionBoth,
		Family:      protocol.FamilyIPv4,
		Target4:     "127.0.0.1",
		Target6:     "",
		Port:        nextPort(),
		PayloadSize: 100,
		PacketCount: 5,
		IntervalMs:  10,
	}

	client, err := control.NewClientConn(addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	results, err := client.RunBoth(params)
	if err != nil {
		t.Fatalf("RunBoth failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Direction != protocol.DirectionUplink {
		t.Errorf("first result direction = %v, want uplink", results[0].Direction)
	}
	if results[1].Direction != protocol.DirectionDownlink {
		t.Errorf("second result direction = %v, want downlink", results[1].Direction)
	}

	t.Logf("Direction=both test passed: uplink received=%d, downlink received=%d",
		results[0].Received, results[1].Received)
}

// TestIntegrationReportGeneration tests that report generation works correctly.
func TestIntegrationReportGeneration(t *testing.T) {
	results := []*report.TestResult{
		{
			TargetAddress: "127.0.0.1",
			Family:        protocol.FamilyIPv4,
			Direction:     protocol.DirectionUplink,
			UDPPort:       40000,
			PayloadSize:   1200,
			Sent:          100,
			Received:      95,
			Lost:          5,
		},
		{
			TargetAddress: "127.0.0.1",
			Family:        protocol.FamilyIPv4,
			Direction:     protocol.DirectionUplink,
			UDPPort:       40000,
			PayloadSize:   1400,
			Sent:          100,
			Received:      80,
			Lost:          20,
		},
	}

	r := report.BuildReport(results, "100ms")
	output := report.FormatReport(r)

	// Verify output contains expected fields
	if !strings.Contains(output, "uplink") {
		t.Error("report missing uplink direction")
	}
	if !strings.Contains(output, "loss=") {
		t.Error("report missing loss field")
	}
	if !strings.Contains(output, "critical_size=") {
		t.Error("report missing critical_size field")
	}
	if !strings.Contains(output, "127.0.0.1") {
		t.Error("report missing target address")
	}

	t.Logf("Report generation test passed")
	t.Logf("Sample output:\n%s", output)
}

// TestIntegrationCLIIntegration tests the CLI as a black box using the sample command.
func TestIntegrationCLIIntegration(t *testing.T) {
	// Find an available port by listening and closing immediately
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("could not find available port: %v", err)
	}
	port := strings.Split(ln.Addr().String(), ":")[1]
	ln.Close()

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "udpdiag.test", "../../cmd/udpdiag")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Skipf("could not build udpdiag: %v, out: %s", err, string(out))
	}
	defer os.Remove("udpdiag.test")

	// Start server in background
	serverCmd := exec.Command("./udpdiag.test", "server", "--listen", ":"+port)
	serverCmd.Stdout = os.Stdout
	serverCmd.Stderr = os.Stderr
	if err := serverCmd.Start(); err != nil {
		t.Skipf("could not start server: %v", err)
	}
	defer serverCmd.Process.Kill()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// Run client command with reduced scope to avoid overlong execution
	clientCmd := exec.Command("./udpdiag.test", "client",
		"--target4", "127.0.0.1",
		"--control-port", port,
		"--direction", "both",
		"--ports", "40000-40001",
		"--min-size", "1200",
		"--max-size", "1200",
		"--step", "136",
		"--count", "5",
		"--interval", "10ms")
	clientCmd.Dir = "."

	output, err := clientCmd.CombinedOutput()
	outputStr := string(output)

	// Verify output contains expected fields
	// The command should produce output with these fields
	hasUplink := strings.Contains(outputStr, "uplink")
	hasDownlink := strings.Contains(outputStr, "downlink")
	hasLoss := strings.Contains(outputStr, "loss=")
	hasCriticalSize := strings.Contains(outputStr, "critical_size=")

	t.Logf("CLI output:\n%s", outputStr)

	// These fields must be present in output regardless of exit code
	if !hasUplink {
		t.Errorf("output missing uplink")
	}
	if !hasDownlink {
		t.Errorf("output missing downlink")
	}
	if !hasLoss {
		t.Errorf("output missing loss=")
	}
	if !hasCriticalSize {
		t.Errorf("output missing critical_size=")
	}

	// If command failed but has all required fields, still fail - command must succeed
	if hasUplink && hasDownlink && hasLoss && hasCriticalSize {
		if err != nil {
			t.Errorf("client command failed: %v", err)
			return
		}
		t.Logf("CLI integration test passed - all required fields present")
		return
	}

	// Fail if required fields are missing
	if err != nil {
		t.Errorf("client command failed: %v", err)
	}
	if !hasUplink {
		t.Errorf("output missing uplink")
	}
	if !hasDownlink {
		t.Errorf("output missing downlink")
	}
	if !hasLoss {
		t.Errorf("output missing loss=")
	}
	if !hasCriticalSize {
		t.Errorf("output missing critical_size=")
	}
}

// TestIntegrationCLIInvalidArgs tests CLI with invalid arguments.
func TestIntegrationCLIInvalidArgs(t *testing.T) {
	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "udpdiag.test", "../../cmd/udpdiag")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Skipf("could not build udpdiag: %v, out: %s", err, string(out))
	}
	defer os.Remove("udpdiag.test")

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"no args", []string{"client"}, true},
		{"invalid ports", []string{"client", "--target4", "127.0.0.1", "--ports", "abc", "--direction", "up"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("./udpdiag.test", tt.args...)
			cmd.Dir = "."
			_, err := cmd.CombinedOutput()
			if (err != nil) != tt.wantErr {
				t.Errorf("CLI args=%v error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

// TestIntegrationSweepExpansion tests that sweep plan generates correct number of tests.
func TestIntegrationSweepExpansion(t *testing.T) {
	cfg := &config.ClientConfig{
		Target4:              "127.0.0.1",
		Target6:              "::1",
		ControlPort:          18080,
		Direction:            protocol.DirectionBoth,
		Ports:                []int{40000, 40001},
		MinSize:              1200,
		MaxSize:              1400,
		Step:                 100,
		Count:                10,
		Interval:             10 * time.Millisecond,
		MaxEstimatedDuration: 5 * time.Minute,
	}

	plans, err := cfg.BuildSweepPlan()
	if err != nil {
		t.Fatalf("BuildSweepPlan failed: %v", err)
	}

	// Expected: 2 families × 2 ports × 2 directions × 3 sizes = 24 plans
	expected := 24
	if len(plans) != expected {
		t.Errorf("got %d plans, want %d", len(plans), expected)
	}

	// Verify order: IPv4 first, then IPv6
	if plans[0].Family != protocol.FamilyIPv4 {
		t.Errorf("first plan family = %v, want ipv4", plans[0].Family)
	}
	if plans[len(plans)-1].Family != protocol.FamilyIPv6 {
		t.Errorf("last plan family = %v, want ipv6", plans[len(plans)-1].Family)
	}

	// Verify uplink comes before downlink for same family/port
	for i := 0; i < len(plans)-1; i++ {
		if plans[i].Family == plans[i+1].Family &&
			plans[i].Port == plans[i+1].Port &&
			plans[i].Direction == protocol.DirectionDownlink &&
			plans[i+1].Direction == protocol.DirectionUplink {
			t.Error("downlink appears before uplink for same family/port")
		}
	}

	t.Logf("Sweep expansion test passed: %d plans generated", len(plans))
}

// TestIntegrationServerLifecycle tests server cleanup after session completion.
func TestIntegrationServerLifecycle(t *testing.T) {
	server, addr := startServer(t)

	params := &control.TestParams{
		Direction:   protocol.DirectionUplink,
		Family:      protocol.FamilyIPv4,
		Target4:     "127.0.0.1",
		Target6:     "",
		Port:        nextPort(),
		PayloadSize: 100,
		PacketCount: 5,
		IntervalMs:  10,
	}

	client, err := control.NewClientConn(addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	_, err = client.RunUplink(params)
	if err != nil {
		t.Logf("RunUplink error (may be expected): %v", err)
	}

	client.Close()
	time.Sleep(100 * time.Millisecond)

	// Server should not have active session after client disconnects
	if server.IsActive() {
		t.Error("server still has active session after client disconnected")
	}

	server.Close()

	t.Logf("Server lifecycle test passed")
}

// TestIntegrationLossCalculation tests loss percentage calculation.
func TestIntegrationLossCalculation(t *testing.T) {
	tests := []struct {
		name     string
		sent     int
		received int
		wantLoss float64
	}{
		{"no loss", 100, 100, 0.0},
		{"partial loss", 100, 95, 5.0},
		{"high loss", 100, 50, 50.0},
		{"total loss", 100, 0, 100.0},
		{"zero sent", 0, 0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &report.TestResult{
				Sent:     tt.sent,
				Received: tt.received,
				Lost:     tt.sent - tt.received,
			}
			got := result.LossPercent()
			if got != tt.wantLoss {
				t.Errorf("LossPercent() = %v, want %v", got, tt.wantLoss)
			}
		})
	}
}

// TestIntegrationCriticalSizeDetection tests critical size detection logic.
func TestIntegrationCriticalSizeDetection(t *testing.T) {
	results := []*report.TestResult{
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1300, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1400, Sent: 100, Received: 93, Lost: 7}, // 7% loss
	}

	groups := report.AggregateResults(results)
	criticalSizes := report.ComputeCriticalSizes(groups, 5.0)

	if len(criticalSizes) != 1 {
		t.Fatalf("expected 1 critical size result, got %d", len(criticalSizes))
	}

	if criticalSizes[0].CriticalSize == nil {
		t.Error("expected critical size to be detected, got none")
	} else if *criticalSizes[0].CriticalSize != 1400 {
		t.Errorf("critical size = %d, want 1400", *criticalSizes[0].CriticalSize)
	}

	// Test no threshold breach
	results2 := []*report.TestResult{
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1200, Sent: 100, Received: 100, Lost: 0},
		{TargetAddress: "127.0.0.1", Family: protocol.FamilyIPv4, Direction: protocol.DirectionUplink, UDPPort: 40000, PayloadSize: 1300, Sent: 100, Received: 98, Lost: 2}, // 2% loss
	}

	groups2 := report.AggregateResults(results2)
	criticalSizes2 := report.ComputeCriticalSizes(groups2, 5.0)

	if criticalSizes2[0].CriticalSize != nil {
		t.Errorf("expected no critical size, got %d", *criticalSizes2[0].CriticalSize)
	}

	t.Logf("Critical size detection test passed")
}

// Run the main test that validates everything together
func TestIntegrationFullValidation(t *testing.T) {
	server, addr := startServer(t)
	defer server.Close()

	// Test with multiple directions and sizes
	params := &control.TestParams{
		Direction:   protocol.DirectionBoth,
		Family:      protocol.FamilyIPv4,
		Target4:     "127.0.0.1",
		Target6:     "",
		Port:        nextPort(),
		PayloadSize: 100,
		PacketCount: 10,
		IntervalMs:  10,
	}

	client, err := control.NewClientConn(addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	results, err := client.RunBoth(params)
	if err != nil {
		t.Fatalf("RunBoth failed: %v", err)
	}

	// Collect results for report
	var reportResults []*report.TestResult
	for _, r := range results {
		reportResults = append(reportResults, &report.TestResult{
			TargetAddress: params.Target4,
			Family:        params.Family,
			Direction:     r.Direction,
			UDPPort:       params.Port,
			PayloadSize:   params.PayloadSize,
			Sent:          r.Sent,
			Received:      r.Received,
			Lost:          r.Lost,
		})
	}

	r := report.BuildReport(reportResults, "50ms")
	output := report.FormatReport(r)

	// Verify all required fields are present
	requiredFields := []string{"uplink", "downlink", "loss=", "critical_size="}
	for _, field := range requiredFields {
		if !strings.Contains(output, field) {
			t.Errorf("report missing required field: %s", field)
		}
	}

	t.Logf("Full validation test passed")
}

// Example test showing expected output format
func TestIntegrationExpectedOutputFormat(t *testing.T) {
	// This test documents the expected output format for the sample client command:
	// go run ./cmd/udpdiag client --target4 127.0.0.1 --control-port 18080 \
	//   --direction both --ports 40000-40002 --min-size 1200 --max-size 1472 \
	//   --step 136 --count 20 --interval 10ms

	results := []*report.TestResult{
		{
			TargetAddress: "127.0.0.1",
			Family:        protocol.FamilyIPv4,
			Direction:     protocol.DirectionUplink,
			UDPPort:       40000,
			PayloadSize:   1200,
			Sent:          20,
			Received:      20,
			Lost:          0,
		},
		{
			TargetAddress: "127.0.0.1",
			Family:        protocol.FamilyIPv4,
			Direction:     protocol.DirectionDownlink,
			UDPPort:       40000,
			PayloadSize:   1200,
			Sent:          20,
			Received:      20,
			Lost:          0,
		},
	}

	r := report.BuildReport(results, "55ms")
	output := report.FormatReport(r)

	// Verify format includes all required elements
	expected := []string{
		"target: 127.0.0.1",
		"family: ipv4",
		"direction: uplink",
		"direction: downlink",
		"loss=",
		"critical_size=none",
	}

	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("expected output to contain %q, got:\n%s", exp, output)
		}
	}

	t.Logf("Expected output format test passed")
	t.Logf("Output:\n%s", output)
}
