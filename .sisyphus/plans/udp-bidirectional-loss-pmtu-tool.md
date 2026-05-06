# UDP 双向丢包与 Path MTU 探测工具

## TL;DR
> **Summary**: 从零实现一个 Go Client/Server 命令行诊断工具，使用 TCP 控制通道协调、UDP 数据通道测量，支持 IPv4/IPv6、上行/下行/双向探测，并输出按包大小统计的丢包率与 5% 阈值临界包大小。
> **Deliverables**:
> - Go 项目骨架与双 CLI（server/client）
> - TCP 控制协议与单会话状态机
> - UDP 上下行探测执行引擎
> - 聚合与控制台报告输出
> - 自动化测试与集成验证脚本/证据
> **Effort**: Large
> **Parallel**: YES - 3 waves
> **Critical Path**: 1 → 2 → 3/4 → 5 → 6 → 7 → 8

## Context
### Original Request
开发一套基于 Client/Server 架构的命令行诊断工具，用于精准探测特定网络路径下，不同大小 UDP 数据包在上行（Uplink）和下行（Downlink）方向的连通性及丢包率，支持 IPv4/IPv6、方向切换、端口范围、包大小范围、发包总数与间隔配置，并输出临界包大小。

### Interview Summary
- 仓库为空，按绿地项目规划。
- 实现语言固定为 Go。
- 临界包大小规则固定为：**按从小到大扫描时，首个丢包率 >= 5% 的包大小**。
- Server 并发范围固定为：**单 Client 串行测试**。
- 测试策略固定为：**tests-after**，但每个任务都必须带 agent 可执行 QA 场景。

### Metis Review (gaps addressed)
- 将“包大小”明确定义为 **UDP payload 字节数**，不是 IP/UDP 头部后的线速总长。
- 明确工具是“基于观察到的 UDP 丢包阈值诊断”，**不是** 对 PMTU 的协议级权威证明。
- 单 Client 模式下，Server 对第二个 Client 返回明确 `BUSY` 错误，不排队。
- 控制协议必须版本化，并定义消息 schema、超时、取消、失败码。
- 必须为大规模 sweep 增加运行时边界估算与保护，避免用户意外跑出超长任务。

## Work Objectives
### Core Objective
构建一个可在 IPv4/IPv6 网络路径上执行 UDP 上行、下行、双向探测的 Go CLI 工具，通过 TCP 控制同步测试参数和状态，输出每组条件的发送/接收统计、丢包率、以及按 5% 阈值判定的临界包大小。

### Deliverables
- `go.mod` 与最小可维护目录结构。
- `cmd/udpdiag` 单二进制 CLI，包含 `server` 与 `client` 子命令。
- `internal/protocol` 控制协议模型与编解码。
- `internal/config` 参数解析、端口范围/包大小 sweep 生成、运行时估算。
- `internal/control` Server 控制平面与单会话状态机。
- `internal/udp` UDP 收发引擎、探测包头、收包统计。
- `internal/report` 聚合器、阈值判定、控制台表格输出。
- `internal/integration` 或 `tests/integration` 集成测试 harness。
- README 级 CLI/help 文案或内嵌 `--help` 文案覆盖关键语义。

### Definition of Done (verifiable conditions with commands)
- `go build ./...` 成功。
- `go test ./...` 成功。
- `go run ./cmd/udpdiag --help` 输出 `server`、`client` 子命令与关键 flags。
- `go run ./cmd/udpdiag server --listen :18080` 可启动控制服务。
- `go run ./cmd/udpdiag client --target4 127.0.0.1 --control-port 18080 --direction both --ports 40000-40002 --min-size 1200 --max-size 1472 --step 136 --count 20 --interval 10ms` 在本机集成场景下输出结果，包含 `uplink`、`downlink`、`loss=`、`critical_size=` 关键字段。
- 当 Server 被占用时，第二个 Client 收到明确 busy 错误并以非零退出。
- 当 sweep 预估总时长超过默认上限时，Client 在未显式覆盖时拒绝执行并给出估算信息。

### Must Have
- 双栈支持：显式区分 IPv4/IPv6 目标与 socket family。
- 方向支持：`up`、`down`、`both`。
- 端口支持：单端口、逗号列表、区间。
- 包大小 sweep：最小值、最大值、步长。
- 发包策略：总数、间隔。
- 控制协议：版本号、准备就绪、开始、完成、结果请求、错误返回、取消。
- 探测包元数据：协议版本、session ID、test ID、direction、family、port、payload size、sequence number、send timestamp。
- 丢包统计：发送数、接收数、丢包率。
- 临界包大小：按 size 升序，取首个 `loss >= 5%`；若全部低于阈值，输出 `critical_size=none`。

### Must NOT Have (guardrails, AI slop patterns, scope boundaries)
- 不做 GUI、TUI、Web API、metrics endpoint。
- 不做多 Client 并发、不排队、不鉴权、不加密、不 NAT 穿透。
- 不依赖 raw socket/ICMP PMTU 机制；该工具只做 UDP 观察性诊断。
- 不做长期历史存储、数据库、后台守护进程。
- 不做自动调优网络或修改系统 MTU。
- 不把“观察到的丢包阈值”表述成“确定性 PMTU 结论”。

## Verification Strategy
> ZERO HUMAN INTERVENTION - all verification is agent-executed.
- Test decision: tests-after + Go `testing` framework。
- QA policy: 每个任务都包含 happy path + failure/edge case agent QA。
- Evidence: `.sisyphus/evidence/task-{N}-{slug}.{ext}`
- Standard evidence files:
  - `.sisyphus/evidence/build.txt`
  - `.sisyphus/evidence/test.txt`
  - `.sisyphus/evidence/server.log`
  - `.sisyphus/evidence/client-report.txt`
  - `.sisyphus/evidence/client-busy.txt`

## Execution Strategy
### Parallel Execution Waves
> Target: 5-8 tasks per wave. <3 per wave (except final) = under-splitting.
> Extract shared dependencies as Wave-1 tasks for max parallelism.

Wave 1: foundation
- Task 1: project skeleton + CLI contract
- Task 2: protocol schema + shared types/state machine

Wave 2: execution layers
- Task 3: client config parser + sweep planner
- Task 4: server control plane + single-client resource lifecycle
- Task 5: UDP probe engine + receiver stats

Wave 3: integration and reporting
- Task 6: uplink/downlink orchestration
- Task 7: aggregation/reporting + threshold detection + runtime bounds
- Task 8: automated tests + integration verification harness

### Dependency Matrix (full, all tasks)
| Task | Depends On | Needed By |
|---|---|---|
| 1 | - | 3,4,6,8 |
| 2 | 1 | 3,4,5,6,7,8 |
| 3 | 1,2 | 6,7,8 |
| 4 | 1,2 | 6,8 |
| 5 | 2 | 6,8 |
| 6 | 3,4,5 | 7,8 |
| 7 | 3,6 | 8 |
| 8 | 1,2,3,4,5,6,7 | F1-F4 |

### Agent Dispatch Summary (wave → task count → categories)
- Wave 1 → 2 tasks → quick / unspecified-low
- Wave 2 → 3 tasks → unspecified-high / deep
- Wave 3 → 3 tasks → unspecified-high / deep

## TODOs
> Implementation + Test = ONE task. Never separate.
> EVERY task MUST have: Agent Profile + Parallelization + QA Scenarios.

- [x] 1. 建立 Go 项目骨架与 CLI 契约

  **What to do**: 初始化 `go.mod`，建立 `cmd/udpdiag`、`internal/protocol`、`internal/config`、`internal/control`、`internal/udp`、`internal/report`、`tests/integration` 目录。实现单二进制 CLI，包含 `server` 与 `client` 子命令，定义完整 flag 契约、默认值、stdout/stderr 分工、退出码约定。`server` 必须包含 `--listen`；`client` 必须包含 `--target4`、`--target6`、`--control-port`、`--direction`、`--ports`、`--min-size`、`--max-size`、`--step`、`--count`、`--interval`、`--max-estimated-duration`。明确包大小语义为 UDP payload 字节数，并将该定义写入 help 文案。
  **Must NOT do**: 不实现真实网络收发；不引入第三方 CLI 框架以外的无关依赖；不增加 JSON 输出、配置文件读取或 daemon 模式。

  **Recommended Agent Profile**:
  - Category: `quick` - Reason: 主要是项目脚手架与 CLI 契约建立。
  - Skills: `[]` - 无额外技能需求。
  - Omitted: [`review-work`] - 该技能属于整体实现后复核，不适合单任务起步阶段。

  **Parallelization**: Can Parallel: NO | Wave 1 | Blocks: 2,3,4,6,8 | Blocked By: -

  **References** (executor has NO interview context - be exhaustive):
  - Source of truth: `.sisyphus/plans/udp-bidirectional-loss-pmtu-tool.md` - CLI 契约、作用域、退出码与参数要求都以本计划为准。
  - Original request basis: Interview summary in `## Context` - server/client 的职责与 flags 来源。

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go build ./...` succeeds.
  - [ ] `go run ./cmd/udpdiag --help` prints `server` and `client`.
  - [ ] `go run ./cmd/udpdiag client --help` prints all required flags including `--target4`, `--target6`, `--ports`, `--min-size`, `--max-size`, `--step`, `--count`, `--interval`.
  - [ ] `go run ./cmd/udpdiag server --help` prints `--listen`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: CLI help smoke test
    Tool: Bash
    Steps: Run `go run ./cmd/udpdiag --help` and `go run ./cmd/udpdiag client --help`; capture output.
    Expected: Output contains `server`, `client`, `--direction`, `--ports`, and text stating packet size means UDP payload bytes.
    Evidence: .sisyphus/evidence/task-1-cli-help.txt

  Scenario: Missing required flags fails fast
    Tool: Bash
    Steps: Run `go run ./cmd/udpdiag client` with no flags.
    Expected: Process exits non-zero and stderr includes a concrete usage/error message.
    Evidence: .sisyphus/evidence/task-1-cli-error.txt
  ```

  **Commit**: YES | Message: `feat(cli): scaffold udp diagnostic tool` | Files: `go.mod`, `cmd/udpdiag/**`, `internal/**`, `tests/integration/**`

- [x] 2. 定义控制协议、错误码与会话状态机

  **What to do**: 在 `internal/protocol` 中定义控制协议版本、消息类型、编码格式、错误码、方向枚举、family 枚举、探测任务请求/响应结构，以及会话状态机。消息至少覆盖：hello/capability、prepare、ready、start、send-complete、fetch-result、result、cancel、busy、invalid-request、internal-error。定义 UDP probe header 结构，包含 `version`、`session_id`、`test_id`、`direction`、`family`、`udp_port`、`payload_size`、`seq`、`send_unix_nano`。状态机必须显式覆盖 `idle -> preparing -> ready -> running -> collecting -> completed/failed/cancelled`。
  **Must NOT do**: 不实现具体 server/client 调度；不使用不透明二进制协议而不留可测试 schema；不省略版本字段。

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: 协议与状态机是整个系统的共享契约。
  - Skills: `[]` - 无额外技能需求。
  - Omitted: [`git-master`] - 当前不是 git 操作任务。

  **Parallelization**: Can Parallel: NO | Wave 1 | Blocks: 3,4,5,6,7,8 | Blocked By: 1

  **References** (executor has NO interview context - be exhaustive):
  - Plan guardrails: `## Metis Review (gaps addressed)` - 版本化、BUSY、超时、诊断而非权威 PMTU。
  - Required metadata: `## Work Objectives > Must Have` - probe header 必须字段。
  - Runtime behavior: `## Definition of Done` - busy、critical_size、duration guard 必须能被后续实现验证。

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go test ./...` succeeds with protocol unit tests covering encode/decode roundtrip and enum validation.
  - [ ] `go test ./... -run TestProtocolStateMachine` succeeds and verifies valid/invalid state transitions.
  - [ ] `go test ./... -run TestProbeHeaderRoundTrip` succeeds.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Protocol roundtrip validation
    Tool: Bash
    Steps: Run `go test ./... -run 'Test(Protocol|ProbeHeader)' -v`.
    Expected: Tests pass and output includes each control message family being serialized/deserialized.
    Evidence: .sisyphus/evidence/task-2-protocol-tests.txt

  Scenario: Invalid transition rejected
    Tool: Bash
    Steps: Run `go test ./... -run TestProtocolStateMachineRejectsInvalidTransition -v`.
    Expected: Test passes, proving state machine forbids illegal transitions such as `idle -> running`.
    Evidence: .sisyphus/evidence/task-2-state-machine-error.txt
  ```

  **Commit**: YES | Message: `feat(protocol): define control messages and session state machine` | Files: `internal/protocol/**`

- [x] 3. 实现 client 参数解析、sweep 规划与运行时边界保护

  **What to do**: 在 `internal/config` 实现 client 参数校验与任务矩阵生成。支持单端口、逗号列表、区间；支持 `up/down/both`；支持 `target4` 与 `target6` 分开传入并生成 family 维度；支持 `min-size/max-size/step` sweep；支持总包数与间隔；估算总测试时长并在超出默认上限时拒绝执行，除非用户显式通过 `--max-estimated-duration` 放宽。保证 `both` 的执行顺序固定为 uplink 后 downlink。
  **Must NOT do**: 不执行真实网络流量；不默默裁剪非法参数；不允许 `min-size > max-size`、`step <= 0`、空端口集、空目标地址通过校验。

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: 涉及参数 DSL、矩阵展开和保护规则。
  - Skills: `[]` - 无额外技能需求。
  - Omitted: [`ai-slop-remover`] - 不是单文件清理任务。

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 6,7,8 | Blocked By: 1,2

  **References** (executor has NO interview context - be exhaustive):
  - CLI contract source: Task 1 output under `cmd/udpdiag`.
  - Protocol enums: Task 2 output under `internal/protocol`.
  - Runtime bound rule: `## Metis Review (gaps addressed)` and `## Definition of Done` duration rejection requirement.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go test ./... -run TestParsePorts` succeeds for single, CSV, and range formats.
  - [ ] `go test ./... -run TestBuildSweepPlan` succeeds and verifies protocol family × port × direction × size expansion.
  - [ ] `go test ./... -run TestRejectOverlongSweep` succeeds and verifies default duration guard.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Valid sweep matrix generation
    Tool: Bash
    Steps: Run `go test ./... -run 'Test(ParsePorts|BuildSweepPlan)' -v`.
    Expected: Tests pass and output confirms `both` expands to ordered `uplink`, then `downlink` across requested ports/sizes.
    Evidence: .sisyphus/evidence/task-3-sweep-tests.txt

  Scenario: Overlong sweep rejected
    Tool: Bash
    Steps: Run `go test ./... -run TestRejectOverlongSweep -v` and `go run ./cmd/udpdiag client --target4 127.0.0.1 --control-port 18080 --direction both --ports 40000-40100 --min-size 100 --max-size 2000 --step 10 --count 1000 --interval 100ms`.
    Expected: Test passes and CLI exits non-zero with an estimated-duration rejection message.
    Evidence: .sisyphus/evidence/task-3-duration-guard.txt
  ```

  **Commit**: YES | Message: `feat(client): add sweep planner and runtime bounds` | Files: `internal/config/**`, `cmd/udpdiag/**`

- [x] 4. 实现 server 控制平面与单 Client 资源生命周期

  **What to do**: 在 `internal/control` 实现 TCP 控制服务端，显式选择监听行为以支持 IPv4/IPv6。维护单 active session；当已有活动会话时，对第二个 client 立即返回 `BUSY` 响应并关闭会话。实现 prepare/ready/start/cancel/result 生命周期，与 UDP 资源分配/释放对接。定义超时：控制连接读写超时、prepare 超时、run 超时、collect 超时；定义取消后清理逻辑，确保 UDP 端口关闭、session 复位到 idle。
  **Must NOT do**: 不做排队；不共享活动测试的 UDP 资源给第二个 client；不吞掉 timeout/cancel 错误；不依赖 goroutine 泄漏式清理。

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: 控制平面、状态管理、资源生命周期和错误路径复杂。
  - Skills: `[]` - 无额外技能需求。
  - Omitted: [`playwright`] - 非浏览器任务。

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 6,8 | Blocked By: 1,2

  **References** (executor has NO interview context - be exhaustive):
  - State machine contract: Task 2 protocol package.
  - Busy behavior: `## Metis Review (gaps addressed)` and `## Definition of Done`.
  - Server flags: Task 1 CLI contract.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go test ./... -run TestServerBusyResponse` succeeds.
  - [ ] `go test ./... -run TestServerSessionLifecycle` succeeds.
  - [ ] `go test ./... -run TestServerCancelCleansResources` succeeds.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Single client lifecycle works
    Tool: Bash
    Steps: Run `go test ./... -run 'TestServer(SessionLifecycle|CancelCleansResources)' -v`.
    Expected: Tests pass and logs show prepare -> ready -> running -> completed/cancelled transitions with resource cleanup.
    Evidence: .sisyphus/evidence/task-4-server-lifecycle.txt

  Scenario: Busy client rejected
    Tool: Bash
    Steps: Run `go test ./... -run TestServerBusyResponse -v`.
    Expected: Test passes and verifies second client gets `BUSY` plus non-success status.
    Evidence: .sisyphus/evidence/task-4-server-busy.txt
  ```

  **Commit**: YES | Message: `feat(server): add single-session control plane` | Files: `internal/control/**`, `cmd/udpdiag/**`

- [x] 5. 实现 UDP 探测引擎与收包统计

  **What to do**: 在 `internal/udp` 实现 uplink/downlink 共用的 UDP 发包与收包能力。发包端必须按给定 payload size 组包，并写入 probe header；收包端必须校验 header 与当前 session/test 匹配，仅统计目标数据包。实现发送节流（按 interval）、序列号递增、收包窗口统计、结束后结果汇总。必须显式按 `udp4` / `udp6` 打开 socket，避免依赖系统双栈隐式行为。
  **Must NOT do**: 不把不同 session/test 的包混算；不以 wall-clock sleep 抖动掩盖序列丢失；不忽略 IPv6 与 IPv4 socket 分离；不使用广播/组播。

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: 底层 UDP 收发、定时和统计逻辑需要仔细实现。
  - Skills: `[]` - 无额外技能需求。
  - Omitted: [`frontend-ui-ux`] - 非 UI 任务。

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 6,8 | Blocked By: 2

  **References** (executor has NO interview context - be exhaustive):
  - Probe header schema: Task 2 protocol package.
  - Family semantics: `## Must Have` dual-stack support requirement.
  - Rate controls: Original PRD flow section for packet count and interval.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go test ./... -run TestUDPProbeHeaderValidation` succeeds.
  - [ ] `go test ./... -run TestUDPSenderRespectsInterval` succeeds.
  - [ ] `go test ./... -run TestUDPReceiverCountsMatchingPacketsOnly` succeeds.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: UDP engine happy path
    Tool: Bash
    Steps: Run `go test ./... -run 'TestUDP(SenderRespectsInterval|ReceiverCountsMatchingPacketsOnly|ProbeHeaderValidation)' -v`.
    Expected: Tests pass and verify correct payload sizing, interval pacing, and session/test filtering.
    Evidence: .sisyphus/evidence/task-5-udp-engine.txt

  Scenario: Mismatched packet ignored
    Tool: Bash
    Steps: Run `go test ./... -run TestUDPReceiverIgnoresMismatchedSession -v`.
    Expected: Test passes and confirms packets with wrong session/test metadata do not affect counts.
    Evidence: .sisyphus/evidence/task-5-udp-mismatch.txt
  ```

  **Commit**: YES | Message: `feat(udp): add probe send and receive engine` | Files: `internal/udp/**`

- [x] 6. 实现 uplink/downlink 编排与 TCP/UDP 协同执行

  **What to do**: 在 client 与 server 间实现完整测试编排。Uplink：client 通过 TCP 发送 `prepare`，收到 `ready` 后按参数向 server UDP 端口发包，结束后通过 TCP 请求 server 返回接收计数。Downlink：client 本地先开启临时 UDP 接收端口并在 `prepare` 中告知 server，server `ready` 后发送 UDP 探测流并在完成后通过 TCP 发送 `send-complete`，client 结合本地计数计算结果。`both` 模式必须按 uplink 再 downlink 顺序执行；每个 test case 之间必须完成资源 teardown 后再进入下一轮。实现 TCP 侧错误传播与非零退出。
  **Must NOT do**: 不并行执行多个 test case；不在 downlink 模式下省略 client 本地 UDP 接收；不在 uplink 结束前提前请求结果；不跨 test case 复用脏状态。

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: 涉及多方向状态协同、套接字配合与错误传播。
  - Skills: `[]` - 无额外技能需求。
  - Omitted: [`librarian`] - 无外部库或远程仓库研究需求。

  **Parallelization**: Can Parallel: NO | Wave 3 | Blocks: 7,8 | Blocked By: 3,4,5

  **References** (executor has NO interview context - be exhaustive):
  - Uplink/downlink flow source: `## Context` 中对 PRD 流程的总结。
  - Control messages and states: Task 2 output under `internal/protocol`.
  - Sweep planning and family/port expansion: Task 3 output under `internal/config`.
  - Server lifecycle and UDP engine: Tasks 4 and 5 outputs.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go test ./... -run TestUplinkOrchestration` succeeds.
  - [ ] `go test ./... -run TestDownlinkOrchestration` succeeds.
  - [ ] `go test ./... -run TestBothModeRunsUplinkThenDownlink` succeeds.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Both-direction orchestration works
    Tool: Bash
    Steps: Run `go test ./... -run 'Test(UplinkOrchestration|DownlinkOrchestration|BothModeRunsUplinkThenDownlink)' -v`.
    Expected: Tests pass and logs confirm ready/start/result ordering plus uplink-before-downlink sequencing.
    Evidence: .sisyphus/evidence/task-6-orchestration.txt

  Scenario: Premature result fetch is rejected
    Tool: Bash
    Steps: Run `go test ./... -run TestResultFetchBeforeRunCompletionFails -v`.
    Expected: Test passes and confirms protocol returns an explicit error instead of partial or undefined results.
    Evidence: .sisyphus/evidence/task-6-orchestration-error.txt
  ```

  **Commit**: YES | Message: `feat(flow): orchestrate uplink and downlink test execution` | Files: `cmd/udpdiag/**`, `internal/control/**`, `internal/udp/**`, `internal/config/**`

- [x] 7. 实现结果聚合、5% 阈值判定与控制台报告输出

  **What to do**: 在 `internal/report` 实现 per-test result 模型、丢包率计算、按 family/port/direction/size 聚合排序、临界包大小判定、以及格式化控制台输出。输出中必须包含：目标地址、family、direction、udp port、payload size、sent、received、loss 百分比、critical_size。对每个 `(family, port, direction)` 组按 size 升序扫描，取首个 `loss >= 5%` 作为 `critical_size`；若无，则输出 `none`。实现对 sweep 预估信息的显示，并在最终报告中注明 `observed threshold` 语义。
  **Must NOT do**: 不把不同方向或端口混成一组计算临界值；不隐藏 0% 或 100% 丢包结果；不把阈值文字写成确定性 PMTU。

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: 结果模型和确定性排序/阈值规则需要严谨实现。
  - Skills: `[]` - 无额外技能需求。
  - Omitted: [`momus`] - Momus 属于计划审核，不是实现任务代理。

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: 8 | Blocked By: 3,6

  **References** (executor has NO interview context - be exhaustive):
  - Threshold rule: `## Interview Summary` and `## Must Have`.
  - Reporting requirements: `## Context` 中的报告生成需求。
  - Result source fields: Tasks 3 and 6 outputs.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go test ./... -run TestLossCalculation` succeeds.
  - [ ] `go test ./... -run TestCriticalSizeFirstAtOrAboveThreshold` succeeds.
  - [ ] `go test ./... -run TestReportFormatting` succeeds and validates required output fields.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Report generation happy path
    Tool: Bash
    Steps: Run `go test ./... -run 'Test(LossCalculation|CriticalSizeFirstAtOrAboveThreshold|ReportFormatting)' -v`.
    Expected: Tests pass and output verifies deterministic sorting plus `critical_size=...` or `critical_size=none`.
    Evidence: .sisyphus/evidence/task-7-report-tests.txt

  Scenario: No threshold breach handled explicitly
    Tool: Bash
    Steps: Run `go test ./... -run TestCriticalSizeNoneWhenAllBelowThreshold -v`.
    Expected: Test passes and verifies report prints `critical_size=none` when every size stays below 5% loss.
    Evidence: .sisyphus/evidence/task-7-report-none.txt
  ```

  **Commit**: YES | Message: `feat(report): add loss aggregation and threshold reporting` | Files: `internal/report/**`, `cmd/udpdiag/**`

- [x] 8. 建立自动化测试与 IPv4/IPv6 端到端集成验证

  **What to do**: 为前述模块补齐 tests-after 自动化验证。至少包含：协议单测、参数单测、server 生命周期单测、UDP 引擎单测、client/server orchestration 单测，以及本机 loopback 集成测试。集成测试必须覆盖 IPv4 `127.0.0.1` 和 IPv6 `::1`；覆盖 `up`、`down`、`both` 至少各一条；覆盖 busy server、无效参数、过长 sweep 拒绝。提供用于 agent 执行的测试命令与证据文件写出路径。若 IPv6 在环境中不可用，测试必须明确 skip 并输出原因，而不是假通过。
  **Must NOT do**: 不依赖人工抓包；不把 flaky sleep 作为通过条件；不只测 happy path；不因为环境限制而静默降级跳过全部网络验证。

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: 需要把所有模块串成稳定、可重复、可诊断的验证体系。
  - Skills: `[]` - 无额外技能需求。
  - Omitted: [`playwright`] - CLI 工具无浏览器交互。

  **Parallelization**: Can Parallel: NO | Wave 3 | Blocks: F1,F2,F3,F4 | Blocked By: 1,2,3,4,5,6,7

  **References** (executor has NO interview context - be exhaustive):
  - All prior task outputs are required dependencies.
  - Evidence policy: `## Verification Strategy`.
  - Done conditions: `## Definition of Done`.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go test ./...` succeeds.
  - [ ] `go build ./...` succeeds.
  - [ ] `go test ./... -run TestIntegrationIPv4Loopback` succeeds.
  - [ ] `go test ./... -run TestIntegrationIPv6Loopback` succeeds or is explicitly skipped with environment reason.
  - [ ] `go run ./cmd/udpdiag client --target4 127.0.0.1 --control-port 18080 --direction both --ports 40000-40002 --min-size 1200 --max-size 1472 --step 136 --count 20 --interval 10ms` succeeds against a locally started server and outputs `uplink`, `downlink`, `loss=`, and `critical_size=`.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: End-to-end IPv4 and IPv6 verification
    Tool: Bash
    Steps: Run `go test ./... -v`; then launch `go run ./cmd/udpdiag server --listen :18080` in the background and run the sample client command for IPv4, plus an IPv6 variant using `--target6 ::1`.
    Expected: Full test suite passes; IPv4 end-to-end succeeds; IPv6 succeeds or explicitly skips with a concrete environment message.
    Evidence: .sisyphus/evidence/task-8-integration.txt

  Scenario: Busy server and invalid input failure paths
    Tool: Bash
    Steps: Start one long-running client test against the server, then start a second client; separately run a client command with invalid `--ports abc` and one with an overlong sweep.
    Expected: Second client fails with `BUSY`; invalid ports fail with parse error; overlong sweep fails with duration guard message.
    Evidence: .sisyphus/evidence/task-8-failure-paths.txt
  ```

  **Commit**: YES | Message: `test(integration): add loopback verification for udp diagnostics` | Files: `tests/integration/**`, `internal/**`, `cmd/udpdiag/**`

## Final Verification Wave (MANDATORY — after ALL implementation tasks)
> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
> **Do NOT auto-proceed after verification. Wait for user's explicit approval before marking work complete.**
> **Never mark F1-F4 as checked before getting user's okay.** Rejection or user feedback -> fix -> re-run -> present again -> wait for okay.
- [x] F1. Plan Compliance Audit — oracle
- [x] F2. Code Quality Review — unspecified-high
- [x] F3. Real Manual QA — unspecified-high (+ playwright if UI)
- [x] F4. Scope Fidelity Check — deep

## Commit Strategy
- Commit 1: `feat(cli): scaffold udp diagnostic tool and protocol contracts`
- Commit 2: `feat(server): add control plane and udp probe execution`
- Commit 3: `feat(client): add sweep orchestration and report generation`
- Commit 4: `test(integration): add end-to-end verification for ipv4 and ipv6 loopback`

## Success Criteria
- Agent can build and test the project with standard Go tooling only.
- Agent can run local loopback uplink/downlink tests over IPv4 and IPv6 without manual setup beyond port availability.
- Reports consistently expose per-size loss and deterministic `critical_size` output.
- Busy-server, invalid args, and overlong sweep protections are all verified by automated checks.
