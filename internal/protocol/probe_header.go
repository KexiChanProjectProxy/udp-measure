package protocol

import (
	"encoding/binary"
	"fmt"
	"time"
)

const (
	ProbeHeaderVersion = 1
	ProbeHeaderSize    = 48
)

// ProbeHeader is the UDP probe packet header.
// All fields are in network byte order (big-endian).
type ProbeHeader struct {
	Version      uint8
	_            uint8
	_            uint16
	SessionID    uint32
	TestID       uint32
	Direction    uint8
	Family       uint8
	UDPPort      uint16
	PayloadSize  uint16
	Sequence     uint32
	SendUnixNano int64
}

func (h *ProbeHeader) String() string {
	return fmt.Sprintf("ProbeHeader{v=%d session=%d test=%d dir=%s fam=%s port=%d size=%d seq=%d nano=%d}",
		h.Version, h.SessionID, h.TestID,
		Direction(h.Direction), Family(h.Family),
		h.UDPPort, h.PayloadSize, h.Sequence, h.SendUnixNano)
}

// MarshalBinary encodes the probe header to binary.
func (h *ProbeHeader) MarshalBinary() ([]byte, error) {
	if h == nil {
		return nil, fmt.Errorf("nil header")
	}
	buf := make([]byte, ProbeHeaderSize)

	buf[0] = h.Version
	binary.BigEndian.PutUint32(buf[4:8], h.SessionID)
	binary.BigEndian.PutUint32(buf[8:12], h.TestID)
	buf[12] = h.Direction
	buf[13] = h.Family
	binary.BigEndian.PutUint16(buf[14:16], h.UDPPort)
	binary.BigEndian.PutUint16(buf[16:18], h.PayloadSize)
	binary.BigEndian.PutUint32(buf[18:22], h.Sequence)
	binary.BigEndian.PutUint64(buf[22:30], uint64(h.SendUnixNano))

	return buf, nil
}

// UnmarshalBinary decodes the probe header from binary.
func (h *ProbeHeader) UnmarshalBinary(data []byte) error {
	if len(data) < ProbeHeaderSize {
		return fmt.Errorf("buffer too short: %d < %d", len(data), ProbeHeaderSize)
	}

	h.Version = data[0]
	h.SessionID = binary.BigEndian.Uint32(data[4:8])
	h.TestID = binary.BigEndian.Uint32(data[8:12])
	h.Direction = data[12]
	h.Family = data[13]
	h.UDPPort = binary.BigEndian.Uint16(data[14:16])
	h.PayloadSize = binary.BigEndian.Uint16(data[16:18])
	h.Sequence = binary.BigEndian.Uint32(data[18:22])
	h.SendUnixNano = int64(binary.BigEndian.Uint64(data[22:30]))

	return nil
}

// SetFromParams sets header fields from typed parameters.
func (h *ProbeHeader) SetFromParams(sessionID, testID uint32, dir Direction, fam Family, port uint16, payloadSize uint16, seq uint32) {
	h.Version = ProbeHeaderVersion
	h.SessionID = sessionID
	h.TestID = testID
	h.Direction = uint8(dir)
	h.Family = uint8(fam)
	h.UDPPort = port
	h.PayloadSize = payloadSize
	h.Sequence = seq
	h.SendUnixNano = time.Now().UnixNano()
}

// Validate checks if header values are sane.
func (h *ProbeHeader) Validate() error {
	if h.Version != ProbeHeaderVersion {
		return fmt.Errorf("version mismatch: got %d, want %d", h.Version, ProbeHeaderVersion)
	}
	if h.Direction != uint8(DirectionUplink) && h.Direction != uint8(DirectionDownlink) {
		return fmt.Errorf("invalid direction: %d", h.Direction)
	}
	if h.Family != uint8(FamilyIPv4) && h.Family != uint8(FamilyIPv6) {
		return fmt.Errorf("invalid family: %d", h.Family)
	}
	return nil
}
