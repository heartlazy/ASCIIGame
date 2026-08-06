# 测试说明文档

> 本文档整理了 `asciigame-go` 项目的全部自动化测试，说明每个测试的目的、覆盖场景与实现方式，供阅读代码时参考。
>
> 运行方式：
> ```bash
> go test ./internal/...                          # 全量运行（约 12s）
> go test -bench BenchmarkRecovery ./internal/server/  # 仅运行性能基准
> ```

---

## 一、协议层测试

**文件**：`internal/protocol/codec_test.go`

### TestFrameRoundtrip

**目的**：验证一个完整的 `GameState` 帧经过 `WriteFrame → ReadFrame` 往返后，所有字段保持一致。

**覆盖点**：
- 4 字节大端长度前缀分帧的编解码正确性
- `GameState` 中的时间戳、2 名玩家（含护盾、背包 5 格）、2 个道具、毒圈半径

**实现**：构造一个包含完整数据的 `*Frame`，写入 `bytes.Buffer`，再从中读取，逐字段断言（ID、HP、HasShield、Inventory、PoisonRadius）。

---

### TestEventOneofRoundtrip

**目的**：验证 `GameEvent` 中 9 种子事件（`AttackEvent`、`DamageEvent`、`KillEvent`、`ShieldEvent`、`AttackResultEvent`、`PickupEvent`、`PoisonEvent`、`BuffWarningEvent`、`BuffExpiredEvent`）均能正确往返。

**覆盖点**：protobuf `oneof event` 的所有分支

**实现**：遍历所有事件类型，逐一写入再读取，断言 `GetGameEvent() != nil` 且事件类型正确。

---

## 二、账号存储测试

**文件**：`internal/server/storage_test.go`

### TestStorageStats

**目的**：验证战绩（胜/负/积分）能正确写入 SQLite 并在重启后持久化。

**场景**：
- 注册 alice → 记录一胜（+1win, +10分）、一负（+1loss, +1分）→ 关闭连接 → 重开 DB → 读取验证
- 对不存在的用户调用 `updateStats`，不报错，不误创建

**实现**：使用 `t.TempDir()` 隔离数据库文件，两次开关 DB 验证持久化。

---

### TestStorageRegisterDuplicate

**目的**：验证重复注册返回 `-1`（用户名已存在），不覆盖已有记录。

**实现**：连续注册同名用户两次，断言第二次返回码为 -1。

---

### TestPasswordBcryptAndLegacyUpgrade

**目的**：验证新用户密码使用 bcrypt 哈希，且旧版 SHA-256 哈希的账号能正常登录并自动升级为 bcrypt。

**覆盖点**：
- 新注册账号的哈希以 `$2` 开头（bcrypt 标识）
- 正确密码 → verify 返回 0；错误密码 → 返回 -2；不存在用户 → 返回 -1
- 手动向 DB 插入 SHA-256 哈希 → verify 成功 → 哈希被原地升级为 bcrypt

**实现**：直接操作 `st.db.Exec` 插入旧格式哈希，验证升级后仍可登录。

---

## 三、双协议集成测试

**文件**：`internal/server/server_test.go`

### TestDualProtocolE2E

**目的**：验证 HTTP（Gin）+ TCP（protobuf）双协议的完整游戏流程端到端正常。

**流程**：
1. HTTP `POST /api/register` 注册 alice、bob
2. HTTP `POST /api/login` 获取 token + player_id
3. TCP 连接 → 发送 `Auth{token}` → 收到 `Ok`
4. HTTP `POST /api/rooms` 创建房间
5. HTTP `POST /api/rooms/:id/join` 加入房间
6. TCP 收到 `PlayerJoin` 通知（bob 加入时 alice 端收到）
7. HTTP `POST /api/rooms/ready` × 2 → 触发游戏开始
8. TCP 收到 `GameStart`、`GameState`（含 2 名玩家，字段验证）
9. TCP 发送 `Move` → 不报错
10. 等待攻击冷却后 TCP 发送 `Attack` → TCP 收到 `AttackEvent`（`GetAttack() != nil`）

**实现**：
- `startDualServer` 用 `httptest.NewServer(engine)` 启动 HTTP，用 `net.Listen("127.0.0.1:0")` 启动 TCP，均绑定随机端口，测试结束自动清理。
- `httpHelper` 封装 HTTP 客户端（含 token 注入）。
- `tcpHelper` 封装 TCP 连接（含 `auth`、`send`、`waitFor`）。

---

### TestHTTPAuth

**目的**：验证 HTTP Auth middleware 正确拒绝无效请求。

**场景**：
- 无 `Authorization` 头 → 401
- 携带无效 token `"invalid-token"` → 401

**实现**：直接用标准 `http.Client` 发送请求，断言响应状态码。

---

## 四、广播脏检测优化测试

**文件**：`internal/server/broadcast_bench_test.go`

> **背景**：服务端游戏循环每 50ms（20Hz）调用 `broadcastState`，但只有状态实际改变时才发送 `GameState` 帧（签名对比）。这三个测试量化该优化的实际效果。

**辅助工具**：
- `countingConn`：包装 `net.Conn`，用 `atomic.Int64` 统计实际读取字节数
- `setupBenchGame`：完整走完双协议建连+游戏启动流程，返回两个带计数的 TCP 连接
- `countFrames`：在指定时间窗口内统计收到的特定类型帧数量

---

### TestDirtyDetection_Idle

**目的**：验证空闲场景下（无任何操作）脏检测是否真正抑制了帧发送。

**步骤**：等待第一帧 `GameState` 到达 → 静默等待 2 秒 → 统计收到的 `GameState` 帧数。

**预期**：≤ 2 帧（优化后）vs ~40 帧（无优化时）

**实测**：`received 0 GAME_STATE frames` ✅

---

### TestDirtyDetection_Active

**目的**：验证有真实操作时帧数合理（每次移动只触发 1 次广播，而非持续 20Hz）。

**步骤**：启动后台 goroutine 每 210ms 发一次 `Move`（共 9 次） → 统计 2 秒内收到的 `GameState` 帧数。

**预期**：~9–20 帧（每次移动触发一次），远少于无优化的 ~40 帧

**实测**：`received 8 GAME_STATE frames` ✅

---

### TestDirtyDetection_TrafficBytes

**目的**：用字节数衡量空闲期流量节省。

**步骤**：等待第一帧 → 重置字节计数器 → 空闲 2 秒 → 读取 `countingConn.bytesRead`。

**预期**：< 2000 字节（优化后）vs ~8000 字节（无优化 × 40帧 × ~200B/帧）

**实测**：`Idle 2s traffic: 0 bytes` ✅

---

## 五、崩溃恢复基准测试

**文件**：`internal/server/recovery_bench_test.go`

> **背景**：服务端每 20s 保存一次快照并截断 WAL，使 WAL 始终只保留最近 20s 的记录；如果不用快照则 WAL 持续增长。这组测试对比两种策略的性能差异。

**辅助工具**：
- `generateWAL(tb, roomID, nRecords)`：生成指定条数的仿真 WAL 文件（含 GAME_START、PLAYER_JOIN、MOVE、ATTACK、DAMAGE、ITEM_SPAWN、POISON_SHRINK 等记录），模拟不同对局时长。

---

### BenchmarkRecovery_WALOnly

**目的**：量化纯 WAL 恢复（无快照）的时间复杂度随记录数的增长关系。

**参数**：records = 100 / 500 / 2000 / 6000（对应约 5s / 25s / 100s / 5min 的对局时长）

**实测结果**：

| 记录数 | 恢复耗时 |
|:------:|:-------:|
| 100 | 128 µs |
| 500 | 344 µs |
| 2000 | 1.15 ms |
| 6000 | 3.36 ms |

---

### BenchmarkRecovery_WithSnapshot

**目的**：量化快照后截断的 WAL（固定约 400 条）的恢复时间，模拟实际部署中最坏情况（上次快照到崩溃之间最多积累 20s 的记录）。

**实测结果**：`432 µs`（无论对局总时长多长，恢复时间恒定）

---

### TestRecoverySpeed_Comparison

**目的**：以人类可读的表格形式对比 WAL+快照 vs 纯 WAL 的恢复耗时（每个场景重复 100 次取平均）。

**实测输出**：

```
Scenario                         | Records | Time
---------------------------------|---------|--------
20s_post_snapshot (WAL+Snapshot) |     400 |  353 µs
1min_pure_WAL                    |    1200 |  687 µs
3min_pure_WAL                    |    3600 |  1.9 ms
5min_pure_WAL                    |    6000 |  3.2 ms
```

---

### TestWALFileSize_Comparison

**目的**：对比两种策略下 WAL 文件的磁盘占用大小。

**实测输出**：

```
Scenario                         | Records | WAL Size
---------------------------------|---------|----------
20s_post_snapshot (truncated)    |     400 |  18 KB
1min_no_snapshot                 |    1200 |  53 KB
3min_no_snapshot                 |    3600 | 160 KB
5min_no_snapshot                 |    6000 | 268 KB
```

---

## 六、崩溃恢复正确性测试

**文件**：`internal/server/recovery_correctness_test.go`

> 以下测试均通过手写 WAL 文本内容构造特定场景，验证 `replayWAL` 函数的解析和状态重建逻辑。

---

### TestRecoveryCorrectness_WALPlusSnapshot

**场景**：快照记录玩家初始位置后，WAL 继续记录了一次移动和一次伤害。

**验证**：
- alice 位置从 `(10,10)` → 移动后变为 `(11,10)` ✅
- alice HP 保持 80（快照值） ✅
- bob 从 90HP 受到 10 伤害后变为 80HP ✅
- 毒圈半径从 20 更新为 19 ✅
- 道具数量为 1 ✅

---

### TestRecoveryCorrectness_PureWAL

**场景**：从 `GAME_START` 开始的完整 WAL，包含两名玩家、道具生成、移动、拾取道具、使用道具、毒圈缩小、攻击造成伤害、玩家死亡。

**验证**：
- alice 连续移动两次 `(5,5)→(6,5)→(7,5)` ✅
- alice HP 仍为 100（使用了血包但已满血） ✅
- bob HP=0，状态为 `StatusDead` ✅
- 毒圈半径 = 24 ✅
- 活跃道具数 = 1（另一个被拾取） ✅

---

### TestRecoveryCorrectness_AtkBuffPersistence

**场景**：WAL 中记录 `USE_ITEM|pid=1,item=2,idx=0,atk_buff_remain=10000`（使用攻击药水）。

**验证**：
- alice.atk = 25（基础 15 + buff 10） ✅
- alice.atkBuffExpire > 0（buff 有过期时间，不会永久存在） ✅
- alice 背包为空（道具已被使用移除） ✅

> 此测试直接验证之前修复的 Bug：旧代码 replay `USE_ITEM` 时不设置 `atkBuffExpire`，导致恢复后 buff 永不过期。

---

### TestRecoveryCorrectness_CorruptSnapshot

**场景**：快照文件内容为乱码 `"CORRUPT DATA{{"`，WAL 文件完整有效。

**验证**：
- `snapshotLoad(7)` 返回 `nil`（损坏快照不可用） ✅
- `replayWAL(...)` 仍能成功返回有效状态 ✅
- alice、bob 均被正确恢复 ✅
- 房间名正确（`"Fallback"`） ✅

---

### TestRecoveryCorrectness_SnapshotJSON

**目的**：验证 JSON 快照格式的所有关键字段经序列化/反序列化后保持完整。

**验证字段**：房间名、毒圈半径、地图数据、玩家坐标/HP/ATK/护盾状态/背包/AtkBuffExpire。

**实现**：直接构造 `snapshotFile` 结构体 → `json.MarshalIndent` → `json.Unmarshal` → 逐字段断言。

---

## 附：测试覆盖范围汇总

| 测试文件 | 测试数 | 覆盖的核心特性 |
|---------|:------:|--------------|
| `codec_test.go` | 2 | protobuf 帧往返、GameEvent oneof |
| `storage_test.go` | 3 | SQLite 账号存储、bcrypt 升级 |
| `server_test.go` | 2 | HTTP+TCP 双协议端到端、Auth middleware |
| `broadcast_bench_test.go` | 3 | 脏检测帧数/流量节省 |
| `recovery_bench_test.go` | 2+2 | WAL 恢复速度/文件大小对比 |
| `recovery_correctness_test.go` | 5 | WAL 重放状态正确性、buff 持久化、容错降级 |
| **合计** | **17+2bench** | — |
