#!/bin/bash
set -euo pipefail

BUNDLE_DIR="${1:-$(cd "$(dirname "$0")/../bundle" && pwd)}"

# 目标架构，默认等于主机架构
TARGET_ARCH="${2:-$(uname -m)}"
case "$TARGET_ARCH" in
  x86_64|amd64) ALPINE_ARCH="x86_64" ;;
  aarch64|arm64) ALPINE_ARCH="aarch64" ;;
  *) echo "不支持的架构: $TARGET_ARCH"; exit 1 ;;
esac

ALPINE_VERSION="3.21"
ALPINE_FILE="alpine-minirootfs-${ALPINE_VERSION}.0-${ALPINE_ARCH}.tar.gz"
ALPINE_URL="https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/releases/${ALPINE_ARCH}/${ALPINE_FILE}"
ALPINE_ROOT="/tmp/alpine-rootfs"
ALPINE_TGZ="$BUNDLE_DIR/alpine-rootfs.tar.gz"

mkdir -p "$BUNDLE_DIR"

echo "==> 构建 Alpine rootfs ($ALPINE_ARCH)"

if [ ! -f "$ALPINE_TGZ" ]; then
  echo "  下载 Alpine mini rootfs..."
  curl -fsSL "$ALPINE_URL" -o "$ALPINE_TGZ"
fi

if [ -f "$ALPINE_TGZ.ok" ]; then
  echo "  Alpine rootfs 已构建，跳过"
  exit 0
fi

echo "  解压..."
rm -rf "$ALPINE_ROOT"
mkdir -p "$ALPINE_ROOT"
tar -xzf "$ALPINE_TGZ" -C "$ALPINE_ROOT"

echo "  安装软件包..."
echo "nameserver 8.8.8.8" > "$ALPINE_ROOT/etc/resolv.conf"
sudo chroot "$ALPINE_ROOT" /sbin/apk update --no-cache
sudo chroot "$ALPINE_ROOT" /sbin/apk add --no-cache \
  nodejs python3 jq libstdc++ libgcc

echo "  重新打包..."
rm -f "$ALPINE_ROOT/etc/resolv.conf"
rm -f "$ALPINE_TGZ"
tar -czf "$ALPINE_TGZ" -C "$ALPINE_ROOT" .
touch "$ALPINE_TGZ.ok"
sudo rm -rf "$ALPINE_ROOT"
echo "  完成: $(ls -lh "$ALPINE_TGZ" | awk '{print $5}')"
