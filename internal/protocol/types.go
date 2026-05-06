// Package protocol defines control messages, session state machine, and UDP probe header.
package protocol

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the version of the control protocol.
const ProtocolVersion = 1

// Direction represents the direction of UDP probe traffic.
type Direction int

const (
	DirectionUplink Direction = iota
	DirectionDownlink
	DirectionBoth
)

func (d Direction) String() string {
	switch d {
	case DirectionUplink:
		return "uplink"
	case DirectionDownlink:
		return "downlink"
	case DirectionBoth:
		return "both"
	default:
		return "unknown"
	}
}

// ParseDirection parses a direction string.
func ParseDirection(s string) (Direction, error) {
	switch s {
	case "uplink", "up":
		return DirectionUplink, nil
	case "downlink", "down":
		return DirectionDownlink, nil
	case "both":
		return DirectionBoth, nil
	default:
		return -1, fmt.Errorf("invalid direction: %q", s)
	}
}

// Family represents the IP address family.
type Family int

const (
	FamilyIPv4 Family = iota
	FamilyIPv6
)

func (f Family) String() string {
	switch f {
	case FamilyIPv4:
		return "ipv4"
	case FamilyIPv6:
		return "ipv6"
	default:
		return "unknown"
	}
}

// ParseFamily parses a family string.
func ParseFamily(s string) (Family, error) {
	switch s {
	case "ipv4", "4":
		return FamilyIPv4, nil
	case "ipv6", "6":
		return FamilyIPv6, nil
	default:
		return -1, fmt.Errorf("invalid family: %q", s)
	}
}

// ErrorCode represents protocol-level error codes.
type ErrorCode int

const (
	ErrNone ErrorCode = iota
	ErrBusy
	ErrInvalidRequest
	ErrInternalError
	ErrTimeout
	ErrCancelled
	ErrSessionNotFound
)

func (e ErrorCode) String() string {
	switch e {
	case ErrNone:
		return "none"
	case ErrBusy:
		return "busy"
	case ErrInvalidRequest:
		return "invalid_request"
	case ErrInternalError:
		return "internal_error"
	case ErrTimeout:
		return "timeout"
	case ErrCancelled:
		return "cancelled"
	case ErrSessionNotFound:
		return "session_not_found"
	default:
		return "unknown"
	}
}

// MessageType represents the type of a control message.
type MessageType int

const (
	MsgTypeHello MessageType = iota
	MsgTypePrepare
	MsgTypeStart
	MsgTypeSendComplete
	MsgTypeFetchResult
	MsgTypeCancel

	MsgTypeReady
	MsgTypeResult
	MsgTypeBusy
	MsgTypeInvalidRequest
	MsgTypeInternalError
)

func (m MessageType) String() string {
	switch m {
	case MsgTypeHello:
		return "hello"
	case MsgTypePrepare:
		return "prepare"
	case MsgTypeReady:
		return "ready"
	case MsgTypeStart:
		return "start"
	case MsgTypeSendComplete:
		return "send_complete"
	case MsgTypeFetchResult:
		return "fetch_result"
	case MsgTypeResult:
		return "result"
	case MsgTypeCancel:
		return "cancel"
	case MsgTypeBusy:
		return "busy"
	case MsgTypeInvalidRequest:
		return "invalid_request"
	case MsgTypeInternalError:
		return "internal_error"
	default:
		return "unknown"
	}
}

// SessionState represents the state of a test session.
type SessionState int

const (
	StateIdle SessionState = iota
	StatePreparing
	StateReady
	StateRunning
	StateCollecting
	StateCompleted
	StateFailed
	StateCancelled
)

func (s SessionState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StatePreparing:
		return "preparing"
	case StateReady:
		return "ready"
	case StateRunning:
		return "running"
	case StateCollecting:
		return "collecting"
	case StateCompleted:
		return "completed"
	case StateFailed:
		return "failed"
	case StateCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// BaseMessage is embedded in all control messages.
type BaseMessage struct {
	Version   int         `json:"version"`
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	TestID    string      `json:"test_id,omitempty"`
	Sequence  int         `json:"seq"`
	Timestamp int64       `json:"send_unix_nano,omitempty"`
}

// --- Client -> Server Messages ---

// Hello is the initial message sent by client to server.
type Hello struct {
	BaseMessage   `json:",inline"`
	ClientVersion int      `json:"client_version"`
	Capability    []string `json:"capability,omitempty"`
}

// Prepare requests preparation for a test.
type Prepare struct {
	BaseMessage `json:",inline"`
	Direction   Direction `json:"direction"`
	Family      Family    `json:"family"`
	UDPPort     int       `json:"udp_port"`
	PayloadSize int       `json:"payload_size"`
	PacketCount int       `json:"packet_count"`
	IntervalMs  int       `json:"interval_ms"`
	// For downlink: client provides the port to receive on
	ClientReceivePort int `json:"client_receive_port,omitempty"`
}

// Start signals the server to begin UDP sending (downlink only).
type Start struct {
	BaseMessage `json:",inline"`
}

// SendComplete indicates client has finished sending UDP probes (uplink).
type SendComplete struct {
	BaseMessage `json:",inline"`
}

// FetchResult requests the test results from server.
type FetchResult struct {
	BaseMessage `json:",inline"`
}

// Cancel requests cancellation of the current session.
type Cancel struct {
	BaseMessage `json:",inline"`
	Reason      string `json:"reason,omitempty"`
}

// --- Server -> Client Messages ---

// Ready indicates server is prepared and ready to start.
type Ready struct {
	BaseMessage `json:",inline"`
	ServerPort  int `json:"server_port,omitempty"`
}

// Result contains the test results.
type Result struct {
	BaseMessage  `json:",inline"`
	Sent         int     `json:"sent"`
	Received     int     `json:"received"`
	LossRate     float64 `json:"loss_rate"`
	CriticalSize int     `json:"critical_size,omitempty"`
}

// Busy indicates server cannot accept a new session.
type Busy struct {
	BaseMessage    `json:",inline"`
	CurrentSession string `json:"current_session,omitempty"`
}

// InvalidRequest indicates malformed request.
type InvalidRequest struct {
	BaseMessage `json:",inline"`
	Code        ErrorCode `json:"code"`
	Message     string    `json:"message"`
}

// InternalError indicates server-side error.
type InternalError struct {
	BaseMessage `json:",inline"`
	Code        ErrorCode `json:"code"`
	Message     string    `json:"message"`
}

// --- Message Envelope ---

// Envelope wraps any message with type information for encoding.
type Envelope struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// --- Marshal/Unmarshal Helpers ---

// Encode encodes a message to JSON bytes.
func Encode(msg any) (json.RawMessage, error) {
	return json.Marshal(msg)
}

// Decode decodes JSON bytes to a message based on type.
func Decode(data json.RawMessage, msgType MessageType) (any, error) {
	switch msgType {
	case MsgTypeHello:
		var m Hello
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypePrepare:
		var m Prepare
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeReady:
		var m Ready
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeStart:
		var m Start
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeSendComplete:
		var m SendComplete
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeFetchResult:
		var m FetchResult
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeResult:
		var m Result
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeCancel:
		var m Cancel
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeBusy:
		var m Busy
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeInvalidRequest:
		var m InvalidRequest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	case MsgTypeInternalError:
		var m InternalError
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unknown message type: %v", msgType)
	}
}

// EncodeMessage encodes a typed message into an Envelope.
func EncodeMessage(msg any, msgType MessageType) ([]byte, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	env := Envelope{
		Type:    msgType,
		Payload: payload,
	}
	return json.Marshal(env)
}

// DecodeEnvelope decodes an Envelope into the typed message.
func DecodeEnvelope(data []byte) (MessageType, any, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, nil, err
	}
	msg, err := Decode(env.Payload, env.Type)
	if err != nil {
		return 0, nil, err
	}
	return env.Type, msg, nil
}

// ValidateDirection checks if direction is valid.
func ValidateDirection(d Direction) bool {
	return d >= 0 && d <= DirectionBoth
}

// ValidateFamily checks if family is valid.
func ValidateFamily(f Family) bool {
	return f >= 0 && f <= FamilyIPv6
}

// ValidateErrorCode checks if error code is valid.
func ValidateErrorCode(e ErrorCode) bool {
	return e >= 0 && e <= ErrSessionNotFound
}
