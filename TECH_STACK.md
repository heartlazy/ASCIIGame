# Go 版技术选型决策（ASCII Battle Royale 移植）

> 来源：deep-research 工作流（6 角度 · 25 来源 · 104 条声明 → 25 条经 3 票对抗式验证全部确认）+ Go 官方文档约定。
> 标注 [已验证] 的结论有引用证据；标注 [推荐·惯用法] 的是基于成熟 Go 实践的判断（调研未找到可验证的独立基准）。

---

## 决策 1：客户端 TUI 库 → **tcell + tview**（备选 Bubble Tea）

**推荐：`gdamore/tcell` + `rivo/tview`** [已验证]

理由：
- tcell 纯 Go、无 CGO、跨平台，**内部对脏 cell 做差分**，只重绘变化的单元格 —— 正契合 20 tick/s 高频重绘 50×20 地图的需求。
- tview 基于 tcell，内置 **Grid / Flex / Pages** 布局，直接映射原 ncurses 的 status/map/msg/help 多窗口布局。
- 与后台网络 goroutine 同步：tview 用 `Application.QueueUpdateDraw()` 从其他 goroutine 安全触发重绘。

**备选：Bubble Tea + Lip Gloss + Bubbles**（43.8k stars, MIT, v2.0.8 @ 2026-07）[已验证]
- Elm 架构（Model/Init/Update/View），v2 起改为 **cell-based 渲染器**（v1 是 line-based）。
- 后台任务用 `Cmd` → `Msg` 异步模型：网络 goroutine 的消息通过 `Msg` 回流到 `Update`，状态同步非常清晰。
- Lip Gloss 是**配套**（不是替代），提供 `JoinHorizontal/JoinVertical` + cell 合成器做面板拼接。

选择建议：
- 想要**接近原 ncurses 的命令式多窗口**、迁移成本低 → **tcell+tview**。
- 想要**更现代、状态驱动、易测试**的架构，且团队接受 Elm 范式 → Bubble Tea。

> ⚠️ caveat：没有实测基准证明任何库能稳定跑满 20 tick/s 全屏重绘；tcell 的优势是从其**文档化的 cell-diffing 设计**推断的。实现后需实测 p99 帧时间。

---

## 决策 2：服务端并发模型 → **每连接一 goroutine + 每房间一 tick goroutine + RWMutex 保护表** [推荐·惯用法]

> 调研未找到可验证的独立证据支撑此模式为"最佳"，但它是 Go 网络服务的教科书级惯用法。以下为工程判断。

**结构**：
- **每 TCP 连接一个 goroutine**：`net.Listener.Accept()` 循环 + `go handleConn(conn)`。这是 Go 标准库网络编程的默认范式（`net/http` 亦如此），goroutine 开销极低，无需 epoll 手写事件循环。
- **每房间一个游戏循环 goroutine**：`time.NewTicker(50ms)` 驱动，替代原每房间 pthread。
- **共享状态（玩家表 / 房间表）**：用 **`sync.RWMutex` + `map`**。

三方案取舍：
| 方案 | 适用 | 本项目评价 |
|------|------|-----------|
| **RWMutex + map** ✅ | 读多写少、访问模式简单 | **首选**。玩家/房间表就是这种模式，代码直观，最贴近原 C 版两级锁粒度 |
| sync.Map | 键集合稳定、大量无竞争并发读 | 本项目表规模小（≤256 玩家/32 房间），收益不明显，且类型不安全（`interface{}`） |
| actor/channel 消息传递 | 状态强隔离、避免锁 | 每房间游戏循环**内部**天然是单 goroutine 串行（可视为 actor），房间内状态改动应尽量收敛到该 goroutine，避免跨房间锁 |

**推荐的混合粒度**（对应原 C 版设计）：
- 全局玩家表 / 房间表：`sync.RWMutex`（对应原 `players_mutex` / `rooms_mutex`）。
- 单个房间的游戏状态：**尽量只由该房间的 tick goroutine 读写**（串行化，天然无锁）；连接 goroutine 收到指令后通过 channel 投递给房间 goroutine，而非直接加房间锁。这比原 C 版逐字节翻译更 Go-idiomatic，也更少死锁。

**50ms tick 抗抖动 + 抗 GC 停顿**：
- `time.Ticker` 会**丢弃**错过的 tick（不会补偿堆积），据此避免 tick 雪崩；若需要严格节奏，可在每 tick 内用 `time.Now()` 重算逻辑时间差（delta time），而非假设恒定 50ms。
- Go GC 是**并发的**，只在 mark/sweep 转换点有**短暂**停顿，不是全程 STW。[已验证]
- **调大 `GOGC` 和/或设 `GOMEMLIMIT`** 可降低 GC 频率与抖动；代价是 `GOGC` 翻倍 → 堆开销翻倍、GC CPU 约减半。[已验证] 本项目内存占用极小，可放心设 `GOGC=200~400` 换取更平滑的 tick。

> ⚠️ caveat：tick drift 的具体表现、GC 在 20 tick/s 负载下是否扰动，需实测（记录每 tick 实际间隔的 p99）。

---

## 决策 3：行协议分帧 → **`bufio.Reader.ReadString('\n')`**（不用 Scanner） [已验证]

**推荐：`bufio.Reader` + `ReadString('\n')` / `ReadBytes('\n')`**

理由：
- `bufio.Scanner` 默认单 token 上限 **64KB**（`MaxScanTokenSize = 64*1024`），**超限会不可恢复地报 `ErrTooLong`**。本项目 `MAP_DATA` 等大字段虽当前 < 64KB，但用 Scanner 是隐患。
- Go 官方文档明确：**需要处理大 token 的程序应改用 `bufio.Reader`**。
- 若坚持 Scanner，必须用 `Scanner.Buffer(buf, max)` 显式抬高上限。

实现要点：
- `ReadString('\n')` 返回**含分隔符**的整行，处理时 `strings.TrimRight(line, "\r\n")`（默认 `ScanLines` 的语义是 LF 必需、CR 可选）。
- 处理**部分读 / 粘包**：`ReadString` 在遇到 EOF 但无分隔符时会返回已读数据 + err，需妥善处理半包。
- 备选：`ReadLine()`（通过 `isPrefix` 返回超长行的分片），但 API 较繁琐，一般不需要。

---

## 决策 4：WAL 与快照 → **手写 `os.File`+`bufio.Writer`+定期 `Sync()`；快照用 JSON** [已验证]

**推荐：手写 WAL**（保持与 C 版文本格式兼容），**快照用 `encoding/json`**（决策已定）。

理由：
- 原 C 版 WAL 是**文本行格式**（`TS|SEQ|ROOM|TYPE|DATA\n`），必须保持兼容以复用 Python 测试 → 直接 `os.File` + `bufio.Writer` 追加，最简单可控。
- **fsync 频率是 durability/性能的调节旋钮**：
  - 优先用 **`fdatasync`**（比 `fsync` 略快，只刷必要的元数据）；Go 里对应 `unix.Fdatasync`（Linux），**Darwin/Windows 会回退到 `Fsync`**。
  - 参考 etcd 生产级 WAL：**每次写都 fdatasync**，并提供 `unsafeNoSync` 全局旁路。本项目沿用原 `WAL_SYNC_INTERVAL_MS=1000ms` 定期刷即可（吞吐/durability 平衡）。
  - ⚠️ **新建文件后需对其父目录额外做一次 `fsync`**，否则崩溃后文件可能不可见（POSIX 语义）。
- 快照 JSON：本项目快照体积很小（单房间 ≤10 玩家 + 50×20 地图），JSON 的性能完全够用，且**易调试、易演进**，胜过二进制对齐的维护成本。

**备选：`tidwall/wal`**（纯 Go, MIT）[已验证]
- 提供 `Open/Write/Read/WriteBatch/Truncate`，支持批量写分组。
- 但它是**二进制分段格式**，与现有 C 版 WAL 文本格式**不兼容**，会破坏 Python 测试对拍 → **本项目不采用**，仅作为"若不需兼容旧格式"的参考。

---

## 决策 5：项目结构 → **`cmd/` + `internal/` 官方布局，单 go.mod** [已验证]

**推荐布局**（Go 官方 modules/layout 指南）：
```
asciigame/                    (单一 go.mod: module github.com/<you>/asciigame)
├── go.mod
├── cmd/
│   ├── server/main.go        # 服务端入口
│   └── client/main.go        # 客户端入口
├── internal/                 # 私有代码，其他 module 无法 import [已验证]
│   ├── protocol/             # 协议编解码（公共，两端共用）
│   ├── config/               # 常量
│   ├── server/               # network/player/room/game/wal/snapshot/recovery/storage/handler
│   └── client/               # network/game/ui
├── data/                     # 运行时：wal/、users.dat（gitignore）
├── test/                     # 复用现有 Python 测试
└── Makefile
```

理由：
- **`cmd/<prog>/main.go`** 是官方约定的多可执行程序布局（server + client 两个二进制）。[已验证]
- **`internal/`** 由工具链保证不被外部 module 导入，适合放全部私有实现代码。[已验证]
- **单 go.mod**：两个二进制共享 `internal/protocol`、`internal/config`，无需多 module。
- `pkg/` 目录**非必需**（golang-standards/project-layout 是社区约定，非官方强制）；本项目全部代码私有，用 `internal/` 即可，不引入 `pkg/`。

**测试与 -race 集成** [已验证]：
- 单元测试就近放包内 `_test.go`；协议对标测试放 `internal/protocol`。
- **`go test -race ./...`** 检测数据竞争 —— 对本项目的多 goroutine 共享状态是**必做项**；CI 里跑 race 版。
- `go build -race` 出带竞态检测的二进制，配合 Python 压力测试跑一轮。

---

## 汇总：一句话结论

| 决策 | 结论 |
|------|------|
| 客户端 TUI | **tcell + tview**（备选 Bubble Tea） |
| 服务端并发 | 每连接一 goroutine + 每房间一 `Ticker(50ms)` goroutine；全局表 `sync.RWMutex`，房间内状态收敛到房间 goroutine（channel 投递指令） |
| GC 调优 | `GOGC=200~400` + 可选 `GOMEMLIMIT`，降 tick 抖动 |
| 行分帧 | `bufio.Reader.ReadString('\n')`（**不用 Scanner**，避 64KB 上限） |
| WAL | 手写 `os.File`+`bufio.Writer`+定期 `Sync()`，保持文本格式兼容；`fdatasync` 优先，新建文件后 fsync 父目录 |
| 快照 | `encoding/json` |
| 项目结构 | 单 go.mod + `cmd/{server,client}` + `internal/`，`go test -race ./...` |

## 待实测验证（调研未覆盖，实现后补基准）
1. TUI 库是否真能稳定 20 tick/s 全屏重绘（测 p99 帧时间）。
2. 50ms tick 的实际抖动分布 + GC 是否扰动。
3. JSON 快照序列化耗时（预期可忽略）。
