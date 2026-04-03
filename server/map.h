/**
 * map.h - 地图模块
 * 
 * 地图生成、碰撞检测、毒圈计算
 */

#ifndef MAP_H
#define MAP_H

#include "../common/config.h"

/* 地图字符定义 */
#define MAP_EMPTY   ' '     /* 可通行区域 */
#define MAP_WALL    '#'     /* 墙壁 */
#define MAP_SPAWN   '$'     /* 道具刷新点 */
#define MAP_POISON  '.'     /* 毒圈区域（客户端渲染） */
#define MAP_PLAYER  '@'     /* 当前玩家（客户端渲染） */
#define MAP_HEALTH  '+'     /* 血包 */
#define MAP_ATTACK  '^'     /* 攻击药水 */
#define MAP_SHIELD  '*'     /* 护盾 */

/* 道具类型（与 player.h 中定义一致） */
#ifndef ITEM_TYPE_DEFINED
#define ITEM_TYPE_DEFINED
typedef enum {
    MAP_ITEM_NONE = 0,
    MAP_ITEM_HEALTH,
    MAP_ITEM_ATTACK,
    MAP_ITEM_SHIELD
} MapItemType;
#endif

/* 地图道具结构 */
typedef struct {
    int x, y;
    MapItemType type;
    int active;
} MapItem;

/**
 * 生成地图
 * @param map 地图数组 [MAP_HEIGHT][MAP_WIDTH + 1]
 */
void map_generate(char map[MAP_HEIGHT][MAP_WIDTH + 1]);

/**
 * 检查位置是否可通行
 * @param map 地图数组
 * @param x X 坐标
 * @param y Y 坐标
 * @return 1 可通行, 0 不可通行
 */
int map_is_walkable(const char map[MAP_HEIGHT][MAP_WIDTH + 1], int x, int y);

/**
 * 检查位置是否在毒圈内
 * @param x X 坐标
 * @param y Y 坐标
 * @param poison_radius 安全区半径
 * @return 1 在毒圈内（危险）, 0 在安全区内
 */
int map_is_in_poison(int x, int y, int poison_radius);

/**
 * 获取地图中心坐标
 * @param cx 输出 X 坐标
 * @param cy 输出 Y 坐标
 */
void map_get_center(int *cx, int *cy);

/**
 * 生成随机可通行位置
 * @param map 地图数组
 * @param x 输出 X 坐标
 * @param y 输出 Y 坐标
 */
void map_random_position(const char map[MAP_HEIGHT][MAP_WIDTH + 1], int *x, int *y);

/**
 * 生成随机道具刷新位置
 * @param map 地图数组
 * @param x 输出 X 坐标
 * @param y 输出 Y 坐标
 */
void map_random_item_position(const char map[MAP_HEIGHT][MAP_WIDTH + 1], int *x, int *y);

/**
 * 计算两点之间的距离
 * @param x1 点1 X 坐标
 * @param y1 点1 Y 坐标
 * @param x2 点2 X 坐标
 * @param y2 点2 Y 坐标
 * @return 曼哈顿距离
 */
int map_distance(int x1, int y1, int x2, int y2);

/**
 * 获取初始毒圈半径
 * @return 初始半径
 */
int map_get_initial_poison_radius(void);

/**
 * 获取道具字符
 * @param type 道具类型
 * @return 道具字符
 */
char map_item_char(MapItemType type);

/**
 * 从字符获取道具类型
 * @param c 字符
 * @return 道具类型
 */
MapItemType map_char_to_item(char c);

#endif /* MAP_H */
