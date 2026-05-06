# Decisions

## Port Range Separation
- **Decision**: Use distinct non-overlapping port ranges for each test package
- **Rationale**: Static counters persist across `go test ./...` parallel execution; ephemeral port binding alone doesn't guarantee isolation when counters collide
- **Ranges assigned**:
  - UDP tests: 45000-45999
  - Integration tests: 50000-64999
  - Control orchestration tests: 65000-69999
