package udp

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

type Receiver struct {
	config     ReceiverConfig
	conn       net.PacketConn
	receivedMu sync.Mutex
	received   []receivedPacket
}

type receivedPacket struct {
	seq      uint32
	recvTime time.Time
	header   *protocol.ProbeHeader
	matched  bool
}

func NewReceiver(config ReceiverConfig, bindAddress string) (*Receiver, error) {
	network := "udp4"
	if config.Family == protocol.FamilyIPv6 {
		network = "udp6"
	}

	conn, err := net.ListenPacket(network, bindAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to create receiver socket: %w", err)
	}

	return &Receiver{
		config:   config,
		conn:     conn,
		received: make([]receivedPacket, 0),
	}, nil
}

func (r *Receiver) Receive(quitCh <-chan struct{}) error {
	buf := make([]byte, 65535)

	for {
		select {
		case <-quitCh:
			return nil
		default:
		}

		r.conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		n, _, err := r.conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("failed to read packet: %w", err)
		}

		if n < protocol.ProbeHeaderSize {
			continue
		}

		header := &protocol.ProbeHeader{}
		if err := header.UnmarshalBinary(buf[:protocol.ProbeHeaderSize]); err != nil {
			continue
		}

		if err := header.Validate(); err != nil {
			continue
		}

		matched := r.matchesExpected(header)

		r.receivedMu.Lock()
		r.received = append(r.received, receivedPacket{
			seq:      header.Sequence,
			recvTime: time.Now(),
			header:   header,
			matched:  matched,
		})
		r.receivedMu.Unlock()
	}
}

func (r *Receiver) matchesExpected(h *protocol.ProbeHeader) bool {
	if h.SessionID != r.config.SessionID {
		return false
	}
	if h.TestID != r.config.TestID {
		return false
	}
	if h.Direction != uint8(r.config.Direction) {
		return false
	}
	if h.Family != uint8(r.config.Family) {
		return false
	}
	return true
}

func (r *Receiver) Collect(quitCh <-chan struct{}) error {
	timeout := time.After(r.config.ReceiveWindow)
	for {
		select {
		case <-quitCh:
			return nil
		case <-timeout:
			return nil
		default:
		}

		r.conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		buf := make([]byte, 65535)
		n, _, err := r.conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("failed to read packet: %w", err)
		}

		if n < protocol.ProbeHeaderSize {
			continue
		}

		header := &protocol.ProbeHeader{}
		if err := header.UnmarshalBinary(buf[:protocol.ProbeHeaderSize]); err != nil {
			continue
		}

		if err := header.Validate(); err != nil {
			continue
		}

		matched := r.matchesExpected(header)

		r.receivedMu.Lock()
		r.received = append(r.received, receivedPacket{
			seq:      header.Sequence,
			recvTime: time.Now(),
			header:   header,
			matched:  matched,
		})
		r.receivedMu.Unlock()
	}
}

func (r *Receiver) Result() *ReceiverResult {
	r.receivedMu.Lock()
	defer r.receivedMu.Unlock()

	result := &ReceiverResult{}

	if len(r.received) == 0 {
		return result
	}

	matchedSeqs := make([]uint32, 0)

	for _, p := range r.received {
		result.Received++
		if p.matched {
			result.Matched++
			matchedSeqs = append(matchedSeqs, p.seq)
		}
	}

	if len(matchedSeqs) > 0 {
		result.FirstSeq = matchedSeqs[0]
		result.LastSeq = matchedSeqs[len(matchedSeqs)-1]

		minSeq := matchedSeqs[0]
		maxSeq := matchedSeqs[0]
		for _, seq := range matchedSeqs[1:] {
			if seq < minSeq {
				minSeq = seq
			}
			if seq > maxSeq {
				maxSeq = seq
			}
		}
		result.SequenceMin = minSeq
		result.SequenceMax = maxSeq
	}

	return result
}

func (r *Receiver) Close() error {
	return r.conn.Close()
}

func (r *Receiver) LocalAddr() net.Addr {
	return r.conn.LocalAddr()
}
