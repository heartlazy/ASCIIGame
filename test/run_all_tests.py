#!/usr/bin/env python3
"""
综合测试运行器 - 运行所有测试并生成报告
"""

import subprocess
import sys
import time
import json
from datetime import datetime
import os
import argparse

# 获取脚本所在目录
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.dirname(SCRIPT_DIR)

# 所有测试脚本配置（功能测试放最前，避免用户数据被其他测试填满）
TEST_SCRIPTS = [
    {
        'name': '功能测试',
        'script': 'test/functional_test.py',
        'args': [],
        'result_file': 'functional_test_results.json'
    },
    {
        'name': '健壮性测试',
        'script': 'test/robustness_test.py',
        'args': [],
        'result_file': 'robustness_test_results.json'
    },
    {
        'name': '压力测试',
        'script': 'test/stress_test.py',
        'args': ['--clients', '200', '--duration', '300'],
        'result_file': 'stress_test_results.json'
    }
]

def check_server(host='127.0.0.1', port=8888):
    """检查服务器是否运行"""
    import socket
    try:
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(2.0)
        sock.connect((host, port))
        sock.close()
        return True
    except:
        return False

def run_test(test_config):
    """运行单个测试脚本"""
    name = test_config['name']
    script = test_config['script']
    args = test_config['args']
    
    print(f"\n{'='*70}")
    print(f"运行: {name} ({script})")
    print(f"{'='*70}\n")
    
    script_path = os.path.join(PROJECT_ROOT, script)
    cmd = [sys.executable, script_path] + args
    start_time = time.time()
    
    try:
        result = subprocess.run(cmd, capture_output=False, text=True, cwd=PROJECT_ROOT)
        elapsed = time.time() - start_time
        
        return {
            'name': name,
            'script': script,
            'success': result.returncode == 0,
            'elapsed': elapsed,
            'returncode': result.returncode
        }
    except Exception as e:
        elapsed = time.time() - start_time
        print(f"错误: {e}")
        return {
            'name': name,
            'script': script,
            'success': False,
            'elapsed': elapsed,
            'error': str(e)
        }


def load_test_details(result_file):
    """加载测试详细结果"""
    filepath = os.path.join(PROJECT_ROOT, result_file)
    if os.path.exists(filepath):
        with open(filepath, 'r', encoding='utf-8') as f:
            return json.load(f)
    return None

def generate_report(results):
    """生成综合测试报告"""
    report_file = os.path.join(PROJECT_ROOT, f"test_report_{datetime.now().strftime('%Y%m%d_%H%M%S')}.md")
    
    with open(report_file, 'w', encoding='utf-8') as f:
        f.write("# ASCII Battle Royale 服务器测试报告\n\n")
        f.write(f"**测试时间**: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n\n")
        f.write("---\n\n")
        
        # 测试概览
        f.write("## 测试概览\n\n")
        total = len(results)
        passed = sum(1 for r in results if r['success'])
        total_time = sum(r['elapsed'] for r in results)
        f.write(f"- **总测试套件**: {total}\n")
        f.write(f"- **通过**: {passed}\n")
        f.write(f"- **失败**: {total - passed}\n")
        f.write(f"- **通过率**: {passed/total*100:.1f}%\n")
        f.write(f"- **总耗时**: {total_time:.1f} 秒\n\n")
        
        # 测试套件结果
        f.write("## 测试套件结果\n\n")
        f.write("| 测试套件 | 状态 | 耗时 |\n")
        f.write("|----------|------|------|\n")
        for r in results:
            status = "✅ 通过" if r['success'] else "❌ 失败"
            f.write(f"| {r['name']} | {status} | {r['elapsed']:.1f}s |\n")
        f.write("\n")
        
        # 健壮性测试详情
        f.write("---\n\n## 健壮性测试详情\n\n")
        data = load_test_details('robustness_test_results.json')
        if data:
            f.write(f"- **总测试项**: {data['total_tests']}\n")
            f.write(f"- **通过**: {data['passed']}\n")
            f.write(f"- **失败**: {data['failed']}\n\n")
            f.write("| 测试项 | 状态 | 耗时(秒) |\n")
            f.write("|--------|------|----------|\n")
            for test in data['tests']:
                status = "✅" if test['passed'] else "❌"
                f.write(f"| {test['name']} | {status} | {test['elapsed']:.2f} |\n")
            f.write("\n")
        
        # 压力测试详情
        f.write("## 压力测试详情\n\n")
        data = load_test_details('stress_test_results.json')
        if data:
            f.write("### 连接统计\n\n")
            f.write(f"- **总客户端数**: {data['total_clients']}\n")
            f.write(f"- **成功连接**: {data['successful_connections']}\n")
            f.write(f"- **失败连接**: {data['failed_connections']}\n")
            rate = data['successful_connections']/data['total_clients']*100 if data['total_clients'] > 0 else 0
            f.write(f"- **连接成功率**: {rate:.2f}%\n\n")
            
            f.write("### 消息统计\n\n")
            f.write(f"- **发送消息数**: {data['total_messages_sent']}\n")
            f.write(f"- **接收消息数**: {data['total_messages_received']}\n")
            f.write(f"- **错误数**: {data['total_errors']}\n\n")
            
            f.write("### 延迟统计 (毫秒)\n\n")
            f.write(f"| 指标 | 数值 |\n")
            f.write(f"|------|------|\n")
            f.write(f"| 平均延迟 | {data['avg_latency_ms']:.2f} ms |\n")
            f.write(f"| 最小延迟 | {data['min_latency_ms']:.2f} ms |\n")
            f.write(f"| 最大延迟 | {data['max_latency_ms']:.2f} ms |\n")
            f.write(f"| P95 延迟 | {data['p95_latency_ms']:.2f} ms |\n")
            f.write(f"| P99 延迟 | {data['p99_latency_ms']:.2f} ms |\n\n")
        
        # 功能测试详情
        f.write("## 功能测试详情\n\n")
        data = load_test_details('functional_test_results.json')
        if data:
            f.write(f"- **总测试项**: {data['total_tests']}\n")
            f.write(f"- **通过**: {data['passed']}\n")
            f.write(f"- **失败**: {data['failed']}\n\n")
            f.write("| 测试项 | 状态 | 耗时(秒) |\n")
            f.write("|--------|------|----------|\n")
            for test in data['tests']:
                status = "✅" if test['passed'] else "❌"
                f.write(f"| {test['name']} | {status} | {test['elapsed']:.2f} |\n")
            f.write("\n")
        
        # 定量结论
        f.write("---\n\n## 定量结论\n\n")
        
        # 计算总分
        score = 0
        max_score = 100
        
        rob_data = load_test_details('robustness_test_results.json')
        if rob_data and rob_data['total_tests'] > 0:
            score += (rob_data['passed'] / rob_data['total_tests']) * 35
        
        stress_data = load_test_details('stress_test_results.json')
        if stress_data and stress_data['total_clients'] > 0:
            rate = stress_data['successful_connections'] / stress_data['total_clients']
            score += rate * 20
            if stress_data['avg_latency_ms'] < 50:
                score += 15
            elif stress_data['avg_latency_ms'] < 100:
                score += 10
            else:
                score += 5
        
        func_data = load_test_details('functional_test_results.json')
        if func_data and func_data['total_tests'] > 0:
            score += (func_data['passed'] / func_data['total_tests']) * 30
        
        f.write(f"**总分: {score:.1f}/100**\n\n")
        
        if score >= 90:
            grade = "A+ (优秀) ⭐⭐⭐⭐⭐"
        elif score >= 80:
            grade = "A (良好) ⭐⭐⭐⭐"
        elif score >= 70:
            grade = "B (及格) ⭐⭐⭐"
        elif score >= 60:
            grade = "C (勉强) ⭐⭐"
        else:
            grade = "D (不及格) ⭐"
        
        f.write(f"**评级: {grade}**\n\n")
        
        # 结论
        f.write("---\n\n## 测试结论\n\n")
        if passed == total:
            f.write("✅ **所有测试通过！服务器表现优秀。**\n")
        elif passed >= total * 0.8:
            f.write("⚠️ **大部分测试通过，但仍有改进空间。**\n")
        else:
            f.write("❌ **多项测试失败，需要重点关注服务器稳定性。**\n")
    
    print(f"\n{'='*70}")
    print(f"测试报告已生成: {report_file}")
    print(f"{'='*70}\n")
    return report_file


def main():
    parser = argparse.ArgumentParser(description='ASCII Battle Royale 综合测试运行器')
    parser.add_argument('--host', default='127.0.0.1', help='服务器地址')
    parser.add_argument('--port', type=int, default=8888, help='服务器端口')
    parser.add_argument('--skip', nargs='*', choices=['robustness', 'stress', 'functional'],
                       default=[], help='跳过指定测试')
    parser.add_argument('--only', choices=['robustness', 'stress', 'functional'],
                       help='只运行指定测试')
    
    args = parser.parse_args()
    
    print(f"\n{'='*70}")
    print(f"ASCII Battle Royale 综合测试套件")
    print(f"{'='*70}")
    print(f"服务器: {args.host}:{args.port}")
    print(f"{'='*70}\n")
    
    # 检查服务器
    if not check_server(args.host, args.port):
        print("❌ 服务器未运行！")
        print("请先启动服务器: ./bin/server")
        return 1
    
    print("✅ 服务器运行正常\n")
    
    # 确定要运行的测试
    tests_to_run = []
    test_map = {
        'functional': TEST_SCRIPTS[0],
        'robustness': TEST_SCRIPTS[1],
        'stress': TEST_SCRIPTS[2]
    }
    
    if args.only:
        tests_to_run = [test_map[args.only]]
    else:
        for key, test in test_map.items():
            if key not in args.skip:
                tests_to_run.append(test)
    
    if not tests_to_run:
        print("没有要运行的测试")
        return 0
    
    print(f"将运行 {len(tests_to_run)} 个测试套件:")
    for t in tests_to_run:
        print(f"  - {t['name']}")
    print()
    
    # 运行测试
    results = []
    for i, test in enumerate(tests_to_run):
        print(f"\n[{i+1}/{len(tests_to_run)}] 开始 {test['name']}...")
        result = run_test(test)
        results.append(result)
        
        if i < len(tests_to_run) - 1:
            print("\n等待2秒后继续下一个测试...")
            time.sleep(2)
    
    # 生成报告
    report_file = generate_report(results)
    
    # 打印摘要
    print(f"\n{'='*70}")
    print("测试摘要")
    print(f"{'='*70}")
    
    total_time = 0
    for r in results:
        status = "✅" if r['success'] else "❌"
        print(f"  {status} {r['name']}: {r['elapsed']:.1f}s")
        total_time += r['elapsed']
    
    passed = sum(1 for r in results if r['success'])
    print(f"\n总计: {passed}/{len(results)} 通过, 总耗时 {total_time:.1f}s")
    print(f"详细报告: {report_file}")
    
    # 运行结果分析
    print(f"\n{'='*70}")
    print("运行结果分析...")
    print(f"{'='*70}")
    
    analyze_script = os.path.join(PROJECT_ROOT, 'test/analyze_results.py')
    if os.path.exists(analyze_script):
        subprocess.run([sys.executable, analyze_script], cwd=PROJECT_ROOT)
    
    return 0 if all(r['success'] for r in results) else 1


if __name__ == '__main__':
    sys.exit(main())
