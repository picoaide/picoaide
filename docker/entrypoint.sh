#!/bin/bash

# 为所有shell会话加载NVM
export NVM_DIR="/root/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"

# 为zsh添加NVM配置
if [ -f ~/.zshrc ] && ! grep -q "NVM_DIR" ~/.zshrc; then
    echo '' >> ~/.zshrc
    echo 'export NVM_DIR="$HOME/.nvm"' >> ~/.zshrc
    echo '[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"' >> ~/.zshrc
fi

# 检查 /root 目录是否为空或只包含挂载的目录
# 如果是，从备份目录复制所有文件
file_count=$(ls -la /root | wc -l)
if [ "$file_count" -le 3 ]; then
    echo "Initializing /root directory from backup..."
    cp -a /root.original/. /root/
    truncate -s 0 /root/.ssh/authorized_keys
fi

# 生成RSA密钥（如果不存在）
if [ ! -f /root/.ssh/id_rsa ]; then
    echo "Generating RSA key pair..."
    ssh-keygen -t rsa -b 4096 -f /root/.ssh/id_rsa -N "" -q
fi

# 检查authorized_keys是否包含公钥，不一致则写入
PUB_KEY=$(cat /root/.ssh/id_rsa.pub)
AUTH_FILE="/root/.ssh/authorized_keys"
if ! grep -qF "$PUB_KEY" "$AUTH_FILE" 2>/dev/null; then
    echo "Writing public key to authorized_keys..."
    echo "$PUB_KEY" >> "$AUTH_FILE"
fi

# 启动SSH服务
echo "Starting SSH service..."
mkdir -p /var/run/sshd
/usr/sbin/sshd
until nc -z localhost 22; do
    sleep 0.5
done
echo "SSH service is running on port 22"

# 启动Chromium无头模式（仅browser变体）
if command -v chromium &>/dev/null; then
    echo "Starting Chromium headless (Mac M1 emulation)..."
    mkdir -p /root/browse_data

    # 生成持久化的指纹种子（每个容器实例唯一，重启不变）
    SEED_FILE=/root/browse_data/.fp_seed
    if [ ! -f "$SEED_FILE" ]; then
        od -An -N4 -tu4 /dev/urandom | tr -d ' \n' > "$SEED_FILE"
    fi
    echo "const INSTANCE_SEED = $(cat "$SEED_FILE");" > /root/chrome-extension/seed.js

    CHROME_VER=$(chromium --version | grep -oP '\d+\.\d+\.\d+\.\d+' | head -1)

    start_chromium() {
        # 清理残留的 profile 锁文件，防止重启后启动失败
        rm -f /root/browse_data/SingletonLock /root/browse_data/SingletonSocket /root/browse_data/SingletonCookie
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Starting Chromium..."
        chromium \
            --headless=new \
            --disable-gpu \
            --no-sandbox \
            --disable-blink-features=AutomationControlled \
            --force-device-scale-factor=2 \
            --remote-debugging-port=9222 \
            --remote-debugging-address=127.0.0.1 \
            --user-data-dir=/root/browse_data \
            --window-size=2560,1545 \
            --lang=zh-CN \
            --load-extension=/root/chrome-extension
    }

    start_chromium &

    # Chromium 进程守护：每30秒检测一次，崩溃则自动重启
    (
        while true; do
            sleep 30
            if ! pgrep -x chromium > /dev/null; then
                echo "[$(date '+%Y-%m-%d %H:%M:%S')] Chromium process died, restarting..."
                start_chromium &
            fi
        done
    ) &
fi

# 启动PicoClaw网关
exec picoclaw gateway -E
