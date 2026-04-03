/**
 * handler.h - 消息处理模块
 * 
 * 处理客户端发送的各种命令
 */

#ifndef HANDLER_H
#define HANDLER_H

#include "player.h"
#include "../common/protocol.h"

/**
 * 处理客户端消息
 * @param player 玩家指针
 * @param msg 解析后的消息
 * @return 0 成功, -1 失败
 */
int handler_process(Player *player, Message *msg);

/**
 * 处理登录命令
 */
int handler_login(Player *player, Message *msg);

/**
 * 处理注册命令
 */
int handler_register(Player *player, Message *msg);

/**
 * 处理房间列表命令
 */
int handler_list_rooms(Player *player, Message *msg);

/**
 * 处理创建房间命令
 */
int handler_create_room(Player *player, Message *msg);

/**
 * 处理加入房间命令
 */
int handler_join_room(Player *player, Message *msg);

/**
 * 处理离开房间命令
 */
int handler_leave_room(Player *player, Message *msg);

/**
 * 处理准备命令
 */
int handler_ready(Player *player, Message *msg);

/**
 * 处理移动命令
 */
int handler_move(Player *player, Message *msg);

/**
 * 处理攻击命令
 */
int handler_attack(Player *player, Message *msg);

/**
 * 处理使用道具命令
 */
int handler_use_item(Player *player, Message *msg);

/**
 * 处理聊天命令
 */
int handler_chat(Player *player, Message *msg);

/**
 * 处理登出命令
 */
int handler_logout(Player *player, Message *msg);

#endif /* HANDLER_H */
