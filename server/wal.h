/**
 * wal.h - Write-Ahead Log 模块
 * 
 * 用于游戏进度持久化和崩溃恢复
 * 格式: TIMESTAMP|SEQUENCE|ROOM_ID|ACTION_TYPE|ACTION_DATA\n
 */

#ifndef WAL_H
#define WAL_H

#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include "../common/config.h"

/* WAL 动作类型 */
typedef enum {
    WAL_GAME_START = 0,
    WAL_PLAYER_JOIN,
    WAL_PLAYER_LEAVE,
    WAL_MOVE,
    WAL_ATTACK,
    WAL_PICKUP,
    WAL_USE_ITEM,
    WAL_DAMAGE,
    WAL_PLAYER_DEATH,
    WAL_ITEM_SPAWN,
    WAL_POISON_SHRINK,
    WAL_GAME_END,
    WAL_CHECKPOINT
} WalActionType;

/* WAL 管理器结构 */
typedef struct WalManager {
    int room_id;                        /* 房间 ID */
    FILE *fp;                           /* 文件指针 */
    atomic_long sequence;               /* C11 原子序列号 */
    pthread_mutex_t lock;               /* 写入锁 */
    char buffer[WAL_BUFFER_SIZE];       /* 写入缓冲区 */
    int buffer_pos;                     /* 缓冲区位置 */
    long last_sync_time;                /* 上次同步时间 */
} WalManager;

/* WAL 记录结构（用于解析） */
typedef struct {
    long timestamp;
    long sequence;
    int room_id;
    WalActionType action_type;
    char action_data[MAX_MSG_LEN];
} WalRecord;

/**
 * 全局 WAL 初始化
 * @return 0 成功, -1 失败
 */
int wal_init(void);

/**
 * 全局 WAL 清理
 */
void wal_cleanup(void);

/**
 * 为房间创建 WAL 管理器
 * @param room_id 房间 ID
 * @return WAL 管理器指针, NULL 失败
 */
WalManager *wal_create_for_room(int room_id);

/**
 * 关闭房间的 WAL 管理器
 * @param wal WAL 管理器指针
 */
void wal_close_for_room(WalManager *wal);

/**
 * 写入 WAL 日志
 * @param wal WAL 管理器指针
 * @param type 动作类型
 * @param data 动作数据
 * @return 0 成功, -1 失败
 */
int wal_write(WalManager *wal, WalActionType type, const char *data);

/**
 * 强制刷盘
 * @param wal WAL 管理器指针
 */
void wal_sync(WalManager *wal);

/**
 * 获取动作类型名称
 * @param type 动作类型
 * @return 类型名称字符串
 */
const char *wal_action_name(WalActionType type);

/**
 * 从名称解析动作类型
 * @param name 类型名称
 * @return 动作类型, -1 表示未知
 */
WalActionType wal_parse_action(const char *name);

/**
 * 解析 WAL 记录
 * @param line WAL 行
 * @param record 输出的记录结构
 * @return 0 成功, -1 失败
 */
int wal_parse_record(const char *line, WalRecord *record);

/**
 * 获取房间的 WAL 文件路径
 * @param room_id 房间 ID
 * @param path 输出路径缓冲区
 * @param max_len 缓冲区最大长度
 */
void wal_get_path(int room_id, char *path, int max_len);

/**
 * 删除房间的 WAL 文件
 * @param room_id 房间 ID
 * @return 0 成功, -1 失败
 */
int wal_delete_for_room(int room_id);

/**
 * 检查房间是否有 WAL 文件
 * @param room_id 房间 ID
 * @return 1 存在, 0 不存在
 */
int wal_exists_for_room(int room_id);

/**
 * 清空房间的 WAL 文件（快照后调用）
 * @param wal WAL 管理器指针
 * @return 0 成功, -1 失败
 */
int wal_truncate(WalManager *wal);

#endif /* WAL_H */
