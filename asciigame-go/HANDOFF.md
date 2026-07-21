# Go 移植断点续接指南

> 本文档供迁移到 Linux 后继续推进阶段 4。记录了当前状态、文件清单、依赖、已知问题和下一步操作。

---

## 项目位置

```
D:\project\ASCIIGame\asciigame-go\          ← Go 项目根目录
D:\project\ASCIIGame\                       ← C 源码 + 移植文档
```

移植到 Linux 时，将整个 `asciigame-go/` 目录复制过去即可（自包含），`PORTING_CHECKLIST.md` 和 `TECH_STACK.md` 也复制过去作为参考。

## 当前状态（2026-07-20）

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
│   ├── server/main.go              # 服务端入口
│   └── client/main.go              # 客户端入口
├── internal/
│   ├── config/config.go            # 常量（照抄 C config.h）
│   ├── protocol/
│   │   ├── protocol.go             # 协议编解码
│   │   └── protocol_test.go        # 对标测试（golden vectors）
│   ├── server/
│   │   ├── server.go               # Server 聚合 + Accept 循环 + 连接处理
│   │   ├── server_test.go          # 双人对战集成测试
│   │   ├── player.go               # Player 结构 + 状态机 + 线程安全发送
│   │   ├── room.go                 # Room 结构 + 状态机 + 玩家增删
│   │   ├── handler.go              # 命令路由 + 全部 handler
│   │   ├── game.go                 # 游戏 tick goroutine + 移动/攻击/道具/毒圈/结束
│   │   ├── gamemap.go              # 地图模板、碰撞、随机位、距离、毒圈判定
│   │   ├── storage.go              # 用户账号（JSON + SHA256）
│   │   ├── wal.go                  # 文本 WAL（兼容 C 格式）
│   │   ├── snapshot.go             # JSON 快照
│   │   ├── recovery.go             # 崩溃恢复 + 登录重连
│   │   └── recovery_test.go        # 崩溃恢复单测
│   └── client/
│       ├── net.go                  # TCP 连接 + 读帧 goroutine → channel
│       ├── state.go                # GameState 镜像 + 消息解析
│       ├── ui.go                   # tview 四面板 UI
│       └── client_test.go          # 双客户端完整对战测试
└── data/                           # 运行时产物（gitignore）
    ├── users.json                  # 用户数据
    └── wal/                        # WAL + snapshot 文件
```

## Go 依赖

```
require (
    github.com/gdamore/tcell/v2 v2.13.10
    github.com/rivo/tview v0.42.0
    github.com/gdamore/encoding v1.0.1    // indirect
    github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
    github.com/rivo/uniseg v0.4.7         // indirect
    golang.org/x/sys v0.38.0             // indirect
    golang.org/x/term v0.37.0            // indirect
    golang.org/x/text v0.31.0            // indirect
)
```

迁移到 Linux 后，运行 `go mod tidy` 确保 `go.sum` 一致。

## 阶段 4 待办清单

在 Linux 环境下按顺序执行：

### 4.1 竞态检测（必须在 Linux 上跑）
```bash
cd asciigame-go
go test -race ./...           # 全量竞态检测
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
- 协议格式是否与 C 版逐字节一致（已有 golden vector 测试覆盖）

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

## 协议红线（不可改动）

帧格式：`CMD|arg1|arg2|...\n`，`|` 分隔，`\n` 结尾。解析时先剥 `\r\n`，按 `|` 切分，**空 token 被丢弃**（`ATTACK|\n` → argc=0）。

GAME_STATE 玩家条目是 **13 字段**：
`id,x,y,hp,atk,def,status,shield,inv0,inv1,inv2,inv3,inv4`

登录成功是 `OK|Login successful|<player_id>\n`（额外参数）。

完整格式参考 `internal/protocol/protocol_test.go` 中的 golden vectors。

## 关键设计决策（备忘）

1. **并发模型**：每连接一 goroutine + 每房间一 `Ticker(50ms)` goroutine；全局 `players`/`rooms` 表用 `sync.RWMutex`；玩家写用独立的 `out chan` + writeLoop goroutine 串行化
2. **锁序**：`room.mu` → `player.mu`，注册表锁为叶子锁；所有网络发送在锁外
3. **行分帧**：服务端用 `bufio.Reader.ReadString('\n')`（非 Scanner，避 64KB 上限）
4. **WAL 格式**：文本格式 `TS|SEQ|ROOM|ACTION|DATA\n`，与 C 版兼容
5. **快照格式**：JSON（`encoding/json`），每 20s 一次，原子写入
6. **恢复语义**：启动扫描 WAL，无 GAME_END 的房间挂起恢复表；玩家登录时逐个重连；首个重连者创建 gaming 房间启动游戏循环；等待 `RecoveryWaitTime`(30s) 期满前不判胜负
7. **C bug 修正**：`game_update_buffs` 发送的 BUFF_EXPIRED/BUFF_WARNING 缺少尾部 `\n`，Go 版发送正确终止的帧

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
- `C:\Users\Administrator\.tclaude\plans\melodic-rolling-fern.md` — 批准的架构方案
