/**
 * client/game.c - 客户端游戏状态模块实现
 */

#include "game.h"
#include "network.h"
#include "../common/protocol.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/time.h>

/* 全局游戏状态 */
GameState g_state;

/* 客户端地图模板 - 开放式设计 */
static const char *map_template[MAP_HEIGHT] = {
    "##################################################",
    "#                    $                           #",
    "#   ##    ##         $         ##    ##    $     #",
    "#   ##    ##    $              ##    ##          #",
    "#              ###        ###              $     #",
    "#   $          # $        $ #          $         #",
    "#              ###        ###                    #",
    "#       $                          $       ##    #",
    "#   ##              ####              ##         #",
    "#   ##     $        #  #        $     ##    $    #",
    "#          $        #  #        $                #",
    "#   ##     $        ####        $     ##         #",
    "#   ##                                ##    $    #",
    "#       $                          $             #",
    "#              ###        ###              $     #",
    "#   $          # $        $ #          $         #",
    "#              ###        ###                    #",
    "#   ##    ##    $              ##    ##          #",
    "#   ##    ##         $         ##    ##    $     #",
    "##################################################"
};

/* 加载地图模板到游戏状态 */
static void load_map_template(void) {
    for (int y = 0; y < MAP_HEIGHT; y++) {
        strncpy(g_state.map[y], map_template[y], MAP_WIDTH);
        g_state.map[y][MAP_WIDTH] = '\0';
    }
}

void game_state_init(void) {
    memset(&g_state, 0, sizeof(GameState));
    pthread_mutex_init(&g_state.lock, NULL);
    atomic_init(&g_state.dirty, 0);
    
    g_state.my_max_hp = 100;
    g_state.poison_radius = 25;
    g_state.attack_effect.active = 0;
    
    /* 初始化空地图 */
    for (int y = 0; y < MAP_HEIGHT; y++) {
        memset(g_state.map[y], ' ', MAP_WIDTH);
        g_state.map[y][MAP_WIDTH] = '\0';
    }
}

/* 获取当前时间（毫秒） */
long game_get_time_ms(void) {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return tv.tv_sec * 1000L + tv.tv_usec / 1000L;
}

/* 触发攻击特效 */
void game_trigger_attack_effect(int x, int y, int radius) {
    pthread_mutex_lock(&g_state.lock);
    g_state.attack_effect.active = 1;
    g_state.attack_effect.x = x;
    g_state.attack_effect.y = y;
    g_state.attack_effect.radius = radius;
    g_state.attack_effect.start_time = game_get_time_ms();
    pthread_mutex_unlock(&g_state.lock);
    game_set_dirty();
}

void game_state_cleanup(void) {
    pthread_mutex_destroy(&g_state.lock);
}

void game_set_dirty(void) {
    atomic_store(&g_state.dirty, 1);
}

int game_clear_dirty(void) {
    return atomic_exchange(&g_state.dirty, 0);
}

void game_add_message(const char *sender, const char *text) {
    pthread_mutex_lock(&g_state.lock);
    
    int idx = (g_state.msg_head + g_state.msg_count) % MAX_MESSAGES;
    
    if (g_state.msg_count < MAX_MESSAGES) {
        g_state.msg_count++;
    } else {
        g_state.msg_head = (g_state.msg_head + 1) % MAX_MESSAGES;
    }
    
    strncpy(g_state.messages[idx].sender, sender ? sender : "", 31);
    g_state.messages[idx].sender[31] = '\0';
    strncpy(g_state.messages[idx].text, text ? text : "", 255);
    g_state.messages[idx].text[255] = '\0';
    
    pthread_mutex_unlock(&g_state.lock);
    game_set_dirty();
}

/* 解析玩家状态字符串 */
static void parse_player_states(const char *states) {
    if (states == NULL || strlen(states) == 0) return;
    
    char buf[4096];
    strncpy(buf, states, sizeof(buf) - 1);
    buf[sizeof(buf) - 1] = '\0';
    
    g_state.player_count = 0;
    
    char *saveptr = NULL;
    char *token = strtok_r(buf, ";", &saveptr);
    
    while (token != NULL && g_state.player_count < MAX_PLAYERS_VIEW) {
        PlayerView *pv = &g_state.players[g_state.player_count];
        
        int id, x, y, hp, atk, def, status, has_shield;
        int inv0, inv1, inv2, inv3, inv4;
        /* 格式: id,x,y,hp,atk,def,status,shield,inv0,inv1,inv2,inv3,inv4 */
        int parsed = sscanf(token, "%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d",
                   &id, &x, &y, &hp, &atk, &def, &status, &has_shield,
                   &inv0, &inv1, &inv2, &inv3, &inv4);
        
        if (parsed >= 4) {
            pv->id = id;
            pv->x = x;
            pv->y = y;
            pv->hp = hp;
            pv->status = status;
            
            /* 第一个玩家设为自己（如果还没设置） */
            if (g_state.my_id == 0) {
                g_state.my_id = id;
            }
            
            /* 检查是否是自己 */
            if (pv->id == g_state.my_id) {
                g_state.my_x = x;
                g_state.my_y = y;
                g_state.my_hp = hp;
                g_state.my_atk = atk;
                g_state.my_def = def;
                
                /* 解析背包数据 */
                if (parsed >= 13) {
                    g_state.my_has_shield = has_shield;
                    g_state.inventory[0] = inv0;
                    g_state.inventory[1] = inv1;
                    g_state.inventory[2] = inv2;
                    g_state.inventory[3] = inv3;
                    g_state.inventory[4] = inv4;
                    g_state.inventory_count = 0;
                    for (int i = 0; i < 5; i++) {
                        if (g_state.inventory[i] > 0) {
                            g_state.inventory_count++;
                        }
                    }
                }
            }
            
            g_state.player_count++;
        }
        
        token = strtok_r(NULL, ";", &saveptr);
    }
}

/* 处理 OK 响应 */
static void handle_ok(Message *msg) {
    pthread_mutex_lock(&g_state.lock);
    /* 清除错误状态 */
    g_state.last_error[0] = '\0';
    if (msg->argc > 0) {
        strncpy(g_state.last_message, msg->args[0], sizeof(g_state.last_message) - 1);
        
        /* 检查是否是登录成功响应，提取玩家ID */
        if (strstr(msg->args[0], "Login successful") != NULL && msg->argc > 1) {
            g_state.my_id = atoi(msg->args[1]);
        }
        
        /* 检查是否是离开房间响应 */
        if (strstr(msg->args[0], "Left room") != NULL) {
            g_state.in_room = 0;
            g_state.is_ready = 0;
            g_state.room_id = -1;
            g_state.room_name[0] = '\0';
        }
        
        /* 检查准备状态响应 */
        if (strcmp(msg->args[0], "Ready") == 0) {
            g_state.is_ready = 1;
        } else if (strcmp(msg->args[0], "Not ready") == 0) {
            g_state.is_ready = 0;
        }
    }
    pthread_mutex_unlock(&g_state.lock);
    
    /* 显示服务器响应消息 */
    if (msg->argc > 0 && strlen(msg->args[0]) > 0) {
        game_add_message("Server", msg->args[0]);
    }
}

/* 处理 ERR 响应 */
static void handle_err(Message *msg) {
    if (msg->argc > 1) {
        pthread_mutex_lock(&g_state.lock);
        strncpy(g_state.last_error, msg->args[1], sizeof(g_state.last_error) - 1);
        pthread_mutex_unlock(&g_state.lock);
        
        /* 过滤掉频繁的游戏内错误（如撞墙、冷却中） */
        const char *err_msg = msg->args[1];
        if (strcmp(err_msg, "Invalid move") == 0 ||
            strcmp(err_msg, "Move on cooldown") == 0 ||
            strcmp(err_msg, "Attack on cooldown") == 0) {
            /* 不显示这些错误，避免刷屏 */
            return;
        }
        
        /* 显示其他错误消息 */
        game_add_message("Error", err_msg);
    }
}

/* 处理房间信息 */
static void handle_room_info(Message *msg) {
    if (msg->argc < 5) return;
    
    pthread_mutex_lock(&g_state.lock);
    g_state.room_id = atoi(msg->args[0]);
    strncpy(g_state.room_name, msg->args[1], sizeof(g_state.room_name) - 1);
    g_state.in_room = 1;
    pthread_mutex_unlock(&g_state.lock);
    
    char buf[128];
    snprintf(buf, sizeof(buf), "Joined room %.20s (ID: %.8s, Players: %.4s/%.4s)", 
             msg->args[1], msg->args[0], msg->args[2], msg->args[3]);
    game_add_message("System", buf);
}

/* 处理房间列表 */
static void handle_room_list(Message *msg) {
    if (msg->argc < 1 || strlen(msg->args[0]) == 0) {
        game_add_message("System", "No rooms available. Press C to create one.");
        return;
    }
    
    game_add_message("System", "=== Room List ===");
    
    /* 解析房间列表: id,name,players,max,status;id,name,... */
    char buf[1024];
    strncpy(buf, msg->args[0], sizeof(buf) - 1);
    buf[sizeof(buf) - 1] = '\0';
    
    char *saveptr = NULL;
    char *token = strtok_r(buf, ";", &saveptr);
    
    while (token != NULL) {
        int id, players, max_players, status;
        char name[64];
        
        if (sscanf(token, "%d,%63[^,],%d,%d,%d", &id, name, &players, &max_players, &status) >= 4) {
            char room_info[128];
            const char *status_str = (status == 0) ? "Waiting" : 
                                     (status == 2) ? "Gaming" : "Other";
            snprintf(room_info, sizeof(room_info), "  [%d] %s (%d/%d) - %s", 
                     id, name, players, max_players, status_str);
            game_add_message("", room_info);
        }
        
        token = strtok_r(NULL, ";", &saveptr);
    }
    
    game_add_message("System", "Press J to join a room by ID");
}

/* 处理游戏开始 */
static void handle_game_start(Message *msg) {
    (void)msg;
    
    pthread_mutex_lock(&g_state.lock);
    g_state.in_game = 1;
    g_state.my_hp = g_state.my_max_hp;
    g_state.my_atk = 15;  /* 初始攻击力 */
    g_state.my_def = 5;   /* 初始防御力 */
    
    /* 加载地图模板 */
    load_map_template();
    
    pthread_mutex_unlock(&g_state.lock);
    
    game_add_message("System", "Game started! Use WASD to move, J/Space to attack!");
}

/* 处理地图数据 */
static void handle_map_data(Message *msg) {
    if (msg->argc < 1) return;
    
    pthread_mutex_lock(&g_state.lock);
    
    /* 解析地图数据，格式: row0,row1,row2,... (逗号分隔) */
    const char *data = msg->args[0];
    int y = 0;
    int x = 0;
    
    /* 手动解析，避免 strtok 的问题 */
    for (int i = 0; data[i] != '\0' && y < MAP_HEIGHT; i++) {
        if (data[i] == ',') {
            /* 填充当前行剩余部分 */
            while (x < MAP_WIDTH) {
                g_state.map[y][x++] = ' ';
            }
            g_state.map[y][MAP_WIDTH] = '\0';
            y++;
            x = 0;
        } else if (x < MAP_WIDTH) {
            g_state.map[y][x++] = data[i];
        }
    }
    
    /* 处理最后一行 */
    if (y < MAP_HEIGHT && x > 0) {
        while (x < MAP_WIDTH) {
            g_state.map[y][x++] = ' ';
        }
        g_state.map[y][MAP_WIDTH] = '\0';
        y++;
    }
    
    /* 填充剩余行 */
    while (y < MAP_HEIGHT) {
        memset(g_state.map[y], ' ', MAP_WIDTH);
        g_state.map[y][MAP_WIDTH] = '\0';
        y++;
    }
    
    pthread_mutex_unlock(&g_state.lock);
    
    game_add_message("System", "Map loaded!");
}

/* 解析道具状态字符串 */
static void parse_item_states(const char *states) {
    if (states == NULL || strlen(states) == 0) {
        g_state.item_count = 0;
        return;
    }
    
    char buf[1024];
    strncpy(buf, states, sizeof(buf) - 1);
    buf[sizeof(buf) - 1] = '\0';
    
    g_state.item_count = 0;
    
    char *saveptr = NULL;
    char *token = strtok_r(buf, ";", &saveptr);
    
    while (token != NULL && g_state.item_count < MAX_ITEMS_VIEW) {
        ItemView *iv = &g_state.items[g_state.item_count];
        
        /* 格式: x,y,type */
        if (sscanf(token, "%d,%d,%d", &iv->x, &iv->y, &iv->type) >= 3) {
            g_state.item_count++;
        }
        
        token = strtok_r(NULL, ";", &saveptr);
    }
}

/* 处理游戏状态 */
static void handle_game_state(Message *msg) {
    if (msg->argc < 4) return;
    
    pthread_mutex_lock(&g_state.lock);
    
    /* 解析玩家状态 */
    parse_player_states(msg->args[1]);
    
    /* 解析道具状态 */
    parse_item_states(msg->args[2]);
    
    /* 解析毒圈半径 */
    g_state.poison_radius = atoi(msg->args[3]);
    
    pthread_mutex_unlock(&g_state.lock);
}

/* 处理游戏事件 */
static void handle_game_event(Message *msg) {
    if (msg->argc < 1) return;
    
    const char *event_type = msg->args[0];
    
    /* 
     * 事件格式 (协议解析后):
     * ATTACK: args[0]="ATTACK", args[1]=attacker, args[2]=x, args[3]=y
     * ATTACK_RESULT: args[0]="ATTACK_RESULT", args[1]=attacker, args[2]=hit_count
     * DAMAGE: args[0]="DAMAGE", args[1]=attacker, args[2]=victim, args[3]=damage, args[4]=hp
     * KILL: args[0]="KILL", args[1]=killer, args[2]=victim
     * SHIELD: args[0]="SHIELD", args[1]=attacker, args[2]=defender
     * PICKUP: args[0]="PICKUP", args[1]=player_id, args[2]=item_type
     */
    
    if (strcmp(event_type, "ATTACK") == 0 && msg->argc >= 4) {
        /* 攻击事件: args[1]=attacker, args[2]=x, args[3]=y */
        int x = atoi(msg->args[2]);
        int y = atoi(msg->args[3]);
        /* 触发攻击特效，攻击范围为3 */
        game_trigger_attack_effect(x, y, 3);
    } else if (strcmp(event_type, "ATTACK_RESULT") == 0 && msg->argc >= 3) {
        /* 攻击结果 */
        int attacker = atoi(msg->args[1]);
        int hit_count = atoi(msg->args[2]);
        
        pthread_mutex_lock(&g_state.lock);
        int my_id = g_state.my_id;
        pthread_mutex_unlock(&g_state.lock);
        
        if (attacker == my_id) {
            char buf[64];
            if (hit_count == 0) {
                snprintf(buf, sizeof(buf), "Attack missed!");
            } else {
                snprintf(buf, sizeof(buf), "Attack hit %d target(s)!", hit_count);
            }
            game_add_message("Combat", buf);
        }
    } else if (strcmp(event_type, "DAMAGE") == 0 && msg->argc >= 5) {
        /* 伤害事件 */
        int attacker = atoi(msg->args[1]);
        int victim = atoi(msg->args[2]);
        int damage = atoi(msg->args[3]);
        int hp = atoi(msg->args[4]);
        
        char buf[128];
        pthread_mutex_lock(&g_state.lock);
        if (victim == g_state.my_id) {
            snprintf(buf, sizeof(buf), "You took %d damage! HP: %d", damage, hp);
            g_state.my_hp = hp;
        } else if (attacker == g_state.my_id) {
            snprintf(buf, sizeof(buf), "You dealt %d damage!", damage);
        } else {
            snprintf(buf, sizeof(buf), "Player %d hit player %d for %d damage", attacker, victim, damage);
        }
        pthread_mutex_unlock(&g_state.lock);
        game_add_message("Combat", buf);
    } else if (strcmp(event_type, "KILL") == 0 && msg->argc >= 3) {
        int killer = atoi(msg->args[1]);
        int victim = atoi(msg->args[2]);
        
        char buf[128];
        pthread_mutex_lock(&g_state.lock);
        if (victim == g_state.my_id) {
            snprintf(buf, sizeof(buf), "You were killed by player %d!", killer);
        } else if (killer == g_state.my_id) {
            snprintf(buf, sizeof(buf), "You killed player %d!", victim);
        } else {
            snprintf(buf, sizeof(buf), "Player %d killed player %d", killer, victim);
        }
        pthread_mutex_unlock(&g_state.lock);
        game_add_message("Combat", buf);
    } else if (strcmp(event_type, "SHIELD") == 0 && msg->argc >= 3) {
        int attacker = atoi(msg->args[1]);
        int defender = atoi(msg->args[2]);
        
        char buf[128];
        pthread_mutex_lock(&g_state.lock);
        if (defender == g_state.my_id) {
            snprintf(buf, sizeof(buf), "Your shield blocked an attack!");
        } else if (attacker == g_state.my_id) {
            snprintf(buf, sizeof(buf), "Your attack was blocked by a shield!");
        } else {
            buf[0] = '\0';
        }
        pthread_mutex_unlock(&g_state.lock);
        if (buf[0] != '\0') {
            game_add_message("Combat", buf);
        }
    } else if (strcmp(event_type, "POISON") == 0) {
        game_add_message("System", "Poison zone shrinking!");
    } else if (strcmp(event_type, "BUFF_WARNING") == 0 && msg->argc >= 3) {
        /* Buff 即将过期提醒: args[1]=player_id, args[2]=seconds */
        int player_id = atoi(msg->args[1]);
        int seconds = atoi(msg->args[2]);
        
        pthread_mutex_lock(&g_state.lock);
        if (player_id == g_state.my_id) {
            char buf[64];
            snprintf(buf, sizeof(buf), "Attack buff expires in %d seconds!", seconds);
            pthread_mutex_unlock(&g_state.lock);
            game_add_message("Buff", buf);
        } else {
            pthread_mutex_unlock(&g_state.lock);
        }
    } else if (strcmp(event_type, "BUFF_EXPIRED") == 0 && msg->argc >= 2) {
        /* Buff 已过期: args[1]=player_id */
        int player_id = atoi(msg->args[1]);
        
        pthread_mutex_lock(&g_state.lock);
        if (player_id == g_state.my_id) {
            pthread_mutex_unlock(&g_state.lock);
            game_add_message("Buff", "Attack buff has expired!");
        } else {
            pthread_mutex_unlock(&g_state.lock);
        }
    } else if (strcmp(event_type, "PICKUP") == 0 && msg->argc >= 3) {
        int player_id = atoi(msg->args[1]);
        int item_type = atoi(msg->args[2]);
        
        pthread_mutex_lock(&g_state.lock);
        if (player_id == g_state.my_id) {
            const char *item_name = (item_type == 1) ? "Health Pack" :
                                    (item_type == 2) ? "Attack Potion" :
                                    (item_type == 3) ? "Shield" : "Item";
            char buf[64];
            snprintf(buf, sizeof(buf), "Picked up %s!", item_name);
            pthread_mutex_unlock(&g_state.lock);
            game_add_message("Item", buf);
        } else {
            pthread_mutex_unlock(&g_state.lock);
        }
    }
}

/* 处理游戏结束 */
static void handle_game_end(Message *msg) {
    pthread_mutex_lock(&g_state.lock);
    g_state.in_game = 0;
    g_state.is_ready = 0;
    
    /* 重置游戏状态 */
    g_state.my_hp = g_state.my_max_hp;
    g_state.my_x = 0;
    g_state.my_y = 0;
    g_state.player_count = 0;
    g_state.poison_radius = 25;
    
    /* 清空地图 */
    for (int y = 0; y < MAP_HEIGHT; y++) {
        memset(g_state.map[y], ' ', MAP_WIDTH);
        g_state.map[y][MAP_WIDTH] = '\0';
    }
    
    pthread_mutex_unlock(&g_state.lock);
    
    if (msg->argc > 0) {
        int winner_id = atoi(msg->args[0]);
        char buf[128];
        pthread_mutex_lock(&g_state.lock);
        int my_id = g_state.my_id;
        pthread_mutex_unlock(&g_state.lock);
        
        if (winner_id == my_id) {
            snprintf(buf, sizeof(buf), "You win!");
        } else if (winner_id < 0) {
            snprintf(buf, sizeof(buf), "Game ended - Draw!");
        } else {
            snprintf(buf, sizeof(buf), "Game ended - Player %d wins!", winner_id);
        }
        game_add_message("System", buf);
    }
}

/* 处理聊天消息 */
static void handle_chat_msg(Message *msg) {
    if (msg->argc >= 2) {
        game_add_message(msg->args[0], msg->args[1]);
    }
}

/* 处理玩家加入 */
static void handle_player_join(Message *msg) {
    if (msg->argc >= 2) {
        char buf[256];
        snprintf(buf, sizeof(buf), "%.32s joined the room", msg->args[1]);
        game_add_message("System", buf);
    }
}

/* 处理玩家离开 */
static void handle_player_leave(Message *msg) {
    if (msg->argc >= 1) {
        char buf[256];
        snprintf(buf, sizeof(buf), "Player %.32s left the room", msg->args[0]);
        game_add_message("System", buf);
    }
}

void game_update_from_server(const char *raw_msg) {
    if (raw_msg == NULL) return;
    
    Message msg;
    if (protocol_parse(raw_msg, &msg) < 0) {
        return;
    }
    
    switch (msg.type) {
        case CMD_OK:
            handle_ok(&msg);
            break;
        case CMD_ERR:
            handle_err(&msg);
            break;
        case CMD_ROOM_LIST:
            handle_room_list(&msg);
            break;
        case CMD_ROOM_INFO:
            handle_room_info(&msg);
            break;
        case CMD_PLAYER_JOIN:
            handle_player_join(&msg);
            break;
        case CMD_PLAYER_LEAVE:
            handle_player_leave(&msg);
            break;
        case CMD_GAME_START:
            handle_game_start(&msg);
            break;
        case CMD_MAP_DATA:
            handle_map_data(&msg);
            break;
        case CMD_GAME_STATE:
            handle_game_state(&msg);
            break;
        case CMD_GAME_EVENT:
            handle_game_event(&msg);
            break;
        case CMD_GAME_END:
            handle_game_end(&msg);
            break;
        case CMD_CHAT_MSG:
            handle_chat_msg(&msg);
            break;
        default:
            break;
    }
    
    game_set_dirty();
}

void *recv_thread_func(void *arg) {
    (void)arg;
    
    char buf[4096];
    char msg_buf[4096];
    int buf_len = 0;
    
    while (network_is_connected()) {
        int n = network_recv(buf + buf_len, (int)(sizeof(buf) - (size_t)buf_len - 1));
        
        if (n < 0) {
            /* 连接断开或错误 */
            pthread_mutex_lock(&g_state.lock);
            g_state.connected = 0;
            pthread_mutex_unlock(&g_state.lock);
            break;
        }
        
        if (n == 0) {
            /* poll 超时，继续循环检查连接状态 */
            continue;
        }
        
        buf_len += n;
        buf[buf_len] = '\0';
        
        /* 提取完整消息 */
        while (protocol_extract_message(buf, &buf_len, msg_buf, sizeof(msg_buf)) > 0) {
            game_update_from_server(msg_buf);
        }
    }
    
    return NULL;
}
