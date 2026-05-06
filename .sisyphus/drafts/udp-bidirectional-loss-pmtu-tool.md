# Draft: UDP 双向丢包与 Path MTU 探测工具

## Requirements (confirmed)
- 产品目标: 开发一套基于 Client/Server 架构的命令行诊断工具，用于探测特定网络路径下不同大小 UDP 数据包在上行和下行方向的连通性及丢包率。
- 核心价值: 使用可靠 TCP 控制通道协调测试参数，使用 UDP 数据通道发送精确大小的探测包，绘制“包大小 vs 丢包率”曲线并定位双向拦截阈值。
- 必须支持 IPv4 和 IPv6。
- Client 必须可显式指定服务端 IPv4 或 IPv6 地址进行独立测试。
- 必须支持三种测试模式: Uplink、Downlink、Both。
- Server 必须监听固定 TCP 控制端口，等待 Client 指令。
- Server 必须能动态开启/关闭指定 UDP 监听端口。
- Server 必须支持接收模式统计和发送模式发包。
- Client 必须解析目标 IP、方向、端口范围、包大小区间、发包策略等参数。
- Client 必须生成测试任务矩阵: 协议 -> 端口 -> 方向 -> 包大小。
- Client 必须输出格式化报告并标示临界包大小。
- TCP 控制通道必须完成任务下发、就绪同步、结果收集。
- Server CLI 必须支持指定 TCP 控制端口，并同时监听 IPv4/IPv6 控制指令。
- Client CLI 必须支持目标地址、控制端口、测试方向、UDP 端口范围、包大小范围、总发包数、发包间隔。

## Technical Decisions
- 架构: 已确认是绿地实现；需要从零建立项目结构、CLI、控制协议、测试体系。
- 控制协议: 采用版本化 TCP 控制协议，定义 prepare/ready/start/send-complete/fetch-result/result/cancel/busy/error 等消息与单会话状态机。
- 结果算法: 临界包大小固定为按 size 升序扫描时首个 `loss >= 5%` 的 payload size；否则输出 `critical_size=none`。
- 测试策略: 由于无现有测试基础设施，需将测试框架与集成测试 harness 纳入实施范围。
- Server 并发: 单 Client 串行测试；第二个 Client 返回 `BUSY`，不排队。
- 包大小语义: CLI 与报告中的 size 明确定义为 UDP payload bytes。

## Research Findings
- 仓库为空目录，无现有源码、配置、文档、构建脚本，也不是 git 仓库。
- 无测试框架、无 CI、无 QA 约定；测试策略需从零设计。
- 无可复用的 CLI、TCP/UDP、IPv4/IPv6、MTU 探测实现模式。

## Open Questions
- 无阻塞性问题；按已确认决策进入计划生成。

## Scope Boundaries
- INCLUDE: Client/Server CLI、TCP 控制通道、UDP 双向探测、IPv4/IPv6、报告输出。
- EXCLUDE: GUI、长期监控平台、自动修复网络问题、与现有系统集成改造（当前无现有系统）。
