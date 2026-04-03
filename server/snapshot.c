/**
 * snapshot.c - 游戏状态快照模块实现
 * 
 * 快照文件格式（二进制）：
 * - 魔数 (4 bytes): "SNAP"
 * - 版本 (4 bytes): 1
 * - 时间戳 (8 bytes)
 * - 房间数据 (sizeof(SnapshotData))
 * - 玩家数据 (变长)
 */

#include "snapshot.h"
#include "player.h"
#include "wal.h"
#include "log.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/stat.h>
#include <errno.h>
#include <time.h>

#define SNAPSHOT_MAGIC "SNAP"
#define SNAPSHOT_VERSION 1

/* 快照中的玩家数据 */
typedef struct {
    int id;
    char username[MAX_USERNAME];
    int x, y;
    int hp, max_hp;
    int atk, def, base_atk;
    int has_shield;
    int status;
    int inventory[MAX_INVENTORY];
    int inventory_count;
    long atk_buff_expire;  /* 攻击 buff 过期时间 */
} SnapshotPlayer;

/* 快照数据结构 */
typedef struct {
    /* 房间基本信息 */
    int room_id;
    char room_name[MAX_ROOM_NAME];
    int max_players;
    int player_count;
    int status;
    
    /* 地图数据 */
    char map[MAP_HEIGHT][MAP_WIDTH + 1];
    int poison_radius;
    
    /* 道具数据 */
    MapItem items[MAX_MAP_ITEMS];
    int item_count;
    
    /* 时间数据 */
    long game_start_time;
    long last_item_spawn;
    long last_poison_shrink;
    
    /* 玩家 ID 列表 */
    int player_ids[MAX_ROOM_PLAYERS];
} SnapshotData;

void snapshot_get_path(int room_id, char *path, int max_len) {
    snprintf(path, (size_t)max_len, "%s/room_%d.snap", SNAPSHOT_DIR, room_id);
}

int snapshot_exists(int room_id) {
    char path[256];
    snapshot_get_path(room_id, path, sizeof(path));
    
    struct stat st;
    return stat(path, &st) == 0 ? 1 : 0;
}

int snapshot_delete(int room_id) {
    char path[256];
    snapshot_get_path(room_id, path, sizeof(path));
    
    if (unlink(path) < 0) {
        if (errno != ENOENT) {
            log_error("Failed to delete snapshot %s: %s", path, strerror(errno));
            return -1;
        }
    }
    
    log_info("Snapshot deleted for room %d", room_id);
    return 0;
}

int snapshot_should_save(Room *room) {
    if (room == NULL || room->status != ROOM_GAMING) {
        return 0;
    }
    
    long now = player_get_time_ms();
    return (now - room->last_snapshot_time) >= SNAPSHOT_INTERVAL_MS;
}

int snapshot_save(Room *room) {
    if (room == NULL) {
        return -1;
    }
    
    /* 使用原始房间 ID（如果是恢复的房间） */
    int room_id = (room->original_room_id >= 0) ? room->original_room_id : room->id;
    
    char path[256];
    snapshot_get_path(room_id, path, sizeof(path));
    
    /* 先写入临时文件 */
    char tmp_path[280];
    snprintf(tmp_path, sizeof(tmp_path), "%s.tmp", path);
    
    FILE *fp = fopen(tmp_path, "wb");
    if (fp == NULL) {
        log_error("Failed to create snapshot file %s: %s", tmp_path, strerror(errno));
        return -1;
    }
    
    /* 写入魔数和版本 */
    fwrite(SNAPSHOT_MAGIC, 1, 4, fp);
    int version = SNAPSHOT_VERSION;
    fwrite(&version, sizeof(version), 1, fp);
    
    /* 写入时间戳 */
    long timestamp = player_get_time_ms();
    fwrite(&timestamp, sizeof(timestamp), 1, fp);
    
    /* 收集房间数据 */
    SnapshotData data;
    memset(&data, 0, sizeof(data));
    
    pthread_mutex_lock(&room->lock);
    
    data.room_id = room_id;
    strncpy(data.room_name, room->name, MAX_ROOM_NAME - 1);
    data.max_players = room->max_players;
    data.player_count = room->player_count;
    data.status = room->status;
    
    memcpy(data.map, room->map, sizeof(data.map));
    data.poison_radius = room->poison_radius;
    
    memcpy(data.items, room->items, sizeof(data.items));
    data.item_count = room->item_count;
    
    data.game_start_time = room->game_start_time;
    data.last_item_spawn = room->last_item_spawn;
    data.last_poison_shrink = room->last_poison_shrink;
    
    memcpy(data.player_ids, room->player_ids, sizeof(data.player_ids));
    
    pthread_mutex_unlock(&room->lock);
    
    /* 写入房间数据 */
    fwrite(&data, sizeof(data), 1, fp);
    
    /* 收集并写入玩家数据 */
    int player_count = 0;
    SnapshotPlayer players[MAX_ROOM_PLAYERS];
    memset(players, 0, sizeof(players));
    
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (data.player_ids[i] < 0) continue;
        
        Player *p = player_find_by_id(data.player_ids[i]);
        if (p == NULL) continue;
        
        pthread_mutex_lock(&p->lock);
        
        players[player_count].id = p->id;
        strncpy(players[player_count].username, p->username, MAX_USERNAME - 1);
        players[player_count].x = p->x;
        players[player_count].y = p->y;
        players[player_count].hp = p->hp;
        players[player_count].max_hp = p->max_hp;
        players[player_count].atk = p->atk;
        players[player_count].def = p->def;
        players[player_count].base_atk = p->base_atk;
        players[player_count].has_shield = p->has_shield;
        players[player_count].status = p->status;
        players[player_count].inventory_count = p->inventory_count;
        players[player_count].atk_buff_expire = p->atk_buff_expire;
        for (int j = 0; j < MAX_INVENTORY; j++) {
            players[player_count].inventory[j] = p->inventory[j].type;
        }
        
        pthread_mutex_unlock(&p->lock);
        player_count++;
    }
    
    /* 写入玩家数量和数据 */
    fwrite(&player_count, sizeof(player_count), 1, fp);
    if (player_count > 0) {
        fwrite(players, sizeof(SnapshotPlayer), (size_t)player_count, fp);
    }
    
    /* 刷盘 */
    fflush(fp);
    fsync(fileno(fp));
    fclose(fp);
    
    /* 原子替换 */
    if (rename(tmp_path, path) < 0) {
        log_error("Failed to rename snapshot file: %s", strerror(errno));
        unlink(tmp_path);
        return -1;
    }
    
    /* 更新快照时间 */
    pthread_mutex_lock(&room->lock);
    room->last_snapshot_time = timestamp;
    pthread_mutex_unlock(&room->lock);
    
    /* 清空 WAL 并写入当前完整状态（确保 WAL 可独立恢复） */
    if (room->wal != NULL) {
        wal_truncate(room->wal);
        
        /* 写入快照标记 */
        char snap_data[256];
        snprintf(snap_data, sizeof(snap_data), 
                 "snapshot_time=%ld,room_name=%s,poison_radius=%d",
                 timestamp, data.room_name, data.poison_radius);
        wal_write(room->wal, WAL_CHECKPOINT, snap_data);
        
        /* 写入所有玩家的当前状态（关键！确保 WAL 可独立恢复） */
        for (int i = 0; i < player_count; i++) {
            /* 计算 buff 剩余时间 */
            long atk_buff_remain = 0;
            if (players[i].atk_buff_expire > timestamp) {
                atk_buff_remain = players[i].atk_buff_expire - timestamp;
            }
            
            char player_data[512];
            snprintf(player_data, sizeof(player_data),
                     "pid=%d,username=%s,x=%d,y=%d,hp=%d,max_hp=%d,atk=%d,def=%d,base_atk=%d,shield=%d,atk_buff_remain=%ld,inv=%d,%d,%d,%d,%d",
                     players[i].id, players[i].username,
                     players[i].x, players[i].y,
                     players[i].hp, players[i].max_hp,
                     players[i].atk, players[i].def, players[i].base_atk,
                     players[i].has_shield,
                     atk_buff_remain,
                     players[i].inventory[0], players[i].inventory[1],
                     players[i].inventory[2], players[i].inventory[3],
                     players[i].inventory[4]);
            wal_write(room->wal, WAL_PLAYER_JOIN, player_data);
        }
        
        /* 写入所有未拾取道具的状态 */
        for (int i = 0; i < data.item_count; i++) {
            if (data.items[i].active) {
                char item_data[128];
                snprintf(item_data, sizeof(item_data), "type=%d,x=%d,y=%d",
                         data.items[i].type, data.items[i].x, data.items[i].y);
                wal_write(room->wal, WAL_ITEM_SPAWN, item_data);
            }
        }
        
        wal_sync(room->wal);
    }
    
    log_info("Snapshot saved for room %d, %d players, %d items", room_id, player_count, data.item_count);
    return 0;
}


int snapshot_load(int room_id, Room *room) {
    if (room == NULL) {
        return -1;
    }
    
    char path[256];
    snapshot_get_path(room_id, path, sizeof(path));
    
    FILE *fp = fopen(path, "rb");
    if (fp == NULL) {
        log_debug("No snapshot file for room %d", room_id);
        return -1;
    }
    
    /* 读取并验证魔数 */
    char magic[5] = {0};
    if (fread(magic, 1, 4, fp) != 4 || strcmp(magic, SNAPSHOT_MAGIC) != 0) {
        log_error("Invalid snapshot magic: %s", magic);
        fclose(fp);
        return -1;
    }
    
    /* 读取并验证版本 */
    int version;
    if (fread(&version, sizeof(version), 1, fp) != 1 || version != SNAPSHOT_VERSION) {
        log_error("Unsupported snapshot version: %d", version);
        fclose(fp);
        return -1;
    }
    
    /* 读取时间戳 */
    long timestamp;
    if (fread(&timestamp, sizeof(timestamp), 1, fp) != 1) {
        log_error("Failed to read snapshot timestamp");
        fclose(fp);
        return -1;
    }
    
    /* 读取房间数据 */
    SnapshotData data;
    if (fread(&data, sizeof(data), 1, fp) != 1) {
        log_error("Failed to read snapshot data");
        fclose(fp);
        return -1;
    }
    
    /* 恢复房间数据 */
    pthread_mutex_lock(&room->lock);
    
    strncpy(room->name, data.room_name, MAX_ROOM_NAME - 1);
    room->max_players = data.max_players;
    room->player_count = 0;  /* 玩家需要重连 */
    room->status = ROOM_GAMING;
    
    memcpy(room->map, data.map, sizeof(room->map));
    room->poison_radius = data.poison_radius;
    
    memcpy(room->items, data.items, sizeof(room->items));
    room->item_count = data.item_count;
    
    room->game_start_time = data.game_start_time;
    room->last_item_spawn = data.last_item_spawn;
    room->last_poison_shrink = data.last_poison_shrink;
    
    /* 清空玩家列表（等待重连） */
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        room->player_ids[i] = -1;
    }
    
    pthread_mutex_unlock(&room->lock);
    
    /* 读取玩家数量 */
    int player_count;
    if (fread(&player_count, sizeof(player_count), 1, fp) != 1) {
        log_error("Failed to read player count");
        fclose(fp);
        return -1;
    }
    
    /* 读取玩家数据（用于生成 .recovery 文件） */
    if (player_count > 0) {
        SnapshotPlayer *players = malloc(sizeof(SnapshotPlayer) * (size_t)player_count);
        if (players == NULL) {
            log_error("Failed to allocate player data");
            fclose(fp);
            return -1;
        }
        
        if (fread(players, sizeof(SnapshotPlayer), (size_t)player_count, fp) != (size_t)player_count) {
            log_error("Failed to read player data");
            free(players);
            fclose(fp);
            return -1;
        }
        
        /* 生成 .recovery 文件 */
        char recovery_path[256];
        snprintf(recovery_path, sizeof(recovery_path), "%s/room_%d.recovery", WAL_DIR, room_id);
        
        FILE *rfp = fopen(recovery_path, "w");
        if (rfp != NULL) {
            /* 确保 room_name 不为空 */
            const char *room_name = data.room_name;
            if (room_name == NULL || strlen(room_name) == 0) {
                room_name = "Recovered";
            }
            
            fprintf(rfp, "room_id=%d\n", room_id);
            fprintf(rfp, "room_name=%s\n", room_name);
            fprintf(rfp, "poison_radius=%d\n", data.poison_radius);
            fprintf(rfp, "total_players=%d\n", player_count);
            
            int alive_count = 0;
            for (int i = 0; i < player_count; i++) {
                if (players[i].status == PLAYER_GAMING && players[i].hp > 0) {
                    alive_count++;
                }
            }
            fprintf(rfp, "alive_count=%d\n", alive_count);
            
            /* 写入地图数据 */
            fprintf(rfp, "map_start\n");
            for (int y = 0; y < MAP_HEIGHT; y++) {
                fprintf(rfp, "%s\n", data.map[y]);
            }
            fprintf(rfp, "map_end\n");
            
            /* 写入玩家数据（包含背包、base_atk 和 atk_buff_remain） */
            long now = player_get_time_ms();
            for (int i = 0; i < player_count; i++) {
                /* 计算 buff 剩余时间 */
                long atk_buff_remain = 0;
                if (players[i].atk_buff_expire > now) {
                    atk_buff_remain = players[i].atk_buff_expire - now;
                }
                
                fprintf(rfp, "player=%d,%s,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%ld\n",
                        players[i].id, players[i].username,
                        players[i].x, players[i].y,
                        players[i].hp, players[i].max_hp,
                        players[i].atk, players[i].def,
                        players[i].has_shield, players[i].status,
                        players[i].inventory_count,
                        players[i].inventory[0], players[i].inventory[1],
                        players[i].inventory[2], players[i].inventory[3],
                        players[i].inventory[4], players[i].base_atk,
                        atk_buff_remain);
            }
            
            /* 写入道具数据 */
            for (int i = 0; i < data.item_count; i++) {
                if (data.items[i].active) {
                    fprintf(rfp, "item=%d,%d,%d\n",
                            data.items[i].x, data.items[i].y, data.items[i].type);
                }
            }
            
            fclose(rfp);
            log_info("Recovery file generated from snapshot for room %d", room_id);
        }
        
        free(players);
    }
    
    fclose(fp);
    
    log_info("Snapshot loaded for room %d, timestamp=%ld", room_id, timestamp);
    return 0;
}
