/**
 * config.h - 全局配置常量和原子变量声明
 * 
 * 《字符大乱斗》ASCII Battle Royale
 */

#ifndef CONFIG_H
#define CONFIG_H

#include <stdatomic.h>
#include <stdint.h>

/* 编译期检查 */
_Static_assert(sizeof(int) >= 4, "int must be at least 32 bits");

/* ============== 服务器配置 ============== */
#define SERVER_PORT         8888
#define MAX_EVENTS          64
#define RECV_BUFFER_SIZE    4096
#define SEND_BUFFER_SIZE    4096

/* ============== 玩家配置 ============== */
#define MAX_PLAYERS         256
#define MAX_USERNAME        32
#define MAX_PASSWORD        64
#define MAX_INVENTORY       5

/* ============== 房间配置 ============== */
#define MAX_ROOMS           32
#define MAX_ROOM_PLAYERS    10
#define MIN_ROOM_PLAYERS    2
#define MAX_ROOM_NAME       32
#define GAME_START_COUNTDOWN 3000   /* 3秒倒计时 */

/* ============== 地图配置 ============== */
#define MAP_WIDTH           50
#define MAP_HEIGHT          20
#define MAX_MAP_ITEMS       20

/* ============== 游戏配置 ============== */
#define TICK_INTERVAL_MS    50      /* 50ms = 20 tick/s */
#define MOVE_COOLDOWN_MS    200     /* 移动冷却 */
#define ATTACK_COOLDOWN_MS  1000    /* 攻击冷却 */
#define ATTACK_RANGE        3       /* 攻击范围（曼哈顿距离） */

#define INITIAL_HP          100
#define INITIAL_ATK         15
#define INITIAL_DEF         5

#define POISON_START_TIME   60000   /* 60秒后开始缩圈 */
#define POISON_SHRINK_INTERVAL 30000 /* 每30秒缩一圈 */
#define POISON_DAMAGE       5       /* 毒圈每秒伤害 */

#define ITEM_SPAWN_INTERVAL 10000   /* 10秒刷新道具 */
#define GAME_MAX_DURATION   300000  /* 5分钟 */

#define HEALTH_RESTORE      30      /* 血包恢复量 */
#define ATK_BUFF_AMOUNT     10      /* 攻击药水增加量 */
#define ATK_BUFF_DURATION   10000   /* 攻击buff持续时间 10秒 */

/* ============== 协议配置 ============== */
#define MAX_ARGS            16
#define MAX_ARG_LEN         2048
#define MAX_MSG_LEN         4096
#define PROTOCOL_DELIMITER  '|'
#define PROTOCOL_TERMINATOR '\n'

/* ============== WAL 配置 ============== */
#define WAL_DIR             "data/wal"
#define WAL_BUFFER_SIZE     4096
#define WAL_SYNC_INTERVAL_MS 1000
#define WAL_MAX_RETRY       3
#define RECOVERY_WAIT_TIME  30000   /* 恢复等待时间 30秒 */

/* ============== 快照配置 ============== */
#define SNAPSHOT_DIR        "data/wal"
#define SNAPSHOT_INTERVAL_MS 20000  /* 快照间隔 20秒 */

/* ============== 存储配置 ============== */
#define USERS_FILE          "data/users.dat"
#define MAX_USERS           10240
#define USER_RECORD_SIZE    128     /* 每条用户记录大小 */
#define PASSWORD_HASH_LEN   65      /* SHA256 hex + null */

/* ============== 错误码定义 ============== */
/* 协议错误 1001-1099 */
#define ERR_INVALID_FORMAT      1001
#define ERR_UNKNOWN_COMMAND     1002
#define ERR_INVALID_ARG_COUNT   1003
#define ERR_INVALID_ARG_FORMAT  1004

/* 认证错误 2001-2099 */
#define ERR_USERNAME_EXISTS     2001
#define ERR_INVALID_CREDENTIALS 2002
#define ERR_USER_LOGGED_IN      2003

/* 房间错误 3001-3099 */
#define ERR_ROOM_NOT_FOUND      3001
#define ERR_ROOM_FULL           3002
#define ERR_GAME_IN_PROGRESS    3003
#define ERR_NOT_IN_ROOM         3004

/* 游戏错误 4001-4099 */
#define ERR_MOVE_COOLDOWN       4001
#define ERR_INVALID_MOVE        4002
#define ERR_ATTACK_COOLDOWN     4003
#define ERR_INVALID_ITEM_INDEX  4004

/* ============== 全局原子变量声明 ============== */
extern atomic_int server_running;
extern atomic_int online_count;

#endif /* CONFIG_H */
