/**
 * recovery.c - 崩溃恢复模块实现
 * 
 * 实现完整的游戏状态恢复功能
 * 支持 WAL + 快照结合的恢复方式
 */

#include "recovery.h"
#include "wal.h"
#include "snapshot.h"
#include "room.h"
#include "player.h"
#include "map.h"
#include "game.h"
#include "log.h"
#include "../common/config.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <dirent.h>
#include <sys/stat.h>
#include <time.h>
#include <errno.h>

/* 恢复状态结构 - 用于存储从 WAL 解析的游戏状态 */
typedef struct {
    int room_id;
    char room_name[MAX_ROOM_NAME];
    int max_players;
    
    /* 玩家状态 */
    struct {
        int id;
        char username[MAX_USERNAME];
        int x, y;
        int hp, max_hp;
        int atk, def, base_atk;
        int has_shield;
        int status;  /* PLAYER_GAMING or PLAYER_DEAD */
        int inventory[MAX_INVENTORY];
        int inventory_count;
        long atk_buff_expire;  /* 攻击 buff 过期时间 */
    } players[MAX_ROOM_PLAYERS];
    int player_count;
    
    /* 地图状态 */
    char map[MAP_HEIGHT][MAP_WIDTH + 1];
    int poison_radius;
    
    /* 道具状态 */
    struct {
        int x, y;
        int type;
        int active;
    } items[MAX_MAP_ITEMS];
    int item_count;
    
    /* 时间信息 */
    long game_start_time;
    long last_timestamp;
} RecoveryState;

/* 恢复房间映射 - 跟踪 original_room_id 到新房间的映射 */
#define MAX_RECOVERY_MAPPINGS 32
static struct {
    int original_room_id;
    int new_room_id;
} recovery_room_map[MAX_RECOVERY_MAPPINGS];
static int recovery_map_count = 0;
static pthread_mutex_t recovery_map_lock = PTHREAD_MUTEX_INITIALIZER;

/* 查找恢复房间映射 */
static int find_recovery_room(int original_room_id) {
    pthread_mutex_lock(&recovery_map_lock);
    for (int i = 0; i < recovery_map_count; i++) {
        if (recovery_room_map[i].original_room_id == original_room_id) {
            int new_id = recovery_room_map[i].new_room_id;
            pthread_mutex_unlock(&recovery_map_lock);
            return new_id;
        }
    }
    pthread_mutex_unlock(&recovery_map_lock);
    return -1;
}

/* 添加恢复房间映射 */
static void add_recovery_room_mapping(int original_room_id, int new_room_id) {
    pthread_mutex_lock(&recovery_map_lock);
    if (recovery_map_count < MAX_RECOVERY_MAPPINGS) {
        recovery_room_map[recovery_map_count].original_room_id = original_room_id;
        recovery_room_map[recovery_map_count].new_room_id = new_room_id;
        recovery_map_count++;
        log_info("Added recovery mapping: original=%d -> new=%d", original_room_id, new_room_id);
    }
    pthread_mutex_unlock(&recovery_map_lock);
}

/* 移除恢复房间映射 */
static void remove_recovery_room_mapping(int original_room_id) {
    pthread_mutex_lock(&recovery_map_lock);
    for (int i = 0; i < recovery_map_count; i++) {
        if (recovery_room_map[i].original_room_id == original_room_id) {
            /* 移动最后一个元素到当前位置 */
            recovery_room_map[i] = recovery_room_map[recovery_map_count - 1];
            recovery_map_count--;
            break;
        }
    }
    pthread_mutex_unlock(&recovery_map_lock);
}

/* 检查 WAL 文件是否包含 GAME_END 记录 */
static int wal_has_game_end(const char *path) {
    FILE *fp = fopen(path, "r");
    if (fp == NULL) {
        return 0;
    }
    
    char line[WAL_BUFFER_SIZE];
    int has_end = 0;
    
    while (fgets(line, sizeof(line), fp) != NULL) {
        if (strstr(line, "|GAME_END|") != NULL) {
            has_end = 1;
            break;
        }
    }
    
    fclose(fp);
    return has_end;
}

/* 解析 GAME_START 数据 */
static int parse_game_start(RecoveryState *state, const char *data) {
    /* 格式: room_name=XXX,max_players=N,map_seed=N */
    char room_name[MAX_ROOM_NAME] = "";
    int max_players = MAX_ROOM_PLAYERS;
    
    char buf[MAX_MSG_LEN];
    strncpy(buf, data, sizeof(buf) - 1);
    buf[sizeof(buf) - 1] = '\0';
    
    char *saveptr = NULL;
    char *token = strtok_r(buf, ",", &saveptr);
    
    while (token != NULL) {
        if (strncmp(token, "room_name=", 10) == 0) {
            strncpy(room_name, token + 10, MAX_ROOM_NAME - 1);
        } else if (strncmp(token, "max_players=", 12) == 0) {
            max_players = atoi(token + 12);
        }
        token = strtok_r(NULL, ",", &saveptr);
    }
    
    strncpy(state->room_name, room_name, MAX_ROOM_NAME - 1);
    state->max_players = max_players;
    
    return 0;
}

/* 解析 PLAYER_JOIN 数据 */
static int parse_player_join(RecoveryState *state, const char *data) {
    /* 格式: pid=N,username=XXX,x=N,y=N,hp=N,max_hp=N,atk=N,def=N,shield=N,atk_buff_remain=N,inv=N,N,N,N,N */
    int pid = -1, x = 0, y = 0, hp = INITIAL_HP, max_hp = INITIAL_HP;
    int atk = INITIAL_ATK, def = INITIAL_DEF, base_atk = INITIAL_ATK;
    int has_shield = 0;
    long atk_buff_remain = 0;
    int inventory[MAX_INVENTORY] = {0};
    char username[MAX_USERNAME] = "";
    
    char buf[MAX_MSG_LEN];
    strncpy(buf, data, sizeof(buf) - 1);
    buf[sizeof(buf) - 1] = '\0';
    
    char *saveptr = NULL;
    char *token = strtok_r(buf, ",", &saveptr);
    
    while (token != NULL) {
        if (strncmp(token, "pid=", 4) == 0) {
            pid = atoi(token + 4);
        } else if (strncmp(token, "username=", 9) == 0) {
            strncpy(username, token + 9, MAX_USERNAME - 1);
        } else if (strncmp(token, "x=", 2) == 0) {
            x = atoi(token + 2);
        } else if (strncmp(token, "y=", 2) == 0) {
            y = atoi(token + 2);
        } else if (strncmp(token, "hp=", 3) == 0) {
            hp = atoi(token + 3);
        } else if (strncmp(token, "max_hp=", 7) == 0) {
            max_hp = atoi(token + 7);
        } else if (strncmp(token, "atk=", 4) == 0) {
            atk = atoi(token + 4);
        } else if (strncmp(token, "def=", 4) == 0) {
            def = atoi(token + 4);
        } else if (strncmp(token, "base_atk=", 9) == 0) {
            base_atk = atoi(token + 9);
        } else if (strncmp(token, "shield=", 7) == 0) {
            has_shield = atoi(token + 7);
        } else if (strncmp(token, "atk_buff_remain=", 16) == 0) {
            atk_buff_remain = atol(token + 16);
        } else if (strncmp(token, "inv=", 4) == 0) {
            /* 解析道具列表: inv=N,N,N,N,N */
            sscanf(token + 4, "%d", &inventory[0]);
            /* 后续道具在接下来的 token 中 */
            for (int i = 1; i < MAX_INVENTORY; i++) {
                token = strtok_r(NULL, ",", &saveptr);
                if (token != NULL) {
                    inventory[i] = atoi(token);
                }
            }
            break;  /* inv 是最后一个字段 */
        }
        token = strtok_r(NULL, ",", &saveptr);
    }
    
    if (pid < 0) {
        return -1;
    }
    
    /* 计算 buff 过期时间 */
    long atk_buff_expire = 0;
    if (atk_buff_remain > 0) {
        /* 有明确的剩余时间 */
        atk_buff_expire = player_get_time_ms() + atk_buff_remain;
    } else if (atk > base_atk) {
        /* 没有存储过期时间，但攻击力高于基础值，说明有 buff */
        /* 设置为全量时间 */
        atk_buff_expire = player_get_time_ms() + ATK_BUFF_DURATION;
        log_info("Player %s has attack buff but no expire time, setting full duration", username);
    }
    
    /* 检查是否已存在同名玩家（恢复重连场景） */
    int existing_idx = -1;
    for (int i = 0; i < state->player_count; i++) {
        if (strcmp(state->players[i].username, username) == 0) {
            existing_idx = i;
            break;
        }
    }
    
    int idx;
    if (existing_idx >= 0) {
        /* 更新现有玩家状态（恢复重连后的最新状态） */
        idx = existing_idx;
        log_debug("Updating existing player %s at index %d", username, idx);
    } else {
        /* 添加新玩家 */
        if (state->player_count >= MAX_ROOM_PLAYERS) {
            return -1;
        }
        idx = state->player_count++;
    }
    
    state->players[idx].id = pid;
    strncpy(state->players[idx].username, username, MAX_USERNAME - 1);
    state->players[idx].x = x;
    state->players[idx].y = y;
    state->players[idx].hp = hp;
    state->players[idx].max_hp = max_hp;
    state->players[idx].atk = atk;
    state->players[idx].def = def;
    state->players[idx].base_atk = base_atk;
    state->players[idx].has_shield = has_shield;
    state->players[idx].atk_buff_expire = atk_buff_expire;
    state->players[idx].status = PLAYER_GAMING;
    state->players[idx].inventory_count = 0;
    for (int i = 0; i < MAX_INVENTORY; i++) {
        state->players[idx].inventory[i] = inventory[i];
        if (inventory[i] > 0) {
            state->players[idx].inventory_count++;
        }
    }
    
    return 0;
}

/* 查找恢复状态中的玩家索引（按 pid 或 username） */
static int find_player_index(RecoveryState *state, int pid) {
    for (int i = 0; i < state->player_count; i++) {
        if (state->players[i].id == pid) {
            return i;
        }
    }
    return -1;
}

/* 解析 MOVE 数据 */
static int parse_move(RecoveryState *state, const char *data) {
    /* 格式: pid=N,dir=X,ox=N,oy=N,nx=N,ny=N */
    int pid = -1, nx = 0, ny = 0;
    
    if (sscanf(data, "pid=%d,%*[^,],%*[^,],%*[^,],nx=%d,ny=%d", &pid, &nx, &ny) < 3) {
        return -1;
    }
    
    int idx = find_player_index(state, pid);
    if (idx >= 0) {
        state->players[idx].x = nx;
        state->players[idx].y = ny;
    }
    
    return 0;
}

/* 解析 DAMAGE 数据 */
static int parse_damage(RecoveryState *state, const char *data) {
    /* 格式: atk=N,vic=N,dmg=N,hp=N 或 atk=N,vic=N,dmg=N,hp=N,shield_broken=1 */
    int vic = -1, hp = 0;
    int shield_broken = 0;
    
    /* 先检查是否有 shield_broken 字段 */
    if (strstr(data, "shield_broken=1") != NULL) {
        shield_broken = 1;
    }
    
    if (sscanf(data, "%*[^,],vic=%d,%*[^,],hp=%d", &vic, &hp) < 2) {
        /* 尝试另一种格式 */
        if (sscanf(data, "atk=%*d,vic=%d,dmg=%*d,hp=%d", &vic, &hp) < 2) {
            return -1;
        }
    }
    
    int idx = find_player_index(state, vic);
    if (idx >= 0) {
        state->players[idx].hp = hp;
        
        /* 如果护盾被打破，更新护盾状态 */
        if (shield_broken) {
            state->players[idx].has_shield = 0;
            log_debug("Player %d shield broken (from WAL)", vic);
        }
    }
    
    return 0;
}

/* 解析 PLAYER_DEATH 数据 */
static int parse_player_death(RecoveryState *state, const char *data) {
    /* 格式: pid=N,killer=N */
    int pid = -1;
    
    if (sscanf(data, "pid=%d", &pid) < 1) {
        return -1;
    }
    
    int idx = find_player_index(state, pid);
    if (idx >= 0) {
        state->players[idx].hp = 0;
        state->players[idx].status = PLAYER_DEAD;
    }
    
    return 0;
}

/* 解析 POISON_SHRINK 数据 */
static int parse_poison_shrink(RecoveryState *state, const char *data) {
    /* 格式: radius=N */
    int radius = 0;
    
    if (sscanf(data, "radius=%d", &radius) < 1) {
        return -1;
    }
    
    state->poison_radius = radius;
    return 0;
}

/* 解析 ITEM_SPAWN 数据 */
static int parse_item_spawn(RecoveryState *state, const char *data) {
    /* 格式: type=N,x=N,y=N */
    int type = 0, x = 0, y = 0;
    
    if (sscanf(data, "type=%d,x=%d,y=%d", &type, &x, &y) < 3) {
        return -1;
    }
    
    if (state->item_count < MAX_MAP_ITEMS) {
        int idx = state->item_count++;
        state->items[idx].type = type;
        state->items[idx].x = x;
        state->items[idx].y = y;
        state->items[idx].active = 1;
    }
    
    return 0;
}

/* 解析 PICKUP 数据 */
static int parse_pickup(RecoveryState *state, const char *data) {
    /* 格式: pid=N,item=N,x=N,y=N */
    int pid = -1, item_type = 0, x = 0, y = 0;
    
    if (sscanf(data, "pid=%d,item=%d,x=%d,y=%d", &pid, &item_type, &x, &y) < 4) {
        return -1;
    }
    
    /* 标记道具为已拾取 */
    for (int i = 0; i < state->item_count; i++) {
        if (state->items[i].x == x && state->items[i].y == y && state->items[i].active) {
            state->items[i].active = 0;
            break;
        }
    }
    
    /* 添加到玩家背包 */
    int idx = find_player_index(state, pid);
    if (idx >= 0 && state->players[idx].inventory_count < MAX_INVENTORY) {
        int inv_idx = state->players[idx].inventory_count++;
        state->players[idx].inventory[inv_idx] = item_type;
    }
    
    return 0;
}

/* 解析 USE_ITEM 数据 */
static int parse_use_item(RecoveryState *state, const char *data) {
    /* 格式: pid=N,item=N,idx=N */
    int pid = -1, item_type = 0, item_idx = 0;
    
    if (sscanf(data, "pid=%d,item=%d,idx=%d", &pid, &item_type, &item_idx) < 3) {
        return -1;
    }
    
    int idx = find_player_index(state, pid);
    if (idx < 0) return -1;
    
    /* 从背包移除道具 */
    if (item_idx >= 0 && item_idx < state->players[idx].inventory_count) {
        for (int i = item_idx; i < state->players[idx].inventory_count - 1; i++) {
            state->players[idx].inventory[i] = state->players[idx].inventory[i + 1];
        }
        state->players[idx].inventory_count--;
    }
    
    /* 应用道具效果 */
    switch (item_type) {
        case ITEM_HEALTH:
            state->players[idx].hp += HEALTH_RESTORE;
            if (state->players[idx].hp > state->players[idx].max_hp) {
                state->players[idx].hp = state->players[idx].max_hp;
            }
            break;
        case ITEM_ATTACK:
            state->players[idx].atk = INITIAL_ATK + ATK_BUFF_AMOUNT;
            break;
        case ITEM_SHIELD:
            state->players[idx].has_shield = 1;
            break;
    }
    
    return 0;
}


/* 从 WAL 文件解析完整游戏状态 */
static int parse_wal_file(const char *path, RecoveryState *state) {
    FILE *fp = fopen(path, "r");
    if (fp == NULL) {
        log_error("Failed to open WAL file: %s", path);
        return -1;
    }
    
    memset(state, 0, sizeof(RecoveryState));
    state->poison_radius = map_get_initial_poison_radius();
    
    char line[WAL_BUFFER_SIZE];
    int record_count = 0;
    
    while (fgets(line, sizeof(line), fp) != NULL) {
        WalRecord record;
        
        if (wal_parse_record(line, &record) < 0) {
            log_warn("Failed to parse WAL record: %s", line);
            continue;
        }
        
        state->room_id = record.room_id;
        state->last_timestamp = record.timestamp;
        
        if (record_count == 0) {
            state->game_start_time = record.timestamp;
        }
        
        switch (record.action_type) {
            case WAL_GAME_START:
                parse_game_start(state, record.action_data);
                /* 生成地图 */
                map_generate(state->map);
                break;
                
            case WAL_PLAYER_JOIN:
                parse_player_join(state, record.action_data);
                break;
                
            case WAL_MOVE:
                parse_move(state, record.action_data);
                break;
                
            case WAL_DAMAGE:
                parse_damage(state, record.action_data);
                break;
                
            case WAL_PLAYER_DEATH:
                parse_player_death(state, record.action_data);
                break;
                
            case WAL_POISON_SHRINK:
                parse_poison_shrink(state, record.action_data);
                break;
                
            case WAL_ITEM_SPAWN:
                parse_item_spawn(state, record.action_data);
                break;
                
            case WAL_PICKUP:
                parse_pickup(state, record.action_data);
                break;
                
            case WAL_USE_ITEM:
                parse_use_item(state, record.action_data);
                break;
                
            case WAL_ATTACK:
            case WAL_PLAYER_LEAVE:
                /* 这些动作不影响最终状态 */
                break;
                
            case WAL_CHECKPOINT:
                /* 解析检查点中的状态更新（包含 room_name 和 poison_radius） */
                {
                    char checkpoint_room_name[MAX_ROOM_NAME] = "";
                    int checkpoint_poison_radius = -1;
                    
                    /* 解析 room_name */
                    char *room_name_ptr = strstr(record.action_data, "room_name=");
                    if (room_name_ptr != NULL) {
                        room_name_ptr += 10;  /* 跳过 "room_name=" */
                        char *end = strchr(room_name_ptr, ',');
                        if (end != NULL) {
                            size_t len = (size_t)(end - room_name_ptr);
                            if (len >= MAX_ROOM_NAME) len = MAX_ROOM_NAME - 1;
                            strncpy(checkpoint_room_name, room_name_ptr, len);
                            checkpoint_room_name[len] = '\0';
                        } else {
                            strncpy(checkpoint_room_name, room_name_ptr, MAX_ROOM_NAME - 1);
                        }
                        if (strlen(checkpoint_room_name) > 0) {
                            strncpy(state->room_name, checkpoint_room_name, MAX_ROOM_NAME - 1);
                            log_debug("Checkpoint updated room_name to %s", state->room_name);
                        }
                    }
                    
                    /* 解析 poison_radius */
                    if (sscanf(record.action_data, "%*[^,],poison_radius=%d", &checkpoint_poison_radius) == 1 ||
                        sscanf(record.action_data, "poison_radius=%d", &checkpoint_poison_radius) == 1) {
                        if (checkpoint_poison_radius > 0) {
                            state->poison_radius = checkpoint_poison_radius;
                            log_debug("Checkpoint updated poison_radius to %d", checkpoint_poison_radius);
                        }
                    }
                }
                break;
                
            case WAL_GAME_END:
                /* 不应该到达这里 */
                fclose(fp);
                return -1;
                
            default:
                break;
        }
        
        record_count++;
    }
    
    fclose(fp);
    
    log_info("Parsed WAL file: %d records, %d players, poison_radius=%d",
             record_count, state->player_count, state->poison_radius);
    
    return record_count > 0 ? 0 : -1;
}

int recovery_list_recoverable_rooms(int *room_ids, int max_count) {
    if (room_ids == NULL || max_count <= 0) {
        return 0;
    }
    
    DIR *dir = opendir(WAL_DIR);
    if (dir == NULL) {
        if (errno == ENOENT) {
            log_info("WAL directory does not exist: %s", WAL_DIR);
            return 0;
        }
        log_error("Failed to open WAL directory: %s", strerror(errno));
        return -1;
    }
    
    log_info("Scanning WAL directory: %s", WAL_DIR);
    
    int count = 0;
    struct dirent *entry;
    int found_ids[MAX_ROOMS];
    int found_count = 0;
    
    while ((entry = readdir(dir)) != NULL && found_count < MAX_ROOMS) {
        if (entry->d_name[0] == '.') {
            continue;
        }
        
        log_debug("Found file in WAL dir: %s", entry->d_name);
        
        /* 检查是否是 .wal 或 .snap 文件 */
        int room_id = -1;
        if (strncmp(entry->d_name, "room_", 5) == 0) {
            const char *num_start = entry->d_name + 5;
            char *end;
            long id = strtol(num_start, &end, 10);
            if (end != num_start && (strcmp(end, ".wal") == 0 || strcmp(end, ".snap") == 0)) {
                room_id = (int)id;
            }
        }
        
        if (room_id < 0) {
            continue;
        }
        
        /* 检查是否已经添加过 */
        int already_found = 0;
        for (int i = 0; i < found_count; i++) {
            if (found_ids[i] == room_id) {
                already_found = 1;
                break;
            }
        }
        if (already_found) {
            continue;
        }
        
        /* 检查 WAL 是否有 GAME_END */
        char wal_path[256];
        wal_get_path(room_id, wal_path, sizeof(wal_path));
        
        int has_end = 0;
        if (wal_exists_for_room(room_id)) {
            has_end = wal_has_game_end(wal_path);
        }
        
        log_info("Room %d: has_wal=%d, has_snap=%d, has_game_end=%d", 
                 room_id, wal_exists_for_room(room_id), snapshot_exists(room_id), has_end);
        
        /* 如果没有 GAME_END，说明游戏未正常结束，需要恢复 */
        if (!has_end && (wal_exists_for_room(room_id) || snapshot_exists(room_id))) {
            found_ids[found_count++] = room_id;
            if (count < max_count) {
                room_ids[count++] = room_id;
                log_info("Found recoverable room: %d", room_id);
            }
        }
    }
    
    closedir(dir);
    return count;
}

int recovery_replay_wal(Room *room, const char *wal_path) {
    if (room == NULL || wal_path == NULL) {
        return -1;
    }
    
    RecoveryState state;
    if (parse_wal_file(wal_path, &state) < 0) {
        return -1;
    }
    
    /* 应用状态到房间 */
    pthread_mutex_lock(&room->lock);
    
    /* 复制地图 */
    memcpy(room->map, state.map, sizeof(room->map));
    
    /* 设置毒圈 */
    room->poison_radius = state.poison_radius;
    
    /* 复制道具 */
    room->item_count = 0;
    for (int i = 0; i < state.item_count && room->item_count < MAX_MAP_ITEMS; i++) {
        if (state.items[i].active) {
            room->items[room->item_count].x = state.items[i].x;
            room->items[room->item_count].y = state.items[i].y;
            room->items[room->item_count].type = state.items[i].type;
            room->items[room->item_count].active = 1;
            room->item_count++;
        }
    }
    
    /* 设置时间 - 调整为当前时间 */
    long now = player_get_time_ms();
    long elapsed = state.last_timestamp - state.game_start_time;
    room->game_start_time = now - elapsed;
    room->last_item_spawn = now;
    room->last_poison_shrink = now;
    
    pthread_mutex_unlock(&room->lock);
    
    log_info("WAL replay completed for room %d", room->id);
    return 0;
}

/* 保存恢复状态到文件（供玩家重连时使用） */
static int save_recovery_info(int room_id, RecoveryState *state) {
    char path[512];
    snprintf(path, sizeof(path), "%s/room_%d.recovery", WAL_DIR, room_id);
    
    FILE *fp = fopen(path, "w");
    if (fp == NULL) {
        log_error("Failed to create recovery info file: %s", path);
        return -1;
    }
    
    /* 确保 room_name 不为空 */
    const char *room_name = state->room_name;
    if (room_name == NULL || strlen(room_name) == 0) {
        room_name = "Recovered";
    }
    
    /* 写入房间信息 */
    fprintf(fp, "room_id=%d\n", state->room_id);
    fprintf(fp, "room_name=%s\n", room_name);
    fprintf(fp, "poison_radius=%d\n", state->poison_radius);
    fprintf(fp, "player_count=%d\n", state->player_count);
    
    /* 计算存活玩家数 */
    int alive_count = 0;
    for (int i = 0; i < state->player_count; i++) {
        if (state->players[i].status == PLAYER_GAMING && state->players[i].hp > 0) {
            alive_count++;
        }
    }
    fprintf(fp, "alive_count=%d\n", alive_count);
    
    /* 写入地图数据（Base64 编码或直接写入） */
    fprintf(fp, "map_start\n");
    for (int y = 0; y < MAP_HEIGHT; y++) {
        fprintf(fp, "%s\n", state->map[y]);
    }
    fprintf(fp, "map_end\n");
    
    /* 写入玩家信息（包含背包、base_atk 和 atk_buff_remain） */
    long now = player_get_time_ms();
    for (int i = 0; i < state->player_count; i++) {
        /* 计算 buff 剩余时间 */
        long atk_buff_remain = 0;
        if (state->players[i].atk_buff_expire > now) {
            atk_buff_remain = state->players[i].atk_buff_expire - now;
        }
        
        /* 格式: player=id,username,x,y,hp,max_hp,atk,def,has_shield,status,inv_count,inv0,inv1,inv2,inv3,inv4,base_atk,atk_buff_remain */
        fprintf(fp, "player=%d,%s,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%ld\n",
                state->players[i].id,
                state->players[i].username,
                state->players[i].x,
                state->players[i].y,
                state->players[i].hp,
                state->players[i].max_hp,
                state->players[i].atk,
                state->players[i].def,
                state->players[i].has_shield,
                state->players[i].status,
                state->players[i].inventory_count,
                state->players[i].inventory[0],
                state->players[i].inventory[1],
                state->players[i].inventory[2],
                state->players[i].inventory[3],
                state->players[i].inventory[4],
                state->players[i].base_atk,
                atk_buff_remain);
    }
    
    /* 写入地图道具信息 */
    fprintf(fp, "item_count=%d\n", state->item_count);
    for (int i = 0; i < state->item_count; i++) {
        if (state->items[i].active) {
            fprintf(fp, "item=%d,%d,%d\n",
                    state->items[i].x,
                    state->items[i].y,
                    state->items[i].type);
        }
    }
    
    fclose(fp);
    log_info("Recovery info saved: %s", path);
    return 0;
}

int recovery_recover_room(int room_id) {
    char wal_path[256];
    wal_get_path(room_id, wal_path, sizeof(wal_path));
    
    /* 首先删除旧的 .recovery 文件，确保从 WAL/快照 重新生成 */
    char recovery_path[512];
    snprintf(recovery_path, sizeof(recovery_path), "%s/room_%d.recovery", WAL_DIR, room_id);
    unlink(recovery_path);
    
    RecoveryState state;
    memset(&state, 0, sizeof(state));
    int has_player_data = 0;
    int has_map_data = 0;
    
    /* 
     * WAL + Snapshot 协同恢复策略：
     * 1. 从快照加载地图数据（地图不存在 WAL 中）
     * 2. 从 WAL 解析玩家最新状态
     * 3. 最终状态 = 快照地图 + WAL 玩家状态
     */
    
    /* 步骤1: 检查是否有快照和 WAL */
    int has_snapshot = snapshot_exists(room_id);
    int has_wal = wal_exists_for_room(room_id);
    
    log_info("Room %d recovery: has_snapshot=%d, has_wal=%d", room_id, has_snapshot, has_wal);
    
    if (!has_snapshot && !has_wal) {
        log_warn("No WAL or snapshot found for room %d", room_id);
        return -1;
    }
    
    /* 步骤2: 从快照加载地图数据 */
    if (has_snapshot) {
        char snap_path[256];
        snapshot_get_path(room_id, snap_path, sizeof(snap_path));
        
        FILE *fp = fopen(snap_path, "rb");
        if (fp != NULL) {
            char magic[5] = {0};
            int version;
            long timestamp;
            
            if (fread(magic, 1, 4, fp) == 4 && strcmp(magic, "SNAP") == 0 &&
                fread(&version, sizeof(version), 1, fp) == 1 &&
                fread(&timestamp, sizeof(timestamp), 1, fp) == 1) {
                
                /* 读取房间数据（包含地图） */
                /* SnapshotData 结构在 snapshot.c 中定义，这里直接读取地图部分 */
                /* 跳过房间基本信息，直接读取地图 */
                int room_id_tmp, max_players, player_count_tmp, status_tmp;
                char room_name_tmp[MAX_ROOM_NAME];
                
                fread(&room_id_tmp, sizeof(int), 1, fp);
                fread(room_name_tmp, MAX_ROOM_NAME, 1, fp);
                fread(&max_players, sizeof(int), 1, fp);
                fread(&player_count_tmp, sizeof(int), 1, fp);
                fread(&status_tmp, sizeof(int), 1, fp);
                
                /* 读取地图数据 */
                fread(state.map, sizeof(state.map), 1, fp);
                fread(&state.poison_radius, sizeof(int), 1, fp);
                
                /* 复制房间名 */
                strncpy(state.room_name, room_name_tmp, MAX_ROOM_NAME - 1);
                
                has_map_data = 1;
                log_info("Loaded map from snapshot for room %d", room_id);
            }
            fclose(fp);
        }
    }
    
    /* 步骤3: 从 WAL 解析玩家状态 */
    if (has_wal) {
        log_info("Parsing WAL for room %d from %s", room_id, wal_path);
        
        /* 保存地图数据（WAL 解析会覆盖） */
        char saved_map[MAP_HEIGHT][MAP_WIDTH + 1];
        int saved_poison_radius = state.poison_radius;
        char saved_room_name[MAX_ROOM_NAME] = "";
        if (has_map_data) {
            memcpy(saved_map, state.map, sizeof(saved_map));
            strncpy(saved_room_name, state.room_name, MAX_ROOM_NAME - 1);
        }
        
        if (parse_wal_file(wal_path, &state) == 0 && state.player_count > 0) {
            has_player_data = 1;
            log_info("WAL parsed: %d players", state.player_count);
            
            /* 恢复地图数据（WAL 解析会生成新地图，需要用快照的地图覆盖） */
            if (has_map_data) {
                memcpy(state.map, saved_map, sizeof(state.map));
                state.poison_radius = saved_poison_radius;
                if (strlen(saved_room_name) > 0) {
                    strncpy(state.room_name, saved_room_name, MAX_ROOM_NAME - 1);
                }
                log_info("Restored map from snapshot after WAL parse");
            }
        } else {
            log_warn("WAL parse failed or empty for room %d", room_id);
        }
    }
    
    /* 步骤4: 如果没有玩家数据，尝试从快照恢复 */
    if (!has_player_data && has_snapshot) {
        log_info("Loading full state from snapshot for room %d", room_id);
        
        Room temp_room;
        memset(&temp_room, 0, sizeof(temp_room));
        pthread_mutex_init(&temp_room.lock, NULL);
        
        if (snapshot_load(room_id, &temp_room) == 0) {
            /* snapshot_load 会生成 .recovery 文件 */
            pthread_mutex_destroy(&temp_room.lock);
            log_info("Snapshot loaded for room %d", room_id);
            
            /* 读取生成的 .recovery 文件检查存活玩家数 */
            FILE *rfp = fopen(recovery_path, "r");
            if (rfp != NULL) {
                char line[256];
                int alive_count = 0;
                while (fgets(line, sizeof(line), rfp) != NULL) {
                    if (strncmp(line, "alive_count=", 12) == 0) {
                        alive_count = atoi(line + 12);
                        break;
                    }
                }
                fclose(rfp);
                
                if (alive_count <= 1) {
                    log_info("Room %d: game already ended (alive=%d), cleaning up", room_id, alive_count);
                    wal_delete_for_room(room_id);
                    snapshot_delete(room_id);
                    unlink(recovery_path);
                    return 0;
                }
                
                log_info("Room %d recovery ready from snapshot: %d alive players", room_id, alive_count);
                return 0;
            }
        } else {
            pthread_mutex_destroy(&temp_room.lock);
            log_error("Failed to load snapshot for room %d", room_id);
            return -1;
        }
    }
    
    if (!has_player_data && !has_map_data) {
        log_error("No recovery data available for room %d", room_id);
        return -1;
    }
    
    /* 步骤5: 检查是否有存活玩家 */
    int alive_count = 0;
    for (int i = 0; i < state.player_count; i++) {
        log_info("Player %d (%s): status=%d, hp=%d", 
                 state.players[i].id, state.players[i].username,
                 state.players[i].status, state.players[i].hp);
        if (state.players[i].status == PLAYER_GAMING && state.players[i].hp > 0) {
            alive_count++;
        }
    }
    
    log_info("Room %d: total_players=%d, alive_count=%d", room_id, state.player_count, alive_count);
    
    if (alive_count <= 1) {
        log_info("Room %d: game already ended (alive=%d), cleaning up", room_id, alive_count);
        wal_delete_for_room(room_id);
        snapshot_delete(room_id);
        return 0;
    }
    
    /* 确保 room_name 有有效值 */
    if (strlen(state.room_name) == 0) {
        snprintf(state.room_name, MAX_ROOM_NAME, "Room_%d", room_id);
        log_warn("Room name was empty, using default: %s", state.room_name);
    }
    
    /* 步骤5: 生成 .recovery 文件 */
    if (save_recovery_info(room_id, &state) < 0) {
        log_error("Failed to save recovery info for room %d", room_id);
        return -1;
    }
    
    log_info("Room %d recovery info saved: %d players, %d alive",
             room_id, state.player_count, alive_count);
    
    return 0;
}

int recovery_check_and_recover(void) {
    int room_ids[MAX_ROOMS];
    int count = recovery_list_recoverable_rooms(room_ids, MAX_ROOMS);
    
    if (count < 0) {
        return -1;
    }
    
    if (count == 0) {
        log_info("No recoverable rooms found (no WAL files without GAME_END)");
        return 0;
    }
    
    log_info("Found %d recoverable rooms, processing...", count);
    
    int recovered = 0;
    for (int i = 0; i < count; i++) {
        if (recovery_recover_room(room_ids[i]) == 0) {
            recovered++;
        }
    }
    
    return recovered;
}

/* 检查玩家是否有待恢复的游戏 */
int recovery_check_player(const char *username, int *room_id) {
    if (username == NULL || room_id == NULL) {
        return 0;
    }
    
    log_info("Checking recovery for player: %s", username);
    
    DIR *dir = opendir(WAL_DIR);
    if (dir == NULL) {
        log_warn("Cannot open WAL directory: %s", WAL_DIR);
        return 0;
    }
    
    struct dirent *entry;
    while ((entry = readdir(dir)) != NULL) {
        /* 查找 .recovery 文件 */
        if (strstr(entry->d_name, ".recovery") == NULL) {
            continue;
        }
        
        log_info("Found recovery file: %s", entry->d_name);
        
        char path[512];
        snprintf(path, sizeof(path), "%s/%s", WAL_DIR, entry->d_name);
        
        FILE *fp = fopen(path, "r");
        if (fp == NULL) continue;
        
        char line[256];
        int found_room_id = -1;
        int found_player = 0;
        
        while (fgets(line, sizeof(line), fp) != NULL) {
            if (strncmp(line, "room_id=", 8) == 0) {
                found_room_id = atoi(line + 8);
            } else if (strncmp(line, "player=", 7) == 0) {
                /* 解析玩家行，检查状态和血量 */
                /* 格式: player=id,username,x,y,hp,max_hp,atk,def,has_shield,status,... */
                char player_username[MAX_USERNAME];
                int pid, px, py, php, pmax_hp, patk, pdef, pshield, pstatus;
                if (sscanf(line + 7, "%d,%[^,],%d,%d,%d,%d,%d,%d,%d,%d",
                           &pid, player_username, &px, &py, &php, &pmax_hp,
                           &patk, &pdef, &pshield, &pstatus) >= 10) {
                    if (strcmp(player_username, username) == 0) {
                        /* 只有存活的玩家才能恢复 */
                        if (pstatus == PLAYER_GAMING && php > 0) {
                            found_player = 1;
                            log_debug("Player %s found in recovery: status=%d, hp=%d", username, pstatus, php);
                        } else {
                            log_debug("Player %s in recovery but dead: status=%d, hp=%d", username, pstatus, php);
                        }
                    }
                }
            }
        }
        
        fclose(fp);
        
        if (found_player && found_room_id >= 0) {
            *room_id = found_room_id;
            closedir(dir);
            log_info("Found recoverable game for player %s in room %d", username, found_room_id);
            return 1;
        }
    }
    
    closedir(dir);
    return 0;
}

int recovery_cleanup_old_wal(int max_age_seconds) {
    DIR *dir = opendir(WAL_DIR);
    if (dir == NULL) {
        return 0;
    }
    
    time_t now = time(NULL);
    int cleaned = 0;
    struct dirent *entry;
    
    while ((entry = readdir(dir)) != NULL) {
        if (entry->d_name[0] == '.') {
            continue;
        }
        
        char path[512];
        snprintf(path, sizeof(path), "%s/%s", WAL_DIR, entry->d_name);
        
        struct stat st;
        if (stat(path, &st) == 0) {
            if (now - st.st_mtime > max_age_seconds) {
                if (unlink(path) == 0) {
                    log_info("Cleaned old file: %s", path);
                    cleaned++;
                }
            }
        }
    }
    
    closedir(dir);
    return cleaned;
}

/* 删除恢复信息文件（不删除 WAL，因为新游戏可能正在使用） */
void recovery_cleanup_room(int room_id) {
    char path[512];
    
    /* 只删除 .recovery 文件 */
    snprintf(path, sizeof(path), "%s/room_%d.recovery", WAL_DIR, room_id);
    unlink(path);
    
    /* 注意：不删除 WAL 文件，因为恢复后的游戏会创建新的 WAL 继续记录 */
    /* WAL 文件会在游戏正常结束时由 room_end_game 处理 */
    
    /* 移除房间映射 */
    remove_recovery_room_mapping(room_id);
    
    log_info("Cleaned up recovery info for room %d", room_id);
}


/* 从恢复文件读取玩家状态 */
int recovery_get_player_state(const char *username, int *room_id,
                               int *x, int *y, int *hp, int *max_hp,
                               int *atk, int *def, int *has_shield,
                               int inventory[MAX_INVENTORY], int *inv_count,
                               int *base_atk, long *atk_buff_remain) {
    if (username == NULL || room_id == NULL) {
        return -1;
    }
    
    DIR *dir = opendir(WAL_DIR);
    if (dir == NULL) {
        return -1;
    }
    
    struct dirent *entry;
    int found = 0;
    
    while ((entry = readdir(dir)) != NULL && !found) {
        if (strstr(entry->d_name, ".recovery") == NULL) {
            continue;
        }
        
        char path[512];
        snprintf(path, sizeof(path), "%s/%s", WAL_DIR, entry->d_name);
        
        FILE *fp = fopen(path, "r");
        if (fp == NULL) continue;
        
        char line[512];
        int file_room_id = -1;
        
        while (fgets(line, sizeof(line), fp) != NULL) {
            /* 移除换行符 */
            size_t len = strlen(line);
            while (len > 0 && (line[len-1] == '\n' || line[len-1] == '\r')) {
                line[--len] = '\0';
            }
            
            if (strncmp(line, "room_id=", 8) == 0) {
                file_room_id = atoi(line + 8);
            } else if (strncmp(line, "player=", 7) == 0) {
                /* 格式: player=id,username,x,y,hp,max_hp,atk,def,has_shield,status,inv_count,inv0,inv1,inv2,inv3,inv4,base_atk,atk_buff_remain */
                int pid, px, py, php, pmax_hp, patk, pdef, pshield, pstatus;
                int pinv_count = 0, pinv[5] = {0};
                int pbase_atk = INITIAL_ATK;
                long patk_buff_remain = 0;
                char pusername[MAX_USERNAME];
                
                int parsed = sscanf(line + 7, "%d,%[^,],%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%ld",
                           &pid, pusername, &px, &py, &php, &pmax_hp,
                           &patk, &pdef, &pshield, &pstatus,
                           &pinv_count, &pinv[0], &pinv[1], &pinv[2], &pinv[3], &pinv[4], &pbase_atk, &patk_buff_remain);
                
                if (parsed >= 10) {
                    if (strcmp(pusername, username) == 0 && pstatus == PLAYER_GAMING && php > 0) {
                        *room_id = file_room_id;
                        if (x) *x = px;
                        if (y) *y = py;
                        if (hp) *hp = php;
                        if (max_hp) *max_hp = pmax_hp;
                        if (atk) *atk = patk;
                        if (def) *def = pdef;
                        if (has_shield) *has_shield = pshield;
                        if (inv_count) *inv_count = pinv_count;
                        if (inventory) {
                            for (int j = 0; j < MAX_INVENTORY; j++) {
                                inventory[j] = pinv[j];
                            }
                        }
                        if (base_atk) *base_atk = pbase_atk;
                        if (atk_buff_remain) *atk_buff_remain = patk_buff_remain;
                        found = 1;
                        break;
                    }
                }
            }
        }
        
        fclose(fp);
    }
    
    closedir(dir);
    return found ? 0 : -1;
}

/* 为恢复的游戏创建房间 */
Room *recovery_create_room_for_game(int original_room_id) {
    char recovery_path[512];
    snprintf(recovery_path, sizeof(recovery_path), "%s/room_%d.recovery", WAL_DIR, original_room_id);
    
    FILE *fp = fopen(recovery_path, "r");
    if (fp == NULL) {
        log_error("Recovery file not found: %s", recovery_path);
        return NULL;
    }
    
    char room_name[MAX_ROOM_NAME] = "Recovered";
    int poison_radius = 25;
    int expected_players = 0;
    
    /* 临时存储道具信息 */
    struct {
        int x, y, type;
    } items[MAX_MAP_ITEMS];
    int item_count = 0;
    
    /* 临时存储地图数据 */
    char map[MAP_HEIGHT][MAP_WIDTH + 1];
    int map_row = 0;
    int reading_map = 0;
    int has_map = 0;
    
    char line[512];
    while (fgets(line, sizeof(line), fp) != NULL) {
        /* 移除换行符 */
        size_t len = strlen(line);
        while (len > 0 && (line[len-1] == '\n' || line[len-1] == '\r')) {
            line[--len] = '\0';
        }
        
        if (strcmp(line, "map_start") == 0) {
            reading_map = 1;
            map_row = 0;
            continue;
        } else if (strcmp(line, "map_end") == 0) {
            reading_map = 0;
            has_map = 1;
            continue;
        }
        
        if (reading_map && map_row < MAP_HEIGHT) {
            strncpy(map[map_row], line, MAP_WIDTH);
            map[map_row][MAP_WIDTH] = '\0';
            map_row++;
            continue;
        }
        
        if (strncmp(line, "room_name=", 10) == 0) {
            /* 只有当值非空时才覆盖默认值 */
            if (strlen(line + 10) > 0) {
                strncpy(room_name, line + 10, MAX_ROOM_NAME - 1);
            }
        } else if (strncmp(line, "poison_radius=", 14) == 0) {
            poison_radius = atoi(line + 14);
        } else if (strncmp(line, "alive_count=", 12) == 0) {
            expected_players = atoi(line + 12);
        } else if (strncmp(line, "item=", 5) == 0 && item_count < MAX_MAP_ITEMS) {
            /* 格式: item=x,y,type */
            int ix, iy, itype;
            if (sscanf(line + 5, "%d,%d,%d", &ix, &iy, &itype) == 3) {
                items[item_count].x = ix;
                items[item_count].y = iy;
                items[item_count].type = itype;
                item_count++;
            }
        }
    }
    fclose(fp);
    
    log_info("Recovery file parsed: room_name=%s, poison_radius=%d, expected=%d, has_map=%d, items=%d",
             room_name, poison_radius, expected_players, has_map, item_count);
    
    /* 创建房间 */
    Room *room = room_create(room_name, MAX_ROOM_PLAYERS);
    if (room == NULL) {
        log_error("Failed to create recovery room (room_name=%s)", room_name);
        return NULL;
    }
    
    /* 设置房间状态 */
    pthread_mutex_lock(&room->lock);
    room->status = ROOM_GAMING;
    room->poison_radius = poison_radius;
    room->game_start_time = player_get_time_ms();
    room->last_item_spawn = room->game_start_time;
    room->last_poison_shrink = room->game_start_time;
    
    /* 设置恢复相关字段 */
    room->is_recovery_room = 1;
    room->expected_players = expected_players;
    room->recovery_start_time = player_get_time_ms();
    room->original_room_id = original_room_id;
    
    /* 恢复地图数据（如果有保存的地图，否则生成新地图） */
    if (has_map) {
        memcpy(room->map, map, sizeof(room->map));
        log_info("Restored map from recovery file");
    } else {
        map_generate(room->map);
        log_warn("No map data in recovery file, generated new map");
    }
    
    /* 恢复地图道具 */
    room->item_count = 0;
    for (int i = 0; i < item_count && room->item_count < MAX_MAP_ITEMS; i++) {
        room->items[room->item_count].x = items[i].x;
        room->items[room->item_count].y = items[i].y;
        room->items[room->item_count].type = items[i].type;
        room->items[room->item_count].active = 1;
        room->item_count++;
    }
    log_info("Restored %d items to recovery room", room->item_count);
    
    /* 创建新的 WAL 文件（清空旧记录，写入完整的恢复状态） */
    /* 这样可以保证连续崩溃时能恢复到最新状态 */
    wal_delete_for_room(original_room_id);
    room->wal = wal_create_for_room(original_room_id);
    
    /* 写入完整的恢复状态到 WAL */
    if (room->wal != NULL) {
        /* 写入游戏开始标记 */
        char wal_data[512];
        snprintf(wal_data, sizeof(wal_data), 
                 "room_name=%s,max_players=%d,recovered_at=%ld,poison_radius=%d",
                 room_name, MAX_ROOM_PLAYERS, player_get_time_ms(), poison_radius);
        wal_write(room->wal, WAL_GAME_START, wal_data);
        
        /* 重新读取 .recovery 文件，将所有玩家状态写入 WAL */
        FILE *rfp = fopen(recovery_path, "r");
        if (rfp != NULL) {
            char rline[512];
            while (fgets(rline, sizeof(rline), rfp) != NULL) {
                /* 移除换行符 */
                size_t rlen = strlen(rline);
                while (rlen > 0 && (rline[rlen-1] == '\n' || rline[rlen-1] == '\r')) {
                    rline[--rlen] = '\0';
                }
                
                if (strncmp(rline, "player=", 7) == 0) {
                    /* 解析玩家数据并写入 WAL */
                    int pid, px, py, php, pmax_hp, patk, pdef, pshield, pstatus;
                    int pinv_count, pinv[5] = {0};
                    int pbase_atk = INITIAL_ATK;
                    long patk_buff_remain = 0;
                    char pusername[MAX_USERNAME];
                    
                    int parsed = sscanf(rline + 7, "%d,%[^,],%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%ld",
                               &pid, pusername, &px, &py, &php, &pmax_hp,
                               &patk, &pdef, &pshield, &pstatus,
                               &pinv_count, &pinv[0], &pinv[1], &pinv[2], &pinv[3], &pinv[4], &pbase_atk, &patk_buff_remain);
                    
                    if (parsed >= 10 && pstatus == PLAYER_GAMING && php > 0) {
                        /* 写入玩家状态到 WAL */
                        char player_wal[512];
                        snprintf(player_wal, sizeof(player_wal),
                                 "pid=%d,username=%s,x=%d,y=%d,hp=%d,max_hp=%d,atk=%d,def=%d,base_atk=%d,shield=%d,atk_buff_remain=%ld,inv=%d,%d,%d,%d,%d",
                                 pid, pusername, px, py, php, pmax_hp, patk, pdef, pbase_atk, pshield,
                                 patk_buff_remain,
                                 pinv[0], pinv[1], pinv[2], pinv[3], pinv[4]);
                        wal_write(room->wal, WAL_PLAYER_JOIN, player_wal);
                        log_info("Wrote player %s state to WAL for recovery", pusername);
                    }
                }
            }
            fclose(rfp);
        }
        
        /* 写入道具状态 */
        for (int i = 0; i < room->item_count; i++) {
            char item_wal[128];
            snprintf(item_wal, sizeof(item_wal), "type=%d,x=%d,y=%d",
                     room->items[i].type, room->items[i].x, room->items[i].y);
            wal_write(room->wal, WAL_ITEM_SPAWN, item_wal);
        }
        
        /* 写入毒圈状态 */
        char poison_wal[64];
        snprintf(poison_wal, sizeof(poison_wal), "radius=%d", poison_radius);
        wal_write(room->wal, WAL_POISON_SHRINK, poison_wal);
        
        wal_sync(room->wal);
        log_info("WAL initialized with complete recovery state");
    }
    
    pthread_mutex_unlock(&room->lock);
    
    log_info("Created recovery room %d (original: %d), expecting %d players, wait %d seconds",
             room->id, original_room_id, expected_players, RECOVERY_WAIT_TIME / 1000);
    
    return room;
}

/* 将玩家恢复到游戏中 */
int recovery_restore_player_to_game(Player *player, int original_room_id) {
    if (player == NULL) {
        return -1;
    }
    
    /* 获取玩家保存的状态 */
    int x, y, hp, max_hp, atk, def, has_shield, base_atk;
    int inventory[MAX_INVENTORY] = {0};
    int inv_count = 0;
    long atk_buff_remain = 0;
    int room_id;
    
    if (recovery_get_player_state(player->username, &room_id,
                                   &x, &y, &hp, &max_hp, &atk, &def,
                                   &has_shield, inventory, &inv_count, &base_atk, &atk_buff_remain) < 0) {
        log_warn("No recovery state found for player %s", player->username);
        return -1;
    }
    
    /* 查找或创建恢复房间 */
    Room *room = NULL;
    
    /* 先查找是否已有恢复房间 */
    int existing_room_id = find_recovery_room(original_room_id);
    if (existing_room_id >= 0) {
        room = room_find_by_id(existing_room_id);
        if (room != NULL) {
            log_info("Found existing recovery room %d for original room %d", existing_room_id, original_room_id);
        }
    }
    
    /* 如果没有找到已存在的恢复房间，创建新的 */
    if (room == NULL) {
        room = recovery_create_room_for_game(original_room_id);
        if (room == NULL) {
            log_error("Failed to create recovery room for original room %d", original_room_id);
            return -1;
        }
        /* 添加映射 */
        add_recovery_room_mapping(original_room_id, room->id);
    }
    
    /* 将玩家加入房间 */
    pthread_mutex_lock(&room->lock);
    int slot = -1;
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] < 0) {
            slot = i;
            break;
        }
    }
    if (slot >= 0) {
        room->player_ids[slot] = player->id;
        room->player_count++;
    }
    pthread_mutex_unlock(&room->lock);
    
    if (slot < 0) {
        log_error("No slot available in recovery room");
        return -1;
    }
    
    /* 恢复玩家状态 */
    pthread_mutex_lock(&player->lock);
    player->room_id = room->id;
    player->status = PLAYER_GAMING;
    player->x = x;
    player->y = y;
    player->hp = hp;
    player->max_hp = max_hp;
    player->atk = atk;
    player->def = def;
    player->base_atk = base_atk;
    player->has_shield = has_shield;
    player->last_move_time = 0;
    player->last_attack_time = 0;
    
    /* 恢复攻击 buff 过期时间 */
    if (atk_buff_remain > 0) {
        player->atk_buff_expire = player_get_time_ms() + atk_buff_remain;
        player->atk_buff_warned = (atk_buff_remain <= 5000) ? 1 : 0;  /* 如果剩余不足5秒，标记已提醒 */
        log_info("Player %s attack buff restored, %ld ms remaining", player->username, atk_buff_remain);
    } else if (atk > base_atk) {
        /* 没有存储过期时间，但攻击力高于基础值，说明有 buff，设置全量时间 */
        player->atk_buff_expire = player_get_time_ms() + ATK_BUFF_DURATION;
        player->atk_buff_warned = 0;
        log_info("Player %s has attack buff but no expire time, setting full duration %d ms", 
                 player->username, ATK_BUFF_DURATION);
    } else {
        player->atk_buff_expire = 0;
        player->atk_buff_warned = 0;
    }
    
    /* 恢复背包 */
    player->inventory_count = 0;
    for (int i = 0; i < inv_count && i < MAX_INVENTORY; i++) {
        if (inventory[i] > 0) {
            player->inventory[player->inventory_count].type = (ItemType)inventory[i];
            player->inventory_count++;
        }
    }
    pthread_mutex_unlock(&player->lock);
    
    /* 写入 WAL */
    if (room->wal != NULL) {
        char wal_data[512];
        snprintf(wal_data, sizeof(wal_data),
                 "pid=%d,username=%s,x=%d,y=%d,hp=%d,max_hp=%d,atk=%d,def=%d,base_atk=%d,shield=%d,atk_buff_remain=%ld,inv=%d,%d,%d,%d,%d",
                 player->id, player->username, x, y, hp, max_hp, atk, def, base_atk, has_shield,
                 atk_buff_remain,
                 inventory[0], inventory[1], inventory[2], inventory[3], inventory[4]);
        wal_write(room->wal, WAL_PLAYER_JOIN, wal_data);
        wal_sync(room->wal);
    }
    
    log_info("Player %s restored to game in room %d at (%d,%d) with %d HP",
             player->username, room->id, x, y, hp);
    
    /* 启动游戏线程（如果还没启动） */
    pthread_mutex_lock(&room->lock);
    if (!room->game_thread_running) {
        room->game_thread_running = 1;
        pthread_mutex_unlock(&room->lock);
        pthread_create(&room->game_thread, NULL, game_thread_func, room);
        pthread_detach(room->game_thread);
    } else {
        pthread_mutex_unlock(&room->lock);
    }
    
    return room->id;
}

/* 从 WAL 文件中获取最大房间 ID */
int recovery_get_max_room_id(void) {
    DIR *dir = opendir(WAL_DIR);
    if (dir == NULL) {
        return 0;
    }
    
    int max_id = 0;
    struct dirent *entry;
    
    while ((entry = readdir(dir)) != NULL) {
        /* 检查是否是 WAL 文件 */
        if (strncmp(entry->d_name, "room_", 5) != 0) {
            continue;
        }
        
        /* 提取房间 ID */
        const char *num_start = entry->d_name + 5;
        char *end;
        long id = strtol(num_start, &end, 10);
        
        /* 检查是否是 .wal 或 .recovery 文件 */
        if (end != num_start && (strcmp(end, ".wal") == 0 || strcmp(end, ".recovery") == 0)) {
            if ((int)id > max_id) {
                max_id = (int)id;
            }
        }
    }
    
    closedir(dir);
    
    if (max_id > 0) {
        log_info("Found max room ID from WAL files: %d", max_id);
    }
    
    return max_id;
}
