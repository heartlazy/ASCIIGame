#!/usr/bin/env python3
"""
性能监控工具 - 实时监控服务器性能指标
"""

import socket
import time
import threading
import argparse
from datetime import datetime
from collections import deque

class PerformanceMonitor:
    """性能监控器"""
    
    def __init__(self, host, port, interval=1):
        self.host = host
        self.port = port
        self.interval = interval
        self.running = False
        
        # 性能指标
        self.metrics = {
            'connection_attempts': 0,
            'successful_connections': 0,
            'failed_connections': 0,
            'response_times': deque(maxlen=100),
            'errors': 0
        }
        
        self.lock = threading.Lock()
    
    def test_connection(self):
        """测试单次连接"""
        start_time = time.time()
        
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(2.0)
            sock.connect((self.host, self.port))
            
            # 发送简单命令
            sock.sendall(b"LIST_ROOMS\n")
            
            # 接收响应
            sock.recv(1024)
            sock.close()
            
            response_time = (time.time() - start_time) * 1000  # ms
            
            with self.lock:
                self.metrics['connection_attempts'] += 1
                self.metrics['successful_connections'] += 1
                self.metrics['response_times'].append(response_time)
            
            return True, response_time
            
        except Exception as e:
            with self.lock:
                self.metrics['connection_attempts'] += 1
                self.metrics['failed_connections'] += 1
                self.metrics['errors'] += 1
            
            return False, 0
    
    def monitor_worker(self):
        """监控工作线程"""
        while self.running:
            self.test_connection()
            time.sleep(self.interval)
    
    def start(self, num_workers=5):
        """启动监控"""
        self.running = True
        
        threads = []
        for _ in range(num_workers):
            t = threading.Thread(target=self.monitor_worker)
            t.daemon = True
            t.start()
            threads.append(t)
        
        return threads
    
    def stop(self):
        """停止监控"""
        self.running = False
    
    def get_stats(self):
        """获取统计信息"""
        with self.lock:
            response_times = list(self.metrics['response_times'])
            
            if response_times:
                avg_response = sum(response_times) / len(response_times)
                min_response = min(response_times)
                max_response = max(response_times)
            else:
                avg_response = min_response = max_response = 0
            
            success_rate = 0
            if self.metrics['connection_attempts'] > 0:
                success_rate = (self.metrics['successful_connections'] / 
                               self.metrics['connection_attempts'] * 100)
            
            return {
                'total_attempts': self.metrics['connection_attempts'],
                'successful': self.metrics['successful_connections'],
                'failed': self.metrics['failed_connections'],
                'success_rate': success_rate,
                'avg_response_ms': avg_response,
                'min_response_ms': min_response,
                'max_response_ms': max_response,
                'errors': self.metrics['errors']
            }


def print_stats(stats, clear_screen=True):
    """打印统计信息"""
    if clear_screen:
        print("\033[2J\033[H", end="")  # 清屏
    
    print(f"{'='*60}")
    print(f"服务器性能监控 - {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"{'='*60}")
    print(f"连接统计:")
    print(f"  总尝试次数: {stats['total_attempts']}")
    print(f"  成功: {stats['successful']}")
    print(f"  失败: {stats['failed']}")
    print(f"  成功率: {stats['success_rate']:.2f}%")
    print(f"\n响应时间 (ms):")
    print(f"  平均: {stats['avg_response_ms']:.2f}")
    print(f"  最小: {stats['min_response_ms']:.2f}")
    print(f"  最大: {stats['max_response_ms']:.2f}")
    print(f"\n错误数: {stats['errors']}")
    print(f"{'='*60}")
    print("\n按 Ctrl+C 停止监控")

def main():
    parser = argparse.ArgumentParser(description='服务器性能监控工具')
    parser.add_argument('--host', default='127.0.0.1', help='服务器地址')
    parser.add_argument('--port', type=int, default=8888, help='服务器端口')
    parser.add_argument('--interval', type=float, default=1.0, 
                       help='测试间隔(秒)')
    parser.add_argument('--workers', type=int, default=5, 
                       help='并发工作线程数')
    parser.add_argument('--duration', type=int, default=0, 
                       help='监控持续时间(秒), 0表示持续运行')
    
    args = parser.parse_args()
    
    print(f"\n{'='*60}")
    print(f"启动性能监控")
    print(f"{'='*60}")
    print(f"服务器: {args.host}:{args.port}")
    print(f"测试间隔: {args.interval}秒")
    print(f"并发线程: {args.workers}")
    print(f"{'='*60}\n")
    
    # 检查服务器
    try:
        test_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        test_sock.settimeout(2.0)
        test_sock.connect((args.host, args.port))
        test_sock.close()
        print("✓ 服务器连接正常\n")
    except Exception as e:
        print(f"✗ 无法连接到服务器: {e}")
        return 1
    
    monitor = PerformanceMonitor(args.host, args.port, args.interval)
    monitor.start(args.workers)
    
    start_time = time.time()
    
    try:
        while True:
            time.sleep(2)
            stats = monitor.get_stats()
            print_stats(stats)
            
            # 检查是否达到持续时间
            if args.duration > 0:
                elapsed = time.time() - start_time
                if elapsed >= args.duration:
                    break
    
    except KeyboardInterrupt:
        print("\n\n监控已停止")
    
    finally:
        monitor.stop()
        
        # 打印最终统计
        print("\n最终统计:")
        stats = monitor.get_stats()
        print_stats(stats, clear_screen=False)
    
    return 0

if __name__ == '__main__':
    import sys
    sys.exit(main())
