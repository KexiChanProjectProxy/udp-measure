// Package control implements server control plane and session lifecycle.
package control

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/protocol"
)

// Timeout values for control plane operations.
const (
	// ControlReadTimeout is the timeout for reading a complete message from client.
	ControlReadTimeout = 30 * time.Second
	// ControlWriteTimeout is the timeout for writing a response to client.
	ControlWriteTimeout = 10 * time.Second
	// PrepareTimeout is the max time allowed for Prepare phase before it times out.
	PrepareTimeout = 60 * time.Second
	// RunTimeout is the max time for the Running state before it times out.
	RunTimeout = 5 * time.Minute
	// CollectTimeout is the max time for Collecting state before it times out.
	CollectTimeout = 60 * time.Second
)

// Server represents a TCP control server that manages diagnostic sessions.
type Server struct {
	listenAddr string
	listener   net.Listener
	session    atomic.Pointer[Session] // currently active session, nil if idle
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex // protects listener close
	closed     bool
}

// NewServer creates a new control server.
func NewServer(listenAddr string) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		listenAddr: listenAddr,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// ListenNetwork returns the network type for listening based on the address.
// Returns "tcp4" for IPv4 addresses, "tcp6" for IPv6, or "tcp" for ambiguous addresses.
func ListenNetwork(addr string) string {
	// If the address is explicitly IPv4 (contains a dot but not a colon), use tcp4
	// If it's explicitly IPv6 (contains colons), use tcp6
	// Otherwise let the system decide
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Could be just a port, try to detect family
		if host == "" {
			return "tcp"
		}
		return "tcp"
	}

	// Check if it's an IP address
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return "tcp4"
		}
		if ip.To16() != nil {
			return "tcp6"
		}
	}

	// If host contains a colon, it's likely IPv6
	if host != "" {
		for _, c := range host {
			if c == ':' {
				return "tcp6"
			}
		}
	}

	// Check for explicit IPv4 dotted decimal
	hasDots := false
	for _, c := range host {
		if c == '.' {
			hasDots = true
			break
		}
	}
	if hasDots {
		return "tcp4"
	}

	return "tcp"
}

// Start begins listening and serving connections.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("server is closed")
	}
	if s.listener == nil {
		network := ListenNetwork(s.listenAddr)
		ln, err := net.Listen(network, s.listenAddr)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("failed to listen on %s: %w", s.listenAddr, err)
		}
		s.listener = ln
	}
	s.mu.Unlock()

	s.wg.Add(1)
	go s.serve()

	return nil
}

// serve accepts incoming TCP connections and handles them.
func (s *Server) serve() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				// Server is shutting down, ignore the error
				return
			default:
				// Log and continue on non-shutdown errors
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// handleConn handles a single TCP client connection.
func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()

	conn.SetReadDeadline(time.Now().Add(ControlReadTimeout))
	conn.SetWriteDeadline(time.Now().Add(ControlWriteTimeout))

	session, acquired, err := s.tryAcquireSession(conn)
	if err != nil {
		s.sendErrorResponse(conn, protocol.ErrInternalError, err.Error())
		conn.Close()
		return
	}

	if !acquired {
		s.sendBusyResponse(conn, session)
		conn.Close()
		return
	}

	session.Run(s.ctx, conn)
	s.releaseSession(session)
	conn.Close()
}

// tryAcquireSession attempts to create a new session.
// Returns (session, acquired, error).
// If acquired is false, session is the current busy session.
func (s *Server) tryAcquireSession(conn net.Conn) (*Session, bool, error) {
	// Create a new session
	session := NewSession()

	// Try to store this session as the active one
	nilPtr := (*Session)(nil)
	if !s.session.CompareAndSwap(nilPtr, session) {
		// Another session is already active
		current := s.session.Load()
		return current, false, nil
	}

	return session, true, nil
}

// sendBusyResponse sends a BUSY response to the client and closes the connection.
func (s *Server) sendBusyResponse(conn net.Conn, currentSession *Session) {
	currentSID := ""
	if currentSession != nil {
		currentSID = currentSession.ID()
	}

	msg := protocol.Busy{
		BaseMessage: protocol.BaseMessage{
			Version:   protocol.ProtocolVersion,
			Type:      protocol.MsgTypeBusy,
			SessionID: currentSID,
		},
		CurrentSession: currentSID,
	}

	data, err := protocol.EncodeMessage(msg, protocol.MsgTypeBusy)
	if err != nil {
		return
	}

	length := int32(len(data))
	lengthBuf := []byte{
		byte(length >> 24),
		byte(length >> 16),
		byte(length >> 8),
		byte(length),
	}

	conn.SetWriteDeadline(time.Now().Add(ControlWriteTimeout))
	conn.Write(lengthBuf)
	conn.Write(data)
}

// sendErrorResponse sends an InternalError response to the client.
func (s *Server) sendErrorResponse(conn net.Conn, code protocol.ErrorCode, msg string) {
	resp := protocol.InternalError{
		BaseMessage: protocol.BaseMessage{
			Version: protocol.ProtocolVersion,
			Type:    protocol.MsgTypeInternalError,
		},
		Code:    code,
		Message: msg,
	}

	data, err := protocol.EncodeMessage(resp, protocol.MsgTypeInternalError)
	if err != nil {
		return
	}

	length := int32(len(data))
	lengthBuf := []byte{
		byte(length >> 24),
		byte(length >> 16),
		byte(length >> 8),
		byte(length),
	}

	conn.SetWriteDeadline(time.Now().Add(ControlWriteTimeout))
	conn.Write(lengthBuf)
	conn.Write(data)
}

// releaseSession releases the session slot and cleans up resources.
func (s *Server) releaseSession(session *Session) {
	nilPtr := (*Session)(nil)
	s.session.CompareAndSwap(session, nilPtr)
}

// Close shuts down the server.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	listener := s.listener
	s.mu.Unlock()

	s.cancel()

	if listener != nil {
		listener.Close()
	}

	s.wg.Wait()
	return nil
}

// Session returns the currently active session, if any.
func (s *Server) Session() *Session {
	return s.session.Load()
}

// IsActive reports whether there is an active session.
func (s *Server) IsActive() bool {
	return s.session.Load() != nil
}
