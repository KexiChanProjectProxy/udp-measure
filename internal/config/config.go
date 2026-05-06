// Package config handles parameter parsing and sweep planning.
package config

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

// ClientConfig holds the parsed and validated client parameters.
type ClientConfig struct {
	Target4              string
	Target6              string
	ControlPort          int
	Direction            protocol.Direction
	Ports                []int
	MinSize              int
	MaxSize              int
	Step                 int
	Count                int
	Interval             time.Duration
	MaxEstimatedDuration time.Duration
}

// SweepPlan represents a single test case in the sweep matrix.
type SweepPlan struct {
	Family      protocol.Family
	Port        int
	Direction   protocol.Direction
	PayloadSize int
}

// ParsePorts parses a port specification string.
// Supports single port (40000), comma-separated (40000,40001), and ranges (40000-40100).
func ParsePorts(spec string) ([]int, error) {
	if spec == "" {
		return nil, fmt.Errorf("port specification cannot be empty")
	}

	// Check if it's a range (contains - but not just a dash between numbers)
	parts := strings.Split(spec, ",")
	var ports []int

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty port in specification")
		}

		// Check if it's a range (contains -)
		if strings.Contains(part, "-") {
			rangePorts, err := parseRange(part)
			if err != nil {
				return nil, err
			}
			ports = append(ports, rangePorts...)
		} else {
			// Single port
			port, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid port %q: %w", part, err)
			}
			if err := validatePort(port); err != nil {
				return nil, err
			}
			ports = append(ports, port)
		}
	}

	if len(ports) == 0 {
		return nil, fmt.Errorf("no valid ports found in specification %q", spec)
	}

	// Sort and deduplicate
	ports = sortAndDedupe(ports)

	return ports, nil
}

// parseRange parses a range specification like "40000-40100".
func parseRange(spec string) ([]int, error) {
	rangeParts := strings.Split(spec, "-")
	if len(rangeParts) != 2 {
		return nil, fmt.Errorf("invalid range specification %q", spec)
	}

	start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
	if err != nil {
		return nil, fmt.Errorf("invalid range start %q: %w", rangeParts[0], err)
	}
	end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid range end %q: %w", rangeParts[1], err)
	}

	if start > end {
		return nil, fmt.Errorf("range start (%d) greater than end (%d)", start, end)
	}

	if err := validatePort(start); err != nil {
		return nil, fmt.Errorf("range start %d: %w", start, err)
	}
	if err := validatePort(end); err != nil {
		return nil, fmt.Errorf("range end %d: %w", end, err)
	}

	ports := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		ports = append(ports, i)
	}

	return ports, nil
}

// validatePort checks that a port number is valid.
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of valid range (1-65535)", port)
	}
	return nil
}

// sortAndDedupe sorts ports and removes duplicates.
func sortAndDedupe(ports []int) []int {
	sort.Ints(ports)
	result := make([]int, 0, len(ports))
	for i, p := range ports {
		if i == 0 || ports[i-1] != p {
			result = append(result, p)
		}
	}
	return result
}

// Validate performs validation of client configuration parameters.
// Returns an error for any invalid configuration.
func (c *ClientConfig) Validate() error {
	// Validate targets - at least one must be non-empty
	if c.Target4 == "" && c.Target6 == "" {
		return fmt.Errorf("either --target4 or --target6 (or both) is required")
	}

	// Validate IPv4 target if provided
	if c.Target4 != "" {
		if err := validateTargetAddress(c.Target4, "ipv4"); err != nil {
			return err
		}
	}

	// Validate IPv6 target if provided
	if c.Target6 != "" {
		if err := validateTargetAddress(c.Target6, "ipv6"); err != nil {
			return err
		}
	}

	// Validate control port
	if c.ControlPort < 1 || c.ControlPort > 65535 {
		return fmt.Errorf("control port %d out of valid range (1-65535)", c.ControlPort)
	}

	// Validate ports - must have at least one
	if len(c.Ports) == 0 {
		return fmt.Errorf("at least one port is required")
	}

	// Validate size sweep parameters
	if c.MinSize > c.MaxSize {
		return fmt.Errorf("min-size (%d) cannot be greater than max-size (%d)", c.MinSize, c.MaxSize)
	}

	if c.Step <= 0 {
		return fmt.Errorf("step (%d) must be greater than 0", c.Step)
	}

	// Validate count and interval
	if c.Count <= 0 {
		return fmt.Errorf("count (%d) must be greater than 0", c.Count)
	}

	if c.Interval <= 0 {
		return fmt.Errorf("interval (%v) must be greater than 0", c.Interval)
	}

	// Validate max estimated duration
	if c.MaxEstimatedDuration <= 0 {
		return fmt.Errorf("max-estimated-duration (%v) must be greater than 0", c.MaxEstimatedDuration)
	}

	return nil
}

// validateTargetAddress validates an IP target address.
func validateTargetAddress(addr, family string) error {
	parsed := net.ParseIP(addr)
	if parsed == nil {
		return fmt.Errorf("invalid %s address: %q", family, addr)
	}

	if family == "ipv4" && parsed.To4() == nil {
		return fmt.Errorf("address %q is not a valid IPv4 address", addr)
	}
	if family == "ipv6" && parsed.To4() != nil {
		return fmt.Errorf("address %q is not a valid IPv6 address", addr)
	}

	return nil
}

// BuildSweepPlan creates a deterministic sweep plan from the client configuration.
// The sweep expands across family × port × direction × size in deterministic order:
// 1. IPv4 first, then IPv6 (if both targets provided)
// 2. Ports in ascending order (guaranteed even if Ports slice is unsorted)
// 3. Direction: uplink first, then downlink (when direction is 'both')
// 4. Size in ascending order
func (c *ClientConfig) BuildSweepPlan() ([]SweepPlan, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	// Sort ports to guarantee ascending order regardless of how ClientConfig was constructed
	sortedPorts := make([]int, len(c.Ports))
	copy(sortedPorts, c.Ports)
	sort.Ints(sortedPorts)

	var plans []SweepPlan

	// Determine which families to test
	families := determineFamilies(c.Target4, c.Target6)

	// Determine which directions to test
	directions := determineDirections(c.Direction)

	// Generate sizes
	sizes := generateSizes(c.MinSize, c.MaxSize, c.Step)

	// Deterministic expansion order:
	// family → port → direction → size
	for _, family := range families {
		for _, port := range sortedPorts {
			for _, direction := range directions {
				for _, size := range sizes {
					plans = append(plans, SweepPlan{
						Family:      family,
						Port:        port,
						Direction:   direction,
						PayloadSize: size,
					})
				}
			}
		}
	}

	return plans, nil
}

// determineFamilies returns the list of families to test based on provided targets.
func determineFamilies(target4, target6 string) []protocol.Family {
	var families []protocol.Family
	if target4 != "" {
		families = append(families, protocol.FamilyIPv4)
	}
	if target6 != "" {
		families = append(families, protocol.FamilyIPv6)
	}
	return families
}

// determineDirections returns the list of directions to test.
func determineDirections(dir protocol.Direction) []protocol.Direction {
	switch dir {
	case protocol.DirectionUplink:
		return []protocol.Direction{protocol.DirectionUplink}
	case protocol.DirectionDownlink:
		return []protocol.Direction{protocol.DirectionDownlink}
	case protocol.DirectionBoth:
		// Deterministic order: uplink first, then downlink
		return []protocol.Direction{protocol.DirectionUplink, protocol.DirectionDownlink}
	default:
		return nil
	}
}

// generateSizes generates the list of payload sizes for the sweep.
func generateSizes(min, max, step int) []int {
	var sizes []int
	for size := min; size <= max; size += step {
		sizes = append(sizes, size)
	}
	return sizes
}

// EstimateDuration estimates the total duration of the sweep plan.
// Each test case takes approximately count × interval time.
func (c *ClientConfig) EstimateDuration(plans []SweepPlan) time.Duration {
	// Each sweep plan item sends c.Count packets at c.Interval interval
	// Total time per plan = c.Count * c.Interval
	// But we also add some overhead for setup/teardown per test
	perTestDuration := c.Interval * time.Duration(c.Count)
	// Add 10% overhead for network variability
	overheadFactor := 1.1

	return time.Duration(float64(perTestDuration)*overheadFactor) * time.Duration(len(plans))
}

// CheckDurationLimit checks if the estimated duration exceeds the limit.
// Returns an error if the sweep would take longer than maxEstimatedDuration.
func (c *ClientConfig) CheckDurationLimit(plans []SweepPlan) error {
	estimated := c.EstimateDuration(plans)
	if estimated > c.MaxEstimatedDuration {
		return fmt.Errorf("estimated sweep duration %v exceeds maximum allowed %v (override with --max-estimated-duration)", estimated, c.MaxEstimatedDuration)
	}
	return nil
}

// NewClientConfig creates a validated ClientConfig from raw parameters.
func NewClientConfig(
	target4, target6 string,
	controlPort int,
	direction protocol.Direction,
	portsSpec string,
	minSize, maxSize, step, count int,
	interval, maxEstimatedDuration time.Duration,
) (*ClientConfig, error) {
	// Parse ports
	ports, err := ParsePorts(portsSpec)
	if err != nil {
		return nil, fmt.Errorf("invalid ports: %w", err)
	}

	cfg := &ClientConfig{
		Target4:              target4,
		Target6:              target6,
		ControlPort:          controlPort,
		Direction:            direction,
		Ports:                ports,
		MinSize:              minSize,
		MaxSize:              maxSize,
		Step:                 step,
		Count:                count,
		Interval:             interval,
		MaxEstimatedDuration: maxEstimatedDuration,
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
