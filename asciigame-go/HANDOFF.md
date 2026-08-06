# Go 移植断点续接指南

> 本文档供迁移到 Linux 后继续推进阶段 4。记录了当前状态、文件清单、依赖、已知问题和下一步操作。

---

## 项目位置

```
D:\project\ASCIIGame\asciigame-go\          ← Go 项目根目录
D:\project\ASCIIGame\                       ← C 源码 + 移植文档
```

移植到 Linux 时，将整个 `asciigame-go/` 目录复制过去即可（自包含），`PORTING_CHECKLIST.md` 和 `TECH_STACK.md` 也复制过去作为参考。

## 当前状态（最后更新 2026-08-03）

| 阶段 | 状态 | 里程碑 |
|------|------|--------|
| 阶段 0：脚手架 | ✅ 完成 | go.mod + config + protocol + 对标测试全绿 |
| 阶段 1：MVP 服务端 | ✅ 完成 | Python functional_test.py 8/8 通过 |
| 阶段 2：MVP 客户端 | ✅ 完成 | tcell+tview 编译通过，headless 双客户端测试通过；需真实 TTY 验证界面 |
| 阶段 3：持久化与恢复 | ✅ 完成 | WAL 文本兼容 + JSON 快照 + 崩溃恢复；robustness 10/10；真实 taskkill→重启→重连验证通过 |
| **阶段 4：质量收尾** | ⏳ 待做 | 见下方"阶段 4 待办清单" |

## 文件清单

```
asciigame-go/
├── go.mod                          # module github.com/heartlazyli/asciigame
├── go.sum
├── Makefile
├── .gitignore
├── bin/                            # 构建产物（gitignore）
│   ├── server.exe                  # (Windows) 或 server (Linux)
│   └── client.exe                  # (Windows) 或 client (Linux)
├── cmd/
│   ├── server/main.go              # 服务端入口（TCP + HTTP）
│   └── client/main.go              # 客户端 TUI 入口（tcell+tview）
├── internal/
│   ├── config/config.go            # 常量（照抄 C config.h）
│   ├── protocol/
│   │   ├── asciigame.proto         # protobuf schema（TCP 线格式）
│   │   ├── asciigame.pb.go         # protoc 生成
│   │   ├── codec.go                # protobuf ↔ 4 字节长度前缀帧编解码
│   │   └── codec_test.go           # 帧往返 / oneof 往返测试
│   ├── server/
│   │   ├── server.go               # Server 聚合 + Accept 循环 + 连接处理
│   │   ├── server_test.go          # 双人对战集成测试
│   │   ├── player.go               # Player 结构 + 状态机 + 线程安全发送
│   │   ├── room.go                 # Room 结构 + 状态机 + 玩家增删
│   │   ├── handler.go              # TCP 命令路由（Move/Attack/UseItem）
│   │   ├── game.go                 # 游戏 tick goroutine + 移动/攻击/道具/毒圈/结束
│   │   ├── gamemap.go              # 地图模板、碰撞、随机位、距离、毒圈判定
│   │   ├── storage.go              # 用户账号（SQLite + bcrypt，兼容旧 JSON/SHA256 迁移）
│   │   ├── http.go                 # Gin HTTP API（注册/登录/房间/聊天）
│   │   ├── token.go                # 登录 token 生成 / 校验
│   │   ├── wal.go                  # 文本 WAL（兼容 C 格式 TS|SEQ|ROOM|ACTION|DATA）
│   │   ├── snapshot.go             # JSON 快照（原子写）
│   │   ├── recovery.go             # 崩溃恢复 + 登录重连
│   │   ├── recovery_bench_test.go # 恢复性能基准
│   │   └── recovery_correctness_test.go # 恢复正确性单测
│   └── client/
│       ├── net.go                  # HTTP client + TCP 连接（protobuf 帧）
│       ├── state.go                # GameState 镜像 + 消息解析
│       ├── ui.go                   # tview 四面板 UI
│       └── debug.go                # 调试 / 无 TTY 模式
└── data/                           # 运行时产物（gitignore）
    ├── game.db                     # SQLite 用户数据库
    └── wal/                        # WAL（*.wal）+ snapshot（*.snap）
```

## Go 依赖

```
require (
    github.com/gin-gonic/gin v1.10.1                           # HTTP API
    google.golang.org/protobuf v1.36.9                         # TCP 线格式（protobuf）
    golang.org/x/crypto v0.54.0                                # bcrypt 密码哈希
    github.com/modernc.org/sqlite v1.55.0                      # 纯 Go SQLite（无 cgo）
    github.com/gdamore/tcell/v2 v2.9.0                         # 终端 UI
    github.com/rivo/tview v0.0.0-20250107065808-5ceddf953ab8   # 终端 UI 组件
)
# 其余（gin-contrib/sse、klauspost/compress、mattn/go-isatty、jinzhu/copier、
# spf13/pflag、google/uuid、gdamore/encoding、lucasb-eyer/go-colorful、
# golang.org/x/sys、golang.org/x/term、golang.org/x/text 等）均为传递依赖
# （// indirect），完整列表见 go.mod。
```

迁移到 Linux 后，运行 `go mod tidy` 确保 `go.sum` 一致。

## 阶段 4 待办清单

**大部分已完成**（2026-08-01）：代码审查（`/code-review` 修复内存泄漏 + tick 分配）、性能基准、CLAUDE.md、GOGC 调优、慢客户端/优雅关闭/战绩/bcrypt/Ticker/Room []*Player 重构、以及一次静态并发审查（发现并修复了 snapshotSave 对 r.wal 字段的 off-lock 读竞争）。**唯一仍需 Linux 的是 4.1 的 `go test -race`**（Windows 无 cgo）。

> **注意**：本文早期版本描述旧的"换行分隔文本协议"和"JSON+SHA256 账号"，现已作废。TCP 线协议已迁移为 **protobuf + 4 字节长度前缀**（见 CLAUDE.md 与 `internal/protocol/`），账号存储迁移为 **SQLite + bcrypt**（兼容旧 JSON/SHA256 数据自动迁移）。下文"TCP 线协议"小节为当前事实。

### 4.1 竞态检测（必须在 Linux 上跑）

已做过一次静态并发审查：锁序（room.mu → player.mu，注册表为叶子锁）全程正确；已修复 `snapshot.go` 中对 `r.wal` 指针的 off-lock 读（现在在锁内捕获局部变量）。仍需在 Linux 上用 race detector 复核动态路径：

```bash
cd asciigame-go
go test -race ./...           # 全量竞态检测（Makefile: make test-race）
go build -race -o bin/server-race ./cmd/server   # 出 race 版服务端
# 用 race 版服务端跑一遍 Python 测试：
./bin/server-race 8888 &
PYTHONUTF8=1 python ../test/functional_test.py --port 8888
PYTHONUTF8=1 python ../test/robustness_test.py --port 8888
# 检查 stderr 有无 race 报告
```

### 4.2 代码审查与简化
用 `/code-review` 和 `/simplify` 对代码做一次系统性检查，重点关注：
- 锁序是否一致（`room.mu` → `player.mu`，注册表锁为叶子锁）
- goroutine 泄漏（每个房间的 gameLoop 是否正确退出）
- 线协议为 protobuf（4 字节长度前缀帧），需保持 schema 向后兼容；帧往返 / oneof 往返测试见 `internal/protocol/codec_test.go`

### 4.3 性能基准
```bash
# Tick 抖动基准
go test -bench=. -benchtime=10s ./internal/server/ -run=NONE

# 协议解析基准
go test -bench=. ./internal/protocol/ -run=NONE
```

### 4.4 生成 CLAUDE.md
在项目根目录运行 `/init` skill，为 Go 项目生成结构化的 CLAUDE.md。

### 4.5 真实终端手动 e2e
启动服务端 + 两个客户端（需要真实 TTY）：
```bash
./bin/server 8888 &
./bin/client 127.0.0.1 8888   # 终端 1
./bin/client 127.0.0.1 8888   # 终端 2
```
验证：登录/建房/加入/准备 → 对战（移动 WASD、攻击 J/空格、道具 1-5、聊天 T）→ 毒圈收缩 → 对局结束。

## TCP 线协议（不可改动）

线格式为 **protobuf + 4 字节大端长度前缀帧**（见 `internal/protocol/asciigame.proto` 与 `codec.go`）。每帧 = `len(4B 大端) || protobuf(Frame)`。旧的"换行分隔文本协议"已被替换，**不要回退**。

`Frame.Payload` 为 oneof，支持：
- 客户端→服务端：`Auth` / `Move` / `Attack` / `UseItem`
- 服务端→客户端：`Ok` / `Error` / `GameStart` / `GameState` / `GameEvent` / `GameEnd` / `ChatMsg` / `Kick` / `PlayerJoin` / `PlayerLeave` / `RoomInfo` / `RoomList` / `MapData`

完整 schema 与构造器（`NewGameState` / `New*Event` 等）见 `internal/protocol/`，往返测试见 `codec_test.go`。

> **WAL 仍为文本格式** `TS|SEQ|ROOM|ACTION|DATA\n`（与 C 版兼容），见 `wal.go` 与下方"关键设计决策 4"。WAL 是内部持久化格式，**不属于 TCP 线协议**。

## 关键设计决策（备忘）

1. **并发模型**：每连接一 goroutine + 每房间一 `Ticker(50ms)` goroutine；全局 `players`/`rooms` 表用 `sync.RWMutex`；玩家写用独立的 `out chan` + writeLoop goroutine 串行化
2. **锁序**：`room.mu` → `player.mu`，注册表锁为叶子锁；所有网络发送在锁外
3. **行分帧**：TCP 服务端用 **4 字节大端长度前缀 + protobuf 解码**（`protocol.ReadFrame`），不再使用文本行分帧
4. **WAL 格式**：文本格式 `TS|SEQ|ROOM|ACTION|DATA\n`，与 C 版兼容
5. **快照格式**：JSON（`encoding/json`），每 20s 一次，原子写入
6. **恢复语义**：启动扫描 WAL，无 GAME_END 的房间挂起恢复表；玩家登录时逐个重连；首个重连者创建 gaming 房间启动游戏循环；等待 `RecoveryWaitTime`(30s) 期满前不判胜负
7. **事件传输**：BUFF_WARNING/BUFF_EXPIRED 等事件通过 protobuf oneof（`GameEvent`）传输，不再依赖文本帧的尾部 `\n`

## 已知环境差异

| 项目 | Windows（开发） | Linux（部署） |
|------|----------------|--------------|
| `go test -race` | 不可用（缺 cgo） | ✅ 正常 |
| Python 测试输出 | GBK 乱码，需 `PYTHONUTF8=1` | ✅ 正常 |
| WAL fsync | `f.Sync()` = FlushFileBuffers | `f.Sync()` = fsync，行为一致 |
| TUI 可视化 | 编译通过但无 TTY 验证 | ✅ 真实终端可用 |

## 测试命令速查

```bash
# 全量单元+集成测试
go test ./...

# 竞态检测（仅 Linux）
go test -race ./...

# 功能测试（需要 Python 3）
PYTHONUTF8=1 python ../test/functional_test.py --port <port>

# 健壮性测试
PYTHONUTF8=1 python ../test/robustness_test.py --port <port>

# 压力测试（可从轻量参数开始）
PYTHONUTF8=1 python ../test/stress_test.py --port <port> --clients 50 --duration 12

# 构建
go build -o bin/server ./cmd/server
go build -o bin/client ./cmd/client
```

## 相关文档

- `D:\project\ASCIIGame\PORTING_CHECKLIST.md` — C 模块清单与移植清单
- `D:\project\ASCIIGame\TECH_STACK.md` — deep-research 技术选型报告
- `C:\Users\Administrator\.tclaude\plans\melodic-rolling-fern.md` — 批准的架构方案（机器相关路径，仅本机有效）
