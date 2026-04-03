/**
 * storage.h - 用户数据存储模块
 * 
 * 管理用户注册、登录验证和统计数据
 */

#ifndef STORAGE_H
#define STORAGE_H

#include "../common/config.h"

/* 用户记录结构（128字节固定大小） */
typedef struct {
    char username[32];          /* 用户名 */
    char password_hash[65];     /* SHA256 哈希（hex 字符串） */
    int wins;                   /* 胜场 */
    int losses;                 /* 败场 */
    int points;                 /* 积分 */
    char reserved[19];          /* 保留字段，凑整到 128 字节 */
} UserRecord;

/**
 * 初始化存储模块
 * @return 0 成功, -1 失败
 */
int storage_init(void);

/**
 * 清理存储模块
 */
void storage_cleanup(void);

/**
 * 注册新用户
 * @param username 用户名
 * @param password 密码（明文，会被哈希）
 * @return 0 成功, -1 用户名已存在, -2 其他错误
 */
int storage_register_user(const char *username, const char *password);

/**
 * 验证用户登录
 * @param username 用户名
 * @param password 密码（明文）
 * @return 0 成功, -1 用户不存在, -2 密码错误
 */
int storage_verify_user(const char *username, const char *password);

/**
 * 更新用户统计数据
 * @param username 用户名
 * @param win 1 表示胜利, 0 表示失败
 * @return 0 成功, -1 失败
 */
int storage_update_stats(const char *username, int win);

/**
 * 获取用户记录
 * @param username 用户名
 * @return 用户记录指针, NULL 表示不存在
 */
UserRecord *storage_get_user(const char *username);

/**
 * 检查用户是否存在
 * @param username 用户名
 * @return 1 存在, 0 不存在
 */
int storage_user_exists(const char *username);

/**
 * 保存所有用户数据到磁盘
 * @return 0 成功, -1 失败
 */
int storage_save(void);

/**
 * 从磁盘加载用户数据
 * @return 0 成功, -1 失败
 */
int storage_load(void);

/**
 * 获取用户数量
 * @return 用户数量
 */
int storage_get_user_count(void);

#endif /* STORAGE_H */
