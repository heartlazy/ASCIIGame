/**
 * client/ui.c - 客户端 UI 模块实现
 */

#include "ui.h"
#include "game.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <locale.h>

/* 窗口指针 */
WINDOW *status_win = NULL;
WINDOW *map_win = NULL;
WINDOW *msg_win = NULL;
WINDOW *help_win = NULL;

/* 窗口尺寸 */
#define STATUS_HEIGHT 3
#define MAP_HEIGHT_UI 22
#define MSG_HEIGHT 8
#define HELP_HEIGHT 1

int ui_init(void) {
    /* 设置 locale 支持宽字符 */
    setlocale(LC_ALL, "");
    
    /* 减少 ESC 键延迟 */
    set_escdelay(25);
    
    /* 初始化 ncurses */
    initscr();
    
    if (has_colors()) {
        start_color();
        use_default_colors();
        
        /* 定义颜色对 */
        init_pair(COLOR_PLAYER, COLOR_GREEN, -1);
        init_pair(COLOR_ENEMY, COLOR_WHITE, -1);
        init_pair(COLOR_WALL, COLOR_WHITE, COLOR_WHITE);
        init_pair(COLOR_POISON, COLOR_RED, -1);
        init_pair(COLOR_ITEM, COLOR_YELLOW, -1);
        init_pair(COLOR_HP_HIGH, COLOR_GREEN, -1);
        init_pair(COLOR_HP_MED, COLOR_YELLOW, -1);
        init_pair(COLOR_HP_LOW, COLOR_RED, -1);
        init_pair(COLOR_ATTACK, COLOR_CYAN, -1);  /* 攻击特效：青色 */
    }
    
    cbreak();
    noecho();
    keypad(stdscr, TRUE);
    curs_set(0);
    nodelay(stdscr, TRUE);
    
    /* 创建窗口 */
    int max_y, max_x;
    getmaxyx(stdscr, max_y, max_x);
    (void)max_x;
    
    int y = 0;
    status_win = newwin(STATUS_HEIGHT, COLS, y, 0);
    y += STATUS_HEIGHT;
    
    map_win = newwin(MAP_HEIGHT_UI, COLS, y, 0);
    y += MAP_HEIGHT_UI;
    
    msg_win = newwin(MSG_HEIGHT, COLS, y, 0);
    
    help_win = newwin(HELP_HEIGHT, COLS, max_y - 1, 0);
    
    scrollok(msg_win, TRUE);
    
    return 0;
}

void ui_cleanup(void) {
    if (status_win) delwin(status_win);
    if (map_win) delwin(map_win);
    if (msg_win) delwin(msg_win);
    if (help_win) delwin(help_win);
    
    endwin();
}

void ui_refresh_status(void) {
    if (status_win == NULL) return;
    
    werase(status_win);
    
    pthread_mutex_lock(&g_state.lock);
    
    /* 第一行：用户信息 */
    const char *status_str;
    if (g_state.in_game) {
        status_str = "In Game";
    } else if (g_state.in_room) {
        status_str = g_state.is_ready ? "READY" : "Not Ready";
    } else {
        status_str = "In Lobby";
    }
    
    mvwprintw(status_win, 0, 0, "User: %s | Room: %s (ID:%d) | Status: %s",
              g_state.logged_in ? g_state.username : "Not logged in",
              g_state.in_room ? g_state.room_name : "Lobby",
              g_state.in_room ? g_state.room_id : 0,
              status_str);
    
    /* 第二行：游戏状态 */
    if (g_state.in_game) {
        int hp_color = COLOR_HP_HIGH;
        if (g_state.my_hp < 30) hp_color = COLOR_HP_LOW;
        else if (g_state.my_hp < 60) hp_color = COLOR_HP_MED;
        
        mvwprintw(status_win, 1, 0, "HP: ");
        wattron(status_win, COLOR_PAIR(hp_color));
        wprintw(status_win, "%d/%d", g_state.my_hp, g_state.my_max_hp);
        wattroff(status_win, COLOR_PAIR(hp_color));
        wprintw(status_win, " | ATK: %d | DEF: %d | Pos: (%d,%d)",
                g_state.my_atk, g_state.my_def, g_state.my_x, g_state.my_y);
        
        /* 显示护盾状态 */
        if (g_state.my_has_shield) {
            wattron(status_win, COLOR_PAIR(COLOR_ITEM) | A_BOLD);
            wprintw(status_win, " [SHIELD]");
            wattroff(status_win, COLOR_PAIR(COLOR_ITEM) | A_BOLD);
        }
        
        /* 第三行：背包 */
        mvwprintw(status_win, 2, 0, "Bag: ");
        for (int i = 0; i < 5; i++) {
            char item_char = '-';
            int color = 0;
            switch (g_state.inventory[i]) {
                case 1: item_char = '+'; color = COLOR_HP_HIGH; break;
                case 2: item_char = '^'; color = COLOR_HP_MED; break;
                case 3: item_char = '*'; color = COLOR_ITEM; break;
            }
            wprintw(status_win, "[%d:", i + 1);
            if (color) wattron(status_win, COLOR_PAIR(color) | A_BOLD);
            wprintw(status_win, "%c", item_char);
            if (color) wattroff(status_win, COLOR_PAIR(color) | A_BOLD);
            wprintw(status_win, "] ");
        }
    } else if (g_state.in_room) {
        mvwprintw(status_win, 1, 0, "Waiting for players... Press R to toggle ready status");
    } else {
        mvwprintw(status_win, 1, 0, "Press L to list rooms, C to create, J to join by ID");
    }
    
    pthread_mutex_unlock(&g_state.lock);
    
    wrefresh(status_win);
}

void ui_refresh_map(void) {
    if (map_win == NULL) return;
    
    werase(map_win);
    box(map_win, 0, 0);
    
    pthread_mutex_lock(&g_state.lock);
    
    if (g_state.in_game) {
        /* 检查攻击特效是否过期 */
        long now = game_get_time_ms();
        int effect_active = g_state.attack_effect.active;
        if (effect_active && (now - g_state.attack_effect.start_time > ATTACK_EFFECT_DURATION_MS)) {
            g_state.attack_effect.active = 0;
            effect_active = 0;
        }
        int effect_x = g_state.attack_effect.x;
        int effect_y = g_state.attack_effect.y;
        int effect_radius = g_state.attack_effect.radius;
        
        /* 绘制地图 */
        for (int y = 0; y < 20 && y < MAP_HEIGHT_UI - 2; y++) {
            for (int x = 0; x < 50 && x < COLS - 2; x++) {
                char c = g_state.map[y][x];
                int color = 0;
                
                /* 检查是否在毒圈内 */
                int cx = 25, cy = 10;
                int dx = abs(x - cx);
                int dy = abs(y - cy);
                int dist = (dx > dy) ? dx : dy;
                
                if (dist > g_state.poison_radius) {
                    c = '.';
                    color = COLOR_POISON;
                } else if (c == '#') {
                    color = COLOR_WALL;
                } else if (c == '+' || c == '^' || c == '*' || c == '$') {
                    color = COLOR_ITEM;
                }
                
                if (color) wattron(map_win, COLOR_PAIR(color));
                mvwaddch(map_win, y + 1, x + 1, c);
                if (color) wattroff(map_win, COLOR_PAIR(color));
            }
        }
        
        /* 绘制攻击特效（环形/菱形） */
        if (effect_active) {
            /* 攻击范围使用曼哈顿距离，绘制菱形边框 */
            for (int r = 1; r <= effect_radius; r++) {
                /* 绘制菱形的四条边 */
                for (int i = 0; i < r; i++) {
                    int px, py;
                    /* 上右边 */
                    px = effect_x + i;
                    py = effect_y - r + i;
                    if (px >= 0 && px < 50 && py >= 0 && py < 20) {
                        wattron(map_win, COLOR_PAIR(COLOR_ATTACK) | A_BOLD);
                        mvwaddch(map_win, py + 1, px + 1, '*');
                        wattroff(map_win, COLOR_PAIR(COLOR_ATTACK) | A_BOLD);
                    }
                    /* 右下边 */
                    px = effect_x + r - i;
                    py = effect_y + i;
                    if (px >= 0 && px < 50 && py >= 0 && py < 20) {
                        wattron(map_win, COLOR_PAIR(COLOR_ATTACK) | A_BOLD);
                        mvwaddch(map_win, py + 1, px + 1, '*');
                        wattroff(map_win, COLOR_PAIR(COLOR_ATTACK) | A_BOLD);
                    }
                    /* 下左边 */
                    px = effect_x - i;
                    py = effect_y + r - i;
                    if (px >= 0 && px < 50 && py >= 0 && py < 20) {
                        wattron(map_win, COLOR_PAIR(COLOR_ATTACK) | A_BOLD);
                        mvwaddch(map_win, py + 1, px + 1, '*');
                        wattroff(map_win, COLOR_PAIR(COLOR_ATTACK) | A_BOLD);
                    }
                    /* 左上边 */
                    px = effect_x - r + i;
                    py = effect_y - i;
                    if (px >= 0 && px < 50 && py >= 0 && py < 20) {
                        wattron(map_win, COLOR_PAIR(COLOR_ATTACK) | A_BOLD);
                        mvwaddch(map_win, py + 1, px + 1, '*');
                        wattroff(map_win, COLOR_PAIR(COLOR_ATTACK) | A_BOLD);
                    }
                }
            }
        }
        
        /* 绘制动态道具 */
        for (int i = 0; i < g_state.item_count; i++) {
            int ix = g_state.items[i].x;
            int iy = g_state.items[i].y;
            int itype = g_state.items[i].type;
            
            if (ix >= 0 && ix < 50 && iy >= 0 && iy < 20) {
                int cx = 25, cy = 10;
                int dx = abs(ix - cx);
                int dy = abs(iy - cy);
                int dist = (dx > dy) ? dx : dy;
                
                if (dist <= g_state.poison_radius) {
                    char c;
                    switch (itype) {
                        case 1: c = '+'; break;
                        case 2: c = '^'; break;
                        case 3: c = '*'; break;
                        default: c = '?'; break;
                    }
                    
                    wattron(map_win, COLOR_PAIR(COLOR_ITEM) | A_BOLD);
                    mvwaddch(map_win, iy + 1, ix + 1, c);
                    wattroff(map_win, COLOR_PAIR(COLOR_ITEM) | A_BOLD);
                }
            }
        }
        
        /* 绘制玩家 */
        for (int i = 0; i < g_state.player_count; i++) {
            PlayerView *pv = &g_state.players[i];
            
            if (pv->x >= 0 && pv->x < 50 && pv->y >= 0 && pv->y < 20) {
                char c;
                int color;
                
                if (pv->id == g_state.my_id) {
                    c = '@';
                    color = COLOR_PLAYER;
                } else {
                    c = 'A' + (i % 26);
                    color = COLOR_ENEMY;
                }
                
                wattron(map_win, COLOR_PAIR(color) | A_BOLD);
                mvwaddch(map_win, pv->y + 1, pv->x + 1, c);
                wattroff(map_win, COLOR_PAIR(color) | A_BOLD);
            }
        }
    } else {
        mvwprintw(map_win, MAP_HEIGHT_UI / 2, (COLS - 20) / 2, "Waiting for game...");
    }
    
    pthread_mutex_unlock(&g_state.lock);
    
    wrefresh(map_win);
}

void ui_refresh_messages(void) {
    if (msg_win == NULL) return;
    
    werase(msg_win);
    box(msg_win, 0, 0);
    mvwprintw(msg_win, 0, 2, " Messages ");
    
    pthread_mutex_lock(&g_state.lock);
    
    int line = 1;
    int start = g_state.msg_head;
    int count = g_state.msg_count;
    
    int display_count = (count < MSG_HEIGHT - 2) ? count : MSG_HEIGHT - 2;
    int display_start = (start + count - display_count) % MAX_MESSAGES;
    
    for (int i = 0; i < display_count && line < MSG_HEIGHT - 1; i++) {
        int idx = (display_start + i) % MAX_MESSAGES;
        mvwprintw(msg_win, line++, 1, "[%s] %s",
                  g_state.messages[idx].sender,
                  g_state.messages[idx].text);
    }
    
    pthread_mutex_unlock(&g_state.lock);
    
    wrefresh(msg_win);
}

void ui_refresh_help(void) {
    if (help_win == NULL) return;
    
    werase(help_win);
    
    pthread_mutex_lock(&g_state.lock);
    int in_game = g_state.in_game;
    int in_room = g_state.in_room;
    int is_ready = g_state.is_ready;
    pthread_mutex_unlock(&g_state.lock);
    
    if (in_game) {
        mvwprintw(help_win, 0, 0, 
                  "WASD: Move | J/Space: Attack | 1-5: Use Item | T: Chat | Q: Quit");
    } else if (in_room) {
        mvwprintw(help_win, 0, 0,
                  "R: %s | L/ESC: Leave Room | T: Chat | Q: Quit (Need 2+ ready)",
                  is_ready ? "Cancel Ready" : "Ready");
    } else {
        mvwprintw(help_win, 0, 0,
                  "C: Create Room | J: Join Room (by ID) | L: List Rooms | Q: Quit");
    }
    
    wrefresh(help_win);
}

void ui_refresh_all(void) {
    ui_refresh_status();
    ui_refresh_map();
    ui_refresh_messages();
    ui_refresh_help();
}

int ui_login_screen(char *username, char *password) {
    echo();
    curs_set(1);
    nodelay(stdscr, FALSE);
    
    clear();
    mvprintw(5, 10, "=== ASCII Battle Royale ===");
    mvprintw(7, 10, "1. Login");
    mvprintw(8, 10, "2. Register");
    mvprintw(9, 10, "Q. Quit");
    mvprintw(11, 10, "Choice: ");
    refresh();
    
    int ch = getch();
    
    if (ch == 'q' || ch == 'Q') {
        noecho();
        curs_set(0);
        nodelay(stdscr, TRUE);
        return -1;
    }
    
    int is_register = (ch == '2');
    
    mvprintw(13, 10, "Username: ");
    refresh();
    getnstr(username, 31);
    
    mvprintw(14, 10, "Password: ");
    refresh();
    noecho();
    getnstr(password, 31);
    
    curs_set(0);
    nodelay(stdscr, TRUE);
    
    return is_register ? 1 : 0;
}

int ui_lobby_screen(void) {
    return 0;
}

int ui_chat_input(char *buf, int max_len) {
    echo();
    curs_set(1);
    nodelay(stdscr, FALSE);
    
    mvprintw(LINES - 2, 0, "Chat: ");
    clrtoeol();
    refresh();
    
    getnstr(buf, max_len - 1);
    
    noecho();
    curs_set(0);
    nodelay(stdscr, TRUE);
    
    move(LINES - 2, 0);
    clrtoeol();
    refresh();
    
    return strlen(buf) > 0 ? 0 : -1;
}

void ui_show_message(const char *msg) {
    game_add_message("Info", msg);
}

void ui_show_error(const char *msg) {
    game_add_message("Error", msg);
}
