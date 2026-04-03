/**
 * main.c - 服务端主程序
 * 
 * 《字符大乱斗》ASCII Battle Royale Server
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <signal.h>
#include <unistd.h>
#include <sys/epoll.h>
#include <errno.h>

#include "log.h"
#include "network.h"
#include "player.h"
#include "room.h"
#include "storage.h"
#include "wal.h"
#include "recovery.h"
#include "handler.h"
#include "../common/protocol.h"
#include "../common/config.h"

/* 全局原子变量定义 */
atomic_int server_running = 1;
atomic_int online_count = 0;

/* 信号处理 */
static void signal_handler(int sig) {
    if (sig == SIGINT || sig == SIGTERM) {
        log_info("Received signal %d, shutting down...", sig);
        atomic_store(&server_running, 0);
    }
}

/* 设置信号处理 */
static void setup_signals(void) {
    struct sigaction sa;
    sa.sa_handler = signal_handler;
    sigemptyset(&sa.sa_mask);
    sa.sa_flags = 0;
    
    sigaction(SIGINT, &sa, NULL);
    sigaction(SIGTERM, &sa, NULL);
    
    /* 忽略 SIGPIPE */
    signal(SIGPIPE, SIG_IGN);
}

/* 处理新连接 */
static void handle_new_connection(int listen_fd, int epoll_fd) {
    int client_fd = accept_connection(listen_fd);
    
    if (client_fd < 0) {
        if (client_fd == -2) {
            /* EAGAIN，正常 */
            return;
        }
        log_error("Failed to accept connection");
        return;
    }
    
    /* 创建玩家 */
    Player *player = player_create(client_fd);
    if (player == NULL) {
        close_connection(client_fd);
        return;
    }
    
    /* 添加到 epoll */
    if (epoll_add(epoll_fd, client_fd, EPOLLIN | EPOLLET) < 0) {
        player_destroy(player);
        close_connection(client_fd);
        return;
    }
    
    atomic_fetch_add(&online_count, 1);
    log_info("New connection: fd=%d, online=%d", client_fd, atomic_load(&online_count));
}

/* 处理客户端断开 */
static void handle_disconnect(Player *player, int epoll_fd) {
    if (player == NULL) return;
    
    int fd = player->fd;
    
    /* 从 epoll 移除 */
    epoll_del(epoll_fd, fd);
    
    /* 如果在房间中，离开房间 */
    if (player->room_id >= 0) {
        Room *room = room_find_by_id(player->room_id);
        if (room != NULL) {
            room_remove_player(room, player);
        }
    }
    
    /* 销毁玩家 */
    player_destroy(player);
    
    /* 关闭连接 */
    close_connection(fd);
    
    atomic_fetch_sub(&online_count, 1);
    log_info("Connection closed: fd=%d, online=%d", fd, atomic_load(&online_count));
}

/* 处理客户端数据 */
static void handle_client_data(Player *player, int epoll_fd) {
    if (player == NULL) return;
    
    /* 接收数据 */
    int ret = recv_to_buffer(player->fd, player->recv_buf, 
                             &player->recv_len, RECV_BUFFER_SIZE - 1);
    
    if (ret == 0) {
        /* 连接关闭 */
        handle_disconnect(player, epoll_fd);
        return;
    } else if (ret < 0 && ret != -2) {
        /* 错误 */
        handle_disconnect(player, epoll_fd);
        return;
    }
    
    /* 处理完整消息 */
    char msg_buf[MAX_MSG_LEN];
    
    while (protocol_extract_message(player->recv_buf, &player->recv_len, 
                                    msg_buf, sizeof(msg_buf)) > 0) {
        Message msg;
        
        if (protocol_parse(msg_buf, &msg) == 0) {
            handler_process(player, &msg);
        } else {
            char err_buf[MAX_MSG_LEN];
            protocol_build_err(err_buf, sizeof(err_buf), 
                               ERR_INVALID_FORMAT, "Invalid message format");
            player_send(player, err_buf);
        }
    }
}

/* 主循环 */
static void main_loop(int listen_fd, int epoll_fd) {
    struct epoll_event events[MAX_EVENTS];
    
    log_info("Server main loop started");
    
    while (atomic_load(&server_running)) {
        int nfds = epoll_wait(epoll_fd, events, MAX_EVENTS, 1000);
        
        if (nfds < 0) {
            if (errno == EINTR) {
                continue;
            }
            log_error("epoll_wait failed: %s", strerror(errno));
            break;
        }
        
        for (int i = 0; i < nfds; i++) {
            int fd = events[i].data.fd;
            uint32_t ev = events[i].events;
            
            if (fd == listen_fd) {
                /* 新连接 */
                handle_new_connection(listen_fd, epoll_fd);
            } else {
                /* 客户端事件 */
                Player *player = player_find_by_fd(fd);
                
                if (ev & (EPOLLERR | EPOLLHUP)) {
                    handle_disconnect(player, epoll_fd);
                } else if (ev & EPOLLIN) {
                    handle_client_data(player, epoll_fd);
                }
            }
        }
    }
    
    log_info("Server main loop ended");
}

/* 优雅关闭 */
static void graceful_shutdown(void) {
    log_info("Performing graceful shutdown...");
    
    /* 保存用户数据 */
    storage_save();
    
    /* 清理模块 */
    room_cleanup();
    player_cleanup();
    storage_cleanup();
    wal_cleanup();
    
    log_info("Graceful shutdown complete");
}

int main(int argc, char *argv[]) {
    int port = SERVER_PORT;
    
    /* 解析命令行参数 */
    if (argc > 1) {
        port = atoi(argv[1]);
        if (port <= 0 || port > 65535) {
            fprintf(stderr, "Invalid port: %s\n", argv[1]);
            return 1;
        }
    }
    
    /* 初始化日志 */
    if (log_init(NULL) < 0) {
        fprintf(stderr, "Failed to initialize logging\n");
        return 1;
    }
    
    log_info("=== ASCII Battle Royale Server ===");
    log_info("Starting server on port %d", port);
    
    /* 设置信号处理 */
    setup_signals();
    
    /* 初始化模块 */
    if (wal_init() < 0) {
        log_error("Failed to initialize WAL");
        return 1;
    }
    
    if (storage_init() < 0) {
        log_error("Failed to initialize storage");
        return 1;
    }
    
    if (player_init() < 0) {
        log_error("Failed to initialize player manager");
        return 1;
    }
    
    if (room_init() < 0) {
        log_error("Failed to initialize room manager");
        return 1;
    }
    
    /* 检查崩溃恢复 */
    recovery_check_and_recover();
    
    /* 创建监听 socket */
    int listen_fd = network_init(port);
    if (listen_fd < 0) {
        log_error("Failed to create listening socket");
        return 1;
    }
    
    /* 创建 epoll */
    int epoll_fd = epoll_create_instance();
    if (epoll_fd < 0) {
        log_error("Failed to create epoll instance");
        close_connection(listen_fd);
        return 1;
    }
    
    /* 添加监听 socket 到 epoll */
    if (epoll_add(epoll_fd, listen_fd, EPOLLIN) < 0) {
        log_error("Failed to add listen socket to epoll");
        close(epoll_fd);
        close_connection(listen_fd);
        return 1;
    }
    
    log_info("Server initialized successfully");
    
    /* 主循环 */
    main_loop(listen_fd, epoll_fd);
    
    /* 清理 */
    close(epoll_fd);
    close_connection(listen_fd);
    graceful_shutdown();
    log_cleanup();
    
    return 0;
}
