/**
 * snapshot.h - 游戏状态快照模块
 * 
 * 定期保存游戏完整状态，配合 WAL 实现快速恢复
 */

#ifndef SNAPSHOT_H
#define SNAPSHOT_H

#include "room.h"
#include "../common/config.h"

/**
 * 保存房间快照
 * @param room 房间指针
 * @return 0 成功, -1 失败
 */
int snapshot_save(Room *room);

/**
 * 加载房间快照
 * @param room_id 房间 ID
 * @param room 输出房间数据（需预分配）
 * @return 0 成功, -1 失败
 */
int snapshot_load(int room_id, Room *room);

/**
 * 检查房间是否有快照
 * @param room_id 房间 ID
 * @return 1 存在, 0 不存在
 */
int snapshot_exists(int room_id);

/**
 * 删除房间快照
 * @param room_id 房间 ID
 * @return 0 成功, -1 失败
 */
int snapshot_delete(int room_id);

/**
 * 获取快照文件路径
 * @param room_id 房间 ID
 * @param path 输出路径缓冲区
 * @param max_len 缓冲区最大长度
 */
void snapshot_get_path(int room_id, char *path, int max_len);

/**
 * 检查是否需要创建快照
 * @param room 房间指针
 * @return 1 需要, 0 不需要
 */
int snapshot_should_save(Room *room);

#endif /* SNAPSHOT_H */
