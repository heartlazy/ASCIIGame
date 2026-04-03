/**
 * map.c - 地图模块实现
 */

#include "map.h"
#include "log.h"
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <math.h>

/* 预定义地图模板 - 开放式设计，墙壁不闭合 */
static const char *map_template[MAP_HEIGHT] = {
    "##################################################",
    "#                    $                           #",
    "#   ##    ##         $         ##    ##    $     #",
    "#   ##    ##    $              ##    ##          #",
    "#              ###        ###              $     #",
    "#   $          # $        $ #          $         #",
    "#              ###        ###                    #",
    "#       $                          $       ##    #",
    "#   ##              ####              ##         #",
    "#   ##     $        #  #        $     ##    $    #",
    "#          $        #  #        $                #",
    "#   ##     $        ####        $     ##         #",
    "#   ##                                ##    $    #",
    "#       $                          $             #",
    "#              ###        ###              $     #",
    "#   $          # $        $ #          $         #",
    "#              ###        ###                    #",
    "#   ##    ##    $              ##    ##          #",
    "#   ##    ##         $         ##    ##    $     #",
    "##################################################"
};

/* 随机数种子初始化标志 */
static int rand_initialized = 0;

static void ensure_rand_init(void) {
    if (!rand_initialized) {
        srand((unsigned int)time(NULL));
        rand_initialized = 1;
    }
}

void map_generate(char map[MAP_HEIGHT][MAP_WIDTH + 1]) {
    ensure_rand_init();
    
    /* 复制模板 */
    for (int y = 0; y < MAP_HEIGHT; y++) {
        strncpy(map[y], map_template[y], MAP_WIDTH);
        map[y][MAP_WIDTH] = '\0';
    }
    
    log_debug("Map generated");
}

int map_is_walkable(const char map[MAP_HEIGHT][MAP_WIDTH + 1], int x, int y) {
    /* 边界检查 */
    if (x < 0 || x >= MAP_WIDTH || y < 0 || y >= MAP_HEIGHT) {
        return 0;
    }
    
    char c = map[y][x];
    
    /* 只有墙壁不可通行，其他都可以 */
    if (c == MAP_WALL) {
        return 0;
    }
    
    /* 空地、刷新点、道具位置都可通行 */
    return 1;
}

int map_is_in_poison(int x, int y, int poison_radius) {
    int cx, cy;
    map_get_center(&cx, &cy);
    
    /* 计算到中心的距离（使用切比雪夫距离，形成方形安全区） */
    int dx = abs(x - cx);
    int dy = abs(y - cy);
    int dist = (dx > dy) ? dx : dy;
    
    return dist > poison_radius;
}

void map_get_center(int *cx, int *cy) {
    if (cx != NULL) {
        *cx = MAP_WIDTH / 2;
    }
    if (cy != NULL) {
        *cy = MAP_HEIGHT / 2;
    }
}

void map_random_position(const char map[MAP_HEIGHT][MAP_WIDTH + 1], int *x, int *y) {
    ensure_rand_init();
    
    int attempts = 0;
    const int max_attempts = 1000;
    
    while (attempts < max_attempts) {
        int rx = rand() % (MAP_WIDTH - 2) + 1;
        int ry = rand() % (MAP_HEIGHT - 2) + 1;
        
        if (map_is_walkable(map, rx, ry)) {
            if (x != NULL) *x = rx;
            if (y != NULL) *y = ry;
            return;
        }
        attempts++;
    }
    
    /* 回退到中心附近 */
    log_warn("Failed to find random position, using center");
    if (x != NULL) *x = MAP_WIDTH / 2;
    if (y != NULL) *y = MAP_HEIGHT / 2;
}

void map_random_item_position(const char map[MAP_HEIGHT][MAP_WIDTH + 1], int *x, int *y) {
    ensure_rand_init();
    
    int attempts = 0;
    const int max_attempts = 1000;
    
    while (attempts < max_attempts) {
        int rx = rand() % (MAP_WIDTH - 2) + 1;
        int ry = rand() % (MAP_HEIGHT - 2) + 1;
        
        char c = map[ry][rx];
        /* 优先选择刷新点，其次是空地 */
        if (c == MAP_SPAWN || c == MAP_EMPTY) {
            if (x != NULL) *x = rx;
            if (y != NULL) *y = ry;
            return;
        }
        attempts++;
    }
    
    /* 回退到普通随机位置 */
    map_random_position(map, x, y);
}

int map_distance(int x1, int y1, int x2, int y2) {
    return abs(x1 - x2) + abs(y1 - y2);
}

int map_get_initial_poison_radius(void) {
    /* 初始安全区覆盖整个地图 */
    int cx = MAP_WIDTH / 2;
    int cy = MAP_HEIGHT / 2;
    
    /* 使用较大的值确保初始时整个地图都是安全的 */
    return (cx > cy) ? cx : cy;
}

char map_item_char(MapItemType type) {
    switch (type) {
        case MAP_ITEM_HEALTH: return MAP_HEALTH;
        case MAP_ITEM_ATTACK: return MAP_ATTACK;
        case MAP_ITEM_SHIELD: return MAP_SHIELD;
        default: return MAP_EMPTY;
    }
}

MapItemType map_char_to_item(char c) {
    switch (c) {
        case MAP_HEALTH: return MAP_ITEM_HEALTH;
        case MAP_ATTACK: return MAP_ITEM_ATTACK;
        case MAP_SHIELD: return MAP_ITEM_SHIELD;
        default: return MAP_ITEM_NONE;
    }
}
