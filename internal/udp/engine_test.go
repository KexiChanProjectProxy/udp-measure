package udp

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

// Dynamic port allocation for tests to avoid port conflicts between test runs
var testPortCounter int

func nextTestPort() int {
	testPortCounter++
	// Use ports in the 45000-45999 range to avoid conflicts with other tests
	return 45000 + (testPortCounter % 1000)
}

func TestUDPProbeHeaderValidation(t *testing.T) {
	header := &protocol.ProbeHeader{}

	header.SetFromParams(
		12345,
		1,
		protocol.DirectionUplink,
		protocol.FamilyIPv4,
		40000,
		100,
		0,
	)

	headerBytes, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("failed to marshal header: %v", err)
	}

	if len(headerBytes) != protocol.ProbeHeaderSize {
		t.Fatalf("header size wrong: got %d, want %d", len(headerBytes), protocol.ProbeHeaderSize)
	}

	parsed := &protocol.ProbeHeader{}
	if err := parsed.UnmarshalBinary(headerBytes); err != nil {
		t.Fatalf("failed to unmarshal header: %v", err)
	}

	if parsed.Version != header.Version {
		t.Errorf("version mismatch: got %d, want %d", parsed.Version, header.Version)
	}
	if parsed.SessionID != header.SessionID {
		t.Errorf("sessionID mismatch: got %d, want %d", parsed.SessionID, header.SessionID)
	}
	if parsed.TestID != header.TestID {
		t.Errorf("testID mismatch: got %d, want %d", parsed.TestID, header.TestID)
	}
	if parsed.Direction != header.Direction {
		t.Errorf("direction mismatch: got %d, want %d", parsed.Direction, header.Direction)
	}
	if parsed.Family != header.Family {
		t.Errorf("family mismatch: got %d, want %d", parsed.Family, header.Family)
	}
	if parsed.UDPPort != header.UDPPort {
		t.Errorf("udpPort mismatch: got %d, want %d", parsed.UDPPort, header.UDPPort)
	}
	if parsed.PayloadSize != header.PayloadSize {
		t.Errorf("payloadSize mismatch: got %d, want %d", parsed.PayloadSize, header.PayloadSize)
	}
	if parsed.Sequence != header.Sequence {
		t.Errorf("sequence mismatch: got %d, want %d", parsed.Sequence, header.Sequence)
	}

	if err := parsed.Validate(); err != nil {
		t.Errorf("header validation failed: %v", err)
	}

	invalidHeader := &protocol.ProbeHeader{Version: 99}
	if err := invalidHeader.Validate(); err == nil {
		t.Error("expected validation error for version mismatch")
	}

	invalidDirHeader := &protocol.ProbeHeader{Version: 1, Direction: 99}
	if err := invalidDirHeader.Validate(); err == nil {
		t.Error("expected validation error for invalid direction")
	}
}

func TestUDPSenderRespectsInterval(t *testing.T) {
	packetCount := 5
	interval := 50 * time.Millisecond
	port := nextTestPort()

	sender, err := NewSender(SenderConfig{
		SessionID:     999,
		TestID:        1,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		TargetAddress: fmt.Sprintf("127.0.0.1:%d", port),
		UDPPort:       uint16(port),
		PayloadSize:   100,
		PacketCount:   packetCount,
		Interval:      interval,
	})
	if err != nil {
		t.Fatalf("failed to create sender: %v", err)
	}
	defer sender.Close()

	receiver, err := NewReceiver(ReceiverConfig{
		SessionID:     999,
		TestID:        1,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		ReceiveWindow: 500 * time.Millisecond,
	}, fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to create receiver: %v", err)
	}
	defer receiver.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		quit := make(chan struct{})
		if err := receiver.Receive(quit); err != nil {
			t.Logf("receiver error: %v", err)
		}
	}()

	time.Sleep(10 * time.Millisecond)

	result, err := sender.Send()
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	if result.Sent != packetCount {
		t.Errorf("sent count wrong: got %d, want %d", result.Sent, packetCount)
	}

	elapsed := result.Duration
	expectedMin := time.Duration(packetCount-1) * interval
	if elapsed < expectedMin {
		t.Errorf("sender finished too quickly: %v < %v (did not respect interval?)", elapsed, expectedMin)
	}

	time.Sleep(50 * time.Millisecond)
}

func TestUDPReceiverCountsMatchingPacketsOnly(t *testing.T) {
	sessionID := uint32(12345)
	testID := uint32(7)
	port := nextTestPort()

	receiver, err := NewReceiver(ReceiverConfig{
		SessionID:     sessionID,
		TestID:        testID,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		ReceiveWindow: 300 * time.Millisecond,
	}, fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to create receiver: %v", err)
	}
	defer receiver.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		quit := make(chan struct{})
		if err := receiver.Collect(quit); err != nil {
			t.Logf("receiver error: %v", err)
		}
	}()

	time.Sleep(10 * time.Millisecond)

	sender, err := NewSender(SenderConfig{
		SessionID:     sessionID,
		TestID:        testID,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		TargetAddress: fmt.Sprintf("127.0.0.1:%d", port),
		UDPPort:       uint16(port),
		PayloadSize:   100,
		PacketCount:   5,
		Interval:      10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create sender: %v", err)
	}
	defer sender.Close()

	_, err = sender.Send()
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	result := receiver.Result()
	if result.Received == 0 {
		t.Error("expected to receive packets")
	}
	if result.Matched == 0 {
		t.Error("expected matched packets")
	}
	if result.Matched != 5 {
		t.Errorf("matched count wrong: got %d, want 5", result.Matched)
	}
}

func TestUDPReceiverIgnoresMismatchedSession(t *testing.T) {
	port := nextTestPort()

	receiver, err := NewReceiver(ReceiverConfig{
		SessionID:     99999,
		TestID:        1,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		ReceiveWindow: 300 * time.Millisecond,
	}, fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to create receiver: %v", err)
	}
	defer receiver.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		quit := make(chan struct{})
		if err := receiver.Collect(quit); err != nil {
			t.Logf("receiver error: %v", err)
		}
	}()

	time.Sleep(10 * time.Millisecond)

	mismatchedSender, err := NewSender(SenderConfig{
		SessionID:     11111,
		TestID:        1,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		TargetAddress: fmt.Sprintf("127.0.0.1:%d", port),
		UDPPort:       uint16(port),
		PayloadSize:   100,
		PacketCount:   5,
		Interval:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create sender: %v", err)
	}

	_, err = mismatchedSender.Send()
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	mismatchedSender.Close()

	time.Sleep(100 * time.Millisecond)

	result := receiver.Result()
	if result.Matched > 0 {
		t.Errorf("expected no matches for mismatched session, got %d", result.Matched)
	}

	wrongTestSender, err := NewSender(SenderConfig{
		SessionID:     99999,
		TestID:        999,
		Direction:     protocol.DirectionUplink,
		Family:       protocol.FamilyIPv4,
		TargetAddress: fmt.Sprintf("127.0.0.1:%d", port),
		UDPPort:       uint16(port),
		PayloadSize:   100,
		PacketCount:   5,
		Interval:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create sender: %v", err)
	}

	_, err = wrongTestSender.Send()
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	wrongTestSender.Close()

	time.Sleep(100 * time.Millisecond)

	result2 := receiver.Result()
	if result2.Matched > 0 {
		t.Errorf("expected no matches for wrong testID, got %d", result2.Matched)
	}
}

func TestUDPSenderPayloadSizeCorrect(t *testing.T) {
	payloadSize := uint16(200)
	port := nextTestPort()

	receiver, err := NewReceiver(ReceiverConfig{
		SessionID:     333,
		TestID:        1,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		ReceiveWindow: 300 * time.Millisecond,
	}, fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to create receiver: %v", err)
	}
	defer receiver.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	var receivedSizes []uint16
	var mu sync.Mutex

	go func() {
		defer wg.Done()
		quit := make(chan struct{})
		conn := receiver.conn
		buf := make([]byte, 65535)

		for {
			select {
			case <-quit:
				return
			default:
			}

			conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				return
			}

			if n < protocol.ProbeHeaderSize {
				continue
			}

			header := &protocol.ProbeHeader{}
			if err := header.UnmarshalBinary(buf[:protocol.ProbeHeaderSize]); err != nil {
				continue
			}

			mu.Lock()
			receivedSizes = append(receivedSizes, header.PayloadSize)
			mu.Unlock()
		}
	}()

	time.Sleep(10 * time.Millisecond)

	sender, err := NewSender(SenderConfig{
		SessionID:     333,
		TestID:        1,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		TargetAddress: fmt.Sprintf("127.0.0.1:%d", port),
		UDPPort:       uint16(port),
		PayloadSize:   payloadSize,
		PacketCount:   3,
		Interval:      10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create sender: %v", err)
	}

	_, err = sender.Send()
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	sender.Close()

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	for _, size := range receivedSizes {
		if size != payloadSize {
			t.Errorf("payload size mismatch: got %d, want %d", size, payloadSize)
		}
	}
}

func TestUDPIPv6Sockets(t *testing.T) {
	if !supportsIPv6() {
		t.Skip("IPv6 not available on this system")
	}

	port := nextTestPort()

	_, err := NewSender(SenderConfig{
		SessionID:     444,
		TestID:        1,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv6,
		TargetAddress: fmt.Sprintf("[::1]:%d", port),
		UDPPort:       uint16(port),
		PayloadSize:   100,
		PacketCount:   1,
		Interval:      10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create IPv6 sender: %v", err)
	}

	receiver, err := NewReceiver(ReceiverConfig{
		SessionID:     444,
		TestID:        1,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv6,
		ReceiveWindow: 100 * time.Millisecond,
	}, fmt.Sprintf("[::1]:%d", port))
	if err != nil {
		t.Fatalf("failed to create IPv6 receiver: %v", err)
	}
	receiver.Close()
}

func supportsIPv6() bool {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
