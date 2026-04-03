/**
 * handler.c - 消息处理模块实现
 */

#include "handler.h"
#include "player.h"
#include "room.h"
#include "game.h"
#include "storage.h"
#include "wal.h"
#include "recovery.h"
#include "log.h"
#include "../common/protocol.h"
#include "../common/config.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* 发送错误响应 */
static void send_error(Player *player, int code, const char *message) {
    char buf[MAX_MSG_LEN];
    protocol_build_err(buf, sizeof(buf), code, message);
    player_send(player, buf);
}

/* 发送成功响应 */
static void send_ok(Player *player, const char *message) {
    char buf[MAX_MSG_LEN];
    protocol_build_ok(buf, sizeof(buf), message);
    player_send(player, buf);
}

int handler_process(Player *player, Message *msg) {
    if (player == NULL || msg == NULL) {
        return -1;
    }
    
    log_debug("Processing command %s from player %d", 
              protocol_cmd_name(msg->type), player->id);
    
    switch (msg->type) {
        case CMD_LOGIN:
            return handler_login(player, msg);
        case CMD_REGISTER:
            return handler_register(player, msg);
        case CMD_LIST_ROOMS:
            return handler_list_rooms(player, msg);
        case CMD_CREATE_ROOM:
            return handler_create_room(player, msg);
        case CMD_JOIN_ROOM:
            return handler_join_room(player, msg);
        case CMD_LEAVE_ROOM:
            return handler_leave_room(player, msg);
        case CMD_READY:
            return handler_ready(player, msg);
        case CMD_MOVE:
            return handler_move(player, msg);
        case CMD_ATTACK:
            return handler_attack(player, msg);
        case CMD_USE_ITEM:
            return handler_use_item(player, msg);
        case CMD_CHAT:
            return handler_chat(player, msg);
        case CMD_LOGOUT:
            return handler_logout(player, msg);
        default:
            send_error(player, ERR_UNKNOWN_COMMAND, "Unknown command");
            return -1;
    }
}

int handler_login(Player *player, Message *msg) {
    if (msg->argc < 2) {
        send_error(player, ERR_INVALID_ARG_COUNT, "Usage: LOGIN|username|password");
        return -1;
    }
    
    const char *username = msg->args[0];
    const char *password = msg->args[1];
    
    /* 检查是否已登录 */
    if (player->status != PLAYER_CONNECTED) {
        send_error(player, ERR_USER_LOGGED_IN, "Already logged in");
        return -1;
    }
    
    /* 检查用户是否已在线 */
    if (player_find_by_username(username) != NULL) {
        send_error(player, ERR_USER_LOGGED_IN, "User already online");
        return -1;
    }
    
    /* 验证用户 */
    int ret = storage_verify_user(username, password);
    if (ret == -1) {
        send_error(player, ERR_INVALID_CREDENTIALS, "User not found");
        return -1;
    } else if (ret == -2) {
        send_error(player, ERR_INVALID_CREDENTIALS, "Invalid password");
        return -1;
    }
    
    /* 登录成功 */
    player_set_username(player, username);
    
    pthread_mutex_lock(&player->lock);
    player->status = PLAYER_LOBBY;
    pthread_mutex_unlock(&player->lock);
    
    log_info("Player %d logged in as %s", player->id, username);
    
    /* 检查是否有待恢复的游戏 */
    int recovery_room_id = -1;
    if (recovery_check_player(username, &recovery_room_id)) {
        log_info("Found recoverable game for player %s in room %d", username, recovery_room_id);
        
        /* 恢复玩家到游戏 */
        int new_room_id = recovery_restore_player_to_game(player, recovery_room_id);
        if (new_room_id >= 0) {
            /* 发送登录成功响应 */
            char buf[MAX_MSG_LEN];
            snprintf(buf, sizeof(buf), "OK|Login successful - Rejoining game|%d\n", player->id);
            player_send(player, buf);
            
            /* 发送游戏开始通知 */
            char start_buf[MAX_MSG_LEN];
            protocol_build_game_start(start_buf, sizeof(start_buf));
            player_send(player, start_buf);
            
            /* 发送房间信息 */
            Room *room = room_find_by_id(new_room_id);
            if (room != NULL) {
                char room_buf[MAX_MSG_LEN];
                protocol_build_room_info(room_buf, sizeof(room_buf), room->id, room->name,
                                         room->player_count, room->max_players, room->status);
                player_send(player, room_buf);
                
                /* 发送地图数据 */
                char map_buf[MAX_MSG_LEN];
                pthread_mutex_lock(&room->lock);
                protocol_build_map_data(map_buf, sizeof(map_buf), room->map);
                
                /* 检查是否所有预期玩家都已重连 */
                int all_reconnected = (room->player_count >= room->expected_players && room->expected_players > 0);
                int current_count = room->player_count;
                int expected = room->expected_players;
                pthread_mutex_unlock(&room->lock);
                
                player_send(player, map_buf);
                
                log_info("Recovery room %d: current=%d, expected=%d", new_room_id, current_count, expected);
                
                /* 只有当所有玩家都重连后才清理恢复文件 */
                /* 注意：等待期过后不删除，让未重连的玩家仍有机会重连 */
                /* .recovery 文件会在游戏正常结束时由 room_end_game 清理 */
                if (all_reconnected) {
                    log_info("All %d players reconnected, cleaning up recovery files", expected);
                    recovery_cleanup_room(recovery_room_id);
                }
            }
            
            log_info("Player %s restored to game in room %d", username, new_room_id);
            
            return 0;
        } else {
            log_warn("Failed to restore player %s to game", username);
            /* 恢复失败，继续正常登录流程 */
        }
    }
    
    /* 发送成功响应，包含玩家ID */
    char buf[MAX_MSG_LEN];
    snprintf(buf, sizeof(buf), "OK|Login successful|%d\n", player->id);
    player_send(player, buf);
    
    return 0;
}

int handler_register(Player *player, Message *msg) {
    if (msg->argc < 2) {
        send_error(player, ERR_INVALID_ARG_COUNT, "Usage: REGISTER|username|password");
        return -1;
    }
    
    const char *username = msg->args[0];
    const char *password = msg->args[1];
    
    /* 验证用户名长度 */
    if (strlen(username) < 1 || strlen(username) >= MAX_USERNAME) {
        send_error(player, ERR_INVALID_ARG_FORMAT, "Invalid username length");
        return -1;
    }
    
    /* 注册用户 */
    int ret = storage_register_user(username, password);
    if (ret == -1) {
        send_error(player, ERR_USERNAME_EXISTS, "Username already exists");
        return -1;
    } else if (ret < 0) {
        send_error(player, ERR_INVALID_FORMAT, "Registration failed");
        return -1;
    }
    
    send_ok(player, "Registration successful");
    log_info("New user registered: %s", username);
    
    return 0;
}

int handler_list_rooms(Player *player, Message *msg) {
    (void)msg;
    
    if (player->status < PLAYER_LOBBY) {
        send_error(player, ERR_INVALID_FORMAT, "Not logged in");
        return -1;
    }
    
    char buf[MAX_MSG_LEN];
    room_get_list(buf, sizeof(buf));
    player_send(player, buf);
    
    return 0;
}

int handler_create_room(Player *player, Message *msg) {
    if (msg->argc < 2) {
        send_error(player, ERR_INVALID_ARG_COUNT, "Usage: CREATE_ROOM|name|max_players");
        return -1;
    }
    
    if (player->status != PLAYER_LOBBY) {
        send_error(player, ERR_INVALID_FORMAT, "Must be in lobby");
        return -1;
    }
    
    const char *name = msg->args[0];
    int max_players = atoi(msg->args[1]);
    
    if (max_players < MIN_ROOM_PLAYERS || max_players > MAX_ROOM_PLAYERS) {
        send_error(player, ERR_INVALID_ARG_FORMAT, "Invalid max players (6-10)");
        return -1;
    }
    
    Room *room = room_create(name, max_players);
    if (room == NULL) {
        send_error(player, ERR_INVALID_FORMAT, "Failed to create room");
        return -1;
    }
    
    /* 加入房间 */
    if (room_add_player(room, player) < 0) {
        room_destroy(room);
        send_error(player, ERR_INVALID_FORMAT, "Failed to join room");
        return -1;
    }
    
    /* 发送房间信息 */
    char buf[MAX_MSG_LEN];
    protocol_build_room_info(buf, sizeof(buf), room->id, room->name,
                             room->player_count, room->max_players, room->status);
    player_send(player, buf);
    
    log_info("Player %d created room %d: %s", player->id, room->id, name);
    
    return 0;
}

int handler_join_room(Player *player, Message *msg) {
    if (msg->argc < 1) {
        send_error(player, ERR_INVALID_ARG_COUNT, "Usage: JOIN_ROOM|room_id");
        return -1;
    }
    
    if (player->status != PLAYER_LOBBY) {
        send_error(player, ERR_INVALID_FORMAT, "Must be in lobby");
        return -1;
    }
    
    int room_id = atoi(msg->args[0]);
    Room *room = room_find_by_id(room_id);
    
    if (room == NULL) {
        send_error(player, ERR_ROOM_NOT_FOUND, "Room not found");
        return -1;
    }
    
    int ret = room_add_player(room, player);
    if (ret == -1) {
        send_error(player, ERR_ROOM_FULL, "Room is full");
        return -1;
    } else if (ret == -2) {
        send_error(player, ERR_GAME_IN_PROGRESS, "Game in progress");
        return -1;
    }
    
    /* 发送房间信息 */
    char buf[MAX_MSG_LEN];
    protocol_build_room_info(buf, sizeof(buf), room->id, room->name,
                             room->player_count, room->max_players, room->status);
    player_send(player, buf);
    
    log_info("Player %d joined room %d", player->id, room_id);
    
    return 0;
}

int handler_leave_room(Player *player, Message *msg) {
    (void)msg;
    
    if (player->status < PLAYER_IN_ROOM) {
        send_error(player, ERR_NOT_IN_ROOM, "Not in a room");
        return -1;
    }
    
    Room *room = room_find_by_id(player->room_id);
    if (room == NULL) {
        send_error(player, ERR_ROOM_NOT_FOUND, "Room not found");
        return -1;
    }
    
    room_remove_player(room, player);
    send_ok(player, "Left room");
    
    log_info("Player %d left room", player->id);
    
    return 0;
}

int handler_ready(Player *player, Message *msg) {
    (void)msg;
    
    if (player->status != PLAYER_IN_ROOM && player->status != PLAYER_READY) {
        send_error(player, ERR_NOT_IN_ROOM, "Not in a room");
        return -1;
    }
    
    Room *room = room_find_by_id(player->room_id);
    if (room == NULL) {
        send_error(player, ERR_ROOM_NOT_FOUND, "Room not found");
        return -1;
    }
    
    /* 切换准备状态 */
    pthread_mutex_lock(&player->lock);
    if (player->status == PLAYER_IN_ROOM) {
        player->status = PLAYER_READY;
    } else {
        player->status = PLAYER_IN_ROOM;
    }
    PlayerStatus new_status = player->status;
    pthread_mutex_unlock(&player->lock);
    
    send_ok(player, new_status == PLAYER_READY ? "Ready" : "Not ready");
    
    /* 检查是否所有人都准备好了 */
    if (room_all_ready(room)) {
        log_info("All players ready in room %d, starting game", room->id);
        
        /* 创建 WAL */
        room->wal = wal_create_for_room(room->id);
        
        /* 启动游戏 */
        room_start_game(room);
        
        /* 启动游戏线程 */
        pthread_create(&room->game_thread, NULL, game_thread_func, room);
        pthread_detach(room->game_thread);
    }
    
    return 0;
}

int handler_move(Player *player, Message *msg) {
    if (msg->argc < 1) {
        send_error(player, ERR_INVALID_ARG_COUNT, "Usage: MOVE|direction");
        return -1;
    }
    
    if (player->status != PLAYER_GAMING) {
        send_error(player, ERR_INVALID_FORMAT, "Not in game");
        return -1;
    }
    
    Room *room = room_find_by_id(player->room_id);
    if (room == NULL) {
        send_error(player, ERR_ROOM_NOT_FOUND, "Room not found");
        return -1;
    }
    
    char direction = msg->args[0][0];
    int ret = game_handle_move(room, player, direction);
    
    if (ret == -1) {
        send_error(player, ERR_MOVE_COOLDOWN, "Move on cooldown");
        return -1;
    } else if (ret == -2) {
        send_error(player, ERR_INVALID_MOVE, "Invalid move");
        return -1;
    }
    
    return 0;
}

int handler_attack(Player *player, Message *msg) {
    (void)msg;
    
    if (player->status != PLAYER_GAMING) {
        send_error(player, ERR_INVALID_FORMAT, "Not in game");
        return -1;
    }
    
    Room *room = room_find_by_id(player->room_id);
    if (room == NULL) {
        send_error(player, ERR_ROOM_NOT_FOUND, "Room not found");
        return -1;
    }
    
    int ret = game_handle_attack(room, player);
    
    if (ret == -1) {
        send_error(player, ERR_ATTACK_COOLDOWN, "Attack on cooldown");
        return -1;
    }
    
    return 0;
}

int handler_use_item(Player *player, Message *msg) {
    if (msg->argc < 1) {
        send_error(player, ERR_INVALID_ARG_COUNT, "Usage: USE_ITEM|index");
        return -1;
    }
    
    if (player->status != PLAYER_GAMING) {
        send_error(player, ERR_INVALID_FORMAT, "Not in game");
        return -1;
    }
    
    Room *room = room_find_by_id(player->room_id);
    if (room == NULL) {
        send_error(player, ERR_ROOM_NOT_FOUND, "Room not found");
        return -1;
    }
    
    int index = atoi(msg->args[0]);
    int ret = game_handle_use_item(room, player, index);
    
    if (ret == -1) {
        send_error(player, ERR_INVALID_ITEM_INDEX, "Invalid item index");
        return -1;
    }
    
    send_ok(player, "Item used");
    return 0;
}

int handler_chat(Player *player, Message *msg) {
    if (msg->argc < 1) {
        return 0;  /* 忽略空消息 */
    }
    
    const char *message = msg->args[0];
    
    /* 忽略空白消息 */
    if (strlen(message) == 0) {
        return 0;
    }
    
    /* 检查是否全是空白 */
    int all_space = 1;
    for (size_t i = 0; i < strlen(message); i++) {
        if (message[i] != ' ' && message[i] != '\t') {
            all_space = 0;
            break;
        }
    }
    if (all_space) {
        return 0;
    }
    
    if (player->room_id < 0) {
        send_error(player, ERR_NOT_IN_ROOM, "Not in a room");
        return -1;
    }
    
    Room *room = room_find_by_id(player->room_id);
    if (room == NULL) {
        send_error(player, ERR_ROOM_NOT_FOUND, "Room not found");
        return -1;
    }
    
    /* 广播聊天消息 */
    char buf[MAX_MSG_LEN];
    protocol_build_chat_msg(buf, sizeof(buf), player->username, message);
    room_broadcast(room, buf);
    
    return 0;
}

int handler_logout(Player *player, Message *msg) {
    (void)msg;
    
    /* 如果在房间中，先离开 */
    if (player->room_id >= 0) {
        Room *room = room_find_by_id(player->room_id);
        if (room != NULL) {
            room_remove_player(room, player);
        }
    }
    
    /* 重置状态 */
    pthread_mutex_lock(&player->lock);
    player->username[0] = '\0';
    player->status = PLAYER_CONNECTED;
    pthread_mutex_unlock(&player->lock);
    
    send_ok(player, "Logged out");
    log_info("Player %d logged out", player->id);
    
    return 0;
}
