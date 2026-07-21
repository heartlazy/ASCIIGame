# C → Go 移植清单（ASCII Battle Royale）

> 本文档由代码分析生成，作为 Go 版本架构设计与实现的依据。
> 源项目：C11 服务端（epoll + pthread + WAL）+ ncurses 客户端，约 7900 行。

---

## 1. 模块清单与职责

### 1.1 通用层 (common/)

| 文件 | 职责 | 关键导出 |
|------|------|--------|
| **protocol.h/c** | 自定义文本协议解析与构造 | 13 消息类型枚举；`Message` struct；`protocol_parse()`、`protocol_extract_message()`、25+ 构造函数 |
| **config.h** | 全局常量 + 原子变量声明 | 60+ 配置常数；`server_running`、`online_count` 原子变量 |

Go 替代：`config.go`（const + 类型）、`protocol.go`（struct + 方法）。

### 1.2 服务端 (server/)

| 文件 | 职责 | 关键数据结构 | 并发机制 |
|------|------|------------|---------|
| **main.c** | 主循环、信号处理、模块初始化 | — | epoll 事件循环，SIGINT/SIGTERM |
| **network.c/h** | epoll 封装、socket 操作 | — | EPOLLIN/EPOLLET/EPOLLHUP |
| **player.c/h** | 玩家管理、状态追踪、消息缓冲 | `Player`(fd,id,username,room_id,status,x/y,hp/atk/def,inventory,recv_buf) | 全局 `players[]` + `players_mutex`；每 Player 一把 `lock` |
| **room.c/h** | 房间管理、加入/离开、房间列表 | `Room`(id,name,players[],status,map,items,poison_radius,wal,game_thread) | 全局 `rooms_mutex`；每房间 `pthread_t game_thread` + `lock` |
| **game.c/h** | 游戏主循环线程、移动/攻击/道具/毒圈 | — | 游戏线程每 50ms tick 一次 |
| **map.c/h** | 地图生成、碰撞检测、毒圈计算 | `MapItem`(x,y,type,active) | 只读，无并发 |
| **handler.c/h** | 消息分发、各命令处理器 | — | 每连接主线程处理 |
| **wal.c/h** | Write-Ahead Log 持久化 | `WalManager`、`WalRecord` | `pthread_mutex_t lock` + `atomic_long sequence` |
| **snapshot.c/h** | 定期二进制快照 | `SnapshotData`、`SnapshotPlayer` | game_thread 内调用 |
| **recovery.c/h** | 崩溃恢复、WAL 重放 | `RecoveryState` | `recovery_map_lock` |
| **storage.c/h** | 用户账号、SHA256、统计 | `UserRecord`(username,password_hash,wins,losses,points) | 全局 `users[]` + mutex |
| **log.c/h** | 日志（DEBUG/INFO/WARN/ERROR） | — | 线程安全宏 |

依赖关系：
```
main.c
 ├─ network.c (epoll)
 ├─ player.c ── room.c ── game.c / wal.c / snapshot.c / recovery.c
 ├─ storage.c
 ├─ handler.c ── protocol.c
 ├─ map.c
 └─ log.c
```

### 1.3 客户端 (client/)

| 文件 | 职责 | 关键数据结构 | 并发机制 |
|------|------|------------|---------|
| **main.c** | 主循环、输入分发、状态管理 | — | `pthread_t recv_tid` 接收线程 |
| **network.c/h** | TCP 连接、消息收发 | — | 接收线程 poll()/blocking recv |
| **game.c/h** | 客户端游戏状态、渲染数据 | `GameState`(in_game,in_room,map[],players[],my_*,attack_effect) | `lock` + `atomic_int dirty` |
| **ui.c/h** | ncurses 渲染 | WINDOW*（status/map/msg/help） | ncurses 非线程安全，主线程独占 |

Go 替代：bubbletea/tview + goroutine + channel。

---

## 2. 通信协议规范（Go 版必须 100% 兼容，以复用 Python 测试）

**格式**：`CMD|arg1|arg2|...\n`
- 分隔符 `|`；终止符 `\n`；字段 ≤2048B；消息 ≤4096B；参数 ≤16 个。
- 参考：`common/protocol.h:1-277`、`common/protocol.c:71-159`。

### 客户端 → 服务端
| 命令 | 格式 |
|------|------|
| LOGIN | `LOGIN\|username\|password\n` |
| REGISTER | `REGISTER\|username\|password\n` |
| LIST_ROOMS | `LIST_ROOMS\|\n` |
| CREATE_ROOM | `CREATE_ROOM\|name\|max_players\n` |
| JOIN_ROOM | `JOIN_ROOM\|room_id\n` |
| LEAVE_ROOM | `LEAVE_ROOM\|\n` |
| READY | `READY\|\n` |
| MOVE | `MOVE\|direction\n`（U/D/L/R） |
| ATTACK | `ATTACK\|\n` |
| USE_ITEM | `USE_ITEM\|index\n` |
| CHAT | `CHAT\|message\n` |
| LOGOUT | `LOGOUT\|\n` |

### 服务端 → 客户端
| 命令 | 格式 |
|------|------|
| OK | `OK\|message\n` |
| ERR | `ERR\|code\|message\n` |
| ROOM_LIST | `ROOM_LIST\|id,name,count,max,status;...\n` |
| ROOM_INFO | `ROOM_INFO\|room_id\|name\|player_count\|max_players\|status\n` |
| PLAYER_JOIN | `PLAYER_JOIN\|player_id\|username\n` |
| PLAYER_LEAVE | `PLAYER_LEAVE\|player_id\n` |
| GAME_START | `GAME_START\n` |
| MAP_DATA | `MAP_DATA\|row1,row2,...\n` |
| GAME_STATE | `GAME_STATE\|ts\|players\|items\|poison_radius\n`（players: `id@x,y,hp;...`；items: `id@x,y,type;...`） |
| GAME_EVENT | `GAME_EVENT\|event_type\|data\n` |
| GAME_END | `GAME_END\|winner_id\|stats\n`（-1=平局） |
| CHAT_MSG | `CHAT_MSG\|sender\|message\n` |
| KICK | `KICK\|reason\n` |

**错误码**：1001-1099 协议 / 2001-2099 认证 / 3001-3099 房间 / 4001-4099 游戏。

> ⚠️ 建议先建立 **protocol 对标测试**（同输入比对 C 与 Go 的构造输出），再动其他模块。

---

## 3. 服务端核心机制（移植难点）

### 3.1 epoll → goroutine + net
- C：单 epoll 线程（`main.c:146-184`）+ 每房间一个 game_thread；非阻塞 + 边缘触发（`network.c:76-86`）。
- Go：`net.Listen` + 每连接一个 goroutine；用 `bufio.Scanner` 按行读消息；`context` 控制优雅关闭。
- ⚠️ 全局玩家/房间表需 `sync.RWMutex` 或 `sync.Map` 保护。

### 3.2 pthread → goroutine + mutex
| 组件 | C 保护 | 源 | Go 替代 |
|------|--------|----|--------|
| Player 管理 | `players_mutex` | player.c:14 | `sync.RWMutex` + `map[int]*Player` |
| Room 管理 | `rooms_mutex` + room.lock | room.c:19 | 两级锁保持粒度 |
| Game thread | 每房间 pthread + room.lock | room.h:48 | 每房间 goroutine + `time.Ticker(50ms)` |
| WAL | wal.lock + atomic seq | wal.h:34 | `sync.Mutex` + `atomic.Int64` |
| server_running | atomic_int | config.h | `atomic.Bool` 或 context |

### 3.3 WAL 持久化
- 格式（文本，**保持兼容**）：`TIMESTAMP|SEQUENCE|ROOM_ID|ACTION_TYPE|ACTION_DATA\n`
- 12 种动作：GAME_START / PLAYER_JOIN / PLAYER_LEAVE / MOVE / ATTACK / PICKUP / USE_ITEM / DAMAGE / PLAYER_DEATH / ITEM_SPAWN / POISON_SHRINK / GAME_END / CHECKPOINT。
- 缓冲 4KB，`WAL_SYNC_INTERVAL_MS`(1s) 定期 fsync；文件 `data/wal/room_<id>.wal`。
- Go：`os.File` + `bufio.Writer` + `time.Ticker` 定期 `Sync()`。

### 3.4 Snapshot / Recovery
- 快照：二进制 `[MAGIC "SNAP"][VERSION 1][TIMESTAMP][SnapshotData][SnapshotPlayer]*`；间隔 20s；文件 `data/wal/room_<id>.snap`。
- 恢复流程（`recovery.c`，`main.c:248` 启动时调用）：扫描 `data/wal/` → 加载最新快照 → 从快照序列号后重放 WAL → 重建房间 → 玩家登录时 `recovery_check_player()` 重连回游戏。
- Go：**快照采用 JSON**（决策已定，`encoding/json`，易调试/易演进；与 C 版快照不互通，但快照是内部文件，不影响协议兼容与 Python 测试）；WAL 保持文本兼容。

### 3.5 Storage
- `UserRecord` 128 字节固定记录：username[32] + password_hash[65](SHA256 hex) + wins/losses/points + reserved。
- 文件 `data/users.dat`，最多 10240 用户。
- Go：`crypto/sha256`（或升级 bcrypt）；建议改 JSON 存储。

### 3.6 状态机
**Player**（player.h:14-22）：DISCONNECTED → CONNECTED →(LOGIN) LOBBY →(JOIN) IN_ROOM →(READY) READY →(GAME_START) GAMING →(死) DEAD；任意态可 → DISCONNECTED。
**Room**（room.h:19-24）：WAITING →(全员准备/倒计时3s) STARTING → GAMING →(剩1人/5min超时) ENDED；全员离开 → WAITING。

### 3.7 Game 逻辑（game.c，50ms/tick，20 tick/s）
每 tick：更新 buff → 更新毒圈 + 毒伤 → 刷新道具 → 检查结束 → 广播 GAME_STATE。
- **移动**：冷却 200ms，1 格，撞墙 `#` 阻挡；WAL MOVE。
- **攻击**：冷却 1000ms，曼哈顿距离 ≤3，伤害 `atk-def`（≥1），护盾挡一次；WAL ATTACK。
- **道具**：背包 5 格；HEALTH(+30HP)、ATTACK(+10atk/10s)、SHIELD(挡1次)；移动到位置自动拾取。
- **毒圈**：60s 开始，每 30s 缩 1（初始 25，切比雪夫距离判定），圈外每秒 -5。
- **结束**：存活 ≤1 → 胜者 id；超 5min → -1 平局；否则 -2 继续。

---

## 4. 客户端 UI（ncurses → bubbletea 推荐）

窗口布局：STATUS(3行) / MAP(22行) / MSG(8行) / HELP(1行)（ui.c:19-22）。
颜色：自己@绿、敌@白、墙#、毒圈.红、道具+^*黄、HP 三档、攻击特效青（ui.h:17-25）。
界面：登录 → 大厅（C 创建 / J 加入 / L 列表 / Q 退出）→ 房间等待（R 准备 / T 聊天 / L 离开）→ 游戏中（WASD 移动 / J/空格 攻击 / 1-5 道具 / T 聊天 / Q 退出）。
`GameState`（client/game.h）为客户端全量状态镜像，含 dirty 标记驱动重绘。

---

## 5. 关键配置常数（common/config.h）

| 类别 | 常数 |
|------|------|
| 网络 | PORT 8888，MAX_EVENTS 64，RECV/SEND_BUFFER 4096 |
| 玩家 | MAX_PLAYERS 256，MAX_USERNAME 32，MAX_PASSWORD 64，MAX_INVENTORY 5 |
| 房间 | MAX_ROOMS 32，MAX_ROOM_PLAYERS 10，MIN 2，倒计时 3000ms |
| 地图 | 50×20，MAX_MAP_ITEMS 20 |
| 游戏 | TICK 50ms，MOVE_CD 200ms，ATK_CD 1000ms，ATK_RANGE 3，HP 100，ATK 15，DEF 5 |
| 毒圈 | 开始 60000ms，缩小间隔 30000ms，每秒伤害 5 |
| 道具 | 刷新 10000ms，血包 +30，攻击 buff +10/10000ms |
| 时长 | GAME_MAX_DURATION 300000ms |
| 协议 | MAX_ARGS 16，MAX_ARG_LEN 2048，MAX_MSG_LEN 4096 |
| WAL | dir data/wal，buffer 4096，sync 1000ms，retry 3，recovery wait 30000ms |
| 快照 | interval 20000ms |
| 存储 | users.dat，MAX_USERS 10240，record 128B，hash 65 |

---

## 6. 移植风险点

**高风险**：
1. 并发模型转换（epoll+pthread → goroutine）——用 `go test -race` 验证。
2. 协议兼容性——格式改动即 Python 测试失败，建对标测试。
3. 持久化一致性——WAL 保持文本格式，快照可转 JSON。
4. 网络分帧——用 `bufio.Scanner` 处理 `\n` 分隔，注意大字段（MAP_DATA）。
5. 游戏 tick 稳定性——GC 停顿可能影响 50ms tick，监控 p99，调 GOGC。

**中风险**：内存管理（malloc/free → GC，可用 sync.Pool）、信号处理（sigaction → signal.Notify + context）、文件 I/O 性能、时间精度（统一 `UnixMilli`）。

**低风险**：字符串（strtok_r → strings.Split，注意 UTF-8）、地图数组（固定数组 → 切片，加边界检查）、常数（const）。

---

## 7. 建议实现顺序与工作量预估

| 优先级 | 模块 | 难度 | 关键点 |
|--------|------|------|--------|
| P0 | protocol / config / log | 低 | 协议 100% 兼容 + 对标测试 |
| P1 | network(server) / player / room / storage / map | 中 | goroutine 模型、状态机、两级锁 |
| P2 | handler / game / wal / snapshot / recovery / main | 中-高 | 游戏循环、持久化、崩溃恢复 |
| P3 | client: network / game / ui(bubbletea) / main | 低-中 | TUI 迁移 |

总预估约 30-45 人天。

---

## 8. 测试策略

1. **协议对标**：同输入比对 C 与 Go 的消息构造输出。
2. **复用 Python 测试**：启动 Go 服务端，直接跑 `test/functional_test.py`、`stress_test.py`、`robustness_test.py`（无需改）。
3. **竞态检测**：`go build -race` / `go test -race`。
4. **性能基准**：BenchmarkProtocolParse / WALWrite / GameTick，确认 tick 延迟 p99 < 10ms。

---

## 9. 下一步

1. 建立 protocol 对标测试（前置）。
2. 实现 core：protocol / config / log / network / player / room。
3. 实现 game：tick 循环 + 全部游戏逻辑。
4. 实现持久化：wal / snapshot / recovery。
5. 实现 handler：全部命令处理器。
6. 跑 Python 测试验证兼容性。
7. 实现 client（可选 bubbletea 升级 UI）。
