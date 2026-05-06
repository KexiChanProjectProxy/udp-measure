package udp

import (
	"fmt"
	"net"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

type Sender struct {
	config SenderConfig
	conn   net.PacketConn
}

func NewSender(config SenderConfig) (*Sender, error) {
	network := "udp4"
	if config.Family == protocol.FamilyIPv6 {
		network = "udp6"
	}

	conn, err := net.ListenPacket(network, ":0")
	if err != nil {
		return nil, fmt.Errorf("failed to create sender socket: %w", err)
	}

	return &Sender{
		config: config,
		conn:   conn,
	}, nil
}

func (s *Sender) Send() (*SenderResult, error) {
	network := "udp4"
	if s.config.Family == protocol.FamilyIPv6 {
		network = "udp6"
	}

	addr := s.config.TargetAddress
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, fmt.Sprintf("%d", s.config.UDPPort))
	}
	targetAddr, err := net.ResolveUDPAddr(network, addr)
	if err != nil {
		s.conn.Close()
		return nil, fmt.Errorf("failed to resolve target address: %w", err)
	}

	packetSize := int(protocol.ProbeHeaderSize) + int(s.config.PayloadSize)
	buf := make([]byte, packetSize)

	header := &protocol.ProbeHeader{}
	header.SetFromParams(
		s.config.SessionID,
		s.config.TestID,
		s.config.Direction,
		s.config.Family,
		s.config.UDPPort,
		s.config.PayloadSize,
		0,
	)

	headerBytes, err := header.MarshalBinary()
	if err != nil {
		s.conn.Close()
		return nil, fmt.Errorf("failed to marshal header: %w", err)
	}
	copy(buf, headerBytes)

	start := time.Now()
	seq := uint32(0)

	for i := 0; i < s.config.PacketCount; i++ {
		seq = uint32(i)
		header.SetFromParams(
			s.config.SessionID,
			s.config.TestID,
			s.config.Direction,
			s.config.Family,
			s.config.UDPPort,
			s.config.PayloadSize,
			seq,
		)

		headerBytes, err := header.MarshalBinary()
		if err != nil {
			s.conn.Close()
			return nil, fmt.Errorf("failed to marshal header: %w", err)
		}
		copy(buf, headerBytes)

		_, err = s.conn.WriteTo(buf, targetAddr)
		if err != nil {
			s.conn.Close()
			return nil, fmt.Errorf("failed to send packet: %w", err)
		}

		if i < s.config.PacketCount-1 {
			time.Sleep(s.config.Interval)
		}
	}

	duration := time.Since(start)

	return &SenderResult{
		Sent:          s.config.PacketCount,
		SequenceStart: 0,
		SequenceEnd:   seq,
		Duration:      duration,
	}, nil
}

func (s *Sender) Close() error {
	return s.conn.Close()
}

func (s *Sender) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}
