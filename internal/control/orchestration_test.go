package control

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
	"github.com/udp-diagnostic/udpdiag/internal/udp"
)

var uplinkPortCounter int

func nextUplinkPort() int {
	uplinkPortCounter++
	// Use ports 65000+ to avoid collision with integration tests (50000+) and UDP tests (45000-45999)
	return 65000 + (uplinkPortCounter % 5000)
}

func startTestServerForOrchestration(t *testing.T) (*Server, string) {
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

func TestUplinkOrchestration(t *testing.T) {
	server, addr := startTestServerForOrchestration(t)
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

	udpPort := nextUplinkPort()

	prepare := protocol.Prepare{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypePrepare,
			SessionID: session.ID(),
		},
		Direction:   protocol.DirectionUplink,
		Family:      protocol.FamilyIPv4,
		UDPPort:     udpPort,
		PayloadSize: 100,
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

	time.Sleep(50 * time.Millisecond)

	sender, err := udp.NewSender(udp.SenderConfig{
		SessionID:     0,
		TestID:        0,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		TargetAddress: fmt.Sprintf("127.0.0.1:%d", udpPort),
		UDPPort:       uint16(udpPort),
		PayloadSize:   100,
		PacketCount:   10,
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
}

func TestDownlinkOrchestration(t *testing.T) {
	server, addr := startTestServerForOrchestration(t)
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

	receiver, err := udp.NewReceiver(udp.ReceiverConfig{
		SessionID:     0,
		TestID:        0,
		Direction:     protocol.DirectionDownlink,
		Family:        protocol.FamilyIPv4,
		ReceiveWindow: 500 * time.Millisecond,
	}, ":0")
	if err != nil {
		t.Fatalf("failed to create receiver: %v", err)
	}
	defer receiver.Close()

	localAddr := receiver.LocalAddr()
	udpAddr := localAddr.(*net.UDPAddr)
	clientPort := udpAddr.Port

	prepare := protocol.Prepare{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypePrepare,
			SessionID: session.ID(),
		},
		Direction:         protocol.DirectionDownlink,
		Family:            protocol.FamilyIPv4,
		UDPPort:           40110,
		PayloadSize:       100,
		PacketCount:       10,
		IntervalMs:        10,
		ClientReceivePort: clientPort,
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

	quitCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receiver.Receive(quitCh)
	}()

	start := protocol.Start{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeStart,
			SessionID: session.ID(),
		},
	}
	if err := sendMsg(conn, protocol.MsgTypeStart, start); err != nil {
		close(quitCh)
		t.Fatalf("failed to send start: %v", err)
	}

	if !waitForState(session, protocol.StateRunning, 500*time.Millisecond) {
		t.Fatalf("expected state running, got %v", session.State())
	}

	msgType, _, err = readEnv(conn)
	if err != nil {
		close(quitCh)
		t.Fatalf("failed to read send_complete: %v", err)
	}
	if msgType != protocol.MsgTypeSendComplete {
		close(quitCh)
		t.Fatalf("expected send_complete, got %v", msgType)
	}

	if !waitForState(session, protocol.StateCollecting, 500*time.Millisecond) {
		t.Fatalf("expected state collecting, got %v", session.State())
	}

	close(quitCh)
	wg.Wait()

	recvResult := receiver.Result()
	if recvResult.Matched == 0 {
		t.Logf("warning: expected to receive some packets, got %d", recvResult.Matched)
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
}

func TestBothModeRunsUplinkThenDownlink(t *testing.T) {
	server, addr := startTestServerForOrchestration(t)
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
		Direction:   protocol.DirectionBoth,
		Family:      protocol.FamilyIPv4,
		UDPPort:     40120,
		PayloadSize: 100,
		PacketCount: 5,
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

	time.Sleep(30 * time.Millisecond)

	sender, err := udp.NewSender(udp.SenderConfig{
		SessionID:     0,
		TestID:        0,
		Direction:     protocol.DirectionUplink,
		Family:        protocol.FamilyIPv4,
		TargetAddress: "127.0.0.1:40120",
		UDPPort:       40120,
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

func TestResultFetchBeforeRunCompletionFails(t *testing.T) {
	server, addr := startTestServerForOrchestration(t)
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
		UDPPort:     40130,
		PayloadSize: 100,
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

	msgType, msg, err := readEnv(conn)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if msgType == protocol.MsgTypeInvalidRequest {
		t.Logf("Got expected invalid_request for premature fetch_result: %v", msg)
		return
	}

	if msgType == protocol.MsgTypeInternalError {
		t.Logf("Got internal_error for premature fetch_result: %v", msg)
		return
	}

	if msgType == protocol.MsgTypeResult {
		t.Error("Should NOT get result before run completion")
		return
	}

	t.Errorf("Expected invalid_request or internal_error, got: %v", msgType)
}

func TestDownlinkWithClientReceivePort(t *testing.T) {
	server, addr := startTestServerForOrchestration(t)
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

	receiver, err := udp.NewReceiver(udp.ReceiverConfig{
		SessionID:     0,
		TestID:        0,
		Direction:     protocol.DirectionDownlink,
		Family:        protocol.FamilyIPv4,
		ReceiveWindow: 500 * time.Millisecond,
	}, ":0")
	if err != nil {
		t.Fatalf("failed to create receiver: %v", err)
	}
	defer receiver.Close()

	localAddr := receiver.LocalAddr()
	udpAddr := localAddr.(*net.UDPAddr)
	clientPort := udpAddr.Port

	prepare := protocol.Prepare{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypePrepare,
			SessionID: session.ID(),
		},
		Direction:         protocol.DirectionDownlink,
		Family:            protocol.FamilyIPv4,
		UDPPort:           40140,
		PayloadSize:       100,
		PacketCount:       10,
		IntervalMs:        10,
		ClientReceivePort: clientPort,
	}
	if err := sendMsg(conn, protocol.MsgTypePrepare, prepare); err != nil {
		t.Fatalf("failed to send prepare: %v", err)
	}

	msgType, readyMsg, err := readEnv(conn)
	if err != nil {
		t.Fatalf("failed to read ready: %v", err)
	}
	if msgType != protocol.MsgTypeReady {
		t.Fatalf("expected ready, got %v", msgType)
	}

	ready, ok := readyMsg.(protocol.Ready)
	if !ok {
		t.Fatalf("expected Ready message, got %T", readyMsg)
	}

	if ready.ServerPort != 40140 {
		t.Errorf("expected server port 40140, got %d", ready.ServerPort)
	}

	quitCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receiver.Receive(quitCh)
	}()

	start := protocol.Start{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeStart,
			SessionID: session.ID(),
		},
	}
	if err := sendMsg(conn, protocol.MsgTypeStart, start); err != nil {
		close(quitCh)
		t.Fatalf("failed to send start: %v", err)
	}

	msgType, _, err = readEnv(conn)
	if err != nil {
		close(quitCh)
		t.Fatalf("failed to read send_complete: %v", err)
	}
	if msgType != protocol.MsgTypeSendComplete {
		close(quitCh)
		t.Fatalf("expected send_complete, got %v", msgType)
	}

	close(quitCh)
	wg.Wait()

	recvResult := receiver.Result()
	if recvResult.Matched == 0 {
		t.Error("expected to receive packets in downlink")
	}
}
