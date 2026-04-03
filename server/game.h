/**
 * game.h - 游戏逻辑模块
 * 
 * 处理移动、攻击、道具、毒圈等游戏逻辑
 */

#ifndef GAME_H
#define GAME_H

#include "room.h"
#include "player.h"

/* 方向定义 */
#define DIR_UP    'U'
#define DIR_DOWN  'D'
#define DIR_LEFT  'L'
#define DIR_RIGHT 'R'

/**
 * 游戏线程入口
 * @param arg 房间指针
 * @return NULL
 */
void *game_thread_func(void *arg);

/* ============== 游戏动作处理 ============== */

/**
 * 处理移动命令
 * @param room 房间指针
 * @param player 玩家指针
 * @param direction 方向 (U/D/L/R)
 * @return 0 成功, -1 冷却中, -2 无效移动
 */
int game_handle_move(Room *room, Player *player, char direction);

/**
 * 处理攻击命令
 * @param room 房间指针
 * @param player 玩家指针
 * @return 0 成功, -1 冷却中
 */
int game_handle_attack(Room *room, Player *player);

/**
 * 处理使用道具命令
 * @param room 房间指针
 * @param player 玩家指针
 * @param item_index 道具索引
 * @return 0 成功, -1 无效索引
 */
int game_handle_use_item(Room *room, Player *player, int item_index);

/* ============== 游戏状态更新 ============== */

/**
 * 更新毒圈
 * @param room 房间指针
 */
void game_update_poison(Room *room);

/**
 * 应用毒圈伤害
 * @param room 房间指针
 */
void game_apply_poison_damage(Room *room);

/**
 * 刷新道具
 * @param room 房间指针
 */
void game_spawn_items(Room *room);

/**
 * 检查游戏结束条件
 * @param room 房间指针
 * @return 胜者 ID, -1 平局, -2 游戏继续
 */
int game_check_end(Room *room);

/* ============== 广播函数 ============== */

/**
 * 广播游戏状态
 * @param room 房间指针
 */
void game_broadcast_state(Room *room);

/**
 * 广播游戏事件
 * @param room 房间指针
 * @param event_type 事件类型
 * @param data 事件数据
 */
void game_broadcast_event(Room *room, const char *event_type, const char *data);

/* ============== 辅助函数 ============== */

/**
 * 计算伤害
 * @param atk 攻击力
 * @param def 防御力
 * @return 伤害值 (最小为 1)
 */
int game_calc_damage(int atk, int def);

/**
 * 检查道具拾取
 * @param room 房间指针
 * @param player 玩家指针
 */
void game_check_item_pickup(Room *room, Player *player);

/**
 * 更新玩家 buff
 * @param player 玩家指针
 */
void game_update_buffs(Player *player);

#endif /* GAME_H */
