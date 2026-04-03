/**
 * storage.c - 用户数据存储模块实现
 */

#include "storage.h"
#include "log.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>
#include <sys/stat.h>
#include <errno.h>

/* 简单的 SHA256 实现（用于密码哈希） */
/* 注意：生产环境应使用 OpenSSL 或其他加密库 */

/* SHA256 常量 */
static const uint32_t sha256_k[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
    0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
    0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
    0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
};

#define ROTR(x, n) (((x) >> (n)) | ((x) << (32 - (n))))
#define CH(x, y, z) (((x) & (y)) ^ (~(x) & (z)))
#define MAJ(x, y, z) (((x) & (y)) ^ ((x) & (z)) ^ ((y) & (z)))
#define EP0(x) (ROTR(x, 2) ^ ROTR(x, 13) ^ ROTR(x, 22))
#define EP1(x) (ROTR(x, 6) ^ ROTR(x, 11) ^ ROTR(x, 25))
#define SIG0(x) (ROTR(x, 7) ^ ROTR(x, 18) ^ ((x) >> 3))
#define SIG1(x) (ROTR(x, 17) ^ ROTR(x, 19) ^ ((x) >> 10))

static void sha256_transform(uint32_t state[8], const uint8_t data[64]) {
    uint32_t a, b, c, d, e, f, g, h, t1, t2, m[64];
    int i;
    
    for (i = 0; i < 16; i++) {
        m[i] = ((uint32_t)data[i * 4] << 24) |
               ((uint32_t)data[i * 4 + 1] << 16) |
               ((uint32_t)data[i * 4 + 2] << 8) |
               ((uint32_t)data[i * 4 + 3]);
    }
    for (; i < 64; i++) {
        m[i] = SIG1(m[i - 2]) + m[i - 7] + SIG0(m[i - 15]) + m[i - 16];
    }
    
    a = state[0]; b = state[1]; c = state[2]; d = state[3];
    e = state[4]; f = state[5]; g = state[6]; h = state[7];
    
    for (i = 0; i < 64; i++) {
        t1 = h + EP1(e) + CH(e, f, g) + sha256_k[i] + m[i];
        t2 = EP0(a) + MAJ(a, b, c);
        h = g; g = f; f = e; e = d + t1;
        d = c; c = b; b = a; a = t1 + t2;
    }
    
    state[0] += a; state[1] += b; state[2] += c; state[3] += d;
    state[4] += e; state[5] += f; state[6] += g; state[7] += h;
}

static void sha256(const char *input, char *output) {
    uint32_t state[8] = {
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
    };
    
    size_t len = strlen(input);
    size_t bits = len * 8;
    
    /* 填充 */
    size_t padded_len = ((len + 8) / 64 + 1) * 64;
    uint8_t *padded = calloc(padded_len, 1);
    if (padded == NULL) {
        output[0] = '\0';
        return;
    }
    
    memcpy(padded, input, len);
    padded[len] = 0x80;
    
    /* 添加长度（大端序） */
    for (int i = 0; i < 8; i++) {
        padded[padded_len - 1 - i] = (uint8_t)(bits >> (i * 8));
    }
    
    /* 处理块 */
    for (size_t i = 0; i < padded_len; i += 64) {
        sha256_transform(state, padded + i);
    }
    
    free(padded);
    
    /* 输出十六进制 */
    for (int i = 0; i < 8; i++) {
        sprintf(output + i * 8, "%08x", state[i]);
    }
    output[64] = '\0';
}

/* 全局状态 */
static UserRecord *users = NULL;
static int user_count = 0;
static pthread_mutex_t storage_mutex = PTHREAD_MUTEX_INITIALIZER;

/* 确保数据目录存在 */
static int ensure_data_dir(void) {
    struct stat st;
    if (stat("data", &st) == -1) {
        if (mkdir("data", 0755) == -1) {
            log_error("Failed to create data directory: %s", strerror(errno));
            return -1;
        }
    }
    return 0;
}

int storage_init(void) {
    pthread_mutex_lock(&storage_mutex);
    
    if (ensure_data_dir() < 0) {
        pthread_mutex_unlock(&storage_mutex);
        return -1;
    }
    
    /* 分配用户数组 */
    users = calloc(MAX_USERS, sizeof(UserRecord));
    if (users == NULL) {
        log_error("Failed to allocate user storage");
        pthread_mutex_unlock(&storage_mutex);
        return -1;
    }
    
    user_count = 0;
    
    /* 尝试加载现有数据 */
    pthread_mutex_unlock(&storage_mutex);
    storage_load();
    
    log_info("Storage initialized with %d users", user_count);
    return 0;
}

void storage_cleanup(void) {
    pthread_mutex_lock(&storage_mutex);
    
    /* 保存数据 */
    pthread_mutex_unlock(&storage_mutex);
    storage_save();
    pthread_mutex_lock(&storage_mutex);
    
    /* 释放内存 */
    if (users != NULL) {
        free(users);
        users = NULL;
    }
    user_count = 0;
    
    pthread_mutex_unlock(&storage_mutex);
    log_info("Storage cleaned up");
}

int storage_register_user(const char *username, const char *password) {
    if (username == NULL || password == NULL) {
        log_error("Register failed: username or password is NULL");
        return -2;
    }
    
    if (strlen(username) == 0 || strlen(username) >= 32) {
        log_error("Register failed: invalid username length: %zu", strlen(username));
        return -2;
    }
    
    pthread_mutex_lock(&storage_mutex);
    
    /* 检查存储是否已初始化 */
    if (users == NULL) {
        log_error("Register failed: storage not initialized (users is NULL)");
        pthread_mutex_unlock(&storage_mutex);
        return -2;
    }
    
    /* 检查用户是否已存在 */
    for (int i = 0; i < user_count; i++) {
        if (strcmp(users[i].username, username) == 0) {
            pthread_mutex_unlock(&storage_mutex);
            return -1;  /* 用户名已存在 */
        }
    }
    
    /* 检查容量 */
    if (user_count >= MAX_USERS) {
        pthread_mutex_unlock(&storage_mutex);
        log_error("User storage full: %d/%d", user_count, MAX_USERS);
        return -2;
    }
    
    /* 创建新用户 */
    UserRecord *user = &users[user_count];
    strncpy(user->username, username, 31);
    user->username[31] = '\0';
    
    /* 哈希密码 */
    sha256(password, user->password_hash);
    
    user->wins = 0;
    user->losses = 0;
    user->points = 0;
    memset(user->reserved, 0, sizeof(user->reserved));
    
    user_count++;
    
    pthread_mutex_unlock(&storage_mutex);
    
    log_info("User registered: %s (total: %d)", username, user_count);
    
    /* 立即保存 */
    storage_save();
    
    return 0;
}

int storage_verify_user(const char *username, const char *password) {
    if (username == NULL || password == NULL) {
        return -1;
    }
    
    pthread_mutex_lock(&storage_mutex);
    
    /* 查找用户 */
    UserRecord *user = NULL;
    for (int i = 0; i < user_count; i++) {
        if (strcmp(users[i].username, username) == 0) {
            user = &users[i];
            break;
        }
    }
    
    if (user == NULL) {
        pthread_mutex_unlock(&storage_mutex);
        return -1;  /* 用户不存在 */
    }
    
    /* 验证密码 */
    char hash[65];
    sha256(password, hash);
    
    if (strcmp(user->password_hash, hash) != 0) {
        pthread_mutex_unlock(&storage_mutex);
        return -2;  /* 密码错误 */
    }
    
    pthread_mutex_unlock(&storage_mutex);
    return 0;
}

int storage_update_stats(const char *username, int win) {
    if (username == NULL) {
        return -1;
    }
    
    pthread_mutex_lock(&storage_mutex);
    
    /* 查找用户 */
    UserRecord *user = NULL;
    for (int i = 0; i < user_count; i++) {
        if (strcmp(users[i].username, username) == 0) {
            user = &users[i];
            break;
        }
    }
    
    if (user == NULL) {
        pthread_mutex_unlock(&storage_mutex);
        return -1;
    }
    
    /* 更新统计 */
    if (win) {
        user->wins++;
        user->points += 10;
    } else {
        user->losses++;
        user->points += 1;
    }
    
    pthread_mutex_unlock(&storage_mutex);
    
    /* 保存 */
    storage_save();
    
    return 0;
}

UserRecord *storage_get_user(const char *username) {
    if (username == NULL) {
        return NULL;
    }
    
    pthread_mutex_lock(&storage_mutex);
    
    for (int i = 0; i < user_count; i++) {
        if (strcmp(users[i].username, username) == 0) {
            pthread_mutex_unlock(&storage_mutex);
            return &users[i];
        }
    }
    
    pthread_mutex_unlock(&storage_mutex);
    return NULL;
}

int storage_user_exists(const char *username) {
    return storage_get_user(username) != NULL ? 1 : 0;
}

int storage_save(void) {
    pthread_mutex_lock(&storage_mutex);
    
    if (ensure_data_dir() < 0) {
        pthread_mutex_unlock(&storage_mutex);
        return -1;
    }
    
    FILE *fp = fopen(USERS_FILE, "wb");
    if (fp == NULL) {
        log_error("Failed to open %s for writing: %s", USERS_FILE, strerror(errno));
        pthread_mutex_unlock(&storage_mutex);
        return -1;
    }
    
    /* 写入用户数量 */
    if (fwrite(&user_count, sizeof(int), 1, fp) != 1) {
        log_error("Failed to write user count");
        fclose(fp);
        pthread_mutex_unlock(&storage_mutex);
        return -1;
    }
    
    /* 写入用户数据 */
    if (user_count > 0) {
        if (fwrite(users, sizeof(UserRecord), (size_t)user_count, fp) != (size_t)user_count) {
            log_error("Failed to write user data");
            fclose(fp);
            pthread_mutex_unlock(&storage_mutex);
            return -1;
        }
    }
    
    fclose(fp);
    pthread_mutex_unlock(&storage_mutex);
    
    log_debug("Saved %d users to %s", user_count, USERS_FILE);
    return 0;
}

int storage_load(void) {
    pthread_mutex_lock(&storage_mutex);
    
    FILE *fp = fopen(USERS_FILE, "rb");
    if (fp == NULL) {
        /* 文件不存在是正常的 */
        if (errno != ENOENT) {
            log_warn("Failed to open %s: %s", USERS_FILE, strerror(errno));
        }
        pthread_mutex_unlock(&storage_mutex);
        return 0;
    }
    
    /* 读取用户数量 */
    int count;
    if (fread(&count, sizeof(int), 1, fp) != 1) {
        log_error("Failed to read user count");
        fclose(fp);
        pthread_mutex_unlock(&storage_mutex);
        return -1;
    }
    
    if (count < 0 || count > MAX_USERS) {
        log_error("Invalid user count: %d", count);
        fclose(fp);
        pthread_mutex_unlock(&storage_mutex);
        return -1;
    }
    
    /* 读取用户数据 */
    if (count > 0) {
        if (fread(users, sizeof(UserRecord), (size_t)count, fp) != (size_t)count) {
            log_error("Failed to read user data");
            fclose(fp);
            pthread_mutex_unlock(&storage_mutex);
            return -1;
        }
    }
    
    user_count = count;
    fclose(fp);
    pthread_mutex_unlock(&storage_mutex);
    
    log_info("Loaded %d users from %s", user_count, USERS_FILE);
    return 0;
}

int storage_get_user_count(void) {
    pthread_mutex_lock(&storage_mutex);
    int count = user_count;
    pthread_mutex_unlock(&storage_mutex);
    return count;
}
