#!/bin/bash
set -euo pipefail

BUNDLE_DIR="$(cd "$(dirname "$0")/../bundle" && pwd)"
ROOTFS_DIR="${1:-/data/picoaide/rootfs}"

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64)  echo "x86_64" ;;
    aarch64|arm64) echo "aarch64" ;;
    *) echo "不支持的架构: $arch"; exit 1 ;;
  esac
}
ARCH="$(detect_arch)"

echo "==> 创建 rootfs: $ROOTFS_DIR"
mkdir -p "$ROOTFS_DIR"/{bin,dev,tmp}

echo "==> 从 bundle/ 复制二进制"
for bin in picoagent busybox jq curl bun; do
  src="$BUNDLE_DIR/$bin"
  if [ -f "${src}.${ARCH}" ]; then
    src="${src}.${ARCH}"
  elif [ -f "$src" ]; then
    :
  else
    echo "  [跳过] $bin 未找到: $src"
    continue
  fi
  cp "$src" "$ROOTFS_DIR/bin/$bin"
  chmod +x "$ROOTFS_DIR/bin/$bin"
  echo "  $bin ($(ls -lh "$src" | awk '{print $5}'))"
done

# busybox symlink (包含 sh,ls,cp,mv,cat,wget,unzip,curl 等)
if [ -f "$ROOTFS_DIR/bin/busybox" ]; then
  for applet in sh ls cat cp mv rm mkdir ln chmod chown echo grep head tail cut tr sort uniq wc wget unzip; do
    ln -sf busybox "$ROOTFS_DIR/bin/$applet" 2>/dev/null || true
  done
fi

# 如果没单独下载 curl，用 busybox 的
if [ ! -f "$ROOTFS_DIR/bin/curl" ] && [ -f "$ROOTFS_DIR/bin/busybox" ]; then
  ln -sf busybox "$ROOTFS_DIR/bin/curl" 2>/dev/null || true
fi

echo "==> 创建 /dev 节点"
for node in null:1:3 zero:1:5 random:1:8 urandom:1:9; do
  IFS=: read -r name major minor <<< "$node"
  mknod "$ROOTFS_DIR/dev/$name" c "$major" "$minor" 2>/dev/null || true
done

echo "==> /etc/resolv.conf"
mkdir -p "$ROOTFS_DIR/etc"
cat > "$ROOTFS_DIR/etc/resolv.conf" << 'EOF'
nameserver 8.8.8.8
nameserver 1.1.1.1
EOF

echo ""
echo "  完成! rootfs: $ROOTFS_DIR ($ARCH)"
du -sh "$ROOTFS_DIR"
ls -lh "$ROOTFS_DIR/bin/" 2>/dev/null
