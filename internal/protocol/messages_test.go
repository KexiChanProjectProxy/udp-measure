package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDirectionString(t *testing.T) {
	tests := []struct {
		d    Direction
		want string
	}{
		{DirectionUplink, "uplink"},
		{DirectionDownlink, "downlink"},
		{DirectionBoth, "both"},
		{Direction(-1), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.d.String(); got != tt.want {
			t.Errorf("Direction(%d).String() = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestParseDirection(t *testing.T) {
	tests := []struct {
		input   string
		wantDir Direction
		wantErr bool
	}{
		{"uplink", DirectionUplink, false},
		{"up", DirectionUplink, false},
		{"downlink", DirectionDownlink, false},
		{"down", DirectionDownlink, false},
		{"both", DirectionBoth, false},
		{"invalid", -1, true},
		{"", -1, true},
	}
	for _, tt := range tests {
		got, err := ParseDirection(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseDirection(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.wantDir {
			t.Errorf("ParseDirection(%q) = %v, want %v", tt.input, got, tt.wantDir)
		}
	}
}

func TestFamilyString(t *testing.T) {
	tests := []struct {
		f    Family
		want string
	}{
		{FamilyIPv4, "ipv4"},
		{FamilyIPv6, "ipv6"},
		{Family(-1), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.f.String(); got != tt.want {
			t.Errorf("Family(%d).String() = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestParseFamily(t *testing.T) {
	tests := []struct {
		input   string
		want    Family
		wantErr bool
	}{
		{"ipv4", FamilyIPv4, false},
		{"4", FamilyIPv4, false},
		{"ipv6", FamilyIPv6, false},
		{"6", FamilyIPv6, false},
		{"invalid", -1, true},
		{"", -1, true},
	}
	for _, tt := range tests {
		got, err := ParseFamily(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseFamily(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseFamily(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestErrorCodeString(t *testing.T) {
	tests := []struct {
		e    ErrorCode
		want string
	}{
		{ErrNone, "none"},
		{ErrBusy, "busy"},
		{ErrInvalidRequest, "invalid_request"},
		{ErrInternalError, "internal_error"},
		{ErrTimeout, "timeout"},
		{ErrCancelled, "cancelled"},
		{ErrSessionNotFound, "session_not_found"},
		{ErrorCode(100), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.e.String(); got != tt.want {
			t.Errorf("ErrorCode(%d).String() = %q, want %q", tt.e, got, tt.want)
		}
	}
}

func TestMessageTypeString(t *testing.T) {
	tests := []struct {
		m    MessageType
		want string
	}{
		{MsgTypeHello, "hello"},
		{MsgTypePrepare, "prepare"},
		{MsgTypeReady, "ready"},
		{MsgTypeStart, "start"},
		{MsgTypeSendComplete, "send_complete"},
		{MsgTypeFetchResult, "fetch_result"},
		{MsgTypeResult, "result"},
		{MsgTypeCancel, "cancel"},
		{MsgTypeBusy, "busy"},
		{MsgTypeInvalidRequest, "invalid_request"},
		{MsgTypeInternalError, "internal_error"},
		{MessageType(100), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("MessageType(%d).String() = %q, want %q", tt.m, got, tt.want)
		}
	}
}

func TestSessionStateString(t *testing.T) {
	tests := []struct {
		s    SessionState
		want string
	}{
		{StateIdle, "idle"},
		{StatePreparing, "preparing"},
		{StateReady, "ready"},
		{StateRunning, "running"},
		{StateCollecting, "collecting"},
		{StateCompleted, "completed"},
		{StateFailed, "failed"},
		{StateCancelled, "cancelled"},
		{SessionState(100), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("SessionState(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestValidateDirection(t *testing.T) {
	if !ValidateDirection(DirectionUplink) {
		t.Error("DirectionUplink should be valid")
	}
	if !ValidateDirection(DirectionDownlink) {
		t.Error("DirectionDownlink should be valid")
	}
	if !ValidateDirection(DirectionBoth) {
		t.Error("DirectionBoth should be valid")
	}
	if ValidateDirection(Direction(-1)) {
		t.Error("Invalid direction should be invalid")
	}
}

func TestValidateFamily(t *testing.T) {
	if !ValidateFamily(FamilyIPv4) {
		t.Error("FamilyIPv4 should be valid")
	}
	if !ValidateFamily(FamilyIPv6) {
		t.Error("FamilyIPv6 should be valid")
	}
	if ValidateFamily(Family(-1)) {
		t.Error("Invalid family should be invalid")
	}
}

func TestValidateErrorCode(t *testing.T) {
	if !ValidateErrorCode(ErrNone) {
		t.Error("ErrNone should be valid")
	}
	if !ValidateErrorCode(ErrBusy) {
		t.Error("ErrBusy should be valid")
	}
	if !ValidateErrorCode(ErrSessionNotFound) {
		t.Error("ErrSessionNotFound should be valid")
	}
	if ValidateErrorCode(ErrorCode(-1)) {
		t.Error("Invalid error code should be invalid")
	}
	if ValidateErrorCode(ErrorCode(100)) {
		t.Error("Out of range error code should be invalid")
	}
}

func TestProtocolMessageRoundTrip(t *testing.T) {
	messages := []struct {
		name    string
		msgType MessageType
		msg     any
	}{
		{
			name:    "hello",
			msgType: MsgTypeHello,
			msg: Hello{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeHello,
					SessionID: "sess-123",
					Sequence:  1,
				},
				ClientVersion: 1,
				Capability:    []string{"ipv4", "ipv6", "uplink", "downlink"},
			},
		},
		{
			name:    "prepare",
			msgType: MsgTypePrepare,
			msg: Prepare{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypePrepare,
					SessionID: "sess-123",
					TestID:    "test-1",
					Sequence:  2,
				},
				Direction:         DirectionUplink,
				Family:            FamilyIPv4,
				UDPPort:           40000,
				PayloadSize:       1200,
				PacketCount:       100,
				IntervalMs:        10,
				ClientReceivePort: 0,
			},
		},
		{
			name:    "ready",
			msgType: MsgTypeReady,
			msg: Ready{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeReady,
					SessionID: "sess-123",
					TestID:    "test-1",
					Sequence:  3,
				},
				ServerPort: 40000,
			},
		},
		{
			name:    "start",
			msgType: MsgTypeStart,
			msg: Start{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeStart,
					SessionID: "sess-123",
					TestID:    "test-1",
					Sequence:  4,
				},
			},
		},
		{
			name:    "send_complete",
			msgType: MsgTypeSendComplete,
			msg: SendComplete{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeSendComplete,
					SessionID: "sess-123",
					TestID:    "test-1",
					Sequence:  5,
				},
			},
		},
		{
			name:    "fetch_result",
			msgType: MsgTypeFetchResult,
			msg: FetchResult{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeFetchResult,
					SessionID: "sess-123",
					TestID:    "test-1",
					Sequence:  6,
				},
			},
		},
		{
			name:    "result",
			msgType: MsgTypeResult,
			msg: Result{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeResult,
					SessionID: "sess-123",
					TestID:    "test-1",
					Sequence:  7,
				},
				Sent:         100,
				Received:     95,
				LossRate:     0.05,
				CriticalSize: 1400,
			},
		},
		{
			name:    "cancel",
			msgType: MsgTypeCancel,
			msg: Cancel{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeCancel,
					SessionID: "sess-123",
					TestID:    "test-1",
					Sequence:  8,
				},
				Reason: "user requested",
			},
		},
		{
			name:    "busy",
			msgType: MsgTypeBusy,
			msg: Busy{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeBusy,
					SessionID: "",
					Sequence:  1,
				},
				CurrentSession: "sess-other",
			},
		},
		{
			name:    "invalid_request",
			msgType: MsgTypeInvalidRequest,
			msg: InvalidRequest{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeInvalidRequest,
					SessionID: "sess-123",
					Sequence:  1,
				},
				Code:    ErrInvalidRequest,
				Message: "malformed packet count",
			},
		},
		{
			name:    "internal_error",
			msgType: MsgTypeInternalError,
			msg: InternalError{
				BaseMessage: BaseMessage{
					Version:   ProtocolVersion,
					Type:      MsgTypeInternalError,
					SessionID: "sess-123",
					Sequence:  1,
				},
				Code:    ErrInternalError,
				Message: "UDP socket creation failed",
			},
		},
	}

	for _, tt := range messages {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeMessage(tt.msg, tt.msgType)
			if err != nil {
				t.Fatalf("EncodeMessage failed: %v", err)
			}

			gotType, gotMsg, err := DecodeEnvelope(encoded)
			if err != nil {
				t.Fatalf("DecodeEnvelope failed: %v", err)
			}

			if gotType != tt.msgType {
				t.Errorf("message type = %v, want %v", gotType, tt.msgType)
			}

			gotJSON, err := json.Marshal(gotMsg)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			wantJSON, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("message mismatch:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestProbeHeaderRoundTrip(t *testing.T) {
	original := &ProbeHeader{
		Version:      ProbeHeaderVersion,
		SessionID:    0xDEADBEEF,
		TestID:       0xCAFEBABE,
		Direction:    uint8(DirectionUplink),
		Family:       uint8(FamilyIPv4),
		UDPPort:      40000,
		PayloadSize:  1200,
		Sequence:     42,
		SendUnixNano: 1234567890123456789,
	}

	data, err := original.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	if len(data) != ProbeHeaderSize {
		t.Errorf("MarshalBinary returned %d bytes, want %d", len(data), ProbeHeaderSize)
	}

	var restored ProbeHeader
	if err := restored.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	if restored.Version != original.Version {
		t.Errorf("Version = %d, want %d", restored.Version, original.Version)
	}
	if restored.SessionID != original.SessionID {
		t.Errorf("SessionID = 0x%08X, want 0x%08X", restored.SessionID, original.SessionID)
	}
	if restored.TestID != original.TestID {
		t.Errorf("TestID = 0x%08X, want 0x%08X", restored.TestID, original.TestID)
	}
	if restored.Direction != original.Direction {
		t.Errorf("Direction = %d, want %d", restored.Direction, original.Direction)
	}
	if restored.Family != original.Family {
		t.Errorf("Family = %d, want %d", restored.Family, original.Family)
	}
	if restored.UDPPort != original.UDPPort {
		t.Errorf("UDPPort = %d, want %d", restored.UDPPort, original.UDPPort)
	}
	if restored.PayloadSize != original.PayloadSize {
		t.Errorf("PayloadSize = %d, want %d", restored.PayloadSize, original.PayloadSize)
	}
	if restored.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", restored.Sequence, original.Sequence)
	}
	if restored.SendUnixNano != original.SendUnixNano {
		t.Errorf("SendUnixNano = %d, want %d", restored.SendUnixNano, original.SendUnixNano)
	}
}

func TestProbeHeaderValidate(t *testing.T) {
	valid := &ProbeHeader{
		Version:   ProbeHeaderVersion,
		Direction: uint8(DirectionUplink),
		Family:    uint8(FamilyIPv4),
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid header failed validation: %v", err)
	}

	badVersion := &ProbeHeader{
		Version:   99,
		Direction: uint8(DirectionUplink),
		Family:    uint8(FamilyIPv4),
	}
	if err := badVersion.Validate(); err == nil {
		t.Error("bad version should fail validation")
	}

	badDir := &ProbeHeader{
		Version:   ProbeHeaderVersion,
		Direction: 99,
		Family:    uint8(FamilyIPv4),
	}
	if err := badDir.Validate(); err == nil {
		t.Error("bad direction should fail validation")
	}

	badFam := &ProbeHeader{
		Version:   ProbeHeaderVersion,
		Direction: uint8(DirectionUplink),
		Family:    99,
	}
	if err := badFam.Validate(); err == nil {
		t.Error("bad family should fail validation")
	}
}

func TestProbeHeaderSetFromParams(t *testing.T) {
	var h ProbeHeader
	h.SetFromParams(100, 200, DirectionDownlink, FamilyIPv6, 50000, 1400, 5)

	if h.Version != ProbeHeaderVersion {
		t.Errorf("Version = %d, want %d", h.Version, ProbeHeaderVersion)
	}
	if h.SessionID != 100 {
		t.Errorf("SessionID = %d, want %d", h.SessionID, 100)
	}
	if h.TestID != 200 {
		t.Errorf("TestID = %d, want %d", h.TestID, 200)
	}
	if h.Direction != uint8(DirectionDownlink) {
		t.Errorf("Direction = %d, want %d", h.Direction, uint8(DirectionDownlink))
	}
	if h.Family != uint8(FamilyIPv6) {
		t.Errorf("Family = %d, want %d", h.Family, uint8(FamilyIPv6))
	}
	if h.UDPPort != 50000 {
		t.Errorf("UDPPort = %d, want %d", h.UDPPort, 50000)
	}
	if h.PayloadSize != 1400 {
		t.Errorf("PayloadSize = %d, want %d", h.PayloadSize, 1400)
	}
	if h.Sequence != 5 {
		t.Errorf("Sequence = %d, want %d", h.Sequence, 5)
	}
	if h.SendUnixNano == 0 {
		t.Error("SendUnixNano should be non-zero")
	}
}

func TestProbeHeaderUnmarshalTruncated(t *testing.T) {
	var h ProbeHeader
	err := h.UnmarshalBinary([]byte{1, 2, 3})
	if err == nil {
		t.Error("truncated data should fail")
	}
}
