package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

func startTestServer(t *testing.T) (*Server, string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		ln, err = net.Listen("tcp6", "[::1]:0")
	}
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	addr := ln.Addr().String()
	server := NewServer(addr)
	ln.Close()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	return server, server.listener.Addr().String()
}

func connectClient(addr string) (net.Conn, error) {
	return net.Dial("tcp", addr)
}

func sendMsg(conn net.Conn, msgType protocol.MessageType, msg any) error {
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

func readMsgRaw(conn net.Conn) ([]byte, error) {
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

func readEnv(conn net.Conn) (protocol.MessageType, any, error) {
	data, err := readMsgRaw(conn)
	if err != nil {
		return 0, nil, err
	}

	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, nil, err
	}

	msg, err := protocol.Decode(env.Payload, env.Type)
	if err != nil {
		return 0, nil, err
	}

	return env.Type, msg, nil
}

func waitForState(session *Session, want protocol.SessionState, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if session.State() == want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestServerBusyResponse(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Close()

	conn1, err := connectClient(addr)
	if err != nil {
		t.Fatalf("client 1 connect failed: %v", err)
	}
	defer conn1.Close()

	time.Sleep(10 * time.Millisecond)

	session := server.Session()
	if session == nil {
		t.Fatal("expected session to be created immediately after connect")
	}

	if !waitForState(session, protocol.StatePreparing, 500*time.Millisecond) {
		t.Fatalf("expected state preparing within timeout, got %v", session.State())
	}

	prepare := protocol.Prepare{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypePrepare,
			SessionID: session.ID(),
		},
		Direction:   protocol.DirectionUplink,
		Family:      protocol.FamilyIPv4,
		UDPPort:     40000,
		PayloadSize: 1200,
		PacketCount: 10,
		IntervalMs:  10,
	}
	if err := sendMsg(conn1, protocol.MsgTypePrepare, prepare); err != nil {
		t.Fatalf("failed to send prepare: %v", err)
	}

	msgType, _, err := readEnv(conn1)
	if err != nil {
		t.Fatalf("failed to read ready: %v", err)
	}
	if msgType != protocol.MsgTypeReady {
		t.Fatalf("expected ready, got %v", msgType)
	}

	if !waitForState(session, protocol.StateReady, 500*time.Millisecond) {
		t.Fatalf("expected state ready within timeout, got %v", session.State())
	}

	conn2, err := connectClient(addr)
	if err != nil {
		t.Fatalf("client 2 connect failed: %v", err)
	}
	defer conn2.Close()

	msgType, msg, err := readEnv(conn2)
	if err != nil {
		t.Fatalf("failed to read busy response: %v", err)
	}

	if msgType != protocol.MsgTypeBusy {
		t.Fatalf("expected busy, got %v", msgType)
	}

	busy, ok := msg.(protocol.Busy)
	if !ok {
		t.Fatalf("expected Busy message, got %T", msg)
	}

	if busy.CurrentSession == "" {
		t.Error("expected current session to be set in busy response")
	}

	if busy.CurrentSession != session.ID() {
		t.Errorf("busy.CurrentSession = %q, want %q", busy.CurrentSession, session.ID())
	}
}

func TestServerSessionLifecycle(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Close()

	conn, err := connectClient(addr)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	time.Sleep(10 * time.Millisecond)

	session := server.Session()
	if session == nil {
		t.Fatal("expected session to be created")
	}

	hello := protocol.Hello{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeHello,
			SessionID: session.ID(),
		},
		ClientVersion: protocol.ProtocolVersion,
	}
	if err := sendMsg(conn, protocol.MsgTypeHello, hello); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}

	if !waitForState(session, protocol.StatePreparing, 500*time.Millisecond) {
		t.Fatalf("expected state preparing, got %v", session.State())
	}

	prepare := protocol.Prepare{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypePrepare,
			SessionID: session.ID(),
		},
		Direction:   protocol.DirectionUplink,
		Family:      protocol.FamilyIPv4,
		UDPPort:     40000,
		PayloadSize: 1200,
		PacketCount: 10,
		IntervalMs:  10,
	}
	if err := sendMsg(conn, protocol.MsgTypePrepare, prepare); err != nil {
		t.Fatalf("failed to send prepare: %v", err)
	}

	msgType, _, err := readEnv(conn)
	if err != nil {
		t.Fatalf("failed to read ready: %v", err)
	}
	if msgType != protocol.MsgTypeReady {
		t.Fatalf("expected ready, got %v", msgType)
	}

	if !waitForState(session, protocol.StateReady, 500*time.Millisecond) {
		t.Fatalf("expected state ready, got %v", session.State())
	}

	start := protocol.Start{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeStart,
			SessionID: session.ID(),
		},
	}
	if err := sendMsg(conn, protocol.MsgTypeStart, start); err != nil {
		t.Fatalf("failed to send start: %v", err)
	}

	if !waitForState(session, protocol.StateRunning, 500*time.Millisecond) {
		t.Fatalf("expected state running, got %v", session.State())
	}

	sendComplete := protocol.SendComplete{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeSendComplete,
			SessionID: session.ID(),
		},
	}
	if err := sendMsg(conn, protocol.MsgTypeSendComplete, sendComplete); err != nil {
		t.Fatalf("failed to send send_complete: %v", err)
	}

	if !waitForState(session, protocol.StateCollecting, 500*time.Millisecond) {
		t.Fatalf("expected state collecting, got %v", session.State())
	}

	fetchResult := protocol.FetchResult{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeFetchResult,
			SessionID: session.ID(),
		},
	}
	if err := sendMsg(conn, protocol.MsgTypeFetchResult, fetchResult); err != nil {
		t.Fatalf("failed to send fetch_result: %v", err)
	}

	msgType, _, err = readEnv(conn)
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	if msgType != protocol.MsgTypeResult {
		t.Fatalf("expected result, got %v", msgType)
	}

	conn.Close()

	time.Sleep(50 * time.Millisecond)

	if !waitForState(session, protocol.StateCompleted, 500*time.Millisecond) {
		t.Fatalf("expected state completed, got %v", session.State())
	}

	if server.IsActive() {
		t.Error("server should not have active session after completed")
	}
}

func TestServerCancelCleansResources(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Close()

	conn, err := connectClient(addr)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	time.Sleep(10 * time.Millisecond)

	session := server.Session()
	if session == nil {
		t.Fatal("expected session to be created")
	}

	hello := protocol.Hello{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeHello,
			SessionID: session.ID(),
		},
		ClientVersion: protocol.ProtocolVersion,
	}
	if err := sendMsg(conn, protocol.MsgTypeHello, hello); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}

	if !waitForState(session, protocol.StatePreparing, 500*time.Millisecond) {
		t.Fatalf("expected state preparing, got %v", session.State())
	}

	prepare := protocol.Prepare{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypePrepare,
			SessionID: session.ID(),
		},
		Direction:   protocol.DirectionUplink,
		Family:      protocol.FamilyIPv4,
		UDPPort:     40000,
		PayloadSize: 1200,
		PacketCount: 10,
		IntervalMs:  10,
	}
	if err := sendMsg(conn, protocol.MsgTypePrepare, prepare); err != nil {
		t.Fatalf("failed to send prepare: %v", err)
	}

	msgType, _, err := readEnv(conn)
	if err != nil {
		t.Fatalf("failed to read ready: %v", err)
	}
	if msgType != protocol.MsgTypeReady {
		t.Fatalf("expected ready, got %v", msgType)
	}

	if !waitForState(session, protocol.StateReady, 500*time.Millisecond) {
		t.Fatalf("expected state ready, got %v", session.State())
	}

	start := protocol.Start{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeStart,
			SessionID: session.ID(),
		},
	}
	if err := sendMsg(conn, protocol.MsgTypeStart, start); err != nil {
		t.Fatalf("failed to send start: %v", err)
	}

	if !waitForState(session, protocol.StateRunning, 500*time.Millisecond) {
		t.Fatalf("expected state running, got %v", session.State())
	}

	cancel := protocol.Cancel{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeCancel,
			SessionID: session.ID(),
		},
		Reason: "test cancellation",
	}
	if err := sendMsg(conn, protocol.MsgTypeCancel, cancel); err != nil {
		t.Fatalf("failed to send cancel: %v", err)
	}

	if !waitForState(session, protocol.StateCancelled, 500*time.Millisecond) {
		t.Fatalf("expected state cancelled, got %v", session.State())
	}

	if server.IsActive() {
		t.Error("server should not have active session after cancel")
	}

	if session.State() != protocol.StateCancelled {
		t.Fatalf("expected state cancelled, got %v", session.State())
	}
}

func TestListenNetwork(t *testing.T) {
	tests := []struct {
		addr     string
		expected string
	}{
		{"127.0.0.1:18080", "tcp4"},
		{"[::1]:18080", "tcp6"},
		{":18080", "tcp"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			result := ListenNetwork(tt.addr)
			if result != tt.expected {
				t.Errorf("ListenNetwork(%q) = %q, want %q", tt.addr, result, tt.expected)
			}
		})
	}
}

func TestSessionStateTransitions(t *testing.T) {
	session := NewSession()

	if session.State() != protocol.StateIdle {
		t.Fatalf("expected initial state idle, got %v", session.State())
	}

	session.setState(protocol.StatePreparing)
	if session.State() != protocol.StatePreparing {
		t.Fatalf("expected state preparing, got %v", session.State())
	}

	session.setState(protocol.StateReady)
	if session.State() != protocol.StateReady {
		t.Fatalf("expected state ready, got %v", session.State())
	}

	session.setState(protocol.StateRunning)
	if session.State() != protocol.StateRunning {
		t.Fatalf("expected state running, got %v", session.State())
	}

	session.setState(protocol.StateCollecting)
	if session.State() != protocol.StateCollecting {
		t.Fatalf("expected state collecting, got %v", session.State())
	}

	session.setState(protocol.StateCompleted)
	if session.State() != protocol.StateCompleted {
		t.Fatalf("expected state completed, got %v", session.State())
	}
}

func TestSessionCancelFromPreparing(t *testing.T) {
	session := NewSession()
	session.setState(protocol.StatePreparing)
	session.handleCancel(context.Background())

	if session.State() != protocol.StateCancelled {
		t.Fatalf("expected state cancelled, got %v", session.State())
	}
}

func TestSessionCancelFromReady(t *testing.T) {
	session := NewSession()
	session.setState(protocol.StatePreparing)
	session.setState(protocol.StateReady)
	session.handleCancel(context.Background())

	if session.State() != protocol.StateCancelled {
		t.Fatalf("expected state cancelled, got %v", session.State())
	}
}

func TestSessionCancelFromRunning(t *testing.T) {
	session := NewSession()
	session.setState(protocol.StatePreparing)
	session.setState(protocol.StateReady)
	session.setState(protocol.StateRunning)
	session.handleCancel(context.Background())

	if session.State() != protocol.StateCancelled {
		t.Fatalf("expected state cancelled, got %v", session.State())
	}
}

func TestSessionInvalidTransition(t *testing.T) {
	session := NewSession()

	err := session.setState(protocol.StateRunning)
	if err == nil {
		t.Error("expected error for idle -> running transition")
	}
}

func TestSessionResetFromTerminal(t *testing.T) {
	session := NewSession()
	session.setState(protocol.StatePreparing)
	session.setState(protocol.StateReady)
	session.setState(protocol.StateRunning)
	session.setState(protocol.StateCollecting)
	session.setState(protocol.StateCompleted)

	session.setState(protocol.StateIdle)
	if session.State() != protocol.StateIdle {
		t.Fatalf("expected state idle after reset, got %v", session.State())
	}
}
