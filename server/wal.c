/**
 * wal.c - Write-Ahead Log 模块实现
 */

#include "wal.h"
#include "log.h"
#include "player.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/stat.h>
#include <errno.h>

/* 动作类型名称映射 */
static const char *action_names[] = {
    "GAME_START",
    "PLAYER_JOIN",
    "PLAYER_LEAVE",
    "MOVE",
    "ATTACK",
    "PICKUP",
    "USE_ITEM",
    "DAMAGE",
    "PLAYER_DEATH",
    "ITEM_SPAWN",
    "POISON_SHRINK",
    "GAME_END",
    "CHECKPOINT"
};

#define ACTION_COUNT (sizeof(action_names) / sizeof(action_names[0]))

/* 确保 WAL 目录存在 */
static int ensure_wal_dir(void) {
    struct stat st;
    
    /* 检查 data 目录 */
    if (stat("data", &st) == -1) {
        if (mkdir("data", 0755) == -1) {
            log_error("Failed to create data directory: %s", strerror(errno));
            return -1;
        }
    }
    
    /* 检查 wal 目录 */
    if (stat(WAL_DIR, &st) == -1) {
        if (mkdir(WAL_DIR, 0755) == -1) {
            log_error("Failed to create WAL directory: %s", strerror(errno));
            return -1;
        }
    }
    
    return 0;
}

int wal_init(void) {
    if (ensure_wal_dir() < 0) {
        return -1;
    }
    
    log_info("WAL system initialized");
    return 0;
}

void wal_cleanup(void) {
    log_info("WAL system cleaned up");
}

void wal_get_path(int room_id, char *path, int max_len) {
    snprintf(path, (size_t)max_len, "%s/room_%d.wal", WAL_DIR, room_id);
}

/* 获取 WAL 文件的最后序列号 */
static int get_last_sequence(int room_id) {
    char path[256];
    wal_get_path(room_id, path, sizeof(path));
    
    FILE *fp = fopen(path, "r");
    if (fp == NULL) {
        return 0;
    }
    
    int last_seq = 0;
    char line[WAL_BUFFER_SIZE];
    
    while (fgets(line, sizeof(line), fp) != NULL) {
        int seq;
        if (sscanf(line, "%*d|%d|", &seq) == 1) {
            if (seq > last_seq) {
                last_seq = seq;
            }
        }
    }
    
    fclose(fp);
    return last_seq;
}

WalManager *wal_create_for_room(int room_id) {
    if (ensure_wal_dir() < 0) {
        return NULL;
    }
    
    WalManager *wal = calloc(1, sizeof(WalManager));
    if (wal == NULL) {
        log_error("Failed to allocate WAL manager");
        return NULL;
    }
    
    wal->room_id = room_id;
    
    /* 获取现有 WAL 的最后序列号，从那里继续 */
    int last_seq = get_last_sequence(room_id);
    atomic_init(&wal->sequence, last_seq + 1);
    
    wal->buffer_pos = 0;
    wal->last_sync_time = player_get_time_ms();
    
    /* 打开文件（追加模式） */
    char path[256];
    wal_get_path(room_id, path, sizeof(path));
    
    wal->fp = fopen(path, "a");
    if (wal->fp == NULL) {
        log_error("Failed to open WAL file %s: %s", path, strerror(errno));
        free(wal);
        return NULL;
    }
    
    /* 初始化锁 */
    pthread_mutex_init(&wal->lock, NULL);
    
    log_info("WAL created for room %d: %s (starting seq=%d)", room_id, path, last_seq + 1);
    return wal;
}

void wal_close_for_room(WalManager *wal) {
    if (wal == NULL) {
        return;
    }
    
    /* 刷新缓冲区 */
    wal_sync(wal);
    
    /* 关闭文件 */
    if (wal->fp != NULL) {
        fclose(wal->fp);
    }
    
    int room_id = wal->room_id;
    
    pthread_mutex_destroy(&wal->lock);
    free(wal);
    
    log_info("WAL closed for room %d", room_id);
}

int wal_write(WalManager *wal, WalActionType type, const char *data) {
    if (wal == NULL || wal->fp == NULL) {
        return -1;
    }
    
    pthread_mutex_lock(&wal->lock);
    
    /* 获取时间戳和序列号 */
    long timestamp = player_get_time_ms();
    long seq = atomic_fetch_add(&wal->sequence, 1);
    
    /* 格式化记录 */
    const char *action_name = wal_action_name(type);
    const char *action_data = data ? data : "";
    
    char record[WAL_BUFFER_SIZE];
    int len = snprintf(record, sizeof(record), "%ld|%ld|%d|%s|%s\n",
                       timestamp, seq, wal->room_id, action_name, action_data);
    
    if (len <= 0 || len >= (int)sizeof(record)) {
        log_error("WAL record too long");
        pthread_mutex_unlock(&wal->lock);
        return -1;
    }
    
    /* 写入文件 */
    if (fwrite(record, 1, (size_t)len, wal->fp) != (size_t)len) {
        log_error("Failed to write WAL record: %s", strerror(errno));
        pthread_mutex_unlock(&wal->lock);
        return -1;
    }
    
    /* 检查是否需要同步 */
    long now = player_get_time_ms();
    if (now - wal->last_sync_time >= WAL_SYNC_INTERVAL_MS) {
        fflush(wal->fp);
        fsync(fileno(wal->fp));
        wal->last_sync_time = now;
    }
    
    pthread_mutex_unlock(&wal->lock);
    
    log_debug("WAL write: room=%d, seq=%ld, type=%s", 
              wal->room_id, seq, action_name);
    
    return 0;
}

void wal_sync(WalManager *wal) {
    if (wal == NULL || wal->fp == NULL) {
        return;
    }
    
    pthread_mutex_lock(&wal->lock);
    
    fflush(wal->fp);
    fsync(fileno(wal->fp));
    wal->last_sync_time = player_get_time_ms();
    
    pthread_mutex_unlock(&wal->lock);
    
    log_debug("WAL synced for room %d", wal->room_id);
}

const char *wal_action_name(WalActionType type) {
    if (type >= 0 && type < (int)ACTION_COUNT) {
        return action_names[type];
    }
    return "UNKNOWN";
}

WalActionType wal_parse_action(const char *name) {
    if (name == NULL) {
        return -1;
    }
    
    for (size_t i = 0; i < ACTION_COUNT; i++) {
        if (strcmp(action_names[i], name) == 0) {
            return (WalActionType)i;
        }
    }
    
    return -1;
}

int wal_parse_record(const char *line, WalRecord *record) {
    if (line == NULL || record == NULL) {
        return -1;
    }
    
    memset(record, 0, sizeof(WalRecord));
    
    /* 复制行用于解析 */
    char buf[WAL_BUFFER_SIZE];
    strncpy(buf, line, sizeof(buf) - 1);
    buf[sizeof(buf) - 1] = '\0';
    
    /* 移除换行符 */
    size_t len = strlen(buf);
    while (len > 0 && (buf[len - 1] == '\n' || buf[len - 1] == '\r')) {
        buf[--len] = '\0';
    }
    
    /* 解析字段 */
    char *saveptr = NULL;
    char *token;
    
    /* 时间戳 */
    token = strtok_r(buf, "|", &saveptr);
    if (token == NULL) return -1;
    record->timestamp = atol(token);
    
    /* 序列号 */
    token = strtok_r(NULL, "|", &saveptr);
    if (token == NULL) return -1;
    record->sequence = atol(token);
    
    /* 房间 ID */
    token = strtok_r(NULL, "|", &saveptr);
    if (token == NULL) return -1;
    record->room_id = atoi(token);
    
    /* 动作类型 */
    token = strtok_r(NULL, "|", &saveptr);
    if (token == NULL) return -1;
    record->action_type = wal_parse_action(token);
    
    /* 动作数据（剩余部分） */
    token = strtok_r(NULL, "", &saveptr);
    if (token != NULL) {
        strncpy(record->action_data, token, sizeof(record->action_data) - 1);
        record->action_data[sizeof(record->action_data) - 1] = '\0';
    }
    
    return 0;
}

int wal_delete_for_room(int room_id) {
    char path[256];
    wal_get_path(room_id, path, sizeof(path));
    
    if (unlink(path) < 0) {
        if (errno != ENOENT) {
            log_error("Failed to delete WAL file %s: %s", path, strerror(errno));
            return -1;
        }
    }
    
    log_info("WAL deleted for room %d", room_id);
    return 0;
}

int wal_exists_for_room(int room_id) {
    char path[256];
    wal_get_path(room_id, path, sizeof(path));
    
    struct stat st;
    return stat(path, &st) == 0 ? 1 : 0;
}

int wal_truncate(WalManager *wal) {
    if (wal == NULL || wal->fp == NULL) {
        return -1;
    }
    
    pthread_mutex_lock(&wal->lock);
    
    /* 关闭当前文件 */
    fclose(wal->fp);
    
    /* 以写模式重新打开（清空文件） */
    char path[256];
    wal_get_path(wal->room_id, path, sizeof(path));
    
    wal->fp = fopen(path, "w");
    if (wal->fp == NULL) {
        log_error("Failed to truncate WAL file %s: %s", path, strerror(errno));
        pthread_mutex_unlock(&wal->lock);
        return -1;
    }
    
    /* 重置序列号 */
    atomic_store(&wal->sequence, 1);
    wal->buffer_pos = 0;
    
    pthread_mutex_unlock(&wal->lock);
    
    log_info("WAL truncated for room %d", wal->room_id);
    return 0;
}
