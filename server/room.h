/**
 * room.h - 房间管理模块
 * 
 * 管理游戏房间的创建、加入、离开和游戏状态
 */

#ifndef ROOM_H
#define ROOM_H

#include <pthread.h>
#include "../common/config.h"
#include "map.h"
#include "player.h"

/* 前向声明 */
struct WalManager;

/* 房间状态 */
typedef enum {
    ROOM_WAITING = 0,   /* 等待玩家 */
    ROOM_STARTING,      /* 倒计时中 */
    ROOM_GAMING,        /* 游戏进行中 */
    ROOM_ENDED          /* 游戏结束 */
} RoomStatus;

/* 房间结构 */
typedef struct {
    int id;                                 /* 房间 ID */
    char name[MAX_ROOM_NAME];               /* 房间名 */
    int max_players;                        /* 最大人数 */
    int player_ids[MAX_ROOM_PLAYERS];       /* 玩家 ID 列表 */
    int player_count;                       /* 当前人数 */
    RoomStatus status;                      /* 房间状态 */
    
    /* 地图数据 */
    char map[MAP_HEIGHT][MAP_WIDTH + 1];    /* 地图 */
    MapItem items[MAX_MAP_ITEMS];           /* 地图道具 */
    int item_count;                         /* 道具数量 */
    int poison_radius;                      /* 毒圈半径 */
    
    /* 时间（毫秒时间戳） */
    long game_start_time;                   /* 游戏开始时间 */
    long last_item_spawn;                   /* 上次道具刷新时间 */
    long last_poison_shrink;                /* 上次毒圈缩小时间 */
    long countdown_start;                   /* 倒计时开始时间 */
    
    /* 线程 */
    pthread_t game_thread;                  /* 游戏线程 */
    int game_thread_running;                /* 游戏线程运行标志 */
    pthread_mutex_t lock;                   /* 房间锁 */
    
    /* WAL */
    struct WalManager *wal;                 /* WAL 管理器 */
    
    /* 恢复相关 */
    int is_recovery_room;                   /* 是否为恢复的房间 */
    int expected_players;                   /* 预期玩家数（恢复时使用） */
    long recovery_start_time;               /* 恢复开始时间 */
    int original_room_id;                   /* 原始房间 ID（恢复时使用） */
    
    /* 快照相关 */
    long last_snapshot_time;                /* 上次快照时间 */
} Room;

/**
 * 初始化房间管理模块
 * @return 0 成功, -1 失败
 */
int room_init(void);

/**
 * 清理房间管理模块
 */
void room_cleanup(void);

/**
 * 创建新房间
 * @param name 房间名
 * @param max_players 最大人数 (6-10)
 * @return 房间指针, NULL 失败
 */
Room *room_create(const char *name, int max_players);

/**
 * 销毁房间
 * @param room 房间指针
 */
void room_destroy(Room *room);

/**
 * 通过 ID 查找房间
 * @param id 房间 ID
 * @return 房间指针, NULL 不存在
 */
Room *room_find_by_id(int id);

/**
 * 添加玩家到房间
 * @param room 房间指针
 * @param player 玩家指针
 * @return 0 成功, -1 房间已满, -2 游戏进行中
 */
int room_add_player(Room *room, Player *player);

/**
 * 从房间移除玩家
 * @param room 房间指针
 * @param player 玩家指针
 * @return 0 成功, -1 失败
 */
int room_remove_player(Room *room, Player *player);

/**
 * 开始游戏
 * @param room 房间指针
 * @return 0 成功, -1 失败
 */
int room_start_game(Room *room);

/**
 * 结束游戏
 * @param room 房间指针
 * @param winner_id 胜者 ID (-1 表示平局)
 */
void room_end_game(Room *room, int winner_id);

/**
 * 广播消息到房间内所有玩家
 * @param room 房间指针
 * @param msg 消息内容
 * @return 成功发送的玩家数
 */
int room_broadcast(Room *room, const char *msg);

/**
 * 获取房间列表
 * @param buf 输出缓冲区
 * @param max_len 缓冲区最大长度
 * @return 写入的字节数
 */
int room_get_list(char *buf, int max_len);

/**
 * 检查房间是否所有玩家都准备好
 * @param room 房间指针
 * @return 1 全部准备, 0 未全部准备
 */
int room_all_ready(Room *room);

/**
 * 获取房间内存活玩家数
 * @param room 房间指针
 * @return 存活玩家数
 */
int room_get_alive_count(Room *room);

/**
 * 获取房间数量
 * @return 房间数量
 */
int room_get_count(void);

/**
 * 获取房间状态名称
 * @param status 房间状态
 * @return 状态名称字符串
 */
const char *room_status_name(RoomStatus status);

#endif /* ROOM_H */
