# Makefile - 字符大乱斗 (ASCII Battle Royale)

CC = gcc
CFLAGS = -std=c11 -Wall -Wextra -D_GNU_SOURCE
CFLAGS_DEBUG = -g -O0 -DDEBUG

SERVER_LIBS = -pthread
CLIENT_LIBS = -pthread -lncursesw

# 目录
BIN_DIR = bin

# 服务端源文件
SERVER_SRCS = server/main.c server/network.c server/player.c server/room.c \
              server/game.c server/map.c server/storage.c server/wal.c \
              server/recovery.c server/handler.c server/log.c server/snapshot.c \
              common/protocol.c

# 客户端源文件
CLIENT_SRCS = client/main.c client/network.c client/game.c client/ui.c \
              common/protocol.c

# 目标
SERVER_BIN = $(BIN_DIR)/server
CLIENT_BIN = $(BIN_DIR)/client

.PHONY: all server client clean dirs test test-robustness test-stress test-functional monitor tune

all: dirs server client

dirs:
	@mkdir -p $(BIN_DIR)
	@mkdir -p data/wal

server: dirs
	$(CC) $(CFLAGS) $(CFLAGS_DEBUG) -I. -o $(SERVER_BIN) $(SERVER_SRCS) $(SERVER_LIBS)
	@echo "Server built: $(SERVER_BIN)"

client: dirs
	$(CC) $(CFLAGS) $(CFLAGS_DEBUG) -I. -o $(CLIENT_BIN) $(CLIENT_SRCS) $(CLIENT_LIBS)
	@echo "Client built: $(CLIENT_BIN)"

clean:
	rm -rf $(BIN_DIR)
	@echo "Cleaned"

# 清理测试数据（用户数据、WAL日志）
clean-data:
	rm -f data/users.dat
	rm -f data/wal/*.wal data/wal/*.recovery
	@echo "Test data cleaned"

# 完全清理（编译产物+测试数据）
clean-all: clean clean-data
	@echo "All cleaned"

test: server
	@echo "Running tests..."
	@python3 test/run_all_tests.py

test-robustness: server
	@echo "Running robustness tests..."
	@python3 test/robustness_test.py

test-stress: server
	@echo "Running stress tests..."
	@python3 test/stress_test.py --clients 50 --duration 30

test-functional: server
	@echo "Running functional tests..."
	@python3 test/functional_test.py

# 清理数据后运行功能测试（确保干净环境）
test-functional-clean: server clean-data
	@echo "Running functional tests with clean data..."
	@python3 test/functional_test.py

monitor:
	@echo "Starting performance monitor..."
	@python3 test/monitor_performance.py

tune:
	@echo "Analyzing server configuration..."
	@python3 scripts/tune_server.py

help:
	@echo "ASCII Battle Royale - Build System"
	@echo ""
	@echo "Targets:"
	@echo "  all              - Build server and client"
	@echo "  server           - Build server only"
	@echo "  client           - Build client only"
	@echo "  clean            - Remove build artifacts"
	@echo "  clean-data       - Remove test data (users, WAL)"
	@echo "  clean-all        - Remove all (build + data)"
	@echo "  test             - Run all tests"
	@echo "  test-robustness  - Run robustness tests only"
	@echo "  test-stress      - Run stress tests only"
	@echo "  test-functional  - Run functional tests only"
	@echo "  test-functional-clean - Run functional tests with clean data"
	@echo "  monitor          - Start performance monitor"
	@echo "  tune             - Analyze and suggest server tuning"
