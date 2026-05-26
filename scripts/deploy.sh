#!/bin/bash
set -euo pipefail

# ============================================================
# PicoAide 部署脚本
# 用法: ./scripts/deploy.sh <服务器IP> [选项]
# 选项:
#   --reset       重置环境后运行 init（清空数据 + 全自动初始化）
#   --no-build    跳过编译（使用现有二进制）
# ============================================================

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SERVER_IP="${1:-}"
RESET=0
SKIP_BUILD=0

shift 2>/dev/null || true
for arg in "$@"; do
  case "$arg" in
    --reset) RESET=1 ;;
    --no-build) SKIP_BUILD=1 ;;
    --help|-h)
      echo "用法: $0 <服务器IP> [--reset] [--no-build]"
      exit 0
      ;;
  esac
done

if [ -z "$SERVER_IP" ]; then
  echo "错误: 请指定服务器 IP"
  echo "用法: $0 <服务器IP> [--reset] [--no-build]"
  exit 1
fi

echo "=========================================="
echo "  PicoAide 部署"
echo "  服务器: $SERVER_IP"
echo "  重置环境: $([ $RESET -eq 1 ] && echo '是' || echo '否')"
echo "  重新编译: $([ $SKIP_BUILD -eq 1 ] && echo '否' || echo '是')"
echo "=========================================="

# ============================================================
# 1. 编译
# ============================================================
if [ "$SKIP_BUILD" -eq 0 ]; then
  echo ""
  echo ">>> 步骤 1/4: 编译 picoaide..."

  mkdir -p "$PROJECT_DIR/bundle"

  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64) GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    *) echo "不支持的架构: $ARCH"; exit 1 ;;
  esac
  echo "    架构: $ARCH"

  echo "    编译 picoagent..."
  CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build \
    -o "$PROJECT_DIR/bundle/picoagent" ./cmd/picoagent/

  bash "$PROJECT_DIR/scripts/download-tools.sh" "$PROJECT_DIR/bundle"

  mkdir -p "$PROJECT_DIR/internal/rootfs/bundle"
  cp "$PROJECT_DIR"/bundle/picoagent "$PROJECT_DIR/internal/rootfs/bundle/"
  cp "$PROJECT_DIR"/bundle/alpine-rootfs.tar.gz "$PROJECT_DIR/internal/rootfs/bundle/"

  echo "    编译 picoaide..."
  CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build \
    -ldflags "-X github.com/picoaide/picoaide/internal/config.Version=2.0.0" \
    -o "$PROJECT_DIR/picoaide" ./cmd/picoaide/

  rm -f "$PROJECT_DIR/internal/rootfs/bundle/picoagent" "$PROJECT_DIR/internal/rootfs/bundle/alpine-rootfs.tar.gz"
  echo "    picoaide: $(ls -lh "$PROJECT_DIR/picoaide" | awk '{print $5}')"
else
  echo ""
  echo ">>> 步骤 1/4: 跳过编译，使用现有二进制"
  if [ ! -f "$PROJECT_DIR/picoaide" ]; then
    echo "错误: picoaide 二进制不存在，请先编译"
    exit 1
  fi
fi

# ============================================================
# 2. 部署二进制
# ============================================================
echo ""
echo ">>> 步骤 2/4: 部署二进制到 $SERVER_IP..."

# 停掉旧服务（systemctl 优先，防止 systemd 自动重启）
echo "    停止旧服务..."
ssh "root@$SERVER_IP" "systemctl stop picoaide 2>/dev/null; pkill -9 picoaide 2>/dev/null; sleep 1" || true
scp "$PROJECT_DIR/picoaide" "root@$SERVER_IP:/usr/sbin/picoaide"

# ============================================================
# 3. init（全自动初始化）
# ============================================================
if [ "$RESET" -eq 1 ]; then
  echo ""
  echo ">>> 步骤 3/4: 重置环境并运行 init..."

  ssh "root@$SERVER_IP" "rm -rf /data/picoaide"
  sleep 1

  # init（创建目录、安装 systemd 服务、创建超管）
  ssh "root@$SERVER_IP" "/usr/sbin/picoaide init"

  # 确保服务已启动（init 的服务安装跳过已存在的服务文件时不会 start）
  ssh "root@$SERVER_IP" "systemctl restart picoaide 2>/dev/null" || \
    ssh "root@$SERVER_IP" "setsid /usr/sbin/picoaide serve > /data/picoaide/startup.log 2>&1 < /dev/null &"
  echo "    服务启动完成"
else
  echo ""
  echo ">>> 步骤 3/4: 跳过 init（保留现有数据）"

  ssh "root@$SERVER_IP" \
    "systemctl start picoaide 2>/dev/null || setsid /usr/sbin/picoaide serve > /data/picoaide/startup.log 2>&1 < /dev/null &"
fi

# ============================================================
# 4. 验证
# ============================================================
echo ""
echo ">>> 步骤 4/4: 验证服务..."
sleep 3
HEALTH=$(ssh "root@$SERVER_IP" "curl -s http://localhost:80/api/health" 2>/dev/null || echo "失败")
VERSION=$(ssh "root@$SERVER_IP" "curl -s http://localhost:80/api/version" 2>/dev/null || echo "失败")

echo "    健康检查: $HEALTH"
echo "    版本: $VERSION"

echo ""
echo "=========================================="
echo "  部署完成"
echo "  服务器: $SERVER_IP"
echo "  健康状态: $HEALTH"
echo "=========================================="
