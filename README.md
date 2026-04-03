# 字符大乱斗 (ASCII Battle Royale)

基于 Linux 的多人实时竞技游戏，作为 Linux 系统编程课程大作业。

## 功能特性

- 2-6 名玩家在 50x20 ASCII 地图上实时对战
- 用户注册/登录系统
- 房间创建/加入/准备机制
- WASD 移动，J/空格攻击
- 道具系统：血包、攻击药水、护盾
- 毒圈系统：60秒后开始缩小
- WAL 持久化和崩溃恢复
- ncurses 终端界面

## 技术栈

- **服务端**: C11 + epoll + pthread + WAL
- **客户端**: C11 + ncurses + pthread
- **通信协议**: 自定义文本协议（管道分隔，换行结尾）

## 编译

```bash
# 编译服务端和客户端
make all

# 仅编译服务端
make server

# 仅编译客户端
make client

# 清理
make clean
```

## 运行

```bash
# 启动服务端（默认端口 8888）
./bin/server [port]

# 启动客户端
./bin/client [host] [port]
```

## 操作说明

### 大厅
- `C` - 创建房间
- `J` - 加入房间
- `L` - 列出房间
- `Q` - 退出

### 房间
- `R` - 准备/取消准备
- `L` - 离开房间
- `T` - 聊天
- `Q` - 退出

### 游戏中
- `W/A/S/D` 或方向键 - 移动
- `J` 或空格 - 攻击
- `1-5` - 使用道具
- `T` - 聊天
- `Q` - 退出

## 协议格式

```
客户端 -> 服务端:
LOGIN|username|password\n
REGISTER|username|password\n
CREATE_ROOM|name|max_players\n
JOIN_ROOM|room_id\n
MOVE|direction\n  (U/D/L/R)
ATTACK|\n
CHAT|message\n

服务端 -> 客户端:
OK|message\n
ERR|code|message\n
GAME_STATE|timestamp|players|items|poison_radius\n
GAME_EVENT|type|data\n
```

## 测试

### 前置条件

1. 确保已安装 Python 3.6+
2. 编译并启动服务器

```bash
make server
./bin/server &
```

### 测试命令

```bash
# 运行所有测试（健壮性 + 压力 + 功能）
make test

# 分别运行各类测试
make test-robustness   # 健壮性测试 (10项)
make test-stress       # 压力测试 (3项)
make test-functional   # 功能测试 (8项)

# 性能监控
make monitor
```

### 直接运行Python脚本

```bash
# 健壮性测试
python3 test/robustness_test.py

# 压力测试（可自定义参数）
python3 test/stress_test.py --clients 100 --duration 60

# 功能测试
python3 test/functional_test.py

# 综合测试
python3 test/run_all_tests.py

# 结果分析
python3 test/analyze_results.py
```

### 测试内容

| 测试类型 | 测试项数 | 说明 |
|---------|---------|------|
| 健壮性测试 | 10 | 畸形消息、连接洪水、缓冲区溢出等 |
| 压力测试 | 3 | 并发连接、快速连接、消息洪水 |
| 功能测试 | 8 | 注册、登录、房间、聊天等 |

### 测试输出

测试完成后生成以下文件：
- `robustness_test_results.json` - 健壮性测试结果
- `stress_test_results.json` - 压力测试结果
- `functional_test_results.json` - 功能测试结果
- `test_report_YYYYMMDD_HHMMSS.md` - 综合测试报告

### 性能指标参考

| 指标 | 优秀 | 良好 | 需改进 |
|------|------|------|--------|
| 连接成功率 | ≥99% | ≥95% | <95% |
| 平均延迟 | <20ms | <50ms | ≥50ms |
| P99延迟 | <100ms | <200ms | ≥200ms |
| 错误率 | <1% | <5% | ≥5% |

## 目录结构

```
├── server/          # 服务端源码
├── client/          # 客户端源码
├── common/          # 公共模块
├── data/            # 数据文件
│   └── wal/         # WAL 日志
├── test/            # 测试代码
│   ├── robustness_test.py    # 健壮性测试
│   ├── stress_test.py        # 压力测试
│   ├── functional_test.py    # 功能测试
│   ├── run_all_tests.py      # 综合测试运行器
│   ├── analyze_results.py    # 结果分析
│   └── monitor_performance.py # 性能监控
├── scripts/         # 工具脚本
│   ├── tune_server.py        # 服务器调优脚本
│   ├── apply_sysctl.sh       # 系统优化脚本（自动生成）
│   └── restore_sysctl.sh     # 恢复脚本（自动生成）
├── docs/            # 文档
├── Makefile
└── README.md
```

## 服务器调优

### 调优脚本使用

调优脚本会自动检测硬件配置，生成优化建议和系统优化脚本。

```bash
# 运行调优分析
make tune

# 或直接运行 Python 脚本
python3 scripts/tune_server.py
```

### 调优输出示例

```
============================================================
服务器调优分析报告
============================================================

【硬件检测】
  CPU核心数: 2
  内存大小: 3.8 GB
  文件描述符限制: 1024
  TCP监听队列: 4096

【配置级别】: MEDIUM

【应用配置建议】(common/config.h)
  #define MAX_EVENTS          64
  #define MAX_PLAYERS         256
  #define MAX_ROOMS           32
  #define TICK_INTERVAL_MS    50

【系统配置建议】
  ⚠️  文件描述符限制过低，建议: ulimit -n 16384
  ✅ TCP监听队列正常

【预期性能】
  最大并发连接: 256
  游戏刷新率: 20 tick/s
  最大房间数: 32
============================================================
```

### 配置级别说明

| 硬件配置 | CPU | 内存 | 配置级别 | MAX_PLAYERS | 刷新率 |
|---------|-----|------|---------|-------------|--------|
| 低配 | 1核 | 1GB | LOW | 64 | 10 tick/s |
| 中配 | 2核 | 2GB+ | MEDIUM | 256 | 20 tick/s |
| 高配 | 4核+ | 4GB+ | HIGH | 512 | 30 tick/s |

### 应用系统优化

调优脚本会自动生成系统优化脚本：

```bash
# 应用系统优化（需要 root 权限）
sudo scripts/apply_sysctl.sh

# 恢复默认配置
sudo scripts/restore_sysctl.sh
```

### 完整调优流程

```bash
# 1. 运行调优分析
make tune

# 2. 根据建议修改 common/config.h（如果需要）

# 3. 应用系统优化
sudo scripts/apply_sysctl.sh

# 4. 重新编译
make clean && make server

# 5. 运行压力测试验证
make test-stress
```

