/**
 * protocol.c - 通信协议模块实现
 */

#include "protocol.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>

/* 命令名称映射表 */
static const struct {
    CommandType type;
    const char *name;
} cmd_map[] = {
    { CMD_LOGIN,        "LOGIN" },
    { CMD_REGISTER,     "REGISTER" },
    { CMD_LIST_ROOMS,   "LIST_ROOMS" },
    { CMD_JOIN_ROOM,    "JOIN_ROOM" },
    { CMD_CREATE_ROOM,  "CREATE_ROOM" },
    { CMD_LEAVE_ROOM,   "LEAVE_ROOM" },
    { CMD_READY,        "READY" },
    { CMD_MOVE,         "MOVE" },
    { CMD_ATTACK,       "ATTACK" },
    { CMD_USE_ITEM,     "USE_ITEM" },
    { CMD_CHAT,         "CHAT" },
    { CMD_LOGOUT,       "LOGOUT" },
    { CMD_OK,           "OK" },
    { CMD_ERR,          "ERR" },
    { CMD_ROOM_LIST,    "ROOM_LIST" },
    { CMD_ROOM_INFO,    "ROOM_INFO" },
    { CMD_PLAYER_JOIN,  "PLAYER_JOIN" },
    { CMD_PLAYER_LEAVE, "PLAYER_LEAVE" },
    { CMD_GAME_START,   "GAME_START" },
    { CMD_MAP_DATA,     "MAP_DATA" },
    { CMD_GAME_STATE,   "GAME_STATE" },
    { CMD_GAME_EVENT,   "GAME_EVENT" },
    { CMD_GAME_END,     "GAME_END" },
    { CMD_CHAT_MSG,     "CHAT_MSG" },
    { CMD_KICK,         "KICK" },
    { CMD_UNKNOWN,      "UNKNOWN" }
};

#define CMD_MAP_SIZE (sizeof(cmd_map) / sizeof(cmd_map[0]))

/* ============== 辅助函数 ============== */

const char *protocol_cmd_name(CommandType type) {
    for (size_t i = 0; i < CMD_MAP_SIZE; i++) {
        if (cmd_map[i].type == type) {
            return cmd_map[i].name;
        }
    }
    return "UNKNOWN";
}

CommandType protocol_parse_cmd(const char *cmd_str) {
    if (cmd_str == NULL) {
        return CMD_UNKNOWN;
    }
    for (size_t i = 0; i < CMD_MAP_SIZE - 1; i++) {
        if (strcmp(cmd_map[i].name, cmd_str) == 0) {
            return cmd_map[i].type;
        }
    }
    return CMD_UNKNOWN;
}

/* ============== 协议解析函数 ============== */

int protocol_parse(const char *raw, Message *msg) {
    if (raw == NULL || msg == NULL) {
        return -1;
    }
    
    /* 初始化消息结构 */
    memset(msg, 0, sizeof(Message));
    msg->type = CMD_UNKNOWN;
    msg->argc = 0;
    
    /* 空消息 */
    if (raw[0] == '\0') {
        return -1;
    }
    
    /* 复制原始消息用于解析 */
    char buf[MAX_MSG_LEN];
    size_t raw_len = strlen(raw);
    if (raw_len >= MAX_MSG_LEN) {
        return -1;
    }
    strncpy(buf, raw, MAX_MSG_LEN - 1);
    buf[MAX_MSG_LEN - 1] = '\0';
    
    /* 移除末尾的换行符 */
    size_t len = strlen(buf);
    while (len > 0 && (buf[len - 1] == '\n' || buf[len - 1] == '\r')) {
        buf[--len] = '\0';
    }
    
    /* 使用管道符分割 */
    char *saveptr = NULL;
    char *token = strtok_r(buf, "|", &saveptr);
    
    if (token == NULL) {
        return -1;
    }
    
    /* 解析命令类型 */
    msg->type = protocol_parse_cmd(token);
    
    /* 解析参数 */
    while ((token = strtok_r(NULL, "|", &saveptr)) != NULL) {
        if (msg->argc >= MAX_ARGS) {
            break;
        }
        strncpy(msg->args[msg->argc], token, MAX_ARG_LEN - 1);
        msg->args[msg->argc][MAX_ARG_LEN - 1] = '\0';
        msg->argc++;
    }
    
    return 0;
}

int protocol_extract_message(char *buf, int *len, char *msg, int max_msg_len) {
    if (buf == NULL || len == NULL || msg == NULL || max_msg_len <= 0) {
        return -1;
    }
    
    if (*len <= 0) {
        return 0;
    }
    
    /* 查找换行符 */
    char *newline = memchr(buf, '\n', (size_t)*len);
    if (newline == NULL) {
        return 0;  /* 无完整消息 */
    }
    
    /* 计算消息长度（包含换行符） */
    int msg_len = (int)(newline - buf + 1);
    
    if (msg_len > max_msg_len) {
        /* 消息过长，丢弃 */
        memmove(buf, newline + 1, (size_t)(*len - msg_len));
        *len -= msg_len;
        return -1;
    }
    
    /* 复制消息（不含换行符） */
    memcpy(msg, buf, (size_t)(msg_len - 1));
    msg[msg_len - 1] = '\0';
    
    /* 移动缓冲区剩余数据 */
    memmove(buf, newline + 1, (size_t)(*len - msg_len));
    *len -= msg_len;
    
    return 1;
}

/* ============== 协议构造函数 ============== */

int protocol_build_ok(char *buf, int max_len, const char *message) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    const char *msg = message ? message : "";
    return snprintf(buf, (size_t)max_len, "OK|%s\n", msg);
}

int protocol_build_err(char *buf, int max_len, int code, const char *message) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    const char *msg = message ? message : "";
    return snprintf(buf, (size_t)max_len, "ERR|%d|%s\n", code, msg);
}

int protocol_build_room_list(char *buf, int max_len, const char *room_data) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    const char *data = room_data ? room_data : "";
    return snprintf(buf, (size_t)max_len, "ROOM_LIST|%s\n", data);
}

int protocol_build_room_info(char *buf, int max_len, int room_id, 
                             const char *name, int player_count, 
                             int max_players, int status) {
    if (buf == NULL || max_len <= 0 || name == NULL) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "ROOM_INFO|%d|%s|%d|%d|%d\n",
                    room_id, name, player_count, max_players, status);
}

int protocol_build_player_join(char *buf, int max_len, int player_id, 
                               const char *username) {
    if (buf == NULL || max_len <= 0 || username == NULL) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "PLAYER_JOIN|%d|%s\n", 
                    player_id, username);
}

int protocol_build_player_leave(char *buf, int max_len, int player_id) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "PLAYER_LEAVE|%d\n", player_id);
}

int protocol_build_game_start(char *buf, int max_len) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "GAME_START\n");
}

/**
 * 构造地图数据消息
 * 地图数据用逗号分隔每行，避免与协议分隔符冲突
 */
int protocol_build_map_data(char *buf, int max_len, 
                            const char map[20][51]) {
    if (buf == NULL || max_len <= 0 || map == NULL) {
        return -1;
    }
    
    int pos = snprintf(buf, (size_t)max_len, "MAP_DATA|");
    
    /* 将地图数据编码为单行，用逗号分隔每行 */
    for (int y = 0; y < 20 && pos < max_len - 2; y++) {
        if (y > 0) {
            buf[pos++] = ',';
        }
        for (int x = 0; x < 50 && pos < max_len - 2; x++) {
            char c = map[y][x];
            /* 跳过空字符 */
            if (c == '\0') c = ' ';
            buf[pos++] = c;
        }
    }
    
    buf[pos++] = '\n';
    buf[pos] = '\0';
    
    return pos;
}

int protocol_build_game_state(char *buf, int max_len, long timestamp,
                              const char *player_states, 
                              const char *item_states,
                              int poison_radius) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    const char *ps = player_states ? player_states : "";
    const char *is = item_states ? item_states : "";
    return snprintf(buf, (size_t)max_len, "GAME_STATE|%ld|%s|%s|%d\n",
                    timestamp, ps, is, poison_radius);
}

int protocol_build_game_event(char *buf, int max_len, const char *event_type, 
                              const char *data) {
    if (buf == NULL || max_len <= 0 || event_type == NULL) {
        return -1;
    }
    const char *d = data ? data : "";
    return snprintf(buf, (size_t)max_len, "GAME_EVENT|%s|%s\n", event_type, d);
}

int protocol_build_game_end(char *buf, int max_len, int winner_id, 
                            const char *stats) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    const char *s = stats ? stats : "";
    return snprintf(buf, (size_t)max_len, "GAME_END|%d|%s\n", winner_id, s);
}

int protocol_build_chat_msg(char *buf, int max_len, const char *sender, 
                            const char *message) {
    if (buf == NULL || max_len <= 0 || sender == NULL || message == NULL) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "CHAT_MSG|%s|%s\n", sender, message);
}

int protocol_build_kick(char *buf, int max_len, const char *reason) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    const char *r = reason ? reason : "";
    return snprintf(buf, (size_t)max_len, "KICK|%s\n", r);
}

/* ============== 客户端命令构造 ============== */

int protocol_build_login(char *buf, int max_len, const char *username, 
                         const char *password) {
    if (buf == NULL || max_len <= 0 || username == NULL || password == NULL) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "LOGIN|%s|%s\n", username, password);
}

int protocol_build_register(char *buf, int max_len, const char *username, 
                            const char *password) {
    if (buf == NULL || max_len <= 0 || username == NULL || password == NULL) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "REGISTER|%s|%s\n", username, password);
}

int protocol_build_create_room(char *buf, int max_len, const char *room_name, 
                               int max_players) {
    if (buf == NULL || max_len <= 0 || room_name == NULL) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "CREATE_ROOM|%s|%d\n", 
                    room_name, max_players);
}

int protocol_build_join_room(char *buf, int max_len, int room_id) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "JOIN_ROOM|%d\n", room_id);
}

int protocol_build_move(char *buf, int max_len, char direction) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "MOVE|%c\n", direction);
}

int protocol_build_attack(char *buf, int max_len) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "ATTACK|\n");
}

int protocol_build_use_item(char *buf, int max_len, int item_index) {
    if (buf == NULL || max_len <= 0) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "USE_ITEM|%d\n", item_index);
}

int protocol_build_chat(char *buf, int max_len, const char *message) {
    if (buf == NULL || max_len <= 0 || message == NULL) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "CHAT|%s\n", message);
}

int protocol_build_simple(char *buf, int max_len, const char *cmd) {
    if (buf == NULL || max_len <= 0 || cmd == NULL) {
        return -1;
    }
    return snprintf(buf, (size_t)max_len, "%s|\n", cmd);
}
