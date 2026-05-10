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

# 从备份目录复制文件到 /root（不覆盖已有文件，包括隐藏目录）
cp -an /root.original/. /root/

# 启动PicoClaw网关
exec picoclaw gateway -E
