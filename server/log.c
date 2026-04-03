/**
 * log.c - 日志模块实现
 */

#include "log.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>
#include <time.h>
#include <pthread.h>

/* 日志级别名称 */
static const char *level_names[] = {
    "DEBUG",
    "INFO",
    "WARN",
    "ERROR"
};

/* 日志级别颜色（ANSI） */
static const char *level_colors[] = {
    "\033[36m",  /* DEBUG: 青色 */
    "\033[32m",  /* INFO:  绿色 */
    "\033[33m",  /* WARN:  黄色 */
    "\033[31m"   /* ERROR: 红色 */
};

static const char *color_reset = "\033[0m";

/* 全局状态 */
static FILE *log_fp = NULL;
static LogLevel log_level = LOG_DEBUG;
static pthread_mutex_t log_mutex = PTHREAD_MUTEX_INITIALIZER;
static int use_color = 0;

/* 获取文件名（去除路径） */
static const char *get_filename(const char *path) {
    if (path == NULL) {
        return "unknown";
    }
    const char *slash = strrchr(path, '/');
    if (slash != NULL) {
        return slash + 1;
    }
    /* Windows 路径 */
    slash = strrchr(path, '\\');
    if (slash != NULL) {
        return slash + 1;
    }
    return path;
}

int log_init(const char *filename) {
    pthread_mutex_lock(&log_mutex);
    
    /* 关闭之前的文件 */
    if (log_fp != NULL && log_fp != stderr) {
        fclose(log_fp);
    }
    
    if (filename == NULL) {
        log_fp = stderr;
        use_color = 1;  /* stderr 支持颜色 */
    } else {
        log_fp = fopen(filename, "a");
        if (log_fp == NULL) {
            log_fp = stderr;
            pthread_mutex_unlock(&log_mutex);
            return -1;
        }
        use_color = 0;  /* 文件不使用颜色 */
    }
    
    pthread_mutex_unlock(&log_mutex);
    return 0;
}

void log_cleanup(void) {
    pthread_mutex_lock(&log_mutex);
    
    if (log_fp != NULL && log_fp != stderr) {
        fclose(log_fp);
    }
    log_fp = NULL;
    
    pthread_mutex_unlock(&log_mutex);
}

void log_set_level(LogLevel level) {
    if (level >= LOG_DEBUG && level <= LOG_ERROR) {
        log_level = level;
    }
}

LogLevel log_get_level(void) {
    return log_level;
}

void log_write(LogLevel level, const char *file, int line, 
               const char *fmt, ...) {
    /* 级别过滤 */
    if (level < log_level) {
        return;
    }
    
    pthread_mutex_lock(&log_mutex);
    
    FILE *fp = log_fp ? log_fp : stderr;
    
    /* 获取时间戳 */
    time_t now = time(NULL);
    struct tm *tm_info = localtime(&now);
    char time_buf[32];
    strftime(time_buf, sizeof(time_buf), "%Y-%m-%d %H:%M:%S", tm_info);
    
    /* 获取文件名 */
    const char *filename = get_filename(file);
    
    /* 输出日志头 */
    if (use_color) {
        fprintf(fp, "[%s] %s[%-5s]%s [%s:%d] ",
                time_buf,
                level_colors[level],
                level_names[level],
                color_reset,
                filename,
                line);
    } else {
        fprintf(fp, "[%s] [%-5s] [%s:%d] ",
                time_buf,
                level_names[level],
                filename,
                line);
    }
    
    /* 输出日志内容 */
    va_list args;
    va_start(args, fmt);
    vfprintf(fp, fmt, args);
    va_end(args);
    
    /* 换行 */
    fprintf(fp, "\n");
    
    /* 刷新缓冲区 */
    fflush(fp);
    
    pthread_mutex_unlock(&log_mutex);
}
