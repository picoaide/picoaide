FROM debian:13

# 设置非交互式安装
ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=zh_CN.UTF-8
ENV TZ=Asia/Shanghai

# 安装基础依赖、SSH和常用开发工具
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    nano \
    vim \
    tmux \
    htop \
    tree \
    iputils-ping \
    netcat-openbsd \
    telnet \
    dnsutils \
    iproute2 \
    iptables \
    sudo \
    psmisc \
    procps \
    lsof \
    strace \
    jq \
    ca-certificates \
    openssh-server \
    openssh-client \
    build-essential \
    git \
    rsync \
    zip \
    unzip \
    fish \
    zsh \
    fzf \
    ripgrep \
    bat \
    locales \
    tzdata \
    && rm -rf /var/lib/apt/lists/* && \
    sed -i '/zh_CN.UTF-8/s/^# //g' /etc/locale.gen && \
    locale-gen zh_CN.UTF-8 && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

# 配置SSH - 只允许密钥登录，禁用密码，启用SFTP
RUN mkdir -p /var/run/sshd && \
    sed -i 's/#*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config && \
    sed -i 's/#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config && \
    sed -i 's/#*PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/#*UsePAM.*/UsePAM no/' /etc/ssh/sshd_config && \
    sed -i 's/#*AllowTcpForwarding.*/AllowTcpForwarding yes/' /etc/ssh/sshd_config && \
    sed -i 's/#*GatewayPorts.*/GatewayPorts yes/' /etc/ssh/sshd_config && \
    sed -i 's/^Subsystem sftp.*/Subsystem sftp internal-sftp/' /etc/ssh/sshd_config

# 创建SSH目录和authorized_keys文件（为空，用户需要手动添加公钥）
RUN mkdir -p /root/.ssh && touch /root/.ssh/authorized_keys && chmod 700 /root/.ssh && chmod 600 /root/.ssh/authorized_keys

# 安装uv
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.local/bin:/root/.cargo/bin:$PATH" \
    UV_DEFAULT_INDEX="https://pypi.tuna.tsinghua.edu.cn/simple"

# 安装nvm
ENV NVM_DIR="/root/.nvm"
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.4/install.sh | bash

# 安装Node.js LTS并设置默认版本
RUN /bin/bash -c '. ~/.nvm/nvm.sh && nvm install --lts && nvm alias default node && npm config set registry https://registry.npmmirror.com'

# 创建系统级NVM配置，确保所有shell都能加载
RUN echo 'export NVM_DIR="/root/.nvm"' > /etc/profile.d/nvm.sh && \
    echo '[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"' >> /etc/profile.d/nvm.sh && \
    chmod +x /etc/profile.d/nvm.sh

# 设置Node.js路径（通过nvm默认版本动态查找）
RUN NODE_BIN=$(find /root/.nvm/versions/node -name node -path "*/bin/node" 2>/dev/null | head -1) && \
    NODE_DIR=$(dirname "$(dirname "$NODE_BIN")") && \
    echo "NODE_DIR=$NODE_DIR" && \
    echo "export PATH=\"${NODE_DIR}/bin:\$PATH\"" >> /etc/profile.d/nvm.sh && \
    echo "export NODE_PATH=\"${NODE_DIR}/lib/node_modules\"" >> /etc/profile.d/nvm.sh
ENV PATH="/root/.nvm/versions/node/v24.15.0/bin:$PATH" \
    NODE_PATH="/root/.nvm/versions/node/v24.15.0/lib/node_modules"

# 安装 picoclaw
ARG PICOCLAW_VERSION
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "x86_64" ]; then \
        DOWNLOAD_ARCH="x86_64"; \
    elif [ "$ARCH" = "aarch64" ]; then \
        DOWNLOAD_ARCH="arm64"; \
    else \
        echo "Unsupported architecture: $ARCH" && exit 1; \
    fi && \
    echo "Installing picoclaw for platform: $ARCH (download: $DOWNLOAD_ARCH)" && \
    echo "Installing picoclaw version: $PICOCLAW_VERSION" && \
    DOWNLOAD_URL="https://github.com/sipeed/picoclaw/releases/download/${PICOCLAW_VERSION}/picoclaw_Linux_${DOWNLOAD_ARCH}.tar.gz" && \
    echo "Downloading from: $DOWNLOAD_URL" && \
    curl -fsSL "$DOWNLOAD_URL" -o /tmp/picoclaw.tar.gz && \
    tar -xzf /tmp/picoclaw.tar.gz -C /tmp && \
    chmod +x /tmp/picoclaw && \
    mv /tmp/picoclaw /usr/bin/ && \
    rm /tmp/picoclaw.tar.gz && \
    picoclaw version && \
    picoclaw onboard && \
    rm -rf /root/.picoclaw/workspace/skills/agent-browser/ && \
    git clone --depth 1 https://github.com/ChromeDevTools/chrome-devtools-mcp.git /tmp/chrome-devtools-mcp && \
    cp -r /tmp/chrome-devtools-mcp/skills/* /root/.picoclaw/workspace/skills/ && \
    rm -rf /tmp/chrome-devtools-mcp

# 注入 chrome-devtools MCP 服务器配置到 picoclaw config
RUN CONFIG="/root/.picoclaw/config.json" && \
    jq '.tools.mcp.servers["chrome-devtools"]={"args":["chrome-devtools-mcp@latest","--browser-url=http://127.0.0.1:9222","--autoConnect"],"command":"npx","enabled":true}' "$CONFIG" > "${CONFIG}.tmp" && mv "${CONFIG}.tmp" "$CONFIG"

# 安装 bocha-search-mcp
RUN git clone https://github.com/BochaAI/bocha-search-mcp.git /mcp/bocha-search-mcp

# 设置 apt 国内源（清华源）- Debian 13
RUN sed -i 's|deb.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || \
    sed -i 's|deb.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list 2>/dev/null || true

# 设置 pip 国内源（清华源）
RUN mkdir -p ~/.pip && \
    echo '[global]' > ~/.pip/pip.conf && \
    echo 'index-url = https://pypi.tuna.tsinghua.edu.cn/simple' >> ~/.pip/pip.conf && \
    echo 'trusted-host = pypi.tuna.tsinghua.edu.cn' >> ~/.pip/pip.conf

# 复制启动脚本
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod +x /usr/local/bin/entrypoint.sh

# 创建root目录备份（必须在所有文件复制之后）
RUN mkdir -p /root.original && \
    cp -a /root/. /root.original/ && \
    rm -rf /root/.ssh/authorized_keys

# 启动SSH服务
CMD ["/usr/local/bin/entrypoint.sh"]
