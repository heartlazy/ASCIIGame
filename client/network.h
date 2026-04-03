/**
 * client/network.h - 客户端网络模块
 */

#ifndef CLIENT_NETWORK_H
#define CLIENT_NETWORK_H

/**
 * 连接服务器
 * @param host 服务器地址
 * @param port 端口
 * @return 0 成功, -1 失败
 */
int network_connect(const char *host, int port);

/**
 * 断开连接
 */
void network_disconnect(void);

/**
 * 发送消息
 * @param msg 消息内容
 * @return 0 成功, -1 失败
 */
int network_send(const char *msg);

/**
 * 接收消息（阻塞）
 * @param buf 接收缓冲区
 * @param max_len 缓冲区最大长度
 * @return 接收的字节数, 0 连接关闭, -1 错误
 */
int network_recv(char *buf, int max_len);

/**
 * 获取 socket fd
 * @return socket fd, -1 未连接
 */
int network_get_fd(void);

/**
 * 检查是否已连接
 * @return 1 已连接, 0 未连接
 */
int network_is_connected(void);

#endif /* CLIENT_NETWORK_H */
