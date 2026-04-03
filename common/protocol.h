/**
 * protocol.h - 通信协议模块
 * 
 * 协议格式: CMD|arg1|arg2|...\n
 * - 管道符分隔参数
 * - 换行符结尾
 */

#ifndef PROTOCOL_H
#define PROTOCOL_H

#include "config.h"

/* 命令类型枚举 */
typedef enum {
    CMD_LOGIN,
    CMD_REGISTER,
    CMD_LIST_ROOMS,
    CMD_JOIN_ROOM,
    CMD_CREATE_ROOM,
    CMD_LEAVE_ROOM,
    CMD_READY,
    CMD_MOVE,
    CMD_ATTACK,
    CMD_USE_ITEM,
    CMD_CHAT,
    CMD_LOGOUT,
    /* 服务端响应 */
    CMD_OK,
    CMD_ERR,
    CMD_ROOM_LIST,
    CMD_ROOM_INFO,
    CMD_PLAYER_JOIN,
    CMD_PLAYER_LEAVE,
    CMD_GAME_START,
    CMD_MAP_DATA,
    CMD_GAME_STATE,
    CMD_GAME_EVENT,
    CMD_GAME_END,
    CMD_CHAT_MSG,
    CMD_KICK,
    CMD_UNKNOWN
} CommandType;

/* 解析后的消息结构 */
typedef struct {
    CommandType type;
    int argc;
    char args[MAX_ARGS][MAX_ARG_LEN];
} Message;

/* ============== 协议解析函数 ============== */

/**
 * 解析协议消息
 * @param raw 原始消息字符串（不含换行符）
 * @param msg 输出的解析结果
 * @return 0 成功, -1 失败
 */
int protocol_parse(const char *raw, Message *msg);

/**
 * 从缓冲区提取完整消息
 * @param buf 接收缓冲区
 * @param len 缓冲区当前长度（会被更新）
 * @param msg 输出的完整消息
 * @param max_msg_len 消息缓冲区最大长度
 * @return 1 提取成功, 0 无完整消息, -1 错误
 */
int protocol_extract_message(char *buf, int *len, char *msg, int max_msg_len);

/**
 * 获取命令类型名称
 * @param type 命令类型
 * @return 命令名称字符串
 */
const char *protocol_cmd_name(CommandType type);

/**
 * 从字符串解析命令类型
 * @param cmd_str 命令字符串
 * @return 命令类型
 */
CommandType protocol_parse_cmd(const char *cmd_str);

/* ============== 协议构造函数 ============== */

/**
 * 构造 OK 响应
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param message 响应消息
 * @return 写入的字节数, -1 失败
 */
int protocol_build_ok(char *buf, int max_len, const char *message);

/**
 * 构造 ERR 响应
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param code 错误码
 * @param message 错误消息
 * @return 写入的字节数, -1 失败
 */
int protocol_build_err(char *buf, int max_len, int code, const char *message);

/**
 * 构造房间列表响应
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param room_data 房间数据字符串 (id,name,count,max,status;...)
 * @return 写入的字节数, -1 失败
 */
int protocol_build_room_list(char *buf, int max_len, const char *room_data);

/**
 * 构造房间信息响应
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param room_id 房间ID
 * @param name 房间名
 * @param player_count 当前人数
 * @param max_players 最大人数
 * @param status 房间状态
 * @return 写入的字节数, -1 失败
 */
int protocol_build_room_info(char *buf, int max_len, int room_id, 
                             const char *name, int player_count, 
                             int max_players, int status);

/**
 * 构造玩家加入通知
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param player_id 玩家ID
 * @param username 用户名
 * @return 写入的字节数, -1 失败
 */
int protocol_build_player_join(char *buf, int max_len, int player_id, 
                               const char *username);

/**
 * 构造玩家离开通知
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param player_id 玩家ID
 * @return 写入的字节数, -1 失败
 */
int protocol_build_player_leave(char *buf, int max_len, int player_id);

/**
 * 构造游戏开始通知
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @return 写入的字节数, -1 失败
 */
int protocol_build_game_start(char *buf, int max_len);

/**
 * 构造地图数据消息
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param map 地图数据 (20行x50列)
 * @return 写入的字节数, -1 失败
 */
int protocol_build_map_data(char *buf, int max_len, 
                            const char map[20][51]);

/**
 * 构造游戏状态消息
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param timestamp 时间戳
 * @param player_states 玩家状态字符串
 * @param item_states 道具状态字符串
 * @param poison_radius 毒圈半径
 * @return 写入的字节数, -1 失败
 */
int protocol_build_game_state(char *buf, int max_len, long timestamp,
                              const char *player_states, 
                              const char *item_states,
                              int poison_radius);

/**
 * 构造游戏事件消息
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param event_type 事件类型
 * @param data 事件数据
 * @return 写入的字节数, -1 失败
 */
int protocol_build_game_event(char *buf, int max_len, const char *event_type, 
                              const char *data);

/**
 * 构造游戏结束消息
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param winner_id 胜者ID (-1 表示平局)
 * @param stats 统计数据
 * @return 写入的字节数, -1 失败
 */
int protocol_build_game_end(char *buf, int max_len, int winner_id, 
                            const char *stats);

/**
 * 构造聊天消息
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param sender 发送者用户名
 * @param message 消息内容
 * @return 写入的字节数, -1 失败
 */
int protocol_build_chat_msg(char *buf, int max_len, const char *sender, 
                            const char *message);

/**
 * 构造踢出消息
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @param reason 踢出原因
 * @return 写入的字节数, -1 失败
 */
int protocol_build_kick(char *buf, int max_len, const char *reason);

/* ============== 客户端命令构造 ============== */

/**
 * 构造登录命令
 */
int protocol_build_login(char *buf, int max_len, const char *username, 
                         const char *password);

/**
 * 构造注册命令
 */
int protocol_build_register(char *buf, int max_len, const char *username, 
                            const char *password);

/**
 * 构造创建房间命令
 */
int protocol_build_create_room(char *buf, int max_len, const char *room_name, 
                               int max_players);

/**
 * 构造加入房间命令
 */
int protocol_build_join_room(char *buf, int max_len, int room_id);

/**
 * 构造移动命令
 * @param direction 方向: 'U', 'D', 'L', 'R'
 */
int protocol_build_move(char *buf, int max_len, char direction);

/**
 * 构造攻击命令
 */
int protocol_build_attack(char *buf, int max_len);

/**
 * 构造使用道具命令
 */
int protocol_build_use_item(char *buf, int max_len, int item_index);

/**
 * 构造聊天命令
 */
int protocol_build_chat(char *buf, int max_len, const char *message);

/**
 * 构造简单命令（无参数）
 */
int protocol_build_simple(char *buf, int max_len, const char *cmd);

#endif /* PROTOCOL_H */
