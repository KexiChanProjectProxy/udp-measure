// Package udp implements UDP probe engine and packet statistics.
package udp

import (
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

// SenderConfig holds configuration for a UDP sender.
type SenderConfig struct {
	// SessionID is the unique session identifier.
	SessionID uint32
	// TestID is the test case identifier within the session.
	TestID uint32
	// Direction is the probe direction (uplink or downlink).
	Direction protocol.Direction
	// Family is the IP address family (ipv4 or ipv6).
	Family protocol.Family
	// TargetAddress is the destination address for probes.
	TargetAddress string
	// UDPPort is the destination UDP port.
	UDPPort uint16
	// PayloadSize is the UDP payload size in bytes.
	PayloadSize uint16
	// PacketCount is the number of packets to send.
	PacketCount int
	// Interval is the time between packets.
	Interval time.Duration
}

// SenderResult holds the result of a sender run.
type SenderResult struct {
	Sent          int
	SequenceStart uint32
	SequenceEnd   uint32
	Duration      time.Duration
}

// ReceiverConfig holds configuration for a UDP receiver.
type ReceiverConfig struct {
	// SessionID is the expected session identifier.
	SessionID uint32
	// TestID is the expected test case identifier.
	TestID uint32
	// Direction is the expected direction.
	Direction protocol.Direction
	// Family is the expected IP address family.
	Family protocol.Family
	// ReceiveWindow is the duration to collect packets.
	ReceiveWindow time.Duration
}

// ReceiverResult holds the result of a receiver run.
type ReceiverResult struct {
	Received    int
	Matched     int
	SequenceMin uint32
	SequenceMax uint32
	FirstSeq    uint32
	LastSeq     uint32
}

// TestParams holds combined sender/receiver parameters for a test.
type TestParams struct {
	SessionID     uint32
	TestID        uint32
	Direction     protocol.Direction
	Family        protocol.Family
	TargetAddress string
	UDPPort       uint16
	PayloadSize   uint16
	PacketCount   int
	IntervalMs    int
}

// IntervalDuration returns the interval as a time.Duration.
func (p *TestParams) IntervalDuration() time.Duration {
	return time.Duration(p.IntervalMs) * time.Millisecond
}
