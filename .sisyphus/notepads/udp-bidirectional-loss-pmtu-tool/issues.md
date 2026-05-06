# Issues Encountered

## Cross-package UDP Port Collision (F2 Blocker)
- **Symptom**: `go test ./... -count=1` failed with `expected result, got internal_error` in `TestIntegrationIPv4Loopback`
- **Root Cause**: Both `internal/control/orchestration_test.go` and `tests/integration/integration_test.go` allocated from `50000+` range
- **Fix**: Changed `internal/control/orchestration_test.go` to use `65000+` range (65000-69999)
- **Verification**: Full suite passes consistently

## Prior Partial Fix
- A previous timed-out fix had already resolved `internal/udp` port collisions by introducing `nextTestPort()` with range 45000-45999
- That fix was correct but insufficient since it didn't address the control/integration collision
