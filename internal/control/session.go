package control

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
	"github.com/udp-diagnostic/udpdiag/internal/udp"
)

type Session struct {
	id                string
	state             protocol.SessionState
	udpPort           int
	direction         protocol.Direction
	family            protocol.Family
	payloadSize       int
	packetCount       int
	intervalMs        int
	clientReceivePort int
	targetAddress     string
	sender            *udp.Sender
	receiver          *udp.Receiver
	sentCount         int
	receivedCount     int
	mu                sync.RWMutex
	conn              net.Conn
	quitCh            chan struct{}
	receiverDone      chan struct{}
}

func NewSession() *Session {
	return &Session{
		id:    generateSessionID(),
		state: protocol.StateIdle,
	}
}

func (s *Session) ID() string {
	return s.id
}

func (s *Session) State() protocol.SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Session) setState(state protocol.SessionState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := protocol.Transition(s.state, state)
	if err != nil {
		return err
	}
	s.state = state
	return nil
}

func (s *Session) Run(ctx context.Context, conn net.Conn) {
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	if err := s.setState(protocol.StatePreparing); err != nil {
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		return
	}

	s.handleMessages(ctx, conn)
}

func (s *Session) handleMessages(ctx context.Context, conn net.Conn) {
	for {
		// Check for terminal state - exit loop if session is done
		if s.State() == protocol.StateFailed || s.State() == protocol.StateCompleted || s.State() == protocol.StateCancelled {
			return
		}

		select {
		case <-ctx.Done():
			if s.handleCancel(context.Background()) {
				return
			}
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(ControlReadTimeout))
		conn.SetWriteDeadline(time.Now().Add(ControlWriteTimeout))

		data, err := readMessage(conn)
		if err != nil {
			if err == io.EOF || isTimeout(err) {
				s.handleTimeout()
				// After handleTimeout, check if we reached a terminal state
				if s.State() == protocol.StateFailed || s.State() == protocol.StateCompleted || s.State() == protocol.StateCancelled {
					return
				}
				continue
			}
			s.sendError(conn, protocol.ErrInternalError, err.Error())
			return
		}

		msgType, msg, err := protocol.DecodeEnvelope(data)
		if err != nil {
			s.sendInvalidRequest(conn, "failed to decode message", err)
			continue
		}

		if err := s.handleMessage(conn, msgType, msg); err != nil {
			return
		}
	}
}

func readMessage(conn net.Conn) ([]byte, error) {
	lengthBuf := make([]byte, 4)
	_, err := conn.Read(lengthBuf)
	if err != nil {
		return nil, err
	}

	length := int32(lengthBuf[0])<<24 | int32(lengthBuf[1])<<16 | int32(lengthBuf[2])<<8 | int32(lengthBuf[3])
	if length <= 0 || length > 1024*1024 {
		return nil, fmt.Errorf("invalid message length: %d", length)
	}

	data := make([]byte, length)
	_, err = conn.Read(data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func writeMessage(conn net.Conn, msgType protocol.MessageType, msg any) error {
	data, err := protocol.EncodeMessage(msg, msgType)
	if err != nil {
		return err
	}

	length := int32(len(data))
	lengthBuf := []byte{
		byte(length >> 24),
		byte(length >> 16),
		byte(length >> 8),
		byte(length),
	}

	if _, err := conn.Write(lengthBuf); err != nil {
		return err
	}

	_, err = conn.Write(data)
	return err
}

func isTimeout(err error) bool {
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}
	return false
}

func (s *Session) handleMessage(conn net.Conn, msgType protocol.MessageType, msg any) error {
	switch msgType {
	case protocol.MsgTypeHello:
		return s.handleHello(conn, msg.(protocol.Hello))
	case protocol.MsgTypePrepare:
		return s.handlePrepare(conn, msg.(protocol.Prepare))
	case protocol.MsgTypeStart:
		return s.handleStart(conn)
	case protocol.MsgTypeSendComplete:
		return s.handleSendComplete(conn)
	case protocol.MsgTypeFetchResult:
		return s.handleFetchResult(conn)
	case protocol.MsgTypeCancel:
		if s.handleCancel(context.Background()) {
			return nil
		}
		return nil
	default:
		s.sendInvalidRequest(conn, "unknown message type", nil)
		return nil
	}
}

func (s *Session) handleHello(conn net.Conn, msg protocol.Hello) error {
	if msg.ClientVersion != protocol.ProtocolVersion {
		s.sendInvalidRequest(conn, "protocol version mismatch", nil)
	}
	return nil
}

func (s *Session) handlePrepare(conn net.Conn, msg protocol.Prepare) error {
	if s.State() != protocol.StatePreparing {
		s.sendInvalidRequest(conn, "unexpected prepare in state "+s.State().String(), nil)
		return nil
	}

	s.udpPort = msg.UDPPort
	s.direction = msg.Direction
	s.family = msg.Family
	s.payloadSize = msg.PayloadSize
	s.packetCount = msg.PacketCount
	s.intervalMs = msg.IntervalMs
	s.clientReceivePort = msg.ClientReceivePort

	if msg.Direction == protocol.DirectionDownlink && msg.ClientReceivePort != 0 {
		if msg.Family == protocol.FamilyIPv6 {
			s.targetAddress = fmt.Sprintf("[::1]:%d", msg.ClientReceivePort)
		} else {
			s.targetAddress = fmt.Sprintf("127.0.0.1:%d", msg.ClientReceivePort)
		}
	} else {
		if msg.Family == protocol.FamilyIPv6 {
			s.targetAddress = fmt.Sprintf("[::1]:%d", msg.UDPPort)
		} else {
			s.targetAddress = fmt.Sprintf("127.0.0.1:%d", msg.UDPPort)
		}
	}

	if err := s.setState(protocol.StateReady); err != nil {
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		return err
	}

	ready := protocol.Ready{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeReady,
			SessionID: s.id,
		},
		ServerPort: s.udpPort,
	}

	if err := writeMessage(conn, protocol.MsgTypeReady, ready); err != nil {
		return err
	}

	return nil
}

func (s *Session) handleStart(conn net.Conn) error {
	if s.State() != protocol.StateReady {
		s.sendInvalidRequest(conn, "unexpected start in state "+s.State().String(), nil)
		return nil
	}

	if err := s.setState(protocol.StateRunning); err != nil {
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		return err
	}

	if s.direction == protocol.DirectionDownlink {
		go s.runDownlinkSender(conn)
	} else {
		go s.runUplinkReceiver(conn)
	}

	return nil
}

func (s *Session) runDownlinkSender(conn net.Conn) {
	sender, err := udp.NewSender(udp.SenderConfig{
		SessionID:     0,
		TestID:        0,
		Direction:     protocol.DirectionDownlink,
		Family:        s.family,
		TargetAddress: s.targetAddress,
		UDPPort:       uint16(s.clientReceivePort),
		PayloadSize:   uint16(s.payloadSize),
		PacketCount:   s.packetCount,
		Interval:      time.Duration(s.intervalMs) * time.Millisecond,
	})
	if err != nil {
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		return
	}
	s.sender = sender

	sendResult, err := sender.Send()
	s.sender = nil
	if err != nil {
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		return
	}
	s.sentCount = sendResult.Sent

	if err := s.setState(protocol.StateCollecting); err != nil {
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		return
	}

	sendComplete := protocol.SendComplete{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeSendComplete,
			SessionID: s.id,
		},
	}
	writeMessage(conn, protocol.MsgTypeSendComplete, sendComplete)
}

func (s *Session) runUplinkReceiver(conn net.Conn) {
	s.mu.Lock()
	s.quitCh = make(chan struct{})
	quitCh := s.quitCh
	s.receiverDone = make(chan struct{})
	receiverDone := s.receiverDone
	bindAddr := fmt.Sprintf(":%d", s.udpPort)
	if s.family == protocol.FamilyIPv6 {
		bindAddr = fmt.Sprintf("[::1]:%d", s.udpPort)
	} else {
		bindAddr = fmt.Sprintf("127.0.0.1:%d", s.udpPort)
	}
	receiver, err := udp.NewReceiver(udp.ReceiverConfig{
		SessionID:     0,
		TestID:        0,
		Direction:     protocol.DirectionUplink,
		Family:        s.family,
		ReceiveWindow: time.Duration(s.packetCount*s.intervalMs+1000) * time.Millisecond,
	}, bindAddr)
	if err != nil {
		s.mu.Unlock()
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		close(receiverDone)
		return
	}
	s.receiver = receiver
	s.mu.Unlock()

	receiver.Receive(quitCh)

	// Close the receiver socket after receive finishes
	receiver.Close()

	s.mu.Lock()
	s.receivedCount = receiver.Result().Received
	s.receiver = nil
	s.mu.Unlock()

	close(receiverDone)
}

func (s *Session) handleSendComplete(conn net.Conn) error {
	state := s.State()
	if state == protocol.StateCollecting {
		return nil
	}
	if state != protocol.StateRunning {
		s.sendInvalidRequest(conn, "unexpected send_complete in state "+state.String(), nil)
		return nil
	}

	if s.direction == protocol.DirectionUplink {
		s.mu.Lock()
		if s.quitCh != nil {
			close(s.quitCh)
			s.quitCh = nil
		}
		receiverDone := s.receiverDone
		s.mu.Unlock()

		if receiverDone != nil {
			<-receiverDone
		}
	}

	if err := s.setState(protocol.StateCollecting); err != nil {
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		return err
	}

	return nil
}

func (s *Session) handleFetchResult(conn net.Conn) error {
	state := s.State()
	if state != protocol.StateCollecting {
		if state == protocol.StateCompleted || state == protocol.StateFailed {
			return s.sendResult(conn)
		}
		s.sendInvalidRequest(conn, "unexpected fetch_result in state "+state.String(), nil)
		return nil
	}

	if err := s.setState(protocol.StateCompleted); err != nil {
		s.sendError(conn, protocol.ErrInternalError, err.Error())
		return err
	}

	return s.sendResult(conn)
}

func (s *Session) handleCancel(ctx context.Context) bool {
	state := s.State()
	if state == protocol.StateIdle || protocol.IsTerminal(state) {
		return false
	}

	s.mu.Lock()
	s.state = protocol.StateCancelled
	s.mu.Unlock()

	s.cleanupNoStateChange()
	return true
}

func (s *Session) handleTimeout() {
	state := s.State()

	s.mu.Lock()
	if state == protocol.StatePreparing {
		s.state = protocol.StateFailed
	} else if state == protocol.StateRunning {
		s.state = protocol.StateCollecting
	} else if state == protocol.StateCollecting {
		s.state = protocol.StateFailed
	}
	s.mu.Unlock()

	s.cleanupNoStateChange()
}

func (s *Session) cleanupNoStateChange() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}

	if s.receiver != nil {
		s.receiver.Close()
		s.receiver = nil
	}
}

func (s *Session) cancelSession() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !protocol.IsTerminal(s.state) {
		s.state = protocol.StateCancelled
	}
	s.cleanupLocked()
}

func (s *Session) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
}

func (s *Session) cleanupLocked() {
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}

	if s.receiver != nil {
		s.receiver.Close()
		s.receiver = nil
	}
}

func (s *Session) sendResult(conn net.Conn) error {
	sent := s.sentCount
	received := s.receivedCount
	if sent == 0 {
		sent = s.packetCount
	}
	lossRate := 0.0
	if sent > 0 {
		lossRate = float64(sent-received) / float64(sent) * 100
	}

	result := protocol.Result{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeResult,
			SessionID: s.id,
		},
		Sent:         sent,
		Received:     received,
		LossRate:     lossRate,
		CriticalSize: 0,
	}

	if err := writeMessage(conn, protocol.MsgTypeResult, result); err != nil {
		conn.Close()
		return err
	}
	conn.Close()
	return nil
}

func (s *Session) sendError(conn net.Conn, code protocol.ErrorCode, msg string) {
	resp := protocol.InternalError{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeInternalError,
			SessionID: s.id,
		},
		Code:    code,
		Message: msg,
	}
	if err := writeMessage(conn, protocol.MsgTypeInternalError, resp); err != nil {
		log.Printf("session %s: failed to send error response: %v", s.id, err)
	}
}

func (s *Session) sendInvalidRequest(conn net.Conn, msg string, err error) {
	resp := protocol.InvalidRequest{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeInvalidRequest,
			SessionID: s.id,
		},
		Code:    protocol.ErrInvalidRequest,
		Message: msg,
	}
	writeMessage(conn, protocol.MsgTypeInvalidRequest, resp)
}

func generateSessionID() string {
	return fmt.Sprintf("session-%d", time.Now().UnixNano())
}
