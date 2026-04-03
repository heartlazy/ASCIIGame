# ASCII Battle Royale 测试文档

## 测试文件结构

```
test/
├── robustness_test.py    # 健壮性测试 (10项)
├── stress_test.py        # 压力测试 (3项)
├── functional_test.py    # 功能测试 (8项)
├── run_all_tests.py      # 综合测试运行器
├── analyze_results.py    # 结果分析工具
├── monitor_performance.py # 性能监控工具
├── test_config.json      # 测试配置
└── TEST_README.md        # 本文档
```

## 快速开始

```bash
# 1. 编译并启动服务器
make server
./bin/server &

# 2. 运行所有测试
make test

# 3. 查看测试报告
cat test_report_*.md
```

## Makefile 测试命令

| 命令 | 说明 |
|------|------|
| `make test` | 运行所有测试 |
| `make test-robustness` | 仅运行健壮性测试 |
| `make test-stress` | 仅运行压力测试 |
| `make test-functional` | 仅运行功能测试 |
| `make monitor` | 启动性能监控 |

## 直接运行Python脚本

```bash
# 健壮性测试
python3 test/robustness_test.py

# 压力测试 (可自定义参数)
python3 test/stress_test.py --clients 100 --duration 60

# 功能测试
python3 test/functional_test.py

# 运行所有测试
python3 test/run_all_tests.py

# 分析结果
python3 test/analyze_results.py

# 性能监控
python3 test/monitor_performance.py --duration 60
```

## 测试内容

### 健壮性测试 (10项)
- 畸形消息处理
- 连接洪水攻击
- 慢速客户端
- 不完整消息
- 快速断开连接
- 同一用户多次登录
- 无效协议序列
- 缓冲区溢出
- 特殊字符处理
- 竞态条件

### 压力测试 (3项)
- 并发连接测试
- 快速连接/断开测试
- 消息洪水测试

### 功能测试 (8项)
- 用户注册
- 用户登录
- 房间操作
- 加入房间
- 游戏准备
- 聊天功能
- 并发用户
- 错误处理

## 定量指标

| 指标 | 优秀 | 良好 | 需改进 |
|------|------|------|--------|
| 连接成功率 | ≥99% | ≥95% | <95% |
| 平均延迟 | <20ms | <50ms | ≥50ms |
| P99延迟 | <100ms | <200ms | ≥200ms |
| 错误率 | <1% | <5% | ≥5% |

## 输出文件

测试完成后生成:
- `robustness_test_results.json` - 健壮性测试结果
- `stress_test_results.json` - 压力测试结果
- `functional_test_results.json` - 功能测试结果
- `test_report_YYYYMMDD_HHMMSS.md` - 综合测试报告
