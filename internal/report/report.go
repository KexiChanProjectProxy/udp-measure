// Package report implements result aggregation and threshold reporting.
package report

import (
	"fmt"
	"sort"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

// ThresholdPercent is the loss percentage that triggers critical size detection.
const ThresholdPercent = 5.0

// TestResult holds the result of a single UDP probe test.
type TestResult struct {
	TargetAddress string
	Family        protocol.Family
	Direction     protocol.Direction
	UDPPort       int
	PayloadSize   int
	Sent          int
	Received      int
	Lost          int
}

// LossPercent calculates the loss percentage for a test result.
func (r *TestResult) LossPercent() float64 {
	if r.Sent == 0 {
		return 0.0
	}
	return float64(r.Lost) / float64(r.Sent) * 100
}

// GroupKey uniquely identifies a group of test results for threshold analysis.
type GroupKey struct {
	Family    protocol.Family
	Port      int
	Direction protocol.Direction
}

// Group represents a group of test results sharing the same family, port, and direction.
type Group struct {
	Key     GroupKey
	Results []*TestResult
}

// SizeKey uniquely identifies a specific (family, port, direction, size) combination.
type SizeKey struct {
	Family      protocol.Family
	Port        int
	Direction   protocol.Direction
	PayloadSize int
}

// CriticalSizeResult holds the critical size determination for a group.
type CriticalSizeResult struct {
	Key          GroupKey
	CriticalSize *int // nil if no size exceeded threshold
	Threshold    float64
}

// Report holds the complete aggregated report.
type Report struct {
	Groups            []Group
	CriticalSizes     []CriticalSizeResult
	TotalTests        int
	TotalSent         int
	TotalReceived     int
	EstimatedDuration string
}

// AggregateResults groups test results by (family, port, direction) and sorts by payload size ascending.
func AggregateResults(results []*TestResult) []Group {
	// Group by (family, port, direction)
	groupsMap := make(map[GroupKey][]*TestResult)

	for _, r := range results {
		key := GroupKey{
			Family:    r.Family,
			Port:      r.UDPPort,
			Direction: r.Direction,
		}
		groupsMap[key] = append(groupsMap[key], r)
	}

	// Convert to slice and sort each group by payload size ascending
	var groups []Group
	for key, grp := range groupsMap {
		// Sort by payload size ascending
		sort.Slice(grp, func(i, j int) bool {
			return grp[i].PayloadSize < grp[j].PayloadSize
		})
		groups = append(groups, Group{
			Key:     key,
			Results: grp,
		})
	}

	// Sort groups for deterministic output: by family, then port, then direction
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Key.Family != groups[j].Key.Family {
			return groups[i].Key.Family < groups[j].Key.Family
		}
		if groups[i].Key.Port != groups[j].Key.Port {
			return groups[i].Key.Port < groups[j].Key.Port
		}
		return groups[i].Key.Direction < groups[j].Key.Direction
	})

	return groups
}

// FindCriticalSize scans sizes in ascending order and returns the first size where loss >= threshold.
// Returns nil if no size reaches the threshold.
func FindCriticalSize(results []*TestResult, threshold float64) *int {
	for _, r := range results {
		if r.LossPercent() >= threshold {
			return &r.PayloadSize
		}
	}
	return nil
}

// ComputeCriticalSizes computes critical size for each group.
func ComputeCriticalSizes(groups []Group, threshold float64) []CriticalSizeResult {
	var results []CriticalSizeResult
	for _, g := range groups {
		cs := FindCriticalSize(g.Results, threshold)
		results = append(results, CriticalSizeResult{
			Key:          g.Key,
			CriticalSize: cs,
			Threshold:    threshold,
		})
	}
	return results
}

// BuildReport creates a complete report from test results.
func BuildReport(results []*TestResult, estimatedDuration string) *Report {
	groups := AggregateResults(results)
	criticalSizes := ComputeCriticalSizes(groups, ThresholdPercent)

	totalSent := 0
	totalReceived := 0
	for _, r := range results {
		totalSent += r.Sent
		totalReceived += r.Received
	}

	return &Report{
		Groups:            groups,
		CriticalSizes:     criticalSizes,
		TotalTests:        len(results),
		TotalSent:         totalSent,
		TotalReceived:     totalReceived,
		EstimatedDuration: estimatedDuration,
	}
}

// FormatReport formats the report for console output.
func FormatReport(r *Report) string {
	var lines []string

	// Header
	lines = append(lines, "=== UDP Diagnostic Report ===")
	if r.EstimatedDuration != "" {
		lines = append(lines, fmt.Sprintf("Estimated duration: %s", r.EstimatedDuration))
	}
	lines = append(lines, "")

	// Per-group details
	lines = append(lines, "--- Per-Size Results ---")

	for _, g := range r.Groups {
		familyStr := "ipv4"
		if g.Key.Family == protocol.FamilyIPv6 {
			familyStr = "ipv6"
		}
		dirStr := "uplink"
		if g.Key.Direction == protocol.DirectionDownlink {
			dirStr = "downlink"
		}

		// Get target address from first result in group (all results in group share same target for given family)
		targetAddr := ""
		if len(g.Results) > 0 {
			targetAddr = g.Results[0].TargetAddress
		}
		lines = append(lines, fmt.Sprintf("target: %s  family: %s  direction: %s  port: %d",
			targetAddr, familyStr, dirStr, g.Key.Port))

		for _, res := range g.Results {
			lossPct := res.LossPercent()
			lines = append(lines, fmt.Sprintf("  size=%d  sent=%d  received=%d  lost=%d  loss=%.1f%%",
				res.PayloadSize, res.Sent, res.Received, res.Lost, lossPct))
		}

		// Find critical size for this group
		for _, cs := range r.CriticalSizes {
			if cs.Key.Family == g.Key.Family && cs.Key.Port == g.Key.Port && cs.Key.Direction == g.Key.Direction {
				if cs.CriticalSize != nil {
					lines = append(lines, fmt.Sprintf("  critical_size=%d (observed threshold: %.1f%% loss first reached at this size)",
						*cs.CriticalSize, cs.Threshold))
				} else {
					lines = append(lines, fmt.Sprintf("  critical_size=none (no size reached %.1f%% observed threshold)",
						cs.Threshold))
				}
				break
			}
		}
		lines = append(lines, "")
	}

	// Summary
	lines = append(lines, "--- Summary ---")
	totalLost := r.TotalSent - r.TotalReceived
	totalLossPct := 0.0
	if r.TotalSent > 0 {
		totalLossPct = float64(totalLost) / float64(r.TotalSent) * 100
	}
	lines = append(lines, fmt.Sprintf("Total: %d tests, %d sent, %d received, %d lost, overall loss=%.1f%%",
		r.TotalTests, r.TotalSent, r.TotalReceived, totalLost, totalLossPct))

	return joinLines(lines)
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
