package protocol

import "fmt"

type invalidTransition struct {
	from, to SessionState
}

func (e invalidTransition) Error() string {
	return fmt.Sprintf("invalid transition: %s -> %s", e.from, e.to)
}

var (
	transitions = map[SessionState]map[SessionState]bool{
		StateIdle: {
			StatePreparing: true,
		},
		StatePreparing: {
			StateReady:     true,
			StateCancelled: true,
			StateFailed:    true,
		},
		StateReady: {
			StateRunning:   true,
			StateCancelled: true,
		},
		StateRunning: {
			StateCollecting: true,
			StateCancelled:  true,
		},
		StateCollecting: {
			StateCompleted: true,
			StateFailed:    true,
		},
		StateCompleted: {
			StateIdle: true,
		},
		StateFailed: {
			StateIdle: true,
		},
		StateCancelled: {
			StateIdle: true,
		},
	}
)

func canTransition(from, to SessionState) bool {
	if m, ok := transitions[from]; ok {
		return m[to]
	}
	return false
}

func Transition(from, to SessionState) error {
	if !canTransition(from, to) {
		return invalidTransition{from: from, to: to}
	}
	return nil
}

func IsTerminal(s SessionState) bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}

func IsActive(s SessionState) bool {
	return s != StateIdle && !IsTerminal(s)
}

func ResetAllowed(s SessionState) bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}
