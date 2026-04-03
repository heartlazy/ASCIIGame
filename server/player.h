/**
 * player.h - 玩家管理模块
 * 
 * 管理在线玩家的连接、状态和游戏属性
 */

#ifndef PLAYER_H
#define PLAYER_H

#include <pthread.h>
#include "../common/config.h"

/* 玩家状态 */
typedef enum {
    PLAYER_DISCONNECTED = 0,
    PLAYER_CONNECTED,       /* 已连接，未登录 */
    PLAYER_LOBBY,           /* 已登录，在大厅 */
    PLAYER_IN_ROOM,         /* 在房间中，未准备 */
    PLAYER_READY,           /* 在房间中，已准备 */
    PLAYER_GAMING,          /* 游戏中 */
    PLAYER_DEAD             /* 游戏中已死亡 */
} PlayerStatus;

/* 道具类型 */
typedef enum {
    ITEM_NONE = 0,
    ITEM_HEALTH,    /* + 血包 */
    ITEM_ATTACK,    /* ^ 攻击药水 */
    ITEM_SHIELD     /* * 护盾 */
} ItemType;

/* 背包道具 */
typedef struct {
    ItemType type;
} InventoryItem;

/* 玩家结构 */
typedef struct {
    int fd;                             /* socket fd */
    int id;                             /* 玩家 ID */
    char username[MAX_USERNAME];        /* 用户名 */
    int room_id;                        /* 所在房间 (-1 = 大厅) */
    PlayerStatus status;                /* 状态 */
    
    /* 游戏属性 */
    int x, y;                           /* 地图坐标 */
    int hp, max_hp;                     /* 生命值 */
    int atk, def;                       /* 攻防 */
    int base_atk;                       /* 基础攻击力 */
    
    /* 冷却时间（毫秒时间戳） */
    long last_move_time;                /* 上次移动时间 */
    long last_attack_time;              /* 上次攻击时间 */
    
    /* 道具和 buff */
    InventoryItem inventory[MAX_INVENTORY]; /* 背包 */
    int inventory_count;                /* 背包物品数 */
    int has_shield;                     /* 是否有护盾 */
    long atk_buff_expire;               /* 攻击 buff 过期时间 */
    int atk_buff_warned;                /* 是否已发送 buff 即将过期提醒 */
    
    /* 网络缓冲 */
    char recv_buf[RECV_BUFFER_SIZE];    /* 接收缓冲区 */
    int recv_len;                       /* 缓冲区已用长度 */
    
    pthread_mutex_t lock;               /* 玩家锁 */
} Player;

/**
 * 初始化玩家管理模块
 * @return 0 成功, -1 失败
 */
int player_init(void);

/**
 * 清理玩家管理模块
 */
void player_cleanup(void);

/**
 * 创建新玩家
 * @param fd socket fd
 * @return 玩家指针, NULL 失败
 */
Player *player_create(int fd);

/**
 * 销毁玩家
 * @param player 玩家指针
 */
void player_destroy(Player *player);

/**
 * 通过 fd 查找玩家
 * @param fd socket fd
 * @return 玩家指针, NULL 不存在
 */
Player *player_find_by_fd(int fd);

/**
 * 通过 ID 查找玩家
 * @param id 玩家 ID
 * @return 玩家指针, NULL 不存在
 */
Player *player_find_by_id(int id);

/**
 * 通过用户名查找玩家
 * @param username 用户名
 * @return 玩家指针, NULL 不存在
 */
Player *player_find_by_username(const char *username);

/**
 * 设置玩家用户名
 * @param player 玩家指针
 * @param username 用户名
 * @return 0 成功, -1 失败
 */
int player_set_username(Player *player, const char *username);

/**
 * 重置玩家游戏状态（游戏开始时调用）
 * @param player 玩家指针
 */
void player_reset_game_state(Player *player);

/**
 * 获取在线玩家数量
 * @return 在线玩家数
 */
int player_get_online_count(void);

/**
 * 向玩家发送消息
 * @param player 玩家指针
 * @param msg 消息内容
 * @return 0 成功, -1 失败
 */
int player_send(Player *player, const char *msg);

/**
 * 添加道具到背包
 * @param player 玩家指针
 * @param type 道具类型
 * @return 0 成功, -1 背包已满
 */
int player_add_item(Player *player, ItemType type);

/**
 * 使用背包道具
 * @param player 玩家指针
 * @param index 道具索引
 * @return 道具类型, ITEM_NONE 失败
 */
ItemType player_use_item(Player *player, int index);

/**
 * 获取当前时间（毫秒）
 * @return 毫秒时间戳
 */
long player_get_time_ms(void);

#endif /* PLAYER_H */
