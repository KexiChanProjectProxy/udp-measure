package protocol

import (
	"testing"
)

func TestValidTransitions(t *testing.T) {
	validTransitions := []struct {
		from, to SessionState
	}{
		{StateIdle, StatePreparing},
		{StatePreparing, StateReady},
		{StatePreparing, StateCancelled},
		{StatePreparing, StateFailed},
		{StateReady, StateRunning},
		{StateReady, StateCancelled},
		{StateRunning, StateCollecting},
		{StateRunning, StateCancelled},
		{StateCollecting, StateCompleted},
		{StateCollecting, StateFailed},
		{StateCompleted, StateIdle},
		{StateFailed, StateIdle},
		{StateCancelled, StateIdle},
	}

	for _, tt := range validTransitions {
		t.Run(tt.from.String()+"_"+tt.to.String(), func(t *testing.T) {
			if err := Transition(tt.from, tt.to); err != nil {
				t.Errorf("Transition(%s, %s) failed: %v", tt.from, tt.to, err)
			}
		})
	}
}

func TestInvalidTransitions(t *testing.T) {
	invalidTransitions := []struct {
		from, to SessionState
	}{
		{StateIdle, StateReady},
		{StateIdle, StateRunning},
		{StateIdle, StateCollecting},
		{StateIdle, StateCompleted},
		{StateIdle, StateFailed},
		{StateIdle, StateCancelled},
		{StatePreparing, StateRunning},
		{StatePreparing, StateCollecting},
		{StatePreparing, StateCompleted},
		{StateReady, StatePreparing},
		{StateReady, StateCompleted},
		{StateReady, StateFailed},
		{StateReady, StateIdle},
		{StateRunning, StateReady},
		{StateRunning, StateCompleted},
		{StateRunning, StateFailed},
		{StateRunning, StateIdle},
		{StateCollecting, StateRunning},
		{StateCollecting, StateReady},
		{StateCollecting, StateCancelled},
		{StateCollecting, StateIdle},
		{StateCompleted, StateRunning},
		{StateCompleted, StatePreparing},
		{StateFailed, StateRunning},
		{StateFailed, StatePreparing},
		{StateCancelled, StateRunning},
		{StateCancelled, StatePreparing},
	}

	for _, tt := range invalidTransitions {
		t.Run(tt.from.String()+"_"+tt.to.String(), func(t *testing.T) {
			if err := Transition(tt.from, tt.to); err == nil {
				t.Errorf("Transition(%s, %s) should have failed", tt.from, tt.to)
			}
		})
	}
}

func TestProtocolStateMachineRejectsInvalidTransition(t *testing.T) {
	testCases := []struct {
		name string
		from SessionState
		to   SessionState
	}{
		{"idle_to_running", StateIdle, StateRunning},
		{"idle_to_collecting", StateIdle, StateCollecting},
		{"ready_to_completed", StateReady, StateCompleted},
		{"running_to_idle", StateRunning, StateIdle},
		{"collecting_to_ready", StateCollecting, StateReady},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Transition(tc.from, tc.to)
			if err == nil {
				t.Errorf("Transition(%s, %s) should have returned error", tc.from, tc.to)
			}
			_, ok := err.(invalidTransition)
			if !ok {
				t.Errorf("error should be invalidTransition, got %T", err)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []SessionState{StateCompleted, StateFailed, StateCancelled}
	nonTerminal := []SessionState{StateIdle, StatePreparing, StateReady, StateRunning, StateCollecting}

	for _, s := range terminal {
		if !IsTerminal(s) {
			t.Errorf("IsTerminal(%s) = false, want true", s)
		}
	}
	for _, s := range nonTerminal {
		if IsTerminal(s) {
			t.Errorf("IsTerminal(%s) = true, want false", s)
		}
	}
}

func TestIsActive(t *testing.T) {
	active := []SessionState{StatePreparing, StateReady, StateRunning, StateCollecting}
	nonActive := []SessionState{StateIdle, StateCompleted, StateFailed, StateCancelled}

	for _, s := range active {
		if !IsActive(s) {
			t.Errorf("IsActive(%s) = false, want true", s)
		}
	}
	for _, s := range nonActive {
		if IsActive(s) {
			t.Errorf("IsActive(%s) = true, want false", s)
		}
	}
}

func TestResetAllowed(t *testing.T) {
	allowed := []SessionState{StateCompleted, StateFailed, StateCancelled}
	notAllowed := []SessionState{StateIdle, StatePreparing, StateReady, StateRunning, StateCollecting}

	for _, s := range allowed {
		if !ResetAllowed(s) {
			t.Errorf("ResetAllowed(%s) = false, want true", s)
		}
	}
	for _, s := range notAllowed {
		if ResetAllowed(s) {
			t.Errorf("ResetAllowed(%s) = true, want false", s)
		}
	}
}

func TestFullHappyPath(t *testing.T) {
	session := StateIdle

	transitions := []SessionState{
		StatePreparing,
		StateReady,
		StateRunning,
		StateCollecting,
		StateCompleted,
	}

	for _, target := range transitions {
		if err := Transition(session, target); err != nil {
			t.Fatalf("Transition(%s, %s) failed during happy path: %v", session, target, err)
		}
		session = target
	}

	if !IsTerminal(session) {
		t.Errorf("final state should be terminal")
	}
}

func TestFullCancelledPath(t *testing.T) {
	session := StateIdle

	path := []SessionState{
		StatePreparing,
		StateReady,
		StateRunning,
		StateCancelled,
	}

	for _, target := range path {
		if err := Transition(session, target); err != nil {
			t.Fatalf("Transition(%s, %s) failed during cancelled path: %v", session, target, err)
		}
		session = target
	}

	if !IsTerminal(session) {
		t.Errorf("final state should be terminal")
	}
}
