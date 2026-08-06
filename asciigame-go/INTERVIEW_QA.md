# 面试问答整理

> 基于简历中三段项目描述，整理高频面试问题及参考回答。
> 代码均来自 `asciigame-go` 项目，可直接展示源码印证。

---

## 一、双协议分层架构设计

### Q1：为什么不全用 HTTP，或者全用 TCP？

**回答：**

这两种极端都有明显的问题。全用 HTTP 的话，游戏状态需要客户端轮询，延迟和无效请求量无法接受——每秒 20 次轮询本身就是负担，还没有服务器主动推送能力。全用 TCP 的话，注册、登录、创建房间这类低频管理操作要自己实现请求-响应语义，并且对接外部系统（排行榜、用户系统）非常不方便。

拆分的依据是**访问模式**：
- 大厅操作（登录/创房/准备）低频、有明确响应、无状态，天然适合 HTTP 的请求-响应模型，后续扩展社交功能也直接复用 Gin 路由。
- 游戏帧（Move/Attack/GameState）每 50ms 一次、服务器主动推送、时延敏感，必须 TCP 长连接。

实现上 HTTP 和 TCP 两个服务器在同一进程里并行启动（`:8080` 和 `:8888`），共享同一份内存中的玩家/房间状态，没有跨进程通信开销。

---

### Q2：HTTP 和 TCP 之间如何共享玩家状态？引入了什么同步问题？

**回答：**

两个协议层共享同一个 `Server` 对象，里面有：
- `players map[int]*Player`，用 `pmu sync.RWMutex` 保护
- `rooms map[int]*Room`，用 `rmu sync.RWMutex` 保护
- 每个 `Player` 和 `Room` 各有自己的 `mu sync.Mutex`

HTTP handler 和 TCP handler 都是 goroutine，都会并发读写这些结构。关键的锁规则是：**锁顺序固定为 `room.mu` → `player.mu`，注册表锁（`pmu/rmu`）是叶子锁**，持有它的时候不允许再获取其他锁，这样彻底避免了死锁。

比如 HTTP 的 `POST /api/rooms/ready` 和 TCP 的 `broadcastState` 都会访问房间内的玩家列表，前者通过 HTTP goroutine 执行，后者在游戏 tick goroutine 里，两者都要先拿 `room.mu`，再拿各个 `player.mu`，顺序一致所以不会死锁。

---

### Q3：Token 鉴权是怎么设计的？有没有安全隐患？

**回答：**

流程是：HTTP `POST /api/login` 验证密码后生成 token（格式 `player_id:16字节随机hex`），存入服务器内存的 `tokenStore`（本质是 `map[string]*Player`）并返回给客户端。客户端 TCP 连接后第一帧发送 `Auth{token}`，服务器验证后将 TCP 连接绑定到对应的 `Player` 对象，后续游戏帧就不用再鉴权了。

已知的局限：
1. Token 存在内存里，服务器重启后所有 token 失效，需要重新登录。
2. 没有过期机制，登出或断线时才清除。
3. 没有加盐，理论上可以枚举（虽然 16 字节随机已经够大）。

对于这个规模的游戏服务，这些都是合理的权衡。生产环境可以换成有签名和过期的 JWT，接口不变，只替换 `tokenStore` 的实现。

---

### Q4：如果 HTTP Ready 触发了游戏开始，TCP 连接还没建立怎么办？

**回答：**

这个边界情况确实需要处理。HTTP `POST /api/rooms/ready` 调用时，如果 `allReady()` 触发，它会启动游戏 tick goroutine（`gameLoop`），后者会开始通过 TCP 广播 `GameStart` 帧。

如果玩家的 TCP 连接还没建立，`broadcastState` 里调用 `p.Send(frame)` 时，会尝试往 `player.out` channel 写帧，但因为没有 TCP 连接绑定，`writeLoop` 还没起来消费这个 channel，channel 会满然后触发 `p.shutdown()`（踢掉连接）。

正确的使用姿势是：客户端应该在 HTTP login 之后立即建立 TCP 连接（发 Auth），再调用 HTTP Ready。实际上这也是合理的业务约束——你想打游戏就先把游戏连接建好。如果要做得更健壮，可以在 `httpReady` 里判断玩家的 TCP 连接是否已就绪，否则返回 400。

---

## 二、广播流量脏检测优化

### Q5：脏检测的具体实现是什么？为什么排除时间戳？

**回答：**

每次 `broadcastState` 执行时，先把当前的 `GameState`（玩家位置/HP/道具/毒圈，**不含 timestamp**）用 `proto.Marshal` 序列化成字节数组作为"签名"，与上次广播时存储的签名 `lastStateSig` 做 `bytes.Equal` 比较，相同就直接 return，不发帧。

排除时间戳的原因很关键：时间戳每一帧都不同，如果把它包含进签名，签名永远不可能相等，优化就完全失效了。我们关心的是**可观测状态**有没有变化——玩家有没有移动、受伤、拾取道具，毒圈有没有缩小。时间戳只是给客户端做延迟测量用的元数据，不代表游戏状态变化。

代码里是这样实现的：
```go
sig, err := proto.Marshal(&protocol.GameState{
    Players: players, Items: items, PoisonRadius: int32(poison),
    // Timestamp excluded — it changes every tick regardless
})
if err == nil {
    if r.lastStateSig != nil && bytes.Equal(sig, r.lastStateSig) {
        return // skip broadcast
    }
    r.lastStateSig = sig
}
r.broadcast(protocol.NewGameState(timestamp, players, items, int32(poison)))
```

---

### Q6：用 protobuf 序列化做签名，有没有性能问题？有更轻量的方案吗？

**回答：**

有一点额外开销，但在这个场景下是合理的。protobuf Marshal 的时间复杂度是 O(状态大小)，一个有 10 名玩家的房间，状态大约几百字节，Marshal 耗时在微秒级，相比 50ms 的 tick 周期完全可以忽略。

更轻量的方案有：
- **Hash**：对序列化字节做 CRC32 或 xxhash，只存 4-8 字节的哈希值，内存更省，但有极小的哈希冲突概率（会偶尔漏发一帧，对游戏状态影响基本可以忽略）。
- **增量标脏**：每次状态改变（移动/受伤/拾取）时设一个脏标志位，tick 时检查标志而不是序列化比较，比较快但需要在每个状态修改点显式设标志，容易遗漏。
- **字段级比较**：逐字段比较 players 数组，更精确但代码量大。

选用 protobuf 序列化签名的原因是：protobuf 保证相同结构相同内容的序列化结果是确定性的（deterministic），不需要自己维护比较逻辑，代码简洁且和协议层天然集成。

---

### Q7：脏检测会不会导致客户端状态不一致？有什么兜底机制？

**回答：**

不会导致不一致，因为我们保证：**任何真实的状态变化都会触发广播**。脏检测只跳过"什么都没发生"的 tick。

保证机制有两个：

1. **事件帧不受脏检测影响**：移动、攻击、拾取、毒圈缩小这些都通过单独的 `GameEvent` 帧立即广播（`broadcastEvent`），不经过脏检测路径。客户端即使没有收到 `GameState`，也能从事件帧知道发生了什么。

2. **签名重置**：每次游戏开始（`startGame`）和结束（`endGame`）时 `lastStateSig` 清零，下一帧一定会广播，避免跨局状态残留。

唯一的场景是：如果客户端因网络问题丢帧，它可能短暂错过某个状态。但因为 `GameState` 是全量快照（不是增量 diff），下次收到帧时会完整同步，不存在累积漂移的问题。

---

### Q8：20Hz 是怎么确定的？为什么不做成可配置的更高频率？

**回答：**

20Hz（50ms/帧）是综合考量的结果：

- **游戏机制决定**：移动冷却 200ms、攻击冷却 1000ms，玩家动作间隔远大于 50ms，更高频率的广播对游戏体验没有帮助。
- **带宽**：单房间 10 玩家的 `GameState` 帧约 200-400 字节，20Hz 下约 8KB/s，10 个并发房间也才 80KB/s，完全在合理范围内。
- **CPU**：每 tick 要扫描玩家、构造帧、序列化，更高频率的 tick 在多房间场景下 CPU 开销线性增长。

20Hz 是实时游戏的常见选择（和 Minecraft 服务器的默认 TPS 一致）。如果要做格斗游戏类需要帧精准的场景，可能需要提到 60Hz，但那时候脏检测的作用就相对减弱了。

---

## 三、WAL + 快照的崩溃恢复机制

> **重要前提（务必理解，否则容易答错）**：本项目的恢复路径**只读 WAL，不读快照文件**（`recoverRoom` 只调用 `replayWAL`，`snapshotLoad` 未参与恢复流程）。快照的真正作用是**在保存时触发 WAL 截断并重写一份完整状态**，从而让 WAL 保持小巧且自包含。简历中"11 倍加速"的本质是：**快照带来的 WAL 截断**让待重放记录从 6000 条降到 400 条。

---

### Q9：WAL 的格式是什么？为什么用文本格式而不是二进制？

**回答：**

WAL 格式是每行一条记录：
```
TIMESTAMP|SEQUENCE|ROOM_ID|ACTION_TYPE|ACTION_DATA\n
```

例如：
```
1722480000000|42|3|MOVE|pid=1,dir=R,ox=10,oy=10,nx=11,ny=10
1722480000050|43|3|DAMAGE|atk=1,vic=2,dmg=10,hp=90
```

共 13 种 action 类型（GAME_START / PLAYER_JOIN / PLAYER_LEAVE / MOVE / ATTACK / PICKUP / USE_ITEM / DAMAGE / PLAYER_DEATH / ITEM_SPAWN / POISON_SHRINK / GAME_END / CHECKPOINT）。

选文本格式有几个实际考量：
1. **可调试**：崩溃后可以直接用 `cat`/`grep` 看 WAL 内容，定位问题快。
2. **与 C 版兼容**：项目是从 C 移植过来的，保持文本格式让两个版本的工具链可以互通。
3. **追加友好**：文本的行追加是最简单的 append-only 模式，`Sync()` 语义清晰。

代价是略大于二进制（同样内容可能多 30-50%），但配合快照截断机制，实际文件始终维持在 18KB 左右。

---

### Q10：fsync 的频率是怎么设计的？如果 fsync 太频繁会有什么影响？

**回答：**

WAL 用 `bufio.Writer` 缓冲，每隔 `WalSyncIntervalMS`（1000ms）触发一次 `Flush() + f.Sync()`，也在游戏开始/结束等关键节点强制同步。实现在 `wal.write()` 里：

```go
if now := nowMS(); now-w.lastSync >= config.WalSyncIntervalMS {
    w.flushLocked()  // bufio.Writer.Flush() + f.Sync()
    w.lastSync = now
}
```

1 秒一次意味着崩溃时**最多丢失 1 秒内的操作**，对这个游戏来说可以接受（1 秒内大约 5 次移动、1 次攻击）。

如果 fsync 更频繁（比如每条记录都 fsync）：
- 在 HDD 上每次 fsync 约 5-10ms，20Hz 游戏每帧都 fsync 会直接让 tick 超时。
- 即使 SSD，频繁 fsync 也会触发写入放大，降低磁盘寿命。

如果更少（比如 10 秒一次）：崩溃丢失数据更多，恢复后玩家状态差异更大，体验更差。

1 秒是一个合理的平衡点，参考 etcd 的 WAL 策略（批量 + 周期性 fdatasync）。

---

### Q11：快照保存和 WAL 截断是怎么协调的？中间崩溃会不会出问题？

**回答：**

`snapshotSave` 的执行顺序是：

1. `writeJSONAtomic()` — 先写临时文件 `room_N.snap.tmp`，再 `os.Rename` 原子替换 `room_N.snap`
2. 更新 `lastSnapshotTime`
3. **此后才** `w.truncate()` 清空 WAL
4. 向新 WAL 写入 `CHECKPOINT`（房间名/毒圈）+ 每个玩家的 `PLAYER_JOIN`（完整状态）+ 所有道具的 `ITEM_SPAWN` + `POISON_SHRINK`
5. `w.sync()` 强制落盘

关键设计：**先原子写快照，再截断 WAL**。分析各个崩溃窗口：

| 崩溃时机 | 结果 |
|---------|------|
| 步骤 1 之中 | `.tmp` 是垃圾文件，旧 `.snap` 完好，WAL 完整 → 正常恢复 |
| 步骤 1-3 之间 | 新快照完整，WAL 未截断仍完整 → 正常恢复（重放略多） |
| 步骤 3-5 之间 | **这是唯一有风险的窗口**：WAL 已被清空但完整状态还没写完，若此刻崩溃会丢失该房间的可恢复数据 |

第三种情况是当前实现的一个已知缺陷。更严谨的做法是**先写新 WAL 再删旧 WAL**（类似双缓冲/WAL 分段），或者在截断前确保快照可作为恢复源。我在项目里选择了较简单的实现，因为这个窗口极短（几个 `bufio` 写入，无 fsync，微秒级），且崩溃后玩家可以重新开局——对课程项目规模是可接受的权衡。这也是我如果重做会优先改进的点。

---

### Q12：为什么 WAL 里要存完整的玩家状态（而不只记录操作）？

**回答：**

因为 WAL 被截断后，必须自包含才能独立恢复。

如果只记录增量操作，重放必须从 `GAME_START` 开始，那 WAL 就永远不能截断，会无限增长。截断的前提是：**截断点要有一份完整状态**。这就是快照保存时向新 WAL 写入 `CHECKPOINT + 每玩家 PLAYER_JOIN + 所有 ITEM_SPAWN + POISON_SHRINK` 的原因——这几条记录合起来等价于一个"WAL 内嵌的快照"，后续增量操作在此基础上重放。

有个值得一提的细节：`CHECKPOINT` 记录本身只存 `snapshot_time/room_name/poison_radius`，**真正的玩家状态在紧随其后的 `PLAYER_JOIN` 记录里**。`replayWAL` 处理 `PLAYER_JOIN` 时用 username 做 upsert（`applyPlayerJoin`），所以重复的 PLAYER_JOIN 会覆盖而非追加，实现了"状态重置"的语义。

另一个踩过的坑是**攻击力 buff 的过期时间**。buff 持续 10 秒，如果只记录"使用了攻击药水"，重放时无法知道还剩多少时间。所以 WAL 记录里存的是 `atk_buff_remain`（剩余毫秒数），恢复时计算 `atkBuffExpire = nowMS() + remain`。这个 Bug 我实际遇到过——早期版本恢复后 buff 永不过期，因为 replay 时只设了 `atk` 数值却没设过期时间戳。

---

### Q13：30 秒恢复等待窗口是什么？为什么需要它？

**回答：**

服务器重启后重建的房间会带上恢复标记（`isRecovery = true`，`expectedPlayers = 存活人数`）。`checkEnd()` 里的逻辑是：如果当前在线人数 < 预期人数，且距恢复开始不足 `RecoveryWaitTime`（30 秒），就返回"继续"，暂缓胜负判定。

这是为了防止一个具体场景：3 人对战中崩溃，重启后只有 1 人先重连。如果立刻按"只剩 1 人存活"判定胜利，另外 2 人还在重连路上就收到游戏结束，体验很差。等待 30 秒给了所有人重新登录的时间。

超过 30 秒仍未到齐的，清除恢复标记按正常规则判定——未重连的视为放弃。

---

### Q14：如果快照文件损坏，恢复还能正常工作吗？

**回答：**

能，而且这里有个反直觉的点：**因为恢复路径本来就不读快照**。

`recoverRoom` 的实现只做三件事：检查 WAL 里有没有 `GAME_END`（有就说明正常结束，清理文件）、调 `replayWAL` 重建状态、检查存活人数是否 > 1。整个过程不涉及 `snapshotLoad`。

所以快照损坏的影响是：
- 恢复本身**完全不受影响**（WAL 自包含）
- 唯一损失是那个 `.snap` 文件占用的磁盘空间

我写了 `TestRecoveryCorrectness_CorruptSnapshot` 验证这一点：写入乱码快照 + 有效 WAL，断言 `snapshotLoad` 返回 nil 但 `replayWAL` 仍能正确恢复玩家和房间名。

补充一下：既然恢复不读快照，快照存在的意义是什么？它是**截断 WAL 的触发器和审计记录**——保存快照这个动作顺带把 WAL 截断并重写完整状态，这才是性能收益的来源。快照文件本身可以看作是"给运维看的状态转储"，或者为将来支持"从快照冷启动"预留的能力。

---

### Q15：这套机制和数据库的 Redo Log / Checkpoint 有什么相似和不同？

**回答：**

相似点：
- WAL 的 append-only + 周期性 fsync 和数据库 Redo Log 本质相同：先写日志再改内存，靠日志保证持久化。
- 快照触发 WAL 截断，对应数据库的 checkpoint 机制——周期性固化状态，回收不再需要的日志空间。
- 都需要"日志自包含"才能截断，我用 CHECKPOINT + PLAYER_JOIN 实现，数据库用脏页刷盘 + LSN 记录实现。

不同点：
- **恢复终点不同**：数据库 crash recovery 后能自动回到崩溃前的完整一致状态；我们恢复后是"半成品"，必须等玩家重连才能继续，因为游戏状态还包含未持久化的客户端连接。
- **持久化强度不同**：数据库事务提交必须 fsync（否则违反 D），我们是 1 秒批量 fsync，允许丢失最近 1 秒——游戏可以接受，数据库不行。
- **没有 MVCC 需求**：我们只需要最新状态，不需要多版本快照读，省掉了版本链和可见性判断的复杂度。
- **没有 undo**：数据库需要 undo log 回滚未提交事务；游戏操作一旦执行就是既定事实，不存在回滚。

总的说这是"够用原则"的设计：借用了数据库最核心的 WAL + checkpoint 思路，砍掉了 ACID 中游戏场景不需要的部分。

---

## 四、并发与工程实践（延伸追问）

> 这部分不在简历正文里，但面试官看到"Go 游戏服务器"几乎一定会追问。

### Q16：一个房间的游戏循环是怎么跑的？为什么用 Ticker 而不是 Sleep？

**回答：**

每个房间在 `startGame` 后启动一个独立 goroutine 跑 `gameLoop`，用 `time.NewTicker(50ms)` 驱动。每 tick 依次执行：更新 buff → 毒圈缩小判定 → 毒圈伤害 → 刷新道具 → 道具过期 → 保存快照（条件满足时）→ 胜负判定 → 广播状态。

一开始我用的是 `time.Sleep(50ms)`，后来改成 `Ticker`，原因是：

- `Sleep(50ms)` 的实际周期是 `50ms + 本次 tick 的处理耗时`，会**累积漂移**。如果每 tick 处理花 5ms，实际周期变成 55ms，20Hz 掉到 18Hz，长时间运行后游戏节奏明显变慢。
- `Ticker` 按固定时间点触发，处理耗时不影响下次触发时刻。而且如果某次处理超过 50ms，Ticker 会**丢弃**错过的 tick 而不是堆积补偿——这正是游戏想要的行为（宁愿掉帧也不要突然快进）。

---

### Q17：慢客户端会不会拖慢整个房间？怎么处理背压？

**回答：**

这是我实际踩过的一个坑。每个 Player 有一个 `out chan *protocol.Frame`（缓冲 256）和一个专职的 `writeLoop` goroutine 消费它，这样广播时不会在锁内做 socket I/O。

但最初 `Send` 的实现是阻塞写 channel：
```go
select {
case p.out <- f:
case <-p.done:
}
```
问题是：如果某个客户端不读数据（网络卡死但 TCP 连接没断），256 个缓冲填满后 `Send` 就**阻塞了调用方**。而调用方往往是房间的 `gameLoop` goroutine——一个慢客户端会冻结整个房间所有玩家的游戏。

修复方案是加 `default` 分支，改成非阻塞：
```go
select {
case p.out <- f:
case <-p.done:
default:
    p.shutdown()  // 缓冲满 = 客户端已经落后 ~13 秒，直接踢
}
```
缓冲满意味着客户端落后了 256 帧（约 13 秒），基本可以判定已经掉线。直接关闭连接，读 goroutine 会解除阻塞并走正常的清理流程。这是典型的"快速失败优于拖累全局"。

---

### Q18：锁的设计是怎样的？怎么保证不死锁？

**回答：**

三层锁：
1. `Server.pmu` / `Server.rmu`（RWMutex）保护玩家表和房间表
2. `Room.mu` 保护房间内状态（成员列表、道具、毒圈、WAL 指针）
3. `Player.mu` 保护玩家字段（坐标、HP、背包、buff）

死锁预防靠两条硬性规则：
- **固定锁顺序**：需要同时持有时，一律 `room.mu` → `player.mu`，绝不反向。
- **注册表锁是叶子锁**：持有 `pmu`/`rmu` 时不允许再获取任何其他锁，用完立即释放。

另外还有一条实践约束：**socket 写入必须在锁外**。`broadcast` 的实现是先在 `room.mu` 内快照出成员列表，释放锁后再逐个 `Send`——因为 `Send` 内部可能触发 `shutdown()`，如果在锁内调用会有重入风险。

我还做了一次静态并发审查（因为 Windows 无 cgo 跑不了 `-race`），发现了一个真实的数据竞争：`snapshotSave` 在锁外读 `r.wal` 字段，而 `destroyRoom` 会在锁内把它置 nil。修复方式是在锁内把指针捕获到局部变量再用。

---

### Q19：Room 里为什么存 `[]*Player` 而不是 `[]int` 玩家 ID？

**回答：**

最早的实现是 `playerIDs [10]int`（照搬 C 版的设计），每次用到玩家时通过 `findPlayerByID(id)` 查全局 map。后来改成了 `members [10]*Player` 直接存指针。

改的原因是热路径开销：`gameLoop` 每 tick 要遍历玩家做 buff 更新、毒圈伤害、胜负判定、状态广播，4 处地方各自都要 `make([]int, ...)` 分配一次切片 + 对每个玩家调一次 `findPlayerByID`（每次都要抢 `pmu.RLock`）。20Hz × 4 处 × N 玩家的重复查表和分配完全没必要。

改成指针后，`membersLocked()` 在房间锁内直接返回 `[]*Player`，省掉了每 tick 的 map 查找和注册表锁竞争。这次重构删了 124 行加了 56 行，同时保留了原来的 slot 语义（nil 表示空位），因为崩溃恢复逻辑依赖固定槽位。

---

### Q20：项目里的测试是怎么组织的？怎么验证这些优化真的有效？

**回答：**

分三类：

**协议/单元层**：`codec_test.go` 验证 protobuf 帧和 9 种 GameEvent oneof 的序列化往返；`storage_test.go` 验证 SQLite 账号存储、战绩累加、bcrypt 升级路径。

**集成层**：`server_test.go` 的 `TestDualProtocolE2E` 走完整流程——HTTP 注册/登录 → TCP Auth → HTTP 创房/加入/准备 → TCP 收 GameStart/GameState → TCP 发 Move/Attack → 收 AttackEvent。用 `httptest.NewServer` + `net.Listen(":0")` 随机端口，测试间完全隔离。

**优化效果验证**（这是我特意做的，为了让简历数字有依据）：
- 脏检测：写了个 `countingConn` 包装 `net.Conn` 统计字节数，测量空闲 2 秒的收帧数和流量——实测 0 帧 / 0 字节，对照理论值 40 帧 / 8KB。
- 恢复性能：写了 `generateWAL(tb, roomID, nRecords)` 生成不同规模的仿真 WAL（100/500/2000/6000 条），用 `go test -bench` 测 `replayWAL` 耗时，得到 128µs → 3.36ms 的线性增长曲线，对比截断后的 400 条只需 350µs。
- 恢复正确性：5 个用例覆盖 WAL+快照场景、纯 WAL 全量重放、buff 过期持久化、快照损坏降级、快照 JSON 字段完整性。

一共 17 个测试 + 2 个 benchmark。我认为**能量化的优化才值得写进简历**，所以专门补了这套测试。
