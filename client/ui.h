/**
 * client/ui.h - 客户端 UI 模块
 */

#ifndef CLIENT_UI_H
#define CLIENT_UI_H

#include <ncurses.h>

/* 窗口指针 */
extern WINDOW *status_win;
extern WINDOW *map_win;
extern WINDOW *msg_win;
extern WINDOW *help_win;

/* 颜色定义 */
#define COLOR_PLAYER 1
#define COLOR_ENEMY 2
#define COLOR_WALL 3
#define COLOR_POISON 4
#define COLOR_ITEM 5
#define COLOR_HP_HIGH 6
#define COLOR_HP_MED 7
#define COLOR_HP_LOW 8
#define COLOR_ATTACK 9   /* 攻击特效颜色 */

/**
 * 初始化 UI
 * @return 0 成功, -1 失败
 */
int ui_init(void);

/**
 * 清理 UI
 */
void ui_cleanup(void);

/**
 * 刷新状态栏
 */
void ui_refresh_status(void);

/**
 * 刷新地图
 */
void ui_refresh_map(void);

/**
 * 刷新消息区
 */
void ui_refresh_messages(void);

/**
 * 刷新帮助栏
 */
void ui_refresh_help(void);

/**
 * 刷新所有
 */
void ui_refresh_all(void);

/**
 * 显示登录界面
 * @param username 输出用户名
 * @param password 输出密码
 * @return 0 登录, 1 注册, -1 退出
 */
int ui_login_screen(char *username, char *password);

/**
 * 显示大厅界面
 * @return 选择的操作
 */
int ui_lobby_screen(void);

/**
 * 聊天输入
 * @param buf 输出缓冲区
 * @param max_len 最大长度
 * @return 0 成功, -1 取消
 */
int ui_chat_input(char *buf, int max_len);

/**
 * 显示消息
 * @param msg 消息内容
 */
void ui_show_message(const char *msg);

/**
 * 显示错误
 * @param msg 错误消息
 */
void ui_show_error(const char *msg);

#endif /* CLIENT_UI_H */
