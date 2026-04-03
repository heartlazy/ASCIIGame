/**
 * player.c - 玩家管理模块实现
 */

#include "player.h"
#include "network.h"
#include "log.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <sys/time.h>

/* 全局状态 */
static Player *players[MAX_PLAYERS];
static int next_player_id = 1;
static pthread_mutex_t players_mutex = PTHREAD_MUTEX_INITIALIZER;

long player_get_time_ms(void) {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return tv.tv_sec * 1000L + tv.tv_usec / 1000L;
}

int player_init(void) {
    pthread_mutex_lock(&players_mutex);
    
    for (int i = 0; i < MAX_PLAYERS; i++) {
        players[i] = NULL;
    }
    next_player_id = 1;
    
    pthread_mutex_unlock(&players_mutex);
    
    log_info("Player manager initialized");
    return 0;
}

void player_cleanup(void) {
    pthread_mutex_lock(&players_mutex);
    
    for (int i = 0; i < MAX_PLAYERS; i++) {
        if (players[i] != NULL) {
            pthread_mutex_destroy(&players[i]->lock);
            free(players[i]);
            players[i] = NULL;
        }
    }
    
    pthread_mutex_unlock(&players_mutex);
    
    log_info("Player manager cleaned up");
}

Player *player_create(int fd) {
    pthread_mutex_lock(&players_mutex);
    
    /* 查找空槽位 */
    int slot = -1;
    for (int i = 0; i < MAX_PLAYERS; i++) {
        if (players[i] == NULL) {
            slot = i;
            break;
        }
    }
    
    if (slot < 0) {
        log_error("No available player slots");
        pthread_mutex_unlock(&players_mutex);
        return NULL;
    }
    
    /* 分配玩家 */
    Player *player = calloc(1, sizeof(Player));
    if (player == NULL) {
        log_error("Failed to allocate player");
        pthread_mutex_unlock(&players_mutex);
        return NULL;
    }
    
    /* 初始化 */
    player->fd = fd;
    player->id = next_player_id++;
    player->username[0] = '\0';
    player->room_id = -1;
    player->status = PLAYER_CONNECTED;
    
    /* 游戏属性 */
    player->x = 0;
    player->y = 0;
    player->hp = INITIAL_HP;
    player->max_hp = INITIAL_HP;
    player->atk = INITIAL_ATK;
    player->base_atk = INITIAL_ATK;
    player->def = INITIAL_DEF;
    
    /* 冷却时间 */
    player->last_move_time = 0;
    player->last_attack_time = 0;
    
    /* 道具 */
    player->inventory_count = 0;
    player->has_shield = 0;
    player->atk_buff_expire = 0;
    player->atk_buff_warned = 0;
    
    /* 网络缓冲 */
    player->recv_len = 0;
    
    /* 初始化锁 */
    pthread_mutex_init(&player->lock, NULL);
    
    players[slot] = player;
    
    pthread_mutex_unlock(&players_mutex);
    
    log_info("Player created: id=%d, fd=%d", player->id, fd);
    return player;
}

void player_destroy(Player *player) {
    if (player == NULL) {
        return;
    }
    
    pthread_mutex_lock(&players_mutex);
    
    /* 从数组中移除 */
    for (int i = 0; i < MAX_PLAYERS; i++) {
        if (players[i] == player) {
            players[i] = NULL;
            break;
        }
    }
    
    int id = player->id;
    int fd = player->fd;
    
    pthread_mutex_destroy(&player->lock);
    free(player);
    
    pthread_mutex_unlock(&players_mutex);
    
    log_info("Player destroyed: id=%d, fd=%d", id, fd);
}

Player *player_find_by_fd(int fd) {
    pthread_mutex_lock(&players_mutex);
    
    for (int i = 0; i < MAX_PLAYERS; i++) {
        if (players[i] != NULL && players[i]->fd == fd) {
            pthread_mutex_unlock(&players_mutex);
            return players[i];
        }
    }
    
    pthread_mutex_unlock(&players_mutex);
    return NULL;
}

Player *player_find_by_id(int id) {
    pthread_mutex_lock(&players_mutex);
    
    for (int i = 0; i < MAX_PLAYERS; i++) {
        if (players[i] != NULL && players[i]->id == id) {
            pthread_mutex_unlock(&players_mutex);
            return players[i];
        }
    }
    
    pthread_mutex_unlock(&players_mutex);
    return NULL;
}

Player *player_find_by_username(const char *username) {
    if (username == NULL) {
        return NULL;
    }
    
    pthread_mutex_lock(&players_mutex);
    
    for (int i = 0; i < MAX_PLAYERS; i++) {
        if (players[i] != NULL && 
            players[i]->username[0] != '\0' &&
            strcmp(players[i]->username, username) == 0) {
            pthread_mutex_unlock(&players_mutex);
            return players[i];
        }
    }
    
    pthread_mutex_unlock(&players_mutex);
    return NULL;
}

int player_set_username(Player *player, const char *username) {
    if (player == NULL || username == NULL) {
        return -1;
    }
    
    pthread_mutex_lock(&player->lock);
    strncpy(player->username, username, MAX_USERNAME - 1);
    player->username[MAX_USERNAME - 1] = '\0';
    pthread_mutex_unlock(&player->lock);
    
    return 0;
}

void player_reset_game_state(Player *player) {
    if (player == NULL) {
        return;
    }
    
    pthread_mutex_lock(&player->lock);
    
    player->hp = INITIAL_HP;
    player->max_hp = INITIAL_HP;
    player->atk = INITIAL_ATK;
    player->base_atk = INITIAL_ATK;
    player->def = INITIAL_DEF;
    
    player->last_move_time = 0;
    player->last_attack_time = 0;
    
    player->inventory_count = 0;
    for (int i = 0; i < MAX_INVENTORY; i++) {
        player->inventory[i].type = ITEM_NONE;
    }
    
    player->has_shield = 0;
    player->atk_buff_expire = 0;
    player->atk_buff_warned = 0;
    
    pthread_mutex_unlock(&player->lock);
}

int player_get_online_count(void) {
    int count = 0;
    
    pthread_mutex_lock(&players_mutex);
    
    for (int i = 0; i < MAX_PLAYERS; i++) {
        if (players[i] != NULL && players[i]->status != PLAYER_DISCONNECTED) {
            count++;
        }
    }
    
    pthread_mutex_unlock(&players_mutex);
    return count;
}

int player_send(Player *player, const char *msg) {
    if (player == NULL || msg == NULL) {
        return -1;
    }
    
    int len = (int)strlen(msg);
    int ret = send_all(player->fd, msg, len);
    
    if (ret < 0) {
        log_error("Failed to send to player %d", player->id);
        return -1;
    }
    
    return 0;
}

int player_add_item(Player *player, ItemType type) {
    if (player == NULL || type == ITEM_NONE) {
        return -1;
    }
    
    pthread_mutex_lock(&player->lock);
    
    if (player->inventory_count >= MAX_INVENTORY) {
        pthread_mutex_unlock(&player->lock);
        return -1;  /* 背包已满 */
    }
    
    player->inventory[player->inventory_count].type = type;
    player->inventory_count++;
    
    pthread_mutex_unlock(&player->lock);
    return 0;
}

ItemType player_use_item(Player *player, int index) {
    if (player == NULL) {
        return ITEM_NONE;
    }
    
    pthread_mutex_lock(&player->lock);
    
    if (index < 0 || index >= player->inventory_count) {
        pthread_mutex_unlock(&player->lock);
        return ITEM_NONE;
    }
    
    ItemType type = player->inventory[index].type;
    
    /* 移除道具（将后面的道具前移） */
    for (int i = index; i < player->inventory_count - 1; i++) {
        player->inventory[i] = player->inventory[i + 1];
    }
    player->inventory_count--;
    player->inventory[player->inventory_count].type = ITEM_NONE;
    
    pthread_mutex_unlock(&player->lock);
    return type;
}
