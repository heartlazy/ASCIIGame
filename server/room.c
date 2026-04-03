/**
 * room.c - 房间管理模块实现
 */

#include "room.h"
#include "player.h"
#include "map.h"
#include "network.h"
#include "wal.h"
#include "recovery.h"
#include "snapshot.h"
#include "log.h"
#include "../common/protocol.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/* 全局状态 */
static Room *rooms[MAX_ROOMS];
static int next_room_id = 1;
static pthread_mutex_t rooms_mutex = PTHREAD_MUTEX_INITIALIZER;

/* 状态名称 */
static const char *status_names[] = {
    "WAITING",
    "STARTING",
    "GAMING",
    "ENDED"
};

int room_init(void) {
    pthread_mutex_lock(&rooms_mutex);
    
    for (int i = 0; i < MAX_ROOMS; i++) {
        rooms[i] = NULL;
    }
    
    /* 从 WAL 文件恢复最大房间 ID，避免 ID 冲突 */
    int max_id = recovery_get_max_room_id();
    next_room_id = max_id + 1;
    
    pthread_mutex_unlock(&rooms_mutex);
    
    log_info("Room manager initialized, next_room_id=%d", next_room_id);
    return 0;
}

void room_cleanup(void) {
    pthread_mutex_lock(&rooms_mutex);
    
    /* 第一步：通知所有游戏线程停止 */
    for (int i = 0; i < MAX_ROOMS; i++) {
        if (rooms[i] != NULL) {
            pthread_mutex_lock(&rooms[i]->lock);
            rooms[i]->game_thread_running = 0;
            rooms[i]->status = ROOM_ENDED;
            pthread_mutex_unlock(&rooms[i]->lock);
        }
    }
    
    pthread_mutex_unlock(&rooms_mutex);
    
    /* 第二步：等待游戏线程退出（最多等待 2 秒） */
    usleep(200000);  /* 等待 200ms 让线程有机会退出 */
    
    pthread_mutex_lock(&rooms_mutex);
    
    /* 第三步：释放资源 */
    for (int i = 0; i < MAX_ROOMS; i++) {
        if (rooms[i] != NULL) {
            pthread_mutex_destroy(&rooms[i]->lock);
            free(rooms[i]);
            rooms[i] = NULL;
        }
    }
    
    pthread_mutex_unlock(&rooms_mutex);
    
    log_info("Room manager cleaned up");
}

Room *room_create(const char *name, int max_players) {
    if (name == NULL || strlen(name) == 0) {
        log_error("room_create: name is NULL or empty");
        return NULL;
    }
    
    /* 验证人数范围 */
    if (max_players < MIN_ROOM_PLAYERS || max_players > MAX_ROOM_PLAYERS) {
        max_players = MAX_ROOM_PLAYERS;
    }
    
    pthread_mutex_lock(&rooms_mutex);
    
    /* 查找空槽位 */
    int slot = -1;
    for (int i = 0; i < MAX_ROOMS; i++) {
        if (rooms[i] == NULL) {
            slot = i;
            break;
        }
    }
    
    if (slot < 0) {
        log_error("No available room slots");
        pthread_mutex_unlock(&rooms_mutex);
        return NULL;
    }
    
    /* 分配房间 */
    Room *room = calloc(1, sizeof(Room));
    if (room == NULL) {
        log_error("Failed to allocate room");
        pthread_mutex_unlock(&rooms_mutex);
        return NULL;
    }
    
    /* 初始化 */
    room->id = next_room_id++;
    strncpy(room->name, name, MAX_ROOM_NAME - 1);
    room->name[MAX_ROOM_NAME - 1] = '\0';
    room->max_players = max_players;
    room->player_count = 0;
    room->status = ROOM_WAITING;
    
    /* 初始化玩家列表 */
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        room->player_ids[i] = -1;
    }
    
    /* 地图数据 */
    room->item_count = 0;
    room->poison_radius = map_get_initial_poison_radius();
    
    /* 时间 */
    room->game_start_time = 0;
    room->last_item_spawn = 0;
    room->last_poison_shrink = 0;
    room->countdown_start = 0;
    
    /* 线程 */
    room->game_thread_running = 0;
    
    /* WAL */
    room->wal = NULL;
    
    /* 恢复相关 */
    room->is_recovery_room = 0;
    room->expected_players = 0;
    room->recovery_start_time = 0;
    room->original_room_id = -1;
    
    /* 快照相关 */
    room->last_snapshot_time = 0;
    
    /* 初始化锁 */
    pthread_mutex_init(&room->lock, NULL);
    
    rooms[slot] = room;
    
    pthread_mutex_unlock(&rooms_mutex);
    
    log_info("Room created: id=%d, name=%s, max=%d", room->id, name, max_players);
    return room;
}

void room_destroy(Room *room) {
    if (room == NULL) {
        return;
    }
    
    pthread_mutex_lock(&rooms_mutex);
    
    /* 从数组中移除 */
    for (int i = 0; i < MAX_ROOMS; i++) {
        if (rooms[i] == room) {
            rooms[i] = NULL;
            break;
        }
    }
    
    int id = room->id;
    
    /* 关闭并删除 WAL 和快照（所有玩家退出，不需要恢复） */
    if (room->wal != NULL) {
        /* 恢复房间使用原始房间 ID 的 WAL 文件 */
        int wal_room_id = (room->original_room_id >= 0) ? room->original_room_id : id;
        wal_close_for_room(room->wal);
        room->wal = NULL;
        wal_delete_for_room(wal_room_id);
        snapshot_delete(wal_room_id);
        log_info("WAL and snapshot deleted for room %d (room destroyed)", wal_room_id);
    }
    
    pthread_mutex_destroy(&room->lock);
    free(room);
    
    pthread_mutex_unlock(&rooms_mutex);
    
    log_info("Room destroyed: id=%d", id);
}

Room *room_find_by_id(int id) {
    pthread_mutex_lock(&rooms_mutex);
    
    for (int i = 0; i < MAX_ROOMS; i++) {
        if (rooms[i] != NULL && rooms[i]->id == id) {
            pthread_mutex_unlock(&rooms_mutex);
            return rooms[i];
        }
    }
    
    pthread_mutex_unlock(&rooms_mutex);
    return NULL;
}

int room_add_player(Room *room, Player *player) {
    if (room == NULL || player == NULL) {
        return -1;
    }
    
    pthread_mutex_lock(&room->lock);
    
    /* 检查房间状态 */
    if (room->status == ROOM_GAMING) {
        pthread_mutex_unlock(&room->lock);
        return -2;  /* 游戏进行中 */
    }
    
    /* 检查人数 */
    if (room->player_count >= room->max_players) {
        pthread_mutex_unlock(&room->lock);
        return -1;  /* 房间已满 */
    }
    
    /* 查找空位 */
    int slot = -1;
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] < 0) {
            slot = i;
            break;
        }
    }
    
    if (slot < 0) {
        pthread_mutex_unlock(&room->lock);
        return -1;
    }
    
    /* 添加玩家 */
    room->player_ids[slot] = player->id;
    room->player_count++;
    
    /* 更新玩家状态 */
    pthread_mutex_lock(&player->lock);
    player->room_id = room->id;
    player->status = PLAYER_IN_ROOM;
    pthread_mutex_unlock(&player->lock);
    
    pthread_mutex_unlock(&room->lock);
    
    log_info("Player %d joined room %d", player->id, room->id);
    
    /* 广播玩家加入 */
    char msg[MAX_MSG_LEN];
    protocol_build_player_join(msg, sizeof(msg), player->id, player->username);
    room_broadcast(room, msg);
    
    return 0;
}

int room_remove_player(Room *room, Player *player) {
    if (room == NULL || player == NULL) {
        return -1;
    }
    
    pthread_mutex_lock(&room->lock);
    
    /* 查找并移除玩家 */
    int found = 0;
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] == player->id) {
            room->player_ids[i] = -1;
            room->player_count--;
            found = 1;
            break;
        }
    }
    
    if (!found) {
        pthread_mutex_unlock(&room->lock);
        return -1;
    }
    
    /* 更新玩家状态 */
    pthread_mutex_lock(&player->lock);
    player->room_id = -1;
    player->status = PLAYER_LOBBY;
    pthread_mutex_unlock(&player->lock);
    
    int room_id = room->id;
    int player_id = player->id;
    int remaining = room->player_count;
    
    log_info("Player %d left room %d", player_id, room_id);
    
    /* 先释放锁，再广播和销毁 */
    pthread_mutex_unlock(&room->lock);
    
    /* 广播玩家离开（不持有 room->lock） */
    if (remaining > 0) {
        char msg[MAX_MSG_LEN];
        protocol_build_player_leave(msg, sizeof(msg), player_id);
        room_broadcast(room, msg);
    }
    
    /* 如果房间空了，销毁房间 */
    if (remaining == 0) {
        room_destroy(room);
    }
    
    return 0;
}

int room_start_game(Room *room) {
    if (room == NULL) {
        return -1;
    }
    
    int player_ids[MAX_ROOM_PLAYERS];
    int id_count = 0;
    
    pthread_mutex_lock(&room->lock);
    
    if (room->status != ROOM_WAITING && room->status != ROOM_STARTING) {
        pthread_mutex_unlock(&room->lock);
        return -1;
    }
    
    if (room->player_count < MIN_ROOM_PLAYERS) {
        pthread_mutex_unlock(&room->lock);
        return -1;
    }
    
    /* 生成地图 */
    map_generate(room->map);
    
    /* 初始化毒圈 */
    room->poison_radius = map_get_initial_poison_radius();
    
    /* 初始化道具 - 在 $ 刷新点生成初始道具 */
    room->item_count = 0;
    for (int y = 0; y < MAP_HEIGHT && room->item_count < MAX_MAP_ITEMS; y++) {
        for (int x = 0; x < MAP_WIDTH && room->item_count < MAX_MAP_ITEMS; x++) {
            if (room->map[y][x] == '$') {
                /* 随机生成道具类型 */
                MapItemType type = (MapItemType)(rand() % 3 + 1);
                room->items[room->item_count].x = x;
                room->items[room->item_count].y = y;
                room->items[room->item_count].type = type;
                room->items[room->item_count].active = 1;
                room->item_count++;
            }
        }
    }
    
    /* 设置时间 */
    room->game_start_time = player_get_time_ms();
    room->last_item_spawn = room->game_start_time;
    room->last_poison_shrink = room->game_start_time;
    
    /* 收集玩家 ID */
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] >= 0) {
            player_ids[id_count++] = room->player_ids[i];
        }
    }
    
    room->status = ROOM_GAMING;
    
    pthread_mutex_unlock(&room->lock);
    
    /* 初始化玩家位置和状态（不持有 room->lock） */
    for (int i = 0; i < id_count; i++) {
        Player *p = player_find_by_id(player_ids[i]);
        if (p != NULL) {
            player_reset_game_state(p);
            
            /* 随机位置 */
            int x, y;
            pthread_mutex_lock(&room->lock);
            map_random_position(room->map, &x, &y);
            pthread_mutex_unlock(&room->lock);
            
            pthread_mutex_lock(&p->lock);
            p->x = x;
            p->y = y;
            p->status = PLAYER_GAMING;
            pthread_mutex_unlock(&p->lock);
        }
    }
    
    log_info("Game started in room %d", room->id);
    
    /* 写入 WAL - 游戏开始 */
    if (room->wal != NULL) {
        char wal_data[512];
        snprintf(wal_data, sizeof(wal_data), "room_name=%s,max_players=%d",
                 room->name, room->max_players);
        wal_write(room->wal, WAL_GAME_START, wal_data);
        
        /* 写入每个玩家的初始状态 */
        for (int i = 0; i < id_count; i++) {
            Player *p = player_find_by_id(player_ids[i]);
            if (p != NULL) {
                pthread_mutex_lock(&p->lock);
                snprintf(wal_data, sizeof(wal_data),
                         "pid=%d,username=%s,x=%d,y=%d,hp=%d,max_hp=%d,atk=%d,def=%d,shield=%d,inv=%d,%d,%d,%d,%d",
                         p->id, p->username, p->x, p->y, p->hp, p->max_hp, p->atk, p->def,
                         p->has_shield,
                         (int)p->inventory[0].type, (int)p->inventory[1].type,
                         (int)p->inventory[2].type, (int)p->inventory[3].type,
                         (int)p->inventory[4].type);
                pthread_mutex_unlock(&p->lock);
                wal_write(room->wal, WAL_PLAYER_JOIN, wal_data);
            }
        }
        
        /* 写入初始道具状态 */
        pthread_mutex_lock(&room->lock);
        for (int i = 0; i < room->item_count; i++) {
            if (room->items[i].active) {
                snprintf(wal_data, sizeof(wal_data), "type=%d,x=%d,y=%d",
                         room->items[i].type, room->items[i].x, room->items[i].y);
                wal_write(room->wal, WAL_ITEM_SPAWN, wal_data);
            }
        }
        pthread_mutex_unlock(&room->lock);
        
        wal_sync(room->wal);
    }
    
    /* 广播游戏开始 */
    char msg[MAX_MSG_LEN];
    protocol_build_game_start(msg, sizeof(msg));
    room_broadcast(room, msg);
    
    /* 地图模板在客户端，不需要发送 */
    
    return 0;
}

void room_end_game(Room *room, int winner_id) {
    if (room == NULL) {
        return;
    }
    
    int player_ids[MAX_ROOM_PLAYERS];
    int count = 0;
    
    pthread_mutex_lock(&room->lock);
    room->status = ROOM_ENDED;
    room->game_thread_running = 0;
    
    /* 收集玩家 ID */
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] >= 0) {
            player_ids[count++] = room->player_ids[i];
        }
    }
    pthread_mutex_unlock(&room->lock);
    
    log_info("Game ended in room %d, winner=%d", room->id, winner_id);
    
    /* 写入 WAL - 游戏结束 */
    pthread_mutex_lock(&room->lock);
    if (room->wal != NULL) {
        char wal_data[64];
        snprintf(wal_data, sizeof(wal_data), "winner=%d", winner_id);
        wal_write(room->wal, WAL_GAME_END, wal_data);
        wal_sync(room->wal);
    }
    pthread_mutex_unlock(&room->lock);
    
    /* 广播游戏结束 */
    char msg[MAX_MSG_LEN];
    protocol_build_game_end(msg, sizeof(msg), winner_id, "");
    room_broadcast(room, msg);
    
    /* 重置玩家状态（不持有 room->lock） */
    for (int i = 0; i < count; i++) {
        Player *p = player_find_by_id(player_ids[i]);
        if (p != NULL) {
            pthread_mutex_lock(&p->lock);
            p->status = PLAYER_IN_ROOM;
            /* 重置游戏相关状态 */
            p->hp = p->max_hp;
            p->x = 0;
            p->y = 0;
            p->has_shield = 0;
            p->atk = p->base_atk;
            p->atk_buff_expire = 0;
            p->atk_buff_warned = 0;
            p->inventory_count = 0;
            pthread_mutex_unlock(&p->lock);
        }
    }
    
    /* 重置房间状态 */
    pthread_mutex_lock(&room->lock);
    room->status = ROOM_WAITING;
    
    /* 清空地图 - 下次开始时会重新生成 */
    memset(room->map, 0, sizeof(room->map));
    
    /* 清空道具 */
    room->item_count = 0;
    for (int i = 0; i < MAX_MAP_ITEMS; i++) {
        room->items[i].active = 0;
    }
    
    /* 重置毒圈 */
    room->poison_radius = map_get_initial_poison_radius();
    
    /* 重置时间 */
    room->game_start_time = 0;
    room->last_item_spawn = 0;
    room->last_poison_shrink = 0;
    
    /* 关闭 WAL 并删除文件（游戏正常结束，不需要恢复） */
    if (room->wal != NULL) {
        /* 恢复房间使用原始房间 ID 的 WAL 文件 */
        int wal_room_id = (room->original_room_id >= 0) ? room->original_room_id : room->id;
        wal_close_for_room(room->wal);
        room->wal = NULL;
        wal_delete_for_room(wal_room_id);
        snapshot_delete(wal_room_id);  /* 同时删除快照 */
        
        /* 删除 .recovery 文件 */
        char recovery_path[512];
        snprintf(recovery_path, sizeof(recovery_path), "%s/room_%d.recovery", WAL_DIR, wal_room_id);
        unlink(recovery_path);
        
        log_info("WAL, snapshot and recovery files deleted for room %d (game ended normally)", wal_room_id);
    }
    
    /* 重置恢复相关字段 */
    room->is_recovery_room = 0;
    room->expected_players = 0;
    room->recovery_start_time = 0;
    room->original_room_id = -1;
    room->last_snapshot_time = 0;
    
    pthread_mutex_unlock(&room->lock);
}

int room_broadcast(Room *room, const char *msg) {
    if (room == NULL || msg == NULL) {
        return 0;
    }
    
    int sent = 0;
    int player_ids[MAX_ROOM_PLAYERS];
    int count = 0;
    
    /* 先收集玩家 ID */
    pthread_mutex_lock(&room->lock);
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] >= 0) {
            player_ids[count++] = room->player_ids[i];
        }
    }
    pthread_mutex_unlock(&room->lock);
    
    /* 然后发送消息（不持有锁） */
    for (int i = 0; i < count; i++) {
        Player *p = player_find_by_id(player_ids[i]);
        if (p != NULL && player_send(p, msg) == 0) {
            sent++;
        }
    }
    
    return sent;
}

int room_get_list(char *buf, int max_len) {
    if (buf == NULL || max_len <= 0) {
        return 0;
    }
    
    /* 先收集房间信息 */
    struct {
        int id;
        char name[MAX_ROOM_NAME];
        int player_count;
        int max_players;
        int status;
    } room_info[MAX_ROOMS];
    int room_count = 0;
    
    pthread_mutex_lock(&rooms_mutex);
    for (int i = 0; i < MAX_ROOMS; i++) {
        if (rooms[i] != NULL) {
            Room *r = rooms[i];
            pthread_mutex_lock(&r->lock);
            room_info[room_count].id = r->id;
            strncpy(room_info[room_count].name, r->name, MAX_ROOM_NAME - 1);
            room_info[room_count].name[MAX_ROOM_NAME - 1] = '\0';
            room_info[room_count].player_count = r->player_count;
            room_info[room_count].max_players = r->max_players;
            room_info[room_count].status = r->status;
            pthread_mutex_unlock(&r->lock);
            room_count++;
        }
    }
    pthread_mutex_unlock(&rooms_mutex);
    
    /* 构建响应字符串 */
    char data[MAX_MSG_LEN] = "";
    int pos = 0;
    int first = 1;
    
    for (int i = 0; i < room_count; i++) {
        int written = snprintf(data + pos, sizeof(data) - (size_t)pos,
                               "%s%d,%s,%d,%d,%d",
                               first ? "" : ";",
                               room_info[i].id, room_info[i].name,
                               room_info[i].player_count,
                               room_info[i].max_players, room_info[i].status);
        
        if (written > 0 && pos + written < (int)sizeof(data)) {
            pos += written;
            first = 0;
        }
    }
    
    return protocol_build_room_list(buf, max_len, data);
}

int room_all_ready(Room *room) {
    if (room == NULL) {
        return 0;
    }
    
    int player_ids[MAX_ROOM_PLAYERS];
    int count = 0;
    int player_count = 0;
    
    pthread_mutex_lock(&room->lock);
    player_count = room->player_count;
    if (player_count < MIN_ROOM_PLAYERS) {
        pthread_mutex_unlock(&room->lock);
        return 0;
    }
    
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] >= 0) {
            player_ids[count++] = room->player_ids[i];
        }
    }
    pthread_mutex_unlock(&room->lock);
    
    int all_ready = 1;
    for (int i = 0; i < count; i++) {
        Player *p = player_find_by_id(player_ids[i]);
        if (p != NULL) {
            pthread_mutex_lock(&p->lock);
            if (p->status != PLAYER_READY) {
                all_ready = 0;
            }
            pthread_mutex_unlock(&p->lock);
            
            if (!all_ready) break;
        }
    }
    
    return all_ready;
}

int room_get_alive_count(Room *room) {
    if (room == NULL) {
        return 0;
    }
    
    int player_ids[MAX_ROOM_PLAYERS];
    int id_count = 0;
    
    pthread_mutex_lock(&room->lock);
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] >= 0) {
            player_ids[id_count++] = room->player_ids[i];
        }
    }
    pthread_mutex_unlock(&room->lock);
    
    int count = 0;
    for (int i = 0; i < id_count; i++) {
        Player *p = player_find_by_id(player_ids[i]);
        if (p != NULL) {
            pthread_mutex_lock(&p->lock);
            if (p->status == PLAYER_GAMING && p->hp > 0) {
                count++;
            }
            pthread_mutex_unlock(&p->lock);
        }
    }
    
    return count;
}

int room_get_count(void) {
    int count = 0;
    
    pthread_mutex_lock(&rooms_mutex);
    
    for (int i = 0; i < MAX_ROOMS; i++) {
        if (rooms[i] != NULL) {
            count++;
        }
    }
    
    pthread_mutex_unlock(&rooms_mutex);
    return count;
}

const char *room_status_name(RoomStatus status) {
    if (status >= 0 && status <= ROOM_ENDED) {
        return status_names[status];
    }
    return "UNKNOWN";
}
