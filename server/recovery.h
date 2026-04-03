/**
 * recovery.h - 崩溃恢复模块
 * 
 * 检测和恢复崩溃的游戏
 */

#ifndef RECOVERY_H
#define RECOVERY_H

#include "room.h"
#include "player.h"
#include "../common/config.h"

/* 前向声明已在 room.h 和 player.h 中定义 */

/**
 * 检查并恢复崩溃的游戏
 * @return 恢复的房间数, -1 失败
 */
int recovery_check_and_recover(void);

/**
 * 恢复单个房间
 * @param room_id 房间 ID
 * @return 0 成功, -1 失败
 */
int recovery_recover_room(int room_id);

/**
 * 列出可恢复的房间
 * @param room_ids 输出房间 ID 数组
 * @param max_count 数组最大长度
 * @return 可恢复的房间数
 */
int recovery_list_recoverable_rooms(int *room_ids, int max_count);

/**
 * 重放 WAL 日志
 * @param room 房间指针
 * @param wal_path WAL 文件路径
 * @return 0 成功, -1 失败
 */
int recovery_replay_wal(Room *room, const char *wal_path);

/**
 * 清理过期的 WAL 文件
 * @param max_age_seconds 最大保留时间（秒）
 * @return 清理的文件数
 */
int recovery_cleanup_old_wal(int max_age_seconds);

/**
 * 检查玩家是否有待恢复的游戏
 * @param username 用户名
 * @param room_id 输出房间 ID
 * @return 1 有待恢复的游戏, 0 没有
 */
int recovery_check_player(const char *username, int *room_id);

/**
 * 清理房间的恢复文件
 * @param room_id 房间 ID
 */
void recovery_cleanup_room(int room_id);

/**
 * 获取玩家的恢复状态
 * @param username 用户名
 * @param room_id 输出房间 ID
 * @param x, y 输出位置
 * @param hp, max_hp 输出血量
 * @param atk, def 输出攻防
 * @param has_shield 输出护盾状态
 * @param inventory 输出背包
 * @param inv_count 输出背包物品数
 * @param base_atk 输出基础攻击力
 * @param atk_buff_remain 输出攻击 buff 剩余时间（毫秒）
 * @return 0 成功, -1 失败
 */
int recovery_get_player_state(const char *username, int *room_id,
                               int *x, int *y, int *hp, int *max_hp,
                               int *atk, int *def, int *has_shield,
                               int inventory[MAX_INVENTORY], int *inv_count,
                               int *base_atk, long *atk_buff_remain);

/**
 * 为恢复的游戏创建房间
 * @param original_room_id 原始房间 ID
 * @return 新房间指针, NULL 失败
 */
Room *recovery_create_room_for_game(int original_room_id);

/**
 * 将玩家恢复到游戏中
 * @param player 玩家指针
 * @param original_room_id 原始房间 ID
 * @return 新房间 ID, -1 失败
 */
int recovery_restore_player_to_game(Player *player, int original_room_id);

/**
 * 从 WAL 文件中获取最大房间 ID
 * 用于服务器重启后恢复房间 ID 计数器
 * @return 最大房间 ID, 0 表示没有 WAL 文件
 */
int recovery_get_max_room_id(void);

#endif /* RECOVERY_H */
