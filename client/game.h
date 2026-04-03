/**
 * client/game.h - 客户端游戏状态模块
 */

#ifndef CLIENT_GAME_H
#define CLIENT_GAME_H

#include <pthread.h>
#include <stdatomic.h>

#define MAX_PLAYERS_VIEW 10
#define MAX_MESSAGES 50
#define MAX_ITEMS_VIEW 20
#define MAP_WIDTH 50
#define MAP_HEIGHT 20
#define ATTACK_EFFECT_DURATION_MS 300  /* 攻击特效持续时间 */

/* 玩家视图 */
typedef struct {
    int id;
    char name[32];
    int x, y;
    int hp;
    int status;
} PlayerView;

/* 道具视图 */
typedef struct {
    int x, y;
    int type;  /* 1=血包, 2=攻击药水, 3=护盾 */
} ItemView;

/* 聊天消息 */
typedef struct {
    char sender[32];
    char text[256];
} ChatMessage;

/* 攻击特效 */
typedef struct {
    int active;         /* 是否激活 */
    int x, y;           /* 攻击中心位置 */
    int radius;         /* 攻击范围 */
    long start_time;    /* 开始时间 */
} AttackEffect;

/* 游戏状态 */
typedef struct {
    /* 连接状态 */
    int connected;
    int logged_in;
    char username[32];
    
    /* 房间状态 */
    int in_room;
    int room_id;
    char room_name[32];
    int is_ready;
    
    /* 游戏状态 */
    int in_game;
    int my_id;
    int my_x, my_y;
    int my_hp, my_max_hp;
    int my_atk, my_def;
    int my_has_shield;
    
    /* 背包 */
    int inventory[5];       /* 道具类型: 0=空, 1=血包, 2=攻击药水, 3=护盾 */
    int inventory_count;
    
    /* 其他玩家 */
    PlayerView players[MAX_PLAYERS_VIEW];
    int player_count;
    
    /* 道具 */
    ItemView items[MAX_ITEMS_VIEW];
    int item_count;
    
    /* 地图 */
    char map[MAP_HEIGHT][MAP_WIDTH + 1];
    int poison_radius;
    
    /* 攻击特效 */
    AttackEffect attack_effect;
    
    /* 消息 */
    ChatMessage messages[MAX_MESSAGES];
    int msg_head;
    int msg_count;
    
    /* 最后的服务器消息 */
    char last_message[256];
    char last_error[256];
    
    /* 同步 */
    pthread_mutex_t lock;
    atomic_int dirty;
} GameState;

/* 全局游戏状态 */
extern GameState g_state;

/**
 * 初始化游戏状态
 */
void game_state_init(void);

/**
 * 清理游戏状态
 */
void game_state_cleanup(void);

/**
 * 从服务器消息更新状态
 * @param msg 原始消息
 */
void game_update_from_server(const char *msg);

/**
 * 添加聊天消息
 * @param sender 发送者
 * @param text 消息内容
 */
void game_add_message(const char *sender, const char *text);

/**
 * 接收线程函数
 * @param arg 未使用
 * @return NULL
 */
void *recv_thread_func(void *arg);

/**
 * 设置 dirty 标志
 */
void game_set_dirty(void);

/**
 * 清除 dirty 标志
 * @return 之前的值
 */
int game_clear_dirty(void);

/**
 * 触发攻击特效
 * @param x 攻击中心X坐标
 * @param y 攻击中心Y坐标
 * @param radius 攻击范围
 */
void game_trigger_attack_effect(int x, int y, int radius);

/**
 * 获取当前时间（毫秒）
 */
long game_get_time_ms(void);

#endif /* CLIENT_GAME_H */
