#!/usr/bin/env python3
"""
服务器调优脚本 - 自动检测硬件并生成优化配置
仅支持Linux系统
"""

import os
import sys
import re

def check_linux():
    """检查是否为Linux系统"""
    if sys.platform != 'linux':
        print("错误: 此脚本仅支持Linux系统")
        sys.exit(1)

def get_cpu_count():
    """获取CPU核心数"""
    try:
        with open('/proc/cpuinfo', 'r') as f:
            return len([l for l in f if l.startswith('processor')])
    except:
        return os.cpu_count() or 1

def get_memory_gb():
    """获取内存大小(GB)"""
    try:
        with open('/proc/meminfo', 'r') as f:
            for line in f:
                if line.startswith('MemTotal'):
                    kb = int(re.search(r'\d+', line).group())
                    return kb / 1024 / 1024
    except:
        return 1.0

def get_file_limit():
    """获取文件描述符限制"""
    try:
        with open('/proc/self/limits', 'r') as f:
            for line in f:
                if 'Max open files' in line:
                    return int(line.split()[3])
    except:
        pass
    return 1024

def get_somaxconn():
    """获取TCP监听队列长度"""
    try:
        with open('/proc/sys/net/core/somaxconn', 'r') as f:
            return int(f.read().strip())
    except:
        return 128

def detect_config_level(cpu, mem):
    """根据硬件检测配置级别"""
    if cpu >= 4 and mem >= 4:
        return 'HIGH'
    elif cpu >= 2 and mem >= 2:
        return 'MEDIUM'
    else:
        return 'LOW'

def generate_config(level):
    """生成配置建议"""
    configs = {
        'LOW': {
            'MAX_EVENTS': 32,
            'MAX_PLAYERS': 64,
            'MAX_ROOMS': 8,
            'TICK_INTERVAL_MS': 100,
            'recommended_ulimit': 4096,
            'recommended_somaxconn': 1024
        },
        'MEDIUM': {
            'MAX_EVENTS': 64,
            'MAX_PLAYERS': 256,
            'MAX_ROOMS': 32,
            'TICK_INTERVAL_MS': 50,
            'recommended_ulimit': 16384,
            'recommended_somaxconn': 2048
        },
        'HIGH': {
            'MAX_EVENTS': 256,
            'MAX_PLAYERS': 512,
            'MAX_ROOMS': 64,
            'TICK_INTERVAL_MS': 33,
            'recommended_ulimit': 65535,
            'recommended_somaxconn': 4096
        }
    }
    return configs.get(level, configs['MEDIUM'])


def print_report(cpu, mem, file_limit, somaxconn, level, config):
    """打印调优报告"""
    print("=" * 60)
    print("服务器调优分析报告")
    print("=" * 60)
    
    print("\n【硬件检测】")
    print(f"  CPU核心数: {cpu}")
    print(f"  内存大小: {mem:.1f} GB")
    print(f"  文件描述符限制: {file_limit}")
    print(f"  TCP监听队列: {somaxconn}")
    
    print(f"\n【配置级别】: {level}")
    
    print("\n【应用配置建议】(common/config.h)")
    print(f"  #define MAX_EVENTS          {config['MAX_EVENTS']}")
    print(f"  #define MAX_PLAYERS         {config['MAX_PLAYERS']}")
    print(f"  #define MAX_ROOMS           {config['MAX_ROOMS']}")
    print(f"  #define TICK_INTERVAL_MS    {config['TICK_INTERVAL_MS']}")
    
    print("\n【系统配置建议】")
    if file_limit < config['recommended_ulimit']:
        print(f"  ⚠️  文件描述符限制过低，建议: ulimit -n {config['recommended_ulimit']}")
    else:
        print(f"  ✅ 文件描述符限制正常")
    
    if somaxconn < config['recommended_somaxconn']:
        print(f"  ⚠️  TCP监听队列过小，建议: echo {config['recommended_somaxconn']} > /proc/sys/net/core/somaxconn")
    else:
        print(f"  ✅ TCP监听队列正常")
    
    print("\n【预期性能】")
    print(f"  最大并发连接: {config['MAX_PLAYERS']}")
    print(f"  游戏刷新率: {1000 // config['TICK_INTERVAL_MS']} tick/s")
    print(f"  最大房间数: {config['MAX_ROOMS']}")
    
    print("\n" + "=" * 60)

def generate_sysctl_script(config, current_somaxconn):
    """生成系统优化脚本和恢复脚本"""
    # 只有当前值不足时才设置somaxconn
    somaxconn_cmd = ""
    if current_somaxconn < config['recommended_somaxconn']:
        somaxconn_cmd = f"echo {config['recommended_somaxconn']} > /proc/sys/net/core/somaxconn"
    else:
        somaxconn_cmd = f"# somaxconn已满足要求({current_somaxconn} >= {config['recommended_somaxconn']})"
    
    # 优化脚本
    script = f"""#!/bin/bash
# 服务器系统优化脚本
# 需要root权限运行

echo "应用系统优化..."

# 文件描述符限制
ulimit -n {config['recommended_ulimit']}

# TCP参数优化
{somaxconn_cmd}
echo 1 > /proc/sys/net/ipv4/tcp_tw_reuse
echo "1024 65535" > /proc/sys/net/ipv4/ip_local_port_range

echo "优化完成！"
echo "当前文件描述符限制: $(ulimit -n)"
echo "当前TCP监听队列: $(cat /proc/sys/net/core/somaxconn)"
"""
    
    # 恢复脚本
    restore_script = """#!/bin/bash
# 恢复系统默认配置脚本
# 需要root权限运行

echo "恢复系统默认配置..."

# 恢复TCP连接复用（默认关闭）
echo 0 > /proc/sys/net/ipv4/tcp_tw_reuse

# 恢复端口范围（默认值）
echo "32768 60999" > /proc/sys/net/ipv4/ip_local_port_range

echo "恢复完成！"
echo "注意: 文件描述符限制会在新shell会话中自动恢复默认值"
echo "注意: somaxconn未修改，如需恢复请重启系统"
echo ""
echo "当前配置:"
echo "  TCP连接复用: $(cat /proc/sys/net/ipv4/tcp_tw_reuse)"
echo "  端口范围: $(cat /proc/sys/net/ipv4/ip_local_port_range)"
"""
    
    os.makedirs('scripts', exist_ok=True)
    
    with open('scripts/apply_sysctl.sh', 'w') as f:
        f.write(script)
    os.chmod('scripts/apply_sysctl.sh', 0o755)
    
    with open('scripts/restore_sysctl.sh', 'w') as f:
        f.write(restore_script)
    os.chmod('scripts/restore_sysctl.sh', 0o755)
    
    print("已生成系统优化脚本: scripts/apply_sysctl.sh")
    print("已生成恢复脚本: scripts/restore_sysctl.sh")

def main():
    check_linux()
    
    print("\n检测服务器硬件配置...\n")
    
    cpu = get_cpu_count()
    mem = get_memory_gb()
    file_limit = get_file_limit()
    somaxconn = get_somaxconn()
    
    level = detect_config_level(cpu, mem)
    config = generate_config(level)
    
    print_report(cpu, mem, file_limit, somaxconn, level, config)
    generate_sysctl_script(config, somaxconn)
    
    print("\n下一步操作:")
    print("1. 根据建议修改 common/config.h")
    print("2. 重新编译: make clean && make server")
    print("3. 应用系统优化: sudo scripts/apply_sysctl.sh")
    print("4. 运行压力测试验证: make test-stress")

if __name__ == '__main__':
    main()
