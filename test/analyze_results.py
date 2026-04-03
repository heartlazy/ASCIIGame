#!/usr/bin/env python3
"""
测试结果分析工具 - 分析测试结果并给出改进建议
"""

import json
import sys
import os
from datetime import datetime

def load_config():
    """加载测试配置"""
    try:
        with open('test/test_config.json', 'r') as f:
            return json.load(f)
    except:
        return None

def analyze_robustness(results, config):
    """分析健壮性测试结果"""
    print("\n" + "="*60)
    print("健壮性测试分析")
    print("="*60)
    
    if not os.path.exists('robustness_test_results.json'):
        print("未找到健壮性测试结果文件")
        return
    
    with open('robustness_test_results.json', 'r') as f:
        data = json.load(f)
    
    total = data['total_tests']
    passed = data['passed']
    failed = data['failed']
    pass_rate = (passed / total * 100) if total > 0 else 0
    
    print(f"\n总体情况:")
    print(f"  测试项: {total}")
    print(f"  通过: {passed}")
    print(f"  失败: {failed}")
    print(f"  通过率: {pass_rate:.1f}%")
    
    # 评级
    if pass_rate >= 90:
        grade = "优秀 ⭐⭐⭐"
        print(f"\n评级: {grade}")
        print("服务器健壮性表现优秀，能够很好地处理异常情况。")
    elif pass_rate >= 70:
        grade = "良好 ⭐⭐"
        print(f"\n评级: {grade}")
        print("服务器健壮性良好，但仍有改进空间。")
    else:
        grade = "需改进 ⭐"
        print(f"\n评级: {grade}")
        print("服务器健壮性需要重点改进。")
    
    # 失败项分析
    if failed > 0:
        print(f"\n失败的测试项:")
        for test in data['tests']:
            if not test['passed']:
                print(f"  ❌ {test['name']}")
                if 'error' in test:
                    print(f"     错误: {test['error']}")
        
        print(f"\n改进建议:")
        for test in data['tests']:
            if not test['passed']:
                name = test['name']
                if '畸形消息' in name:
                    print("  • 加强输入验证，过滤非法字符")
                elif '连接洪水' in name:
                    print("  • 实现连接限流和速率限制")
                elif '缓冲区' in name:
                    print("  • 检查缓冲区边界，防止溢出")
                elif '竞态' in name:
                    print("  • 加强并发控制，使用互斥锁")
                elif '特殊字符' in name:
                    print("  • 实现输入清理和转义")

def analyze_stress(results, config):
    """分析压力测试结果"""
    print("\n" + "="*60)
    print("压力测试分析")
    print("="*60)
    
    if not os.path.exists('stress_test_results.json'):
        print("未找到压力测试结果文件")
        return
    
    with open('stress_test_results.json', 'r') as f:
        data = json.load(f)
    
    # 连接统计
    total_clients = data['total_clients']
    successful = data['successful_connections']
    failed = data['failed_connections']
    success_rate = (successful / total_clients * 100) if total_clients > 0 else 0
    
    print(f"\n连接性能:")
    print(f"  并发客户端: {total_clients}")
    print(f"  成功连接: {successful}")
    print(f"  失败连接: {failed}")
    print(f"  成功率: {success_rate:.2f}%")
    
    # 延迟统计
    avg_latency = data['avg_latency_ms']
    p95_latency = data['p95_latency_ms']
    p99_latency = data['p99_latency_ms']
    
    print(f"\n延迟性能:")
    print(f"  平均延迟: {avg_latency:.2f} ms")
    print(f"  P95 延迟: {p95_latency:.2f} ms")
    print(f"  P99 延迟: {p99_latency:.2f} ms")
    
    # 消息统计
    sent = data['total_messages_sent']
    received = data['total_messages_received']
    errors = data['total_errors']
    
    print(f"\n消息统计:")
    print(f"  发送: {sent}")
    print(f"  接收: {received}")
    print(f"  错误: {errors}")
    
    if sent > 0:
        error_rate = (errors / sent * 100)
        print(f"  错误率: {error_rate:.2f}%")
    
    # 性能评级
    thresholds = config.get('thresholds', {}) if config else {}
    
    issues = []
    recommendations = []
    
    # 检查成功率
    min_success_rate = thresholds.get('connection_success_rate_min', 95.0)
    if success_rate < min_success_rate:
        issues.append(f"连接成功率 ({success_rate:.1f}%) 低于阈值 ({min_success_rate}%)")
        recommendations.append("• 检查服务器资源限制 (ulimit -n)")
        recommendations.append("• 优化连接处理逻辑")
        recommendations.append("• 考虑使用连接池")
    
    # 检查延迟
    max_avg_latency = thresholds.get('avg_latency_ms_max', 50.0)
    if avg_latency > max_avg_latency:
        issues.append(f"平均延迟 ({avg_latency:.1f}ms) 高于阈值 ({max_avg_latency}ms)")
        recommendations.append("• 优化消息处理逻辑")
        recommendations.append("• 减少不必要的系统调用")
        recommendations.append("• 考虑使用零拷贝技术")
    
    max_p99_latency = thresholds.get('p99_latency_ms_max', 200.0)
    if p99_latency > max_p99_latency:
        issues.append(f"P99延迟 ({p99_latency:.1f}ms) 高于阈值 ({max_p99_latency}ms)")
        recommendations.append("• 检查是否有阻塞操作")
        recommendations.append("• 优化锁的使用")
        recommendations.append("• 考虑异步I/O")
    
    # 检查错误率
    if sent > 0:
        error_rate = (errors / sent * 100)
        max_error_rate = thresholds.get('error_rate_max', 5.0)
        if error_rate > max_error_rate:
            issues.append(f"错误率 ({error_rate:.1f}%) 高于阈值 ({max_error_rate}%)")
            recommendations.append("• 加强错误处理")
            recommendations.append("• 增加日志记录")
            recommendations.append("• 实现重试机制")
    
    # 性能评级
    if not issues:
        grade = "优秀 ⭐⭐⭐"
        print(f"\n评级: {grade}")
        print("服务器性能表现优秀，满足所有性能指标。")
    elif len(issues) <= 2:
        grade = "良好 ⭐⭐"
        print(f"\n评级: {grade}")
        print("服务器性能良好，但有少量指标需要优化。")
    else:
        grade = "需改进 ⭐"
        print(f"\n评级: {grade}")
        print("服务器性能需要重点改进。")
    
    if issues:
        print(f"\n发现的问题:")
        for issue in issues:
            print(f"  ⚠️  {issue}")
    
    if recommendations:
        print(f"\n改进建议:")
        for rec in recommendations:
            print(f"  {rec}")

def generate_summary(config):
    """生成综合摘要"""
    print("\n" + "="*60)
    print("综合评估")
    print("="*60)
    
    # 计算总体得分
    robustness_score = 0
    stress_score = 0
    
    if os.path.exists('robustness_test_results.json'):
        with open('robustness_test_results.json', 'r') as f:
            data = json.load(f)
            if data['total_tests'] > 0:
                robustness_score = (data['passed'] / data['total_tests']) * 50
    
    if os.path.exists('stress_test_results.json'):
        with open('stress_test_results.json', 'r') as f:
            data = json.load(f)
            
            # 连接成功率 (20分)
            if data['total_clients'] > 0:
                success_rate = data['successful_connections'] / data['total_clients']
                stress_score += success_rate * 20
            
            # 延迟性能 (20分)
            avg_latency = data['avg_latency_ms']
            if avg_latency < 20:
                stress_score += 20
            elif avg_latency < 50:
                stress_score += 15
            elif avg_latency < 100:
                stress_score += 10
            else:
                stress_score += 5
            
            # 错误率 (10分)
            if data['total_messages_sent'] > 0:
                error_rate = data['total_errors'] / data['total_messages_sent']
                if error_rate < 0.01:
                    stress_score += 10
                elif error_rate < 0.05:
                    stress_score += 7
                elif error_rate < 0.1:
                    stress_score += 4
    
    total_score = robustness_score + stress_score
    
    print(f"\n总体得分: {total_score:.1f}/100")
    print(f"  健壮性得分: {robustness_score:.1f}/50")
    print(f"  压力测试得分: {stress_score:.1f}/50")
    
    if total_score >= 90:
        print(f"\n总体评价: 优秀 ⭐⭐⭐⭐⭐")
        print("服务器质量优秀，可以投入生产使用。")
    elif total_score >= 75:
        print(f"\n总体评价: 良好 ⭐⭐⭐⭐")
        print("服务器质量良好，建议修复已知问题后投入使用。")
    elif total_score >= 60:
        print(f"\n总体评价: 及格 ⭐⭐⭐")
        print("服务器基本可用，但需要进行优化。")
    else:
        print(f"\n总体评价: 需改进 ⭐⭐")
        print("服务器存在较多问题，建议重点改进后再投入使用。")
    
    print(f"\n关键指标对比:")
    print(f"{'指标':<20} {'当前值':<15} {'推荐值':<15} {'状态'}")
    print("-" * 60)
    
    if os.path.exists('stress_test_results.json'):
        with open('stress_test_results.json', 'r') as f:
            data = json.load(f)
            
            # 连接成功率
            if data['total_clients'] > 0:
                success_rate = data['successful_connections'] / data['total_clients'] * 100
                status = "✅" if success_rate >= 95 else "⚠️"
                print(f"{'连接成功率':<20} {success_rate:>6.1f}%{'':<8} {'>95%':<15} {status}")
            
            # 平均延迟
            avg_latency = data['avg_latency_ms']
            status = "✅" if avg_latency < 50 else "⚠️"
            print(f"{'平均延迟':<20} {avg_latency:>6.1f}ms{'':<7} {'<50ms':<15} {status}")
            
            # P99延迟
            p99_latency = data['p99_latency_ms']
            status = "✅" if p99_latency < 200 else "⚠️"
            print(f"{'P99延迟':<20} {p99_latency:>6.1f}ms{'':<7} {'<200ms':<15} {status}")
            
            # 错误率
            if data['total_messages_sent'] > 0:
                error_rate = data['total_errors'] / data['total_messages_sent'] * 100
                status = "✅" if error_rate < 5 else "⚠️"
                print(f"{'错误率':<20} {error_rate:>6.2f}%{'':<8} {'<5%':<15} {status}")

def main():
    print("\n" + "="*60)
    print("测试结果分析报告")
    print("="*60)
    print(f"生成时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    
    # 加载配置
    config = load_config()
    
    # 分析健壮性测试
    analyze_robustness(None, config)
    
    # 分析压力测试
    analyze_stress(None, config)
    
    # 生成综合摘要
    generate_summary(config)
    
    print("\n" + "="*60)
    print("分析完成")
    print("="*60 + "\n")

if __name__ == '__main__':
    sys.exit(main())
