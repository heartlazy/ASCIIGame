/**
 * log.h - 日志模块
 * 
 * 支持 DEBUG/INFO/WARN/ERROR 级别
 * 输出格式: [时间戳] [级别] [文件:行号] 消息
 */

#ifndef LOG_H
#define LOG_H

#include <stdio.h>

/* 日志级别 */
typedef enum {
    LOG_DEBUG = 0,
    LOG_INFO  = 1,
    LOG_WARN  = 2,
    LOG_ERROR = 3
} LogLevel;

/**
 * 初始化日志系统
 * @param filename 日志文件名，NULL 表示输出到 stderr
 * @return 0 成功, -1 失败
 */
int log_init(const char *filename);

/**
 * 清理日志系统
 */
void log_cleanup(void);

/**
 * 设置日志级别
 * @param level 最低输出级别
 */
void log_set_level(LogLevel level);

/**
 * 获取当前日志级别
 * @return 当前日志级别
 */
LogLevel log_get_level(void);

/**
 * 写入日志（内部函数，通过宏调用）
 * @param level 日志级别
 * @param file 源文件名
 * @param line 行号
 * @param fmt 格式字符串
 * @param ... 可变参数
 */
void log_write(LogLevel level, const char *file, int line, 
               const char *fmt, ...);

/* 日志输出宏 - 使用 GNU 扩展 ##__VA_ARGS__ 处理空参数 */
#define log_debug(...) \
    log_write(LOG_DEBUG, __FILE__, __LINE__, __VA_ARGS__)

#define log_info(...) \
    log_write(LOG_INFO, __FILE__, __LINE__, __VA_ARGS__)

#define log_warn(...) \
    log_write(LOG_WARN, __FILE__, __LINE__, __VA_ARGS__)

#define log_error(...) \
    log_write(LOG_ERROR, __FILE__, __LINE__, __VA_ARGS__)

#endif /* LOG_H */
