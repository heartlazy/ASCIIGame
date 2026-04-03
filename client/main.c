/**
 * client/main.c - 客户端主程序
 * 
 * 《字符大乱斗》ASCII Battle Royale Client
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <pthread.h>

#include "network.h"
#include "game.h"
#include "ui.h"
#include "../common/protocol.h"
#include "../common/config.h"

static pthread_t recv_tid;
static int running = 1;

/* 发送命令并等待响应 */
static int send_command(const char *cmd) {
    if (network_send(cmd) < 0) {
        ui_show_error("Failed to send command");
        return -1;
    }
    return 0;
}

/* 处理游戏中的输入 */
static void handle_game_input(int ch) {
    char buf[MAX_MSG_LEN];
    
    switch (ch) {
        case 'w': case 'W': case KEY_UP:
            protocol_build_move(buf, sizeof(buf), 'U');
            send_command(buf);
            break;
            
        case 's': case 'S': case KEY_DOWN:
            protocol_build_move(buf, sizeof(buf), 'D');
            send_command(buf);
            break;
            
        case 'a': case 'A': case KEY_LEFT:
            protocol_build_move(buf, sizeof(buf), 'L');
            send_command(buf);
            break;
            
        case 'd': case 'D': case KEY_RIGHT:
            protocol_build_move(buf, sizeof(buf), 'R');
            send_command(buf);
            break;
            
        case 'j': case 'J': case ' ': case KEY_ENTER: case '\n':
            protocol_build_attack(buf, sizeof(buf));
            send_command(buf);
            break;
            
        case '1': case '2': case '3': case '4': case '5':
            protocol_build_use_item(buf, sizeof(buf), ch - '1');
            send_command(buf);
            break;
            
        case 't': case 'T': {
            char chat_buf[256];
            if (ui_chat_input(chat_buf, sizeof(chat_buf)) == 0) {
                protocol_build_chat(buf, sizeof(buf), chat_buf);
                send_command(buf);
            }
            break;
        }
    }
}

/* 处理房间中的输入 */
static void handle_room_input(int ch) {
    char buf[MAX_MSG_LEN];
    
    switch (ch) {
        case 'r': case 'R':
            protocol_build_simple(buf, sizeof(buf), "READY");
            send_command(buf);
            break;
            
        case 'l': case 'L':
        case 27:  /* ESC 键也可以离开房间 */
            protocol_build_simple(buf, sizeof(buf), "LEAVE_ROOM");
            send_command(buf);
            break;
            
        case 't': case 'T': {
            char chat_buf[256];
            if (ui_chat_input(chat_buf, sizeof(chat_buf)) == 0) {
                protocol_build_chat(buf, sizeof(buf), chat_buf);
                send_command(buf);
            }
            break;
        }
    }
}

/* 处理大厅中的输入 */
static void handle_lobby_input(int ch) {
    char buf[MAX_MSG_LEN];
    
    switch (ch) {
        case 'c': case 'C': {
            /* 创建房间 */
            char name[32] = "Room";
            protocol_build_create_room(buf, sizeof(buf), name, 6);
            send_command(buf);
            break;
        }
        
        case 'j': case 'J': {
            /* 加入房间 - 提示输入房间ID */
            echo();
            curs_set(1);
            nodelay(stdscr, FALSE);
            
            mvprintw(LINES - 2, 0, "Enter room ID: ");
            clrtoeol();
            refresh();
            
            char room_id_str[16];
            getnstr(room_id_str, 15);
            
            noecho();
            curs_set(0);
            nodelay(stdscr, TRUE);
            
            move(LINES - 2, 0);
            clrtoeol();
            refresh();
            
            int room_id = atoi(room_id_str);
            if (room_id > 0) {
                protocol_build_join_room(buf, sizeof(buf), room_id);
                send_command(buf);
            }
            break;
        }
        
        case 'l': case 'L':
            /* 列出房间 */
            protocol_build_simple(buf, sizeof(buf), "LIST_ROOMS");
            send_command(buf);
            break;
    }
}

/* 主循环 */
static void main_loop(void) {
    while (running) {
        /* 检查连接状态 */
        pthread_mutex_lock(&g_state.lock);
        int connected = g_state.connected;
        pthread_mutex_unlock(&g_state.lock);
        
        if (!connected) {
            /* 连接断开，显示提示 */
            game_add_message("System", "Connection lost! Press Q to quit.");
            ui_refresh_all();
            
            /* 等待用户按 Q 退出 */
            timeout(100);
            int ch = getch();
            if (ch == 'q' || ch == 'Q') {
                running = 0;
                break;
            }
            continue;
        }
        
        /* 检查 dirty 标志 */
        if (game_clear_dirty()) {
            ui_refresh_all();
        }
        
        /* 读取输入 - 使用 timeout 模式减少 CPU 占用 */
        timeout(50);  /* 50ms 超时 */
        int ch = getch();
        
        if (ch == ERR) {
            /* 无输入，继续循环 */
            continue;
        }
        
        /* 退出 */
        if (ch == 'q' || ch == 'Q') {
            running = 0;
            break;
        }
        
        /* 根据状态处理输入 */
        pthread_mutex_lock(&g_state.lock);
        int in_game = g_state.in_game;
        int in_room = g_state.in_room;
        pthread_mutex_unlock(&g_state.lock);
        
        if (in_game) {
            handle_game_input(ch);
        } else if (in_room) {
            handle_room_input(ch);
        } else {
            handle_lobby_input(ch);
        }
        
        /* 刷新 UI */
        ui_refresh_all();
    }
}

int main(int argc, char *argv[]) {
    const char *host = "127.0.0.1";
    int port = SERVER_PORT;
    
    /* 解析命令行参数 */
    if (argc > 1) {
        host = argv[1];
    }
    if (argc > 2) {
        port = atoi(argv[2]);
    }
    
    /* 初始化游戏状态 */
    game_state_init();
    
    /* 初始化 UI */
    if (ui_init() < 0) {
        fprintf(stderr, "Failed to initialize UI\n");
        return 1;
    }
    
    /* 显示登录界面 */
    char username[32], password[32];
    int action = ui_login_screen(username, password);
    
    if (action < 0) {
        ui_cleanup();
        return 0;
    }
    
    /* 连接服务器 */
    ui_show_message("Connecting to server...");
    ui_refresh_all();
    
    if (network_connect(host, port) < 0) {
        ui_show_error("Failed to connect to server");
        ui_refresh_all();
        sleep(2);
        ui_cleanup();
        return 1;
    }
    
    pthread_mutex_lock(&g_state.lock);
    g_state.connected = 1;
    pthread_mutex_unlock(&g_state.lock);
    
    /* 启动接收线程 */
    pthread_create(&recv_tid, NULL, recv_thread_func, NULL);
    
    /* 发送登录/注册命令 */
    char buf[MAX_MSG_LEN];
    
    /* 清除之前的错误状态 */
    pthread_mutex_lock(&g_state.lock);
    g_state.last_error[0] = '\0';
    pthread_mutex_unlock(&g_state.lock);
    
    if (action == 1) {
        /* 注册 */
        protocol_build_register(buf, sizeof(buf), username, password);
        send_command(buf);
        
        /* 等待注册响应 */
        usleep(500000);
        
        /* 检查注册结果 */
        pthread_mutex_lock(&g_state.lock);
        int has_error = (g_state.last_error[0] != '\0');
        pthread_mutex_unlock(&g_state.lock);
        
        if (has_error) {
            ui_show_error("Registration failed");
            ui_refresh_all();
            sleep(2);
            network_disconnect();
            ui_cleanup();
            return 1;
        }
        
        /* 注册成功后登录 */
        pthread_mutex_lock(&g_state.lock);
        g_state.last_error[0] = '\0';
        pthread_mutex_unlock(&g_state.lock);
        
        protocol_build_login(buf, sizeof(buf), username, password);
        send_command(buf);
        usleep(500000);
    } else {
        /* 直接登录 */
        protocol_build_login(buf, sizeof(buf), username, password);
        send_command(buf);
        usleep(500000);
    }
    
    /* 检查登录结果 */
    pthread_mutex_lock(&g_state.lock);
    int has_error = (g_state.last_error[0] != '\0');
    pthread_mutex_unlock(&g_state.lock);
    
    if (has_error) {
        ui_show_error("Login failed - user not found or wrong password");
        ui_refresh_all();
        sleep(2);
        network_disconnect();
        ui_cleanup();
        return 1;
    }
    
    /* 更新状态 */
    pthread_mutex_lock(&g_state.lock);
    g_state.logged_in = 1;
    strncpy(g_state.username, username, sizeof(g_state.username) - 1);
    pthread_mutex_unlock(&g_state.lock);
    
    ui_show_message("Connected! Press C to create room, J to join, L to list");
    ui_refresh_all();
    
    /* 主循环 */
    main_loop();
    
    /* 清理 */
    network_disconnect();
    pthread_join(recv_tid, NULL);
    game_state_cleanup();
    ui_cleanup();
    
    return 0;
}
