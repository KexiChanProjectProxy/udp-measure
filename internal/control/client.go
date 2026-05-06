package control

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
	"github.com/udp-diagnostic/udpdiag/internal/udp"
)

type ClientConn struct {
	conn       net.Conn
	sessionID  string
	serverAddr string
}

func NewClientConn(serverAddr string) (*ClientConn, error) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	return &ClientConn{
		conn:       conn,
		serverAddr: serverAddr,
	}, nil
}

func (c *ClientConn) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *ClientConn) readMsg() ([]byte, error) {
	lengthBuf := make([]byte, 4)
	_, err := c.conn.Read(lengthBuf)
	if err != nil {
		return nil, err
	}

	length := int32(lengthBuf[0])<<24 | int32(lengthBuf[1])<<16 | int32(lengthBuf[2])<<8 | int32(lengthBuf[3])
	if length <= 0 || length > 1024*1024 {
		return nil, fmt.Errorf("invalid message length: %d", length)
	}

	data := make([]byte, length)
	_, err = c.conn.Read(data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (c *ClientConn) writeMsg(msgType protocol.MessageType, msg any) error {
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

	c.conn.SetWriteDeadline(time.Now().Add(ControlWriteTimeout))
	if _, err := c.conn.Write(lengthBuf); err != nil {
		return err
	}

	_, err = c.conn.Write(data)
	return err
}

func (c *ClientConn) recvResponse() (protocol.MessageType, any, error) {
	c.conn.SetReadDeadline(time.Now().Add(ControlReadTimeout))
	data, err := c.readMsg()
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

func (c *ClientConn) getSessionID() string {
	return c.sessionID
}

func (c *ClientConn) setSessionID(id string) {
	c.sessionID = id
}

type TestResult struct {
	Direction protocol.Direction
	Sent      int
	Received  int
	Lost      int
}

type TestParams struct {
	Direction   protocol.Direction
	Family      protocol.Family
	Target4     string
	Target6     string
	Port        int
	PayloadSize int
	PacketCount int
	IntervalMs  int
}

func (c *ClientConn) RunUplink(params *TestParams) (*TestResult, error) {
	c.conn.SetReadDeadline(time.Now().Add(ControlReadTimeout))
	c.conn.SetWriteDeadline(time.Now().Add(ControlWriteTimeout))

	c.sessionID = fmt.Sprintf("client-%d", time.Now().UnixNano())

	hello := protocol.Hello{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeHello,
			SessionID: c.sessionID,
		},
		ClientVersion: protocol.ProtocolVersion,
	}
	if err := c.writeMsg(protocol.MsgTypeHello, hello); err != nil {
		return nil, fmt.Errorf("failed to send hello: %w", err)
	}

	targetAddr := params.Target4
	if params.Family == protocol.FamilyIPv6 {
		targetAddr = params.Target6
	}

	prepare := protocol.Prepare{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypePrepare,
			SessionID: c.sessionID,
		},
		Direction:   params.Direction,
		Family:      params.Family,
		UDPPort:     params.Port,
		PayloadSize: params.PayloadSize,
		PacketCount: params.PacketCount,
		IntervalMs:  params.IntervalMs,
	}
	if err := c.writeMsg(protocol.MsgTypePrepare, prepare); err != nil {
		return nil, fmt.Errorf("failed to send prepare: %w", err)
	}

	msgType, _, err := c.recvResponse()
	if err != nil {
		return nil, fmt.Errorf("failed to recv ready: %w", err)
	}
	if msgType != protocol.MsgTypeReady {
		return nil, fmt.Errorf("expected ready, got %v", msgType)
	}

	time.Sleep(10 * time.Millisecond)

	start := protocol.Start{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeStart,
			SessionID: c.sessionID,
		},
	}
	if err := c.writeMsg(protocol.MsgTypeStart, start); err != nil {
		return nil, fmt.Errorf("failed to send start: %w", err)
	}

	time.Sleep(50 * time.Millisecond)

	sender, err := udp.NewSender(udp.SenderConfig{
		SessionID:     0,
		TestID:        0,
		Direction:     protocol.DirectionUplink,
		Family:        params.Family,
		TargetAddress: targetAddr,
		UDPPort:       uint16(params.Port),
		PayloadSize:   uint16(params.PayloadSize),
		PacketCount:   params.PacketCount,
		Interval:      time.Duration(params.IntervalMs) * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sender: %w", err)
	}
	defer sender.Close()

	if _, err := sender.Send(); err != nil {
		return nil, fmt.Errorf("failed to send: %w", err)
	}

	sendComplete := protocol.SendComplete{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeSendComplete,
			SessionID: c.sessionID,
		},
	}
	if err := c.writeMsg(protocol.MsgTypeSendComplete, sendComplete); err != nil {
		return nil, fmt.Errorf("failed to send complete: %w", err)
	}

	fetchResult := protocol.FetchResult{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeFetchResult,
			SessionID: c.sessionID,
		},
	}
	if err := c.writeMsg(protocol.MsgTypeFetchResult, fetchResult); err != nil {
		return nil, fmt.Errorf("failed to send fetch_result: %w", err)
	}

	msgType, msgData, err := c.recvResponse()
	if err != nil {
		return nil, fmt.Errorf("failed to recv result: %w", err)
	}
	if msgType != protocol.MsgTypeResult {
		return nil, fmt.Errorf("expected result, got %v", msgType)
	}

	result, ok := msgData.(protocol.Result)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", msgData)
	}

	time.Sleep(50 * time.Millisecond)

	return &TestResult{
		Direction: protocol.DirectionUplink,
		Sent:      result.Sent,
		Received:  result.Received,
		Lost:      result.Sent - result.Received,
	}, nil
}

func (c *ClientConn) RunDownlink(params *TestParams) (*TestResult, error) {
	c.conn.SetReadDeadline(time.Now().Add(ControlReadTimeout))
	c.conn.SetWriteDeadline(time.Now().Add(ControlWriteTimeout))

	c.sessionID = fmt.Sprintf("client-%d", time.Now().UnixNano())

	hello := protocol.Hello{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeHello,
			SessionID: c.sessionID,
		},
		ClientVersion: protocol.ProtocolVersion,
	}
	if err := c.writeMsg(protocol.MsgTypeHello, hello); err != nil {
		return nil, fmt.Errorf("failed to send hello: %w", err)
	}

	receiver, err := udp.NewReceiver(udp.ReceiverConfig{
		SessionID:     0,
		TestID:        0,
		Direction:     protocol.DirectionDownlink,
		Family:        params.Family,
		ReceiveWindow: time.Duration(params.PacketCount*params.IntervalMs+1000) * time.Millisecond,
	}, ":0")
	if err != nil {
		return nil, fmt.Errorf("failed to create receiver: %w", err)
	}
	defer receiver.Close()

	localAddr := receiver.LocalAddr()
	udpAddr := localAddr.(*net.UDPAddr)
	clientPort := udpAddr.Port

	prepare := protocol.Prepare{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypePrepare,
			SessionID: c.sessionID,
		},
		Direction:         params.Direction,
		Family:            params.Family,
		UDPPort:           params.Port,
		PayloadSize:       params.PayloadSize,
		PacketCount:       params.PacketCount,
		IntervalMs:        params.IntervalMs,
		ClientReceivePort: clientPort,
	}
	if err := c.writeMsg(protocol.MsgTypePrepare, prepare); err != nil {
		return nil, fmt.Errorf("failed to send prepare: %w", err)
	}

	msgType, _, err := c.recvResponse()
	if err != nil {
		return nil, fmt.Errorf("failed to recv ready: %w", err)
	}
	if msgType != protocol.MsgTypeReady {
		return nil, fmt.Errorf("expected ready, got %v", msgType)
	}

	quitCh := make(chan struct{})
	go receiver.Receive(quitCh)
	defer func() { close(quitCh) }()

	start := protocol.Start{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeStart,
			SessionID: c.sessionID,
		},
	}
	if err := c.writeMsg(protocol.MsgTypeStart, start); err != nil {
		return nil, fmt.Errorf("failed to send start: %w", err)
	}

	msgType, _, err = c.recvResponse()
	if err != nil {
		return nil, fmt.Errorf("failed to recv send_complete: %w", err)
	}
	if msgType != protocol.MsgTypeSendComplete {
		return nil, fmt.Errorf("expected send_complete, got %v", msgType)
	}

	time.Sleep(50 * time.Millisecond)
	recvResult := receiver.Result()

	fetchResult := protocol.FetchResult{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeFetchResult,
			SessionID: c.sessionID,
		},
	}
	if err := c.writeMsg(protocol.MsgTypeFetchResult, fetchResult); err != nil {
		return nil, fmt.Errorf("failed to send fetch_result: %w", err)
	}

	msgType, msgData, err := c.recvResponse()
	if err != nil {
		return nil, fmt.Errorf("failed to recv result: %w", err)
	}
	if msgType != protocol.MsgTypeResult {
		return nil, fmt.Errorf("expected result, got %v", msgType)
	}

	result, ok := msgData.(protocol.Result)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", msgData)
	}

	return &TestResult{
		Direction: protocol.DirectionDownlink,
		Sent:      result.Sent,
		Received:  recvResult.Matched,
		Lost:      result.Sent - recvResult.Matched,
	}, nil
}

func (c *ClientConn) RunBoth(params *TestParams) ([]*TestResult, error) {
	upParams := *params
	upParams.Direction = protocol.DirectionUplink
	upResult, err := c.RunUplink(&upParams)
	if err != nil {
		return nil, err
	}

	c.Close()
	time.Sleep(100 * time.Millisecond)

	downParams := *params
	downParams.Direction = protocol.DirectionDownlink
	downClient, err := NewClientConn(c.serverAddr)
	if err != nil {
		return nil, err
	}
	defer downClient.Close()
	downResult, err := downClient.RunDownlink(&downParams)
	if err != nil {
		return nil, err
	}

	return []*TestResult{upResult, downResult}, nil
}
