/**
 * game.c - 游戏逻辑模块实现
 */

#include "game.h"
#include "room.h"
#include "player.h"
#include "map.h"
#include "wal.h"
#include "snapshot.h"
#include "log.h"
#include "../common/protocol.h"
#include "../common/config.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/* ============== 辅助函数 ============== */

int game_calc_damage(int atk, int def) {
    int damage = atk - def;
    return damage > 0 ? damage : 1;
}

void game_update_buffs(Player *player) {
    if (player == NULL) return;
    
    long now = player_get_time_ms();
    
    pthread_mutex_lock(&player->lock);
    
    /* 检查攻击 buff 是否过期 */
    if (player->atk_buff_expire > 0) {
        long remaining = player->atk_buff_expire - now;
        
        if (remaining <= 0) {
            /* buff 已过期 */
            player->atk = player->base_atk;
            player->atk_buff_expire = 0;
            player->atk_buff_warned = 0;
            log_debug("Player %d attack buff expired", player->id);
            
            /* 发送 buff 过期通知 */
            pthread_mutex_unlock(&player->lock);
            char msg[128];
            char event_data[64];
            snprintf(event_data, sizeof(event_data), "%d", player->id);
            protocol_build_game_event(msg, sizeof(msg), "BUFF_EXPIRED", event_data);
            /* 移除末尾换行符，player_send 会添加 */
            size_t len = strlen(msg);
            if (len > 0 && msg[len-1] == '\n') msg[len-1] = '\0';
            player_send(player, msg);
            return;
        } else if (remaining <= 5000 && !player->atk_buff_warned) {
            /* buff 剩余 5 秒，发送提醒 */
            player->atk_buff_warned = 1;
            int seconds = (int)(remaining / 1000);
            log_debug("Player %d attack buff expiring in %d seconds", player->id, seconds);
            
            pthread_mutex_unlock(&player->lock);
            char msg[128];
            char event_data[64];
            snprintf(event_data, sizeof(event_data), "%d|%d", player->id, seconds);
            protocol_build_game_event(msg, sizeof(msg), "BUFF_WARNING", event_data);
            /* 移除末尾换行符，player_send 会添加 */
            size_t len = strlen(msg);
            if (len > 0 && msg[len-1] == '\n') msg[len-1] = '\0';
            player_send(player, msg);
            return;
        }
    }
    
    pthread_mutex_unlock(&player->lock);
}

/* ============== 移动逻辑 ============== */

int game_handle_move(Room *room, Player *player, char direction) {
    if (room == NULL || player == NULL) return -1;
    
    long now = player_get_time_ms();
    
    pthread_mutex_lock(&player->lock);
    
    /* 检查冷却 */
    if (now - player->last_move_time < MOVE_COOLDOWN_MS) {
        pthread_mutex_unlock(&player->lock);
        return -1;  /* 冷却中 */
    }
    
    /* 计算新位置 */
    int nx = player->x;
    int ny = player->y;
    
    switch (direction) {
        case DIR_UP:    ny--; break;
        case DIR_DOWN:  ny++; break;
        case DIR_LEFT:  nx--; break;
        case DIR_RIGHT: nx++; break;
        default:
            pthread_mutex_unlock(&player->lock);
            return -2;  /* 无效方向 */
    }
    
    int ox = player->x;
    int oy = player->y;
    
    pthread_mutex_unlock(&player->lock);
    
    /* 检查碰撞 */
    pthread_mutex_lock(&room->lock);
    
    if (!map_is_walkable(room->map, nx, ny)) {
        pthread_mutex_unlock(&room->lock);
        return -2;  /* 无效移动 */
    }
    
    pthread_mutex_unlock(&room->lock);
    
    /* 写入 WAL */
    if (room->wal != NULL) {
        char data[256];
        snprintf(data, sizeof(data), "pid=%d,dir=%c,ox=%d,oy=%d,nx=%d,ny=%d",
                 player->id, direction, ox, oy, nx, ny);
        wal_write(room->wal, WAL_MOVE, data);
    }
    
    /* 更新位置 */
    pthread_mutex_lock(&player->lock);
    player->x = nx;
    player->y = ny;
    player->last_move_time = now;
    pthread_mutex_unlock(&player->lock);
    
    /* 检查道具拾取 */
    game_check_item_pickup(room, player);
    
    return 0;
}

/* ============== 攻击逻辑 ============== */

int game_handle_attack(Room *room, Player *player) {
    if (room == NULL || player == NULL) return -1;
    
    long now = player_get_time_ms();
    
    pthread_mutex_lock(&player->lock);
    
    /* 检查冷却 */
    if (now - player->last_attack_time < ATTACK_COOLDOWN_MS) {
        pthread_mutex_unlock(&player->lock);
        return -1;  /* 冷却中 */
    }
    
    int atk_x = player->x;
    int atk_y = player->y;
    int atk_power = player->atk;
    int attacker_id = player->id;
    
    player->last_attack_time = now;
    
    pthread_mutex_unlock(&player->lock);
    
    /* 写入 WAL */
    if (room->wal != NULL) {
        char data[256];
        snprintf(data, sizeof(data), "pid=%d,x=%d,y=%d,atk=%d",
                 attacker_id, atk_x, atk_y, atk_power);
        wal_write(room->wal, WAL_ATTACK, data);
    }
    
    /* 广播攻击事件 */
    char attack_event[64];
    snprintf(attack_event, sizeof(attack_event), "%d|%d|%d", attacker_id, atk_x, atk_y);
    game_broadcast_event(room, "ATTACK", attack_event);
    
    int hit_count = 0;
    
    /* 查找范围内的敌人 */
    pthread_mutex_lock(&room->lock);
    
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] < 0) continue;
        
        Player *target = player_find_by_id(room->player_ids[i]);
        if (target == NULL || target->id == attacker_id) continue;
        
        pthread_mutex_lock(&target->lock);
        
        /* 检查是否在攻击范围内 */
        int dist = map_distance(atk_x, atk_y, target->x, target->y);
        
        if (dist <= ATTACK_RANGE && target->status == PLAYER_GAMING && target->hp > 0) {
            /* 计算伤害 */
            int damage = game_calc_damage(atk_power, target->def);
            
            /* 检查护盾 */
            if (target->has_shield) {
                target->has_shield = 0;
                log_debug("Player %d shield absorbed attack from %d", 
                          target->id, attacker_id);
                
                /* 写入护盾被打破的 WAL 记录 */
                if (room->wal != NULL) {
                    char shield_data[256];
                    snprintf(shield_data, sizeof(shield_data), 
                             "atk=%d,vic=%d,dmg=0,hp=%d,shield_broken=1",
                             attacker_id, target->id, target->hp);
                    wal_write(room->wal, WAL_DAMAGE, shield_data);
                }
                
                /* 广播护盾吸收事件 */
                char shield_event[64];
                snprintf(shield_event, sizeof(shield_event), "%d|%d", attacker_id, target->id);
                pthread_mutex_unlock(&target->lock);
                pthread_mutex_unlock(&room->lock);
                game_broadcast_event(room, "SHIELD", shield_event);
                pthread_mutex_lock(&room->lock);
                continue;
            } else {
                target->hp -= damage;
                hit_count++;
                
                /* 写入伤害 WAL */
                if (room->wal != NULL) {
                    char dmg_data[256];
                    snprintf(dmg_data, sizeof(dmg_data), 
                             "atk=%d,vic=%d,dmg=%d,hp=%d",
                             attacker_id, target->id, damage, target->hp);
                    wal_write(room->wal, WAL_DAMAGE, dmg_data);
                }
                
                /* 广播伤害事件 */
                char dmg_event[64];
                snprintf(dmg_event, sizeof(dmg_event), "%d|%d|%d|%d", 
                         attacker_id, target->id, damage, target->hp);
                pthread_mutex_unlock(&target->lock);
                pthread_mutex_unlock(&room->lock);
                game_broadcast_event(room, "DAMAGE", dmg_event);
                pthread_mutex_lock(&room->lock);
                
                /* 重新获取 target 锁检查死亡 */
                target = player_find_by_id(room->player_ids[i]);
                if (target == NULL) continue;
                
                pthread_mutex_lock(&target->lock);
                
                /* 检查死亡 */
                if (target->hp <= 0) {
                    target->hp = 0;
                    target->status = PLAYER_DEAD;
                    
                    /* 写入死亡 WAL */
                    if (room->wal != NULL) {
                        char death_data[128];
                        snprintf(death_data, sizeof(death_data), 
                                 "pid=%d,killer=%d", target->id, attacker_id);
                        wal_write(room->wal, WAL_PLAYER_DEATH, death_data);
                    }
                    
                    /* 广播死亡事件 */
                    char event_data[128];
                    snprintf(event_data, sizeof(event_data), "%d|%d", 
                             attacker_id, target->id);
                    
                    pthread_mutex_unlock(&target->lock);
                    pthread_mutex_unlock(&room->lock);
                    
                    game_broadcast_event(room, "KILL", event_data);
                    
                    pthread_mutex_lock(&room->lock);
                    continue;
                }
            }
        }
        
        pthread_mutex_unlock(&target->lock);
    }
    
    pthread_mutex_unlock(&room->lock);
    
    log_debug("Player %d attacked at (%d,%d), hit %d targets", 
              attacker_id, atk_x, atk_y, hit_count);
    
    /* 发送攻击结果反馈给攻击者 */
    char result_event[64];
    snprintf(result_event, sizeof(result_event), "%d|%d", attacker_id, hit_count);
    game_broadcast_event(room, "ATTACK_RESULT", result_event);
    
    return 0;
}

/* ============== 道具逻辑 ============== */

void game_check_item_pickup(Room *room, Player *player) {
    if (room == NULL || player == NULL) return;
    
    pthread_mutex_lock(&player->lock);
    int px = player->x;
    int py = player->y;
    int inv_count = player->inventory_count;
    pthread_mutex_unlock(&player->lock);
    
    /* 背包已满 */
    if (inv_count >= MAX_INVENTORY) return;
    
    pthread_mutex_lock(&room->lock);
    
    for (int i = 0; i < room->item_count; i++) {
        MapItem *item = &room->items[i];
        
        if (item->active && item->x == px && item->y == py) {
            /* 拾取道具 */
            ItemType type = (ItemType)item->type;
            item->active = 0;
            
            /* 写入 WAL */
            if (room->wal != NULL) {
                char data[128];
                snprintf(data, sizeof(data), "pid=%d,item=%d,x=%d,y=%d",
                         player->id, type, px, py);
                wal_write(room->wal, WAL_PICKUP, data);
            }
            
            pthread_mutex_unlock(&room->lock);
            
            /* 添加到背包 */
            player_add_item(player, type);
            
            /* 广播拾取事件 */
            char event_data[64];
            snprintf(event_data, sizeof(event_data), "%d|%d", player->id, type);
            game_broadcast_event(room, "PICKUP", event_data);
            
            log_debug("Player %d picked up item type %d", player->id, type);
            return;
        }
    }
    
    pthread_mutex_unlock(&room->lock);
}

int game_handle_use_item(Room *room, Player *player, int item_index) {
    if (room == NULL || player == NULL) return -1;
    
    ItemType type = player_use_item(player, item_index);
    if (type == ITEM_NONE) {
        return -1;  /* 无效索引 */
    }
    
    /* 写入 WAL */
    if (room->wal != NULL) {
        char data[128];
        snprintf(data, sizeof(data), "pid=%d,item=%d,idx=%d",
                 player->id, type, item_index);
        wal_write(room->wal, WAL_USE_ITEM, data);
    }
    
    /* 应用道具效果 */
    pthread_mutex_lock(&player->lock);
    
    switch (type) {
        case ITEM_HEALTH:
            player->hp += HEALTH_RESTORE;
            if (player->hp > player->max_hp) {
                player->hp = player->max_hp;
            }
            log_debug("Player %d used health pack, hp=%d", player->id, player->hp);
            break;
            
        case ITEM_ATTACK:
            player->atk = player->base_atk + ATK_BUFF_AMOUNT;
            player->atk_buff_expire = player_get_time_ms() + ATK_BUFF_DURATION;
            player->atk_buff_warned = 0;  /* 重置提醒标志 */
            log_debug("Player %d used attack potion, atk=%d", player->id, player->atk);
            break;
            
        case ITEM_SHIELD:
            player->has_shield = 1;
            log_debug("Player %d activated shield", player->id);
            break;
            
        default:
            break;
    }
    
    pthread_mutex_unlock(&player->lock);
    
    return 0;
}

void game_spawn_items(Room *room) {
    if (room == NULL) return;
    
    pthread_mutex_lock(&room->lock);
    
    long now = player_get_time_ms();
    
    /* 检查刷新间隔 */
    if (now - room->last_item_spawn < ITEM_SPAWN_INTERVAL) {
        pthread_mutex_unlock(&room->lock);
        return;
    }
    
    room->last_item_spawn = now;
    
    /* 查找空闲道具槽 */
    int slot = -1;
    for (int i = 0; i < MAX_MAP_ITEMS; i++) {
        if (!room->items[i].active) {
            slot = i;
            break;
        }
    }
    
    if (slot < 0) {
        pthread_mutex_unlock(&room->lock);
        return;  /* 道具已满 */
    }
    
    /* 随机位置和类型 */
    int x, y;
    map_random_item_position(room->map, &x, &y);
    
    MapItemType type = (MapItemType)(rand() % 3 + 1);  /* 1-3 */
    
    room->items[slot].x = x;
    room->items[slot].y = y;
    room->items[slot].type = type;
    room->items[slot].active = 1;
    
    if (slot >= room->item_count) {
        room->item_count = slot + 1;
    }
    
    /* 写入 WAL */
    if (room->wal != NULL) {
        char data[128];
        snprintf(data, sizeof(data), "type=%d,x=%d,y=%d", type, x, y);
        wal_write(room->wal, WAL_ITEM_SPAWN, data);
    }
    
    pthread_mutex_unlock(&room->lock);
    
    log_debug("Item spawned: type=%d at (%d,%d)", type, x, y);
}

/* ============== 毒圈逻辑 ============== */

void game_update_poison(Room *room) {
    if (room == NULL) return;
    
    pthread_mutex_lock(&room->lock);
    
    long now = player_get_time_ms();
    long elapsed = now - room->game_start_time;
    
    /* 60秒后开始缩圈 */
    if (elapsed < POISON_START_TIME) {
        pthread_mutex_unlock(&room->lock);
        return;
    }
    
    /* 每30秒缩小一次 */
    if (now - room->last_poison_shrink < POISON_SHRINK_INTERVAL) {
        pthread_mutex_unlock(&room->lock);
        return;
    }
    
    if (room->poison_radius > 1) {
        room->poison_radius--;
        room->last_poison_shrink = now;
        
        /* 写入 WAL */
        if (room->wal != NULL) {
            char data[64];
            snprintf(data, sizeof(data), "radius=%d", room->poison_radius);
            wal_write(room->wal, WAL_POISON_SHRINK, data);
        }
        
        pthread_mutex_unlock(&room->lock);
        
        /* 广播毒圈事件 */
        game_broadcast_event(room, "POISON", "");
        
        log_debug("Poison zone shrunk to radius %d", room->poison_radius);
    } else {
        pthread_mutex_unlock(&room->lock);
    }
}

void game_apply_poison_damage(Room *room) {
    if (room == NULL) return;
    
    pthread_mutex_lock(&room->lock);
    int radius = room->poison_radius;
    pthread_mutex_unlock(&room->lock);
    
    pthread_mutex_lock(&room->lock);
    
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] < 0) continue;
        
        Player *p = player_find_by_id(room->player_ids[i]);
        if (p == NULL) continue;
        
        pthread_mutex_lock(&p->lock);
        
        if (p->status == PLAYER_GAMING && p->hp > 0) {
            if (map_is_in_poison(p->x, p->y, radius)) {
                /* 毒圈伤害（每秒5点，每tick约0.25点） */
                int damage = POISON_DAMAGE * TICK_INTERVAL_MS / 1000;
                if (damage < 1) damage = 1;
                
                p->hp -= damage;
                
                if (p->hp <= 0) {
                    p->hp = 0;
                    p->status = PLAYER_DEAD;
                    
                    /* 写入死亡 WAL */
                    if (room->wal != NULL) {
                        char data[64];
                        snprintf(data, sizeof(data), "pid=%d,killer=-1", p->id);
                        wal_write(room->wal, WAL_PLAYER_DEATH, data);
                    }
                    
                    log_debug("Player %d died from poison", p->id);
                }
            }
        }
        
        pthread_mutex_unlock(&p->lock);
    }
    
    pthread_mutex_unlock(&room->lock);
}

/* ============== 胜负判定 ============== */

int game_check_end(Room *room) {
    if (room == NULL) return -2;
    
    pthread_mutex_lock(&room->lock);
    
    long now = player_get_time_ms();
    long elapsed = now - room->game_start_time;
    
    /* 检查超时 */
    if (elapsed >= GAME_MAX_DURATION) {
        /* 所有存活者胜利 */
        pthread_mutex_unlock(&room->lock);
        return -1;  /* 平局/多人胜利 */
    }
    
    /* 恢复房间等待期内不判定胜负 */
    int is_recovery = room->is_recovery_room;
    int expected = room->expected_players;
    long recovery_start = room->recovery_start_time;
    int current_players = room->player_count;
    
    pthread_mutex_unlock(&room->lock);
    
    if (is_recovery && expected > 0) {
        /* 如果所有预期玩家都已重连，立即清除等待状态 */
        if (current_players >= expected) {
            pthread_mutex_lock(&room->lock);
            room->is_recovery_room = 0;
            room->expected_players = 0;
            pthread_mutex_unlock(&room->lock);
            log_info("All %d expected players reconnected, recovery wait ended", expected);
        } else {
            long recovery_elapsed = now - recovery_start;
            if (recovery_elapsed < RECOVERY_WAIT_TIME) {
                /* 还在等待其他玩家重连 */
                return -2;  /* 游戏继续 */
            }
            /* 等待期结束，未重连的玩家视为放弃 */
            pthread_mutex_lock(&room->lock);
            room->is_recovery_room = 0;
            room->expected_players = 0;
            pthread_mutex_unlock(&room->lock);
            log_info("Recovery wait ended: %d/%d players reconnected", current_players, expected);
        }
    }
    
    /* 统计存活玩家 */
    int alive_count = 0;
    int last_alive_id = -1;
    
    pthread_mutex_lock(&room->lock);
    
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] < 0) continue;
        
        Player *p = player_find_by_id(room->player_ids[i]);
        if (p == NULL) continue;
        
        pthread_mutex_lock(&p->lock);
        if (p->status == PLAYER_GAMING && p->hp > 0) {
            alive_count++;
            last_alive_id = p->id;
        }
        pthread_mutex_unlock(&p->lock);
    }
    
    pthread_mutex_unlock(&room->lock);
    
    if (alive_count == 0) {
        return -1;  /* 平局 */
    } else if (alive_count == 1) {
        return last_alive_id;  /* 单人胜利 */
    }
    
    return -2;  /* 游戏继续 */
}

/* ============== 广播函数 ============== */

void game_broadcast_state(Room *room) {
    if (room == NULL) return;
    
    char player_states[MAX_MSG_LEN] = "";
    char item_states[MAX_MSG_LEN] = "";
    int ps_pos = 0;
    int is_pos = 0;
    int first_player = 1;
    int first_item = 1;
    
    pthread_mutex_lock(&room->lock);
    
    long timestamp = player_get_time_ms();
    int poison_radius = room->poison_radius;
    
    /* 构建玩家状态 - 格式: id,x,y,hp,atk,def,status,shield,inv0,inv1,inv2,inv3,inv4 */
    for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
        if (room->player_ids[i] < 0) continue;
        
        Player *p = player_find_by_id(room->player_ids[i]);
        if (p == NULL) continue;
        
        pthread_mutex_lock(&p->lock);
        
        int written = snprintf(player_states + ps_pos, 
                               sizeof(player_states) - (size_t)ps_pos,
                               "%s%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d",
                               first_player ? "" : ";",
                               p->id, p->x, p->y, p->hp, p->atk, p->def, p->status,
                               p->has_shield,
                               (int)p->inventory[0].type, (int)p->inventory[1].type, 
                               (int)p->inventory[2].type, (int)p->inventory[3].type, 
                               (int)p->inventory[4].type);
        
        pthread_mutex_unlock(&p->lock);
        
        if (written > 0) {
            ps_pos += written;
            first_player = 0;
        }
    }
    
    /* 构建道具状态 */
    for (int i = 0; i < room->item_count; i++) {
        if (!room->items[i].active) continue;
        
        int written = snprintf(item_states + is_pos,
                               sizeof(item_states) - (size_t)is_pos,
                               "%s%d,%d,%d",
                               first_item ? "" : ";",
                               room->items[i].x, room->items[i].y, 
                               room->items[i].type);
        
        if (written > 0) {
            is_pos += written;
            first_item = 0;
        }
    }
    
    pthread_mutex_unlock(&room->lock);
    
    /* 构建并广播消息 */
    char msg[MAX_MSG_LEN];
    protocol_build_game_state(msg, sizeof(msg), timestamp, 
                              player_states, item_states, poison_radius);
    room_broadcast(room, msg);
}

void game_broadcast_event(Room *room, const char *event_type, const char *data) {
    if (room == NULL || event_type == NULL) return;
    
    char msg[MAX_MSG_LEN];
    protocol_build_game_event(msg, sizeof(msg), event_type, data ? data : "");
    room_broadcast(room, msg);
}

/* ============== 游戏线程 ============== */

void *game_thread_func(void *arg) {
    Room *room = (Room *)arg;
    if (room == NULL) return NULL;
    
    log_info("Game thread started for room %d", room->id);
    
    pthread_mutex_lock(&room->lock);
    room->game_thread_running = 1;
    pthread_mutex_unlock(&room->lock);
    
    while (1) {
        pthread_mutex_lock(&room->lock);
        int running = room->game_thread_running;
        RoomStatus status = room->status;
        pthread_mutex_unlock(&room->lock);
        
        if (!running || status != ROOM_GAMING) {
            break;
        }
        
        /* 更新玩家 buff */
        pthread_mutex_lock(&room->lock);
        for (int i = 0; i < MAX_ROOM_PLAYERS; i++) {
            if (room->player_ids[i] >= 0) {
                Player *p = player_find_by_id(room->player_ids[i]);
                if (p != NULL) {
                    game_update_buffs(p);
                }
            }
        }
        pthread_mutex_unlock(&room->lock);
        
        /* 更新毒圈 */
        game_update_poison(room);
        
        /* 应用毒圈伤害 */
        game_apply_poison_damage(room);
        
        /* 刷新道具 */
        game_spawn_items(room);
        
        /* 定期保存快照 */
        if (snapshot_should_save(room)) {
            snapshot_save(room);
        }
        
        /* 检查胜负 */
        int winner = game_check_end(room);
        if (winner != -2) {
            room_end_game(room, winner);
            break;
        }
        
        /* 广播状态 */
        game_broadcast_state(room);
        
        /* 等待下一个 tick */
        usleep(TICK_INTERVAL_MS * 1000);
    }
    
    log_info("Game thread ended for room %d", room->id);
    return NULL;
}
