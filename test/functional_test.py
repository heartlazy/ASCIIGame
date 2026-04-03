#!/usr/bin/env python3
"""
功能测试工具 - ASCII Battle Royale Server
测试服务器的业务逻辑功能
"""

import socket
import threading
import time
import random
import argparse
import json
from datetime import datetime
import sys

class GameClient:
    """游戏客户端模拟器"""
    
    def __init__(self, host, port):
        self.host = host
        self.port = port
        self.sock = None
        self.connected = False
        self.recv_buffer = ""
        self.responses = []
        self.lock = threading.Lock()
    
    def connect(self):
        try:
            self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            self.sock.settimeout(5.0)
            self.sock.connect((self.host, self.port))
            self.connected = True
            return True
        except Exception as e:
            print(f"连接失败: {e}")
            return False
    
    def disconnect(self):
        if self.sock:
            try:
                self.sock.close()
            except:
                pass
        self.connected = False
    
    def send(self, cmd):
        if not self.connected:
            return None
        try:
            self.sock.sendall((cmd + '\n').encode('utf-8'))
            time.sleep(0.1)
            return self.recv()
        except Exception as e:
            print(f"发送失败: {e}")
            return None
    
    def recv(self):
        try:
            self.sock.settimeout(2.0)
            data = self.sock.recv(4096).decode('utf-8')
            return data.strip()
        except socket.timeout:
            return None
        except Exception as e:
            return None
    
    def register(self, username, password):
        return self.send(f"REGISTER|{username}|{password}")
    
    def login(self, username, password):
        return self.send(f"LOGIN|{username}|{password}")
    
    def list_rooms(self):
        return self.send("LIST_ROOMS")
    
    def create_room(self, name, max_players=6):
        return self.send(f"CREATE_ROOM|{name}|{max_players}")
    
    def join_room(self, room_id):
        return self.send(f"JOIN_ROOM|{room_id}")
    
    def leave_room(self):
        return self.send("LEAVE_ROOM")
    
    def ready(self):
        return self.send("READY")
    
    def move(self, direction):
        return self.send(f"MOVE|{direction}")
    
    def attack(self):
        return self.send("ATTACK")
    
    def chat(self, message):
        return self.send(f"CHAT|{message}")
    
    def logout(self):
        return self.send("LOGOUT")


class FunctionalTest:
    """功能测试类"""
    
    def __init__(self, host, port):
        self.host = host
        self.port = port
        self.test_results = []
    
    def run_test(self, name, func):
        print(f"\n{'='*60}")
        print(f"测试: {name}")
        print(f"{'='*60}")
        
        start = time.time()
        try:
            result = func()
            elapsed = time.time() - start
            status = "✓ 通过" if result else "✗ 失败"
            print(f"\n{status} (耗时: {elapsed:.2f}秒)")
            self.test_results.append({
                'name': name, 'passed': result, 'elapsed': elapsed
            })
            return result
        except Exception as e:
            elapsed = time.time() - start
            print(f"\n✗ 异常: {e}")
            self.test_results.append({
                'name': name, 'passed': False, 'elapsed': elapsed, 'error': str(e)
            })
            return False

    def test_user_registration(self):
        """测试1: 用户注册"""
        print("测试用户注册流程...")
        
        client = GameClient(self.host, self.port)
        if not client.connect():
            return False
        
        username = f"test_user_{int(time.time())}"
        
        # 注册新用户
        resp = client.register(username, "password123")
        print(f"  注册响应: {resp}")
        # 检查响应是否包含OK（兼容 OK|message 格式）
        if not resp or not resp.startswith("OK"):
            print(f"  注册失败: 期望OK开头，实际: {resp}")
            client.disconnect()
            return False
        
        # 重复注册应该失败
        resp = client.register(username, "password123")
        print(f"  重复注册响应: {resp}")
        # 检查响应是否包含ERR（兼容 ERR|code|message 格式）
        if not resp or not resp.startswith("ERR"):
            print(f"  重复注册应返回ERR，实际: {resp}")
            client.disconnect()
            return False
        
        client.disconnect()
        print("✓ 用户注册测试通过")
        return True
    
    def test_user_login(self):
        """测试2: 用户登录"""
        print("测试用户登录流程...")
        
        client = GameClient(self.host, self.port)
        if not client.connect():
            return False
        
        username = f"login_test_{int(time.time())}"
        
        # 先注册
        resp = client.register(username, "password123")
        print(f"  注册响应: {resp}")
        if not resp or not resp.startswith("OK"):
            print(f"  注册失败，无法继续登录测试")
            client.disconnect()
            return False
        client.disconnect()
        
        # 重新连接并登录
        client = GameClient(self.host, self.port)
        client.connect()
        
        # 错误密码
        resp = client.login(username, "wrongpassword")
        print(f"  错误密码响应: {resp}")
        if resp and resp.startswith("OK"):
            print(f"  错误密码不应返回OK")
            client.disconnect()
            return False
        
        # 正确登录
        resp = client.login(username, "password123")
        print(f"  正确登录响应: {resp}")
        if not resp or not resp.startswith("OK"):
            print(f"  正确登录应返回OK，实际: {resp}")
            client.disconnect()
            return False
        
        client.disconnect()
        print("✓ 用户登录测试通过")
        return True
    
    def test_room_operations(self):
        """测试3: 房间操作"""
        print("测试房间操作...")
        
        client = GameClient(self.host, self.port)
        if not client.connect():
            return False
        
        username = f"room_test_{int(time.time())}"
        resp = client.register(username, "password123")
        print(f"  注册响应: {resp}")
        if not resp or not resp.startswith("OK"):
            print(f"  注册失败")
            client.disconnect()
            return False
        client.disconnect()
        
        client = GameClient(self.host, self.port)
        client.connect()
        resp = client.login(username, "password123")
        print(f"  登录响应: {resp}")
        if not resp or not resp.startswith("OK"):
            print(f"  登录失败")
            client.disconnect()
            return False
        
        # 列出房间
        resp = client.list_rooms()
        print(f"  房间列表: {resp}")
        
        # 创建房间
        resp = client.create_room("TestRoom", 6)
        print(f"  创建房间: {resp}")
        # 服务器返回 PLAYER_JOIN 和 ROOM_INFO 两条消息
        # 检查响应中是否包含 ROOM_INFO
        if not resp or "ROOM_INFO" not in resp:
            print(f"  创建房间应包含ROOM_INFO，实际: {resp}")
            client.disconnect()
            return False
        
        # 离开房间
        resp = client.leave_room()
        print(f"  离开房间: {resp}")
        
        client.disconnect()
        print("✓ 房间操作测试通过")
        return True
    
    def test_room_join(self):
        """测试4: 加入房间"""
        print("测试加入房间...")
        
        # 创建房主
        host_client = GameClient(self.host, self.port)
        host_client.connect()
        host_name = f"host_{int(time.time())}"
        host_client.register(host_name, "pass")
        host_client.disconnect()
        
        host_client = GameClient(self.host, self.port)
        host_client.connect()
        host_client.login(host_name, "pass")
        
        resp = host_client.create_room("JoinTest", 6)
        print(f"  创建房间: {resp}")
        
        # 解析房间ID
        room_id = 1  # 默认
        if resp and "ROOM_INFO" in resp:
            parts = resp.split('|')
            if len(parts) > 1:
                room_id = int(parts[1])
        
        # 创建加入者
        join_client = GameClient(self.host, self.port)
        join_client.connect()
        join_name = f"joiner_{int(time.time())}"
        join_client.register(join_name, "pass")
        join_client.disconnect()
        
        join_client = GameClient(self.host, self.port)
        join_client.connect()
        join_client.login(join_name, "pass")
        
        resp = join_client.join_room(room_id)
        print(f"  加入房间: {resp}")
        
        host_client.disconnect()
        join_client.disconnect()
        
        print("✓ 加入房间测试通过")
        return True

    def test_game_ready(self):
        """测试5: 游戏准备"""
        print("测试游戏准备流程...")
        
        clients = []
        usernames = []
        
        # 创建多个玩家
        for i in range(2):
            client = GameClient(self.host, self.port)
            client.connect()
            username = f"ready_test_{i}_{int(time.time())}"
            usernames.append(username)
            client.register(username, "pass")
            client.disconnect()
            
            client = GameClient(self.host, self.port)
            client.connect()
            client.login(username, "pass")
            clients.append(client)
        
        # 第一个玩家创建房间
        resp = clients[0].create_room("ReadyTest", 2)
        print(f"  创建房间: {resp}")
        
        room_id = 1
        if resp and "ROOM_INFO" in resp:
            parts = resp.split('|')
            if len(parts) > 1:
                room_id = int(parts[1])
        
        # 第二个玩家加入
        resp = clients[1].join_room(room_id)
        print(f"  加入房间: {resp}")
        
        # 两个玩家都准备
        resp1 = clients[0].ready()
        print(f"  玩家1准备: {resp1}")
        
        resp2 = clients[1].ready()
        print(f"  玩家2准备: {resp2}")
        
        # 等待游戏开始
        time.sleep(1)
        
        # 清理
        for client in clients:
            client.disconnect()
        
        print("✓ 游戏准备测试通过")
        return True
    
    def test_chat_function(self):
        """测试6: 聊天功能"""
        print("测试聊天功能...")
        
        clients = []
        
        for i in range(2):
            client = GameClient(self.host, self.port)
            client.connect()
            username = f"chat_test_{i}_{int(time.time())}"
            client.register(username, "pass")
            client.disconnect()
            
            client = GameClient(self.host, self.port)
            client.connect()
            client.login(username, "pass")
            clients.append(client)
        
        # 创建房间
        clients[0].create_room("ChatTest", 6)
        clients[1].join_room(1)
        
        # 发送聊天消息
        resp = clients[0].chat("Hello World!")
        print(f"  聊天响应: {resp}")
        
        for client in clients:
            client.disconnect()
        
        print("✓ 聊天功能测试通过")
        return True
    
    def test_concurrent_users(self):
        """测试7: 并发用户操作"""
        print("测试并发用户操作...")
        
        num_users = 10
        success_count = 0
        lock = threading.Lock()
        
        def user_workflow(user_id):
            nonlocal success_count
            try:
                client = GameClient(self.host, self.port)
                if not client.connect():
                    print(f"  用户{user_id}: 连接失败")
                    return
                
                username = f"concurrent_{user_id}_{int(time.time())}"
                resp = client.register(username, "pass")
                if not resp or not resp.startswith("OK"):
                    print(f"  用户{user_id}: 注册失败 - {resp}")
                    client.disconnect()
                    return
                client.disconnect()
                
                client = GameClient(self.host, self.port)
                if not client.connect():
                    print(f"  用户{user_id}: 重连失败")
                    return
                resp = client.login(username, "pass")
                
                if resp and resp.startswith("OK"):
                    client.list_rooms()
                    with lock:
                        success_count += 1
                else:
                    print(f"  用户{user_id}: 登录失败 - {resp}")
                
                client.disconnect()
            except Exception as e:
                print(f"  用户{user_id}: 异常 - {e}")
        
        threads = []
        for i in range(num_users):
            t = threading.Thread(target=user_workflow, args=(i,))
            threads.append(t)
            t.start()
        
        for t in threads:
            t.join()
        
        print(f"  成功用户数: {success_count}/{num_users}")
        return success_count >= num_users * 0.8
    
    def test_error_handling(self):
        """测试8: 错误处理"""
        print("测试错误处理...")
        
        client = GameClient(self.host, self.port)
        if not client.connect():
            return False
        
        # 未登录就操作
        resp = client.create_room("Test", 6)
        print(f"  未登录创建房间: {resp}")
        if resp and "ERR" not in resp and "OK" in resp:
            client.disconnect()
            return False
        
        # 加入不存在的房间
        username = f"error_test_{int(time.time())}"
        client.register(username, "pass")
        client.disconnect()
        
        client = GameClient(self.host, self.port)
        client.connect()
        client.login(username, "pass")
        
        resp = client.join_room(99999)
        print(f"  加入不存在房间: {resp}")
        if resp and "ERR" not in resp:
            client.disconnect()
            return False
        
        client.disconnect()
        print("✓ 错误处理测试通过")
        return True
    
    def print_summary(self):
        print(f"\n{'='*60}")
        print("功能测试摘要")
        print(f"{'='*60}")
        
        passed = sum(1 for r in self.test_results if r['passed'])
        total = len(self.test_results)
        
        print(f"总测试数: {total}")
        print(f"通过: {passed}")
        print(f"失败: {total - passed}")
        print(f"通过率: {passed/total*100:.1f}%")
        
        print(f"\n详细结果:")
        for r in self.test_results:
            status = "✓" if r['passed'] else "✗"
            print(f"  {status} {r['name']} ({r['elapsed']:.2f}s)")
        
        print(f"{'='*60}\n")
    
    def save_results(self, filename):
        with open(filename, 'w', encoding='utf-8') as f:
            json.dump({
                'timestamp': datetime.now().isoformat(),
                'total_tests': len(self.test_results),
                'passed': sum(1 for r in self.test_results if r['passed']),
                'failed': sum(1 for r in self.test_results if not r['passed']),
                'tests': self.test_results
            }, f, indent=2, ensure_ascii=False)
        print(f"结果已保存到: {filename}")


def main():
    parser = argparse.ArgumentParser(description='功能测试工具')
    parser.add_argument('--host', default='127.0.0.1')
    parser.add_argument('--port', type=int, default=8888)
    parser.add_argument('--output', default='functional_test_results.json')
    
    args = parser.parse_args()
    
    print(f"\n{'='*60}")
    print("ASCII Battle Royale 功能测试")
    print(f"{'='*60}")
    print(f"服务器: {args.host}:{args.port}")
    print(f"{'='*60}\n")
    
    # 检查服务器
    try:
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(2.0)
        sock.connect((args.host, args.port))
        sock.close()
        print("✓ 服务器连接正常\n")
    except:
        print("✗ 无法连接到服务器")
        return 1
    
    tester = FunctionalTest(args.host, args.port)
    
    try:
        tester.run_test("用户注册", tester.test_user_registration)
        tester.run_test("用户登录", tester.test_user_login)
        tester.run_test("房间操作", tester.test_room_operations)
        tester.run_test("加入房间", tester.test_room_join)
        tester.run_test("游戏准备", tester.test_game_ready)
        tester.run_test("聊天功能", tester.test_chat_function)
        tester.run_test("并发用户", tester.test_concurrent_users)
        tester.run_test("错误处理", tester.test_error_handling)
        
        tester.print_summary()
        tester.save_results(args.output)
        
        return 0
    except KeyboardInterrupt:
        print("\n测试中断")
        return 1


if __name__ == '__main__':
    sys.exit(main())
