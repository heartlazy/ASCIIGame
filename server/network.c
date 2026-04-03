/**
 * network.c - 网络模块实现
 */

#include "network.h"
#include "log.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <arpa/inet.h>

int network_init(int port) {
    int listen_fd;
    struct sockaddr_in addr;
    int opt = 1;
    
    /* 创建 socket */
    listen_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (listen_fd < 0) {
        log_error("Failed to create socket: %s", strerror(errno));
        return -1;
    }
    
    /* 设置 SO_REUSEADDR */
    if (setsockopt(listen_fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt)) < 0) {
        log_error("Failed to set SO_REUSEADDR: %s", strerror(errno));
        close(listen_fd);
        return -1;
    }
    
    /* 设置非阻塞 */
    if (set_nonblocking(listen_fd) < 0) {
        close(listen_fd);
        return -1;
    }
    
    /* 绑定地址 */
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = INADDR_ANY;
    addr.sin_port = htons((uint16_t)port);
    
    if (bind(listen_fd, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
        log_error("Failed to bind port %d: %s", port, strerror(errno));
        close(listen_fd);
        return -1;
    }
    
    /* 开始监听 */
    if (listen(listen_fd, SOMAXCONN) < 0) {
        log_error("Failed to listen: %s", strerror(errno));
        close(listen_fd);
        return -1;
    }
    
    log_info("Server listening on port %d", port);
    return listen_fd;
}

int epoll_create_instance(void) {
    int epoll_fd = epoll_create1(EPOLL_CLOEXEC);
    if (epoll_fd < 0) {
        log_error("Failed to create epoll: %s", strerror(errno));
        return -1;
    }
    return epoll_fd;
}

int epoll_add(int epoll_fd, int fd, uint32_t events) {
    struct epoll_event ev;
    ev.events = events;
    ev.data.fd = fd;
    
    if (epoll_ctl(epoll_fd, EPOLL_CTL_ADD, fd, &ev) < 0) {
        log_error("Failed to add fd %d to epoll: %s", fd, strerror(errno));
        return -1;
    }
    return 0;
}

int epoll_del(int epoll_fd, int fd) {
    if (epoll_ctl(epoll_fd, EPOLL_CTL_DEL, fd, NULL) < 0) {
        log_error("Failed to remove fd %d from epoll: %s", fd, strerror(errno));
        return -1;
    }
    return 0;
}

int epoll_mod(int epoll_fd, int fd, uint32_t events) {
    struct epoll_event ev;
    ev.events = events;
    ev.data.fd = fd;
    
    if (epoll_ctl(epoll_fd, EPOLL_CTL_MOD, fd, &ev) < 0) {
        log_error("Failed to modify fd %d in epoll: %s", fd, strerror(errno));
        return -1;
    }
    return 0;
}

int set_nonblocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) {
        log_error("Failed to get fd flags: %s", strerror(errno));
        return -1;
    }
    
    if (fcntl(fd, F_SETFL, flags | O_NONBLOCK) < 0) {
        log_error("Failed to set non-blocking: %s", strerror(errno));
        return -1;
    }
    return 0;
}

int send_all(int fd, const char *buf, int len) {
    int total_sent = 0;
    
    while (total_sent < len) {
        ssize_t sent = send(fd, buf + total_sent, (size_t)(len - total_sent), 0);
        
        if (sent < 0) {
            if (errno == EAGAIN || errno == EWOULDBLOCK) {
                /* 非阻塞模式，缓冲区满 */
                if (total_sent > 0) {
                    return total_sent;
                }
                return -2;  /* EAGAIN */
            }
            log_error("Send failed on fd %d: %s", fd, strerror(errno));
            return -1;
        }
        
        if (sent == 0) {
            /* 连接关闭 */
            return total_sent > 0 ? total_sent : -1;
        }
        
        total_sent += (int)sent;
    }
    
    return total_sent;
}

int recv_to_buffer(int fd, char *buf, int *len, int max_len) {
    if (*len >= max_len) {
        log_warn("Receive buffer full on fd %d", fd);
        return -1;
    }
    
    ssize_t received = recv(fd, buf + *len, (size_t)(max_len - *len), 0);
    
    if (received < 0) {
        if (errno == EAGAIN || errno == EWOULDBLOCK) {
            return -2;  /* EAGAIN */
        }
        log_error("Recv failed on fd %d: %s", fd, strerror(errno));
        return -1;
    }
    
    if (received == 0) {
        /* 连接关闭 */
        return 0;
    }
    
    *len += (int)received;
    return (int)received;
}

void close_connection(int fd) {
    if (fd >= 0) {
        close(fd);
        log_debug("Connection closed: fd=%d", fd);
    }
}

int accept_connection(int listen_fd) {
    struct sockaddr_in client_addr;
    socklen_t addr_len = sizeof(client_addr);
    
    int client_fd = accept(listen_fd, (struct sockaddr *)&client_addr, &addr_len);
    
    if (client_fd < 0) {
        if (errno == EAGAIN || errno == EWOULDBLOCK) {
            return -2;  /* EAGAIN */
        }
        log_error("Accept failed: %s", strerror(errno));
        return -1;
    }
    
    /* 设置非阻塞 */
    if (set_nonblocking(client_fd) < 0) {
        close(client_fd);
        return -1;
    }
    
    /* 设置 TCP_NODELAY 禁用 Nagle 算法 */
    int opt = 1;
    if (setsockopt(client_fd, IPPROTO_TCP, TCP_NODELAY, &opt, sizeof(opt)) < 0) {
        log_warn("Failed to set TCP_NODELAY: %s", strerror(errno));
    }
    
    char ip_str[INET_ADDRSTRLEN];
    inet_ntop(AF_INET, &client_addr.sin_addr, ip_str, sizeof(ip_str));
    log_info("New connection from %s:%d, fd=%d", 
             ip_str, ntohs(client_addr.sin_port), client_fd);
    
    return client_fd;
}
