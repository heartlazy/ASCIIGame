/**
 * client/network.c - 客户端网络模块实现
 */

#include "network.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <arpa/inet.h>
#include <netdb.h>
#include <fcntl.h>
#include <poll.h>

static int sock_fd = -1;
static volatile int disconnecting = 0;

int network_connect(const char *host, int port) {
    if (sock_fd >= 0) {
        close(sock_fd);
    }
    
    disconnecting = 0;
    
    /* 创建 socket */
    sock_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (sock_fd < 0) {
        perror("socket");
        return -1;
    }
    
    /* 解析主机名 */
    struct hostent *he = gethostbyname(host);
    if (he == NULL) {
        fprintf(stderr, "Failed to resolve host: %s\n", host);
        close(sock_fd);
        sock_fd = -1;
        return -1;
    }
    
    /* 设置地址 */
    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)port);
    memcpy(&addr.sin_addr, he->h_addr_list[0], (size_t)he->h_length);
    
    /* 连接 */
    if (connect(sock_fd, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
        perror("connect");
        close(sock_fd);
        sock_fd = -1;
        return -1;
    }
    
    /* 设置 TCP_NODELAY */
    int opt = 1;
    setsockopt(sock_fd, IPPROTO_TCP, TCP_NODELAY, &opt, sizeof(opt));
    
    return 0;
}

void network_disconnect(void) {
    disconnecting = 1;
    
    if (sock_fd >= 0) {
        /* 关闭读写，让阻塞的 recv 返回 */
        shutdown(sock_fd, SHUT_RDWR);
        close(sock_fd);
        sock_fd = -1;
    }
}

int network_send(const char *msg) {
    if (sock_fd < 0 || msg == NULL || disconnecting) {
        return -1;
    }
    
    size_t len = strlen(msg);
    size_t sent = 0;
    
    while (sent < len) {
        ssize_t n = send(sock_fd, msg + sent, len - sent, MSG_NOSIGNAL);
        if (n <= 0) {
            return -1;
        }
        sent += (size_t)n;
    }
    
    return 0;
}

int network_recv(char *buf, int max_len) {
    if (sock_fd < 0 || buf == NULL || max_len <= 0 || disconnecting) {
        return -1;
    }
    
    /* 使用 poll 设置超时，避免永久阻塞 */
    struct pollfd pfd;
    pfd.fd = sock_fd;
    pfd.events = POLLIN;
    
    int ret = poll(&pfd, 1, 100);  /* 100ms 超时 */
    
    if (ret <= 0 || disconnecting) {
        /* 超时或错误，返回 0 让调用者重试 */
        if (ret == 0) return 0;
        return -1;
    }
    
    if (pfd.revents & (POLLERR | POLLHUP | POLLNVAL)) {
        return -1;
    }
    
    ssize_t n = recv(sock_fd, buf, (size_t)(max_len - 1), 0);
    
    if (n < 0) {
        if (errno == EAGAIN || errno == EWOULDBLOCK) {
            return 0;
        }
        return -1;
    }
    
    if (n == 0) {
        /* 连接关闭 */
        return -1;
    }
    
    buf[n] = '\0';
    return (int)n;
}

int network_get_fd(void) {
    return sock_fd;
}

int network_is_connected(void) {
    return (sock_fd >= 0 && !disconnecting) ? 1 : 0;
}
