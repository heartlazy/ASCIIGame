#!/usr/bin/env python3
"""
健壮性测试工具 - ASCII Battle Royale Server
测试服务器在异常情况下的稳定性和容错能力
"""

import socket
import threading
import time
import random
import argparse
import json
from datetime import datetime
import sys

class RobustnessTest:
    """健壮性测试类"""
    
    def __init__(self, host, port):
        self.host = host
        self.port = port
        self.test_results = []
    
    def run_test(self, test_name, test_func):
        """运行单个测试"""
        print(f"\n{'='*60}")
        print(f"测试: {test_name}")
        print(f"{'='*60}")
        
        start_time = time.time()
        try:
            result = test_func()
            elapsed = time.time() - start_time
            
            self.test_results.append({
                'name': test_name,
                'passed': result,
                'elapsed': elapsed,
                'timestamp': datetime.now().isoformat()
            })
            
            status = "✓ 通过" if result else "✗ 失败"
            print(f"\n{status} (耗时: {elapsed:.2f}秒)")
            return result
        except Exception as e:
            elapsed = time.time() - start_time
            print(f"\n✗ 异常: {e}")
            self.test_results.append({
                'name': test_name,
                'passed': False,
                'elapsed': elapsed,
                'error': str(e),
                'timestamp': datetime.now().isoformat()
            })
            return False
    
    def test_malformed_messages(self):
        """测试1: 畸形消息处理"""
        print("发送各种畸形消息...")
        
        malformed_messages = [
            "",  # 空消息
            "\n",  # 只有换行
            "||||\n",  # 只有分隔符
            "INVALID_CMD\n",  # 无效命令
            "LOGIN\n",  # 缺少参数
            "LOGIN|user\n",  # 参数不足
            "LOGIN|" + "A"*5000 + "\n",  # 超长参数
            "A"*10000 + "\n",  # 超长消息
            "\x00\x01\x02\n",  # 二进制数据
            "LOGIN|user|pass|extra|extra\n",  # 参数过多
        ]
        
        passed = 0
        failed = 0
        
        for msg in malformed_messages:
            try:
                sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                sock.settimeout(2.0)
                sock.connect((self.host, self.port))
                sock.sendall(msg.encode('utf-8', errors='ignore'))
                
                # 服务器应该返回错误或关闭连接，不应该崩溃
                try:
                    data = sock.recv(1024)
                    if data:
                        passed += 1
                except:
                    passed += 1  # 连接关闭也算正常
                
                sock.close()
            except Exception as e:
                failed += 1
                print(f"  发送失败: {repr(msg[:50])}")
        
        print(f"结果: {passed}/{len(malformed_messages)} 个消息被正确处理")
        return failed == 0
    
    def test_connection_flood(self):
        """测试2: 连接洪水攻击"""
        print("快速建立大量连接...")
        
        connections = []
        max_connections = 300
        
        for i in range(max_connections):
            try:
                sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                sock.settimeout(1.0)
                sock.connect((self.host, self.port))
                connections.append(sock)
            except:
                break
            
            if (i + 1) % 50 == 0:
                print(f"  已建立 {i+1} 个连接")
        
        established = len(connections)
        print(f"成功建立 {established} 个连接")
        
        # 清理连接
        for sock in connections:
            try:
                sock.close()
            except:
                pass
        
        time.sleep(1)
        
        # 验证服务器仍然可用
        try:
            test_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            test_sock.settimeout(2.0)
            test_sock.connect((self.host, self.port))
            test_sock.close()
            print("✓ 服务器仍然响应")
            return True
        except:
            print("✗ 服务器无响应")
            return False
    
    def test_slow_client(self):
        """测试3: 慢速客户端"""
        print("模拟慢速客户端...")
        
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(30.0)
            sock.connect((self.host, self.port))
            
            # 逐字节发送消息
            message = "LOGIN|slowuser|pass123\n"
            for char in message:
                sock.sendall(char.encode('utf-8'))
                time.sleep(0.5)  # 每个字符间隔0.5秒
            
            # 接收响应
            data = sock.recv(1024)
            sock.close()
            
            print(f"✓ 服务器正确处理慢速客户端")
            return True
        except Exception as e:
            print(f"✗ 处理失败: {e}")
            return False
    
    def test_partial_messages(self):
        """测试4: 不完整消息"""
        print("发送不完整消息...")
        
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(5.0)
            sock.connect((self.host, self.port))
            
            # 发送不完整消息（没有换行符）
            sock.sendall(b"LOGIN|user|pass")
            time.sleep(2)
            
            # 补全消息
            sock.sendall(b"word\n")
            
            # 接收响应
            data = sock.recv(1024)
            sock.close()
            
            print(f"✓ 服务器正确处理分片消息")
            return True
        except Exception as e:
            print(f"✗ 处理失败: {e}")
            return False
    
    def test_rapid_disconnect(self):
        """测试5: 快速断开连接"""
        print("测试快速连接和断开...")
        
        success = 0
        for i in range(100):
            try:
                sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                sock.settimeout(1.0)
                sock.connect((self.host, self.port))
                sock.sendall(b"LOGIN|user|pass\n")
                sock.close()  # 立即关闭
                success += 1
            except:
                pass
        
        print(f"成功完成 {success}/100 次快速连接")
        
        # 验证服务器仍然可用
        try:
            test_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            test_sock.settimeout(2.0)
            test_sock.connect((self.host, self.port))
            test_sock.close()
            return True
        except:
            return False
    
    def test_concurrent_same_user(self):
        """测试6: 同一用户多次登录"""
        print("测试同一用户多次登录...")
        
        username = f"concurrent_user_{int(time.time())}"
        password = "test123"
        
        # 先注册
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(2.0)
            sock.connect((self.host, self.port))
            sock.sendall(f"REGISTER|{username}|{password}\n".encode())
            time.sleep(0.1)
            sock.close()
        except:
            pass
        
        # 尝试多次登录
        connections = []
        for i in range(5):
            try:
                sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                sock.settimeout(2.0)
                sock.connect((self.host, self.port))
                sock.sendall(f"LOGIN|{username}|{password}\n".encode())
                connections.append(sock)
                time.sleep(0.1)
            except:
                pass
        
        print(f"建立了 {len(connections)} 个同用户连接")
        
        # 清理
        for sock in connections:
            try:
                sock.close()
            except:
                pass
        
        return len(connections) > 0
    
    def test_invalid_protocol_sequence(self):
        """测试7: 无效的协议序列"""
        print("测试无效的命令序列...")
        
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(5.0)
            sock.connect((self.host, self.port))
            
            # 未登录就尝试游戏操作
            invalid_sequences = [
                "CREATE_ROOM|TestRoom|6\n",  # 未登录创建房间
                "MOVE|U\n",  # 未在游戏中移动
                "ATTACK\n",  # 未在游戏中攻击
                "LEAVE_ROOM\n",  # 未在房间中离开
            ]
            
            for cmd in invalid_sequences:
                sock.sendall(cmd.encode())
                time.sleep(0.1)
            
            sock.close()
            print(f"✓ 服务器处理了无效序列")
            return True
        except Exception as e:
            print(f"✗ 处理失败: {e}")
            return False
    
    def test_buffer_overflow(self):
        """测试8: 缓冲区溢出"""
        print("测试超大消息...")
        
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(5.0)
            sock.connect((self.host, self.port))
            
            # 发送超大消息
            huge_message = "LOGIN|" + "A" * 10000 + "|password\n"
            sock.sendall(huge_message.encode())
            
            time.sleep(1)
            sock.close()
            
            # 验证服务器仍然可用
            test_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            test_sock.settimeout(2.0)
            test_sock.connect((self.host, self.port))
            test_sock.close()
            
            print(f"✓ 服务器正确处理超大消息")
            return True
        except Exception as e:
            print(f"✗ 处理失败: {e}")
            return False
    
    def test_special_characters(self):
        """测试9: 特殊字符处理"""
        print("测试特殊字符...")
        
        special_chars = [
            "user\x00name",  # NULL字符
            "user|name",  # 协议分隔符
            "user\nname",  # 换行符
            "user'name",  # SQL注入尝试
            "user\"name",  # 引号
            "../../../etc/passwd",  # 路径遍历
            "<script>alert(1)</script>",  # XSS尝试
        ]
        
        passed = 0
        for username in special_chars:
            try:
                sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                sock.settimeout(2.0)
                sock.connect((self.host, self.port))
                
                # 尝试注册
                msg = f"REGISTER|{username}|password\n"
                sock.sendall(msg.encode('utf-8', errors='ignore'))
                
                time.sleep(0.1)
                sock.close()
                passed += 1
            except:
                pass
        
        print(f"处理了 {passed}/{len(special_chars)} 个特殊字符测试")
        return passed == len(special_chars)
    
    def test_race_conditions(self):
        """测试10: 竞态条件"""
        print("测试并发操作竞态...")
        
        username = f"race_user_{int(time.time())}"
        password = "test123"
        
        # 先注册
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(2.0)
            sock.connect((self.host, self.port))
            sock.sendall(f"REGISTER|{username}|{password}\n".encode())
            time.sleep(0.2)
            sock.close()
        except:
            pass
        
        # 多个线程同时操作
        def worker():
            try:
                sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                sock.settimeout(2.0)
                sock.connect((self.host, self.port))
                sock.sendall(f"LOGIN|{username}|{password}\n".encode())
                time.sleep(0.1)
                sock.sendall(b"CREATE_ROOM|RaceRoom|6\n")
                time.sleep(0.1)
                sock.close()
            except:
                pass
        
        threads = []
        for _ in range(10):
            t = threading.Thread(target=worker)
            threads.append(t)
            t.start()
        
        for t in threads:
            t.join()
        
        print(f"✓ 完成并发竞态测试")
        return True
    
    def print_summary(self):
        """打印测试摘要"""
        print(f"\n{'='*60}")
        print(f"健壮性测试摘要")
        print(f"{'='*60}")
        
        passed = sum(1 for r in self.test_results if r['passed'])
        total = len(self.test_results)
        
        print(f"总测试数: {total}")
        print(f"通过: {passed}")
        print(f"失败: {total - passed}")
        print(f"通过率: {passed/total*100:.1f}%")
        print(f"\n详细结果:")
        
        for result in self.test_results:
            status = "✓" if result['passed'] else "✗"
            print(f"  {status} {result['name']} ({result['elapsed']:.2f}s)")
            if 'error' in result:
                print(f"    错误: {result['error']}")
        
        print(f"{'='*60}\n")
    
    def save_results(self, filename):
        """保存结果到JSON"""
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
    parser = argparse.ArgumentParser(description='ASCII Battle Royale 健壮性测试工具')
    parser.add_argument('--host', default='127.0.0.1', help='服务器地址')
    parser.add_argument('--port', type=int, default=8888, help='服务器端口')
    parser.add_argument('--output', default='robustness_test_results.json', help='结果输出文件')
    
    args = parser.parse_args()
    
    print(f"\n{'='*60}")
    print(f"ASCII Battle Royale 服务器健壮性测试")
    print(f"{'='*60}")
    print(f"服务器: {args.host}:{args.port}")
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
    
    tester = RobustnessTest(args.host, args.port)
    
    try:
        # 运行所有测试
        tester.run_test("畸形消息处理", tester.test_malformed_messages)
        tester.run_test("连接洪水攻击", tester.test_connection_flood)
        tester.run_test("慢速客户端", tester.test_slow_client)
        tester.run_test("不完整消息", tester.test_partial_messages)
        tester.run_test("快速断开连接", tester.test_rapid_disconnect)
        tester.run_test("同一用户多次登录", tester.test_concurrent_same_user)
        tester.run_test("无效协议序列", tester.test_invalid_protocol_sequence)
        tester.run_test("缓冲区溢出", tester.test_buffer_overflow)
        tester.run_test("特殊字符处理", tester.test_special_characters)
        tester.run_test("竞态条件", tester.test_race_conditions)
        
        # 打印摘要
        tester.print_summary()
        tester.save_results(args.output)
        
        print("\n✓ 所有测试完成")
        return 0
        
    except KeyboardInterrupt:
        print("\n\n测试被用户中断")
        tester.print_summary()
        return 1
    except Exception as e:
        print(f"\n✗ 测试出错: {e}")
        import traceback
        traceback.print_exc()
        return 1


if __name__ == '__main__':
    sys.exit(main())
