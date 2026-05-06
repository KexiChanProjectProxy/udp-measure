# Learnings

## Port Collision in Parallel Test Execution
- When `go test ./...` runs in parallel, multiple test packages may allocate UDP ports from overlapping ranges
- `internal/control/orchestration_test.go` and `tests/integration/integration_test.go` both used `50000+` base
- This caused sporadic `expected result, got internal_error` failures in integration tests when run as part of full suite

## Test Isolation Strategy
- Static counters with package-level variables (`uplinkPortCounter`, `portCounter`) persist across the entire test run when using `go test ./...`
- Using distinct port ranges per package prevents collision without requiring ephemeral port binding

## Port Ranges Used
- `internal/udp`: 45000-45999 (dynamically allocated via `nextTestPort()`)
- `tests/integration`: 50000-64999 (via `nextPort()`)
- `internal/control/orchestration_test.go`: 65000-69999 (via `nextUplinkPort()`) - FIXED from 50000+
- `internal/control/session_test.go`: hardcoded 40000 (not involved in collision)
