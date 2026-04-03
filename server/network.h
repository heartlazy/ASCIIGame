/**
 * network.h - 网络模块
 * 
 * 封装 socket 和 epoll 操作
 */

#ifndef NETWORK_H
#define NETWORK_H

#include <sys/epoll.h>
#include <stdint.h>

/**
 * 初始化监听 socket
 * @param port 监听端口
 * @return socket fd, -1 失败
 */
int network_init(int port);

/**
 * 创建 epoll 实例
 * @return epoll fd, -1 失败
 */
int epoll_create_instance(void);

/**
 * 添加 fd 到 epoll
 * @param epoll_fd epoll 实例
 * @param fd 要添加的 fd
 * @param events 监听的事件
 * @return 0 成功, -1 失败
 */
int epoll_add(int epoll_fd, int fd, uint32_t events);

/**
 * 从 epoll 移除 fd
 * @param epoll_fd epoll 实例
 * @param fd 要移除的 fd
 * @return 0 成功, -1 失败
 */
int epoll_del(int epoll_fd, int fd);

/**
 * 修改 epoll 事件
 * @param epoll_fd epoll 实例
 * @param fd 要修改的 fd
 * @param events 新的事件
 * @return 0 成功, -1 失败
 */
int epoll_mod(int epoll_fd, int fd, uint32_t events);

/**
 * 设置 socket 为非阻塞
 * @param fd socket fd
 * @return 0 成功, -1 失败
 */
int set_nonblocking(int fd);

/**
 * 发送数据（处理部分发送）
 * @param fd socket fd
 * @param buf 数据缓冲区
 * @param len 数据长度
 * @return 发送的字节数, -1 失败, -2 EAGAIN
 */
int send_all(int fd, const char *buf, int len);

/**
 * 接收数据到缓冲区
 * @param fd socket fd
 * @param buf 接收缓冲区
 * @param len 当前缓冲区已用长度（会被更新）
 * @param max_len 缓冲区最大长度
 * @return 接收的字节数, 0 连接关闭, -1 错误, -2 EAGAIN
 */
int recv_to_buffer(int fd, char *buf, int *len, int max_len);

/**
 * 关闭连接
 * @param fd socket fd
 */
void close_connection(int fd);

/**
 * 接受新连接
 * @param listen_fd 监听 socket
 * @return 新连接的 fd, -1 失败, -2 EAGAIN
 */
int accept_connection(int listen_fd);

#endif /* NETWORK_H */
