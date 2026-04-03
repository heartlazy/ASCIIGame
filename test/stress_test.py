#!/usr/bin/env python3
"""
压力测试工具 - ASCII Battle Royale Server
测试服务器在高负载下的性能和稳定性
"""

import socket
import threading
import time
import random
import argparse
import json
from datetime import datetime
from collections import defaultdict
import sys

class TestClient:
    """模拟客户端"""
    
    def __init__(self, client_id, host, port):
        self.client_id = client_id
        self.host = host
        self.port = port
        self.sock = None
        self.connected = False
        self.username = f"test_user_{client_id}"
        self.password = "test123"
        self.recv_buffer = ""
        
        # 统计数据
        self.stats = {
            'sent': 0,
            'received': 0,
            'errors': 0,
            'latencies': []
        }
    
    def connect(self):
        """连接到服务器"""
        try:
            self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            self.sock.settimeout(5.0)
            self.sock.connect((self.host, self.port))
            self.connected = True
            return True
        except Exception as e:
            self.stats['errors'] += 1
            return False
    
    def disconnect(self):
        """断开连接"""
        if self.sock:
            try:
                self.sock.close()
            except:
                pass
        self.connected = False
    
    def send_command(self, cmd):
        """发送命令"""
        if not self.connected:
            return False
        
        try:
            start_time = time.time()
            message = cmd + '\n'
            self.sock.sendall(message.encode('utf-8'))
            self.stats['sent'] += 1
            
            # 尝试接收响应
            self.sock.settimeout(1.0)
            data = self.sock.recv(4096)
            if data:
                self.stats['received'] += 1
                latency = (time.time() - start_time) * 1000  # ms
                self.stats['latencies'].append(latency)
            
            return True
        except socket.timeout:
            return True  # 超时不算错误，服务器可能还在处理
        except Exception as e:
            self.stats['errors'] += 1
            self.connected = False
            return False
    
    def register_and_login(self):
        """注册并登录"""
        # 先尝试注册
        self.send_command(f"REGISTER|{self.username}|{self.password}")
        time.sleep(0.1)
        # 登录
        return self.send_command(f"LOGIN|{self.username}|{self.password}")
    
    def simulate_gameplay(self, duration):
        """模拟游戏行为"""
        end_time = time.time() + duration
        
        while time.time() < end_time and self.connected:
            # 随机执行操作
            action = random.choice([
                'LIST_ROOMS',
                'CREATE_ROOM|TestRoom|6',
                'MOVE|U',
                'MOVE|D',
                'MOVE|L',
                'MOVE|R',
                'ATTACK',
                'CHAT|Hello'
            ])
            
            self.send_command(action)
            time.sleep(random.uniform(0.02, 0.1))  # 20-100ms间隔，更激进


class StressTest:
    """压力测试主类"""
    
    def __init__(self, host, port):
        self.host = host
        self.port = port
        self.clients = []
        self.lock = threading.Lock()  # 线程安全锁
        self.results = {
            'start_time': None,
            'end_time': None,
            'total_clients': 0,
            'successful_connections': 0,
            'failed_connections': 0,
            'total_messages_sent': 0,
            'total_messages_received': 0,
            'total_errors': 0,
            'avg_latency_ms': 0,
            'min_latency_ms': 0,
            'max_latency_ms': 0,
            'p95_latency_ms': 0,
            'p99_latency_ms': 0
        }
    
    def test_concurrent_connections(self, num_clients, duration):
        """测试并发连接"""
        print(f"\n{'='*60}")
        print(f"测试1: 并发连接测试")
        print(f"{'='*60}")
        print(f"目标: {num_clients} 个并发客户端")
        print(f"持续时间: {duration} 秒")
        print(f"{'='*60}\n")
        
        self.results['start_time'] = datetime.now().isoformat()
        self.results['total_clients'] = num_clients
        
        # 创建客户端
        threads = []
        for i in range(num_clients):
            client = TestClient(i, self.host, self.port)
            self.clients.append(client)
            
            thread = threading.Thread(target=self._client_worker, args=(client, duration))
            threads.append(thread)
            thread.start()
            
            # 避免同时连接过多
            if i % 20 == 0:
                time.sleep(0.05)
        
        # 等待所有线程完成
        for thread in threads:
            thread.join()
        
        self.results['end_time'] = datetime.now().isoformat()
        self._calculate_results()
    
    def _client_worker(self, client, duration):
        """客户端工作线程"""
        # 连接
        if client.connect():
            with self.lock:
                self.results['successful_connections'] += 1
            
            # 注册登录
            if client.register_and_login():
                # 模拟游戏行为
                client.simulate_gameplay(duration)
        else:
            with self.lock:
                self.results['failed_connections'] += 1
        
        # 断开连接
        client.disconnect()
    
    def test_rapid_connections(self, num_connections, interval_ms):
        """测试快速连接/断开"""
        print(f"\n{'='*60}")
        print(f"测试2: 快速连接/断开测试")
        print(f"{'='*60}")
        print(f"连接次数: {num_connections}")
        print(f"连接间隔: {interval_ms} ms")
        print(f"{'='*60}\n")
        
        success = 0
        failed = 0
        
        for i in range(num_connections):
            client = TestClient(i, self.host, self.port)
            
            if client.connect():
                success += 1
                client.send_command(f"REGISTER|rapid_test_{i}|test123")
                time.sleep(0.01)
                client.disconnect()
            else:
                failed += 1
            
            time.sleep(interval_ms / 1000.0)
            
            if (i + 1) % 100 == 0:
                print(f"进度: {i+1}/{num_connections} - 成功: {success}, 失败: {failed}")
        
        print(f"\n结果: 成功 {success}/{num_connections}, 失败 {failed}/{num_connections}")
        print(f"成功率: {success/num_connections*100:.2f}%")
    
    def test_message_flood(self, num_clients, messages_per_client):
        """测试消息洪水"""
        print(f"\n{'='*60}")
        print(f"测试3: 消息洪水测试")
        print(f"{'='*60}")
        print(f"客户端数: {num_clients}")
        print(f"每客户端消息数: {messages_per_client}")
        print(f"总消息数: {num_clients * messages_per_client}")
        print(f"{'='*60}\n")
        
        clients = []
        
        # 连接所有客户端
        print("连接客户端...")
        for i in range(num_clients):
            client = TestClient(i, self.host, self.port)
            if client.connect():
                client.register_and_login()
                clients.append(client)
        
        print(f"已连接 {len(clients)} 个客户端")
        
        # 发送消息洪水
        print("发送消息...")
        start_time = time.time()
        
        for _ in range(messages_per_client):
            for client in clients:
                client.send_command("LIST_ROOMS")
        
        elapsed = time.time() - start_time
        
        # 断开所有客户端
        for client in clients:
            client.disconnect()
        
        total_messages = len(clients) * messages_per_client
        print(f"\n发送 {total_messages} 条消息耗时: {elapsed:.2f} 秒")
        print(f"吞吐量: {total_messages/elapsed:.2f} 消息/秒")
    
    def _calculate_results(self):
        """计算统计结果"""
        # 汇总所有客户端的统计数据
        all_latencies = []
        
        for client in self.clients:
            self.results['total_messages_sent'] += client.stats['sent']
            self.results['total_messages_received'] += client.stats['received']
            self.results['total_errors'] += client.stats['errors']
            all_latencies.extend(client.stats['latencies'])
        
        # 计算延迟统计
        if all_latencies:
            all_latencies.sort()
            self.results['avg_latency_ms'] = sum(all_latencies) / len(all_latencies)
            self.results['min_latency_ms'] = all_latencies[0]
            self.results['max_latency_ms'] = all_latencies[-1]
            # 安全计算百分位数
            p95_idx = min(int(len(all_latencies) * 0.95), len(all_latencies) - 1)
            p99_idx = min(int(len(all_latencies) * 0.99), len(all_latencies) - 1)
            self.results['p95_latency_ms'] = all_latencies[p95_idx]
            self.results['p99_latency_ms'] = all_latencies[p99_idx]
    
    def print_results(self):
        """打印测试结果"""
        print(f"\n{'='*60}")
        print(f"压力测试结果")
        print(f"{'='*60}")
        print(f"开始时间: {self.results['start_time']}")
        print(f"结束时间: {self.results['end_time']}")
        print(f"\n连接统计:")
        print(f"  总客户端数: {self.results['total_clients']}")
        print(f"  成功连接: {self.results['successful_connections']}")
        print(f"  失败连接: {self.results['failed_connections']}")
        print(f"  连接成功率: {self.results['successful_connections']/self.results['total_clients']*100:.2f}%")
        print(f"\n消息统计:")
        print(f"  发送消息数: {self.results['total_messages_sent']}")
        print(f"  接收消息数: {self.results['total_messages_received']}")
        print(f"  错误数: {self.results['total_errors']}")
        print(f"\n延迟统计 (ms):")
        print(f"  平均延迟: {self.results['avg_latency_ms']:.2f}")
        print(f"  最小延迟: {self.results['min_latency_ms']:.2f}")
        print(f"  最大延迟: {self.results['max_latency_ms']:.2f}")
        print(f"  P95 延迟: {self.results['p95_latency_ms']:.2f}")
        print(f"  P99 延迟: {self.results['p99_latency_ms']:.2f}")
        print(f"{'='*60}\n")
    
    def save_results(self, filename):
        """保存结果到JSON文件"""
        with open(filename, 'w', encoding='utf-8') as f:
            json.dump(self.results, f, indent=2, ensure_ascii=False)
        print(f"结果已保存到: {filename}")


def main():
    parser = argparse.ArgumentParser(description='ASCII Battle Royale 压力测试工具')
    parser.add_argument('--host', default='127.0.0.1', help='服务器地址')
    parser.add_argument('--port', type=int, default=8888, help='服务器端口')
    parser.add_argument('--test', choices=['all', 'concurrent', 'rapid', 'flood'], 
                       default='all', help='测试类型')
    parser.add_argument('--clients', type=int, default=200, help='并发客户端数')
    parser.add_argument('--duration', type=int, default=300, help='测试持续时间(秒)')
    parser.add_argument('--output', default='stress_test_results.json', help='结果输出文件')
    
    args = parser.parse_args()
    
    print(f"\n{'='*60}")
    print(f"ASCII Battle Royale 服务器压力测试")
    print(f"{'='*60}")
    print(f"服务器: {args.host}:{args.port}")
    print(f"测试类型: {args.test}")
    print(f"{'='*60}\n")
    
    # 检查服务器是否可达
    try:
        test_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        test_sock.settimeout(2.0)
        test_sock.connect((args.host, args.port))
        test_sock.close()
        print("✓ 服务器连接正常\n")
    except Exception as e:
        print(f"✗ 无法连接到服务器: {e}")
        print("请确保服务器正在运行")
        return 1
    
    tester = StressTest(args.host, args.port)
    
    try:
        if args.test in ['all', 'concurrent']:
            tester.test_concurrent_connections(args.clients, args.duration)
            tester.print_results()
            tester.save_results(args.output)
        
        if args.test in ['all', 'rapid']:
            tester.test_rapid_connections(1000, 5)
        
        if args.test in ['all', 'flood']:
            tester.test_message_flood(50, 200)
        
        print("\n✓ 所有测试完成")
        return 0
        
    except KeyboardInterrupt:
        print("\n\n测试被用户中断")
        return 1
    except Exception as e:
        print(f"\n✗ 测试出错: {e}")
        import traceback
        traceback.print_exc()
        return 1


if __name__ == '__main__':
    sys.exit(main())
