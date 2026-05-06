#!/bin/bash
# chmod +x deploy.sh

# sed -i 's/\r//' ./deploy.sh

# --- 配置区 ---
BASE_DIR="/usr/local/myserver"
WEBSERVER_DIR="$BASE_DIR/webServer"
V5RESULT_DIR="$BASE_DIR/v5-result"

# GitHub 仓库路径 (格式: 用户名/仓库名)
REPO_WEBSERVER="gzjjjfree/webServer"
REPO_V5RESULT="gzjjjfree/v5-result"

# --- 环境检查 ---
if [ "$EUID" -ne 0 ]; then
  echo "请以 root 权限运行此脚本 (使用 sudo)";
  exit 1
fi

ARCH=$(uname -m)
case $ARCH in
    x86_64)  TARGET_ARCH="amd64" ;;
    aarch64|arm64) TARGET_ARCH="arm64" ;;
    *) echo "暂不支持的架构: $ARCH"; exit 1 ;;
esac

echo "检测到系统架构为: $TARGET_ARCH";
echo

# --- 创建目录 ---
echo "正在准备目录结构...";
echo
mkdir -p "$WEBSERVER_DIR/html"
mkdir -p "$V5RESULT_DIR"

# --- 下载函数 ---
download_file() {
    local repo=$1
    local pattern=$2
    local output=$3
    local url=$4 

    # 判断是 GitHub API 还是直接下载链接
    if [[ "$url" == *"api.github.com"* ]]; then
        # 动态替换变量名 (解决你之前的 \$repo 错误)
        local target_url="${url/\$repo/$repo}"
        
        # 1. 解析 API 获取下载地址
        local download_url=$(curl -sS "$target_url" 2>/dev/null | \
            grep "browser_download_url" | \
            grep -i "$pattern" | \
            cut -d '"' -f 4)
            
        if [ -z "$download_url" ]; then
            echo "错误: 无法在 $repo 中找到匹配 $pattern 的发布包" >&2
            return 1
        fi
    else
        # 2. 如果是直接链接 (如 Raw 链接)，直接使用
        local download_url="$url"
    fi

    # 执行下载
    curl -sSL -o "$output" "$download_url"
    chmod +x "$output"
}

# --- 执行部署 ---

echo "开始部署服务器组件...";
echo

# 定义 API 模板 (注意这里用单引号，防止在本行就被解析)
API_URL='https://api.github.com/repos/$repo/releases/latest'

# 1. 部署 webServer
download_file "$REPO_WEBSERVER" "webServer" "$WEBSERVER_DIR/webServer" "$API_URL"
echo "webServer  已部署";
echo

# 2. 部署 v5-result
download_file "$REPO_V5RESULT" "linux-$TARGET_ARCH" "$V5RESULT_DIR/v5-result" "$API_URL"
echo "v5-result 已部署";
echo

# 3. 部署 v5-result-windows
download_file "$REPO_V5RESULT" "windows-$TARGET_ARCH" "$V5RESULT_DIR/v5-result-windows.exe" "$API_URL"
echo "v5-result-windows 已部署";
echo

# 4. 下载 geosite.dat (直接传入 Raw 链接，函数会自动识别并直接下载)
RAW_URL="https://raw.githubusercontent.com/gzjjjfree/v5-result/v5-result/geosite.dat"
download_file "$REPO_V5RESULT" "geosite" "$V5RESULT_DIR/geosite.dat" "$RAW_URL"
echo "geosite.dat 已下载";
echo

# 初始化配置文件 (如果不存在)
# 生成前端服务配置文件
if [ ! -f "$WEBSERVER_DIR/server_conf.json" ]; then
    cat <<EOF > "$WEBSERVER_DIR/server_conf.json"
{
  "v2rayPort": "10001",
  "getApiPort": "10002",
  "postApiPort": "10003",
  "grpcApiPort": "10004",
  "isWs": "/ws",
  "isServers": "false",
  "servers": ["yourdomain.com", "anotherdomain.com"]
}
EOF
    echo "已生成默认 server_conf.json。isServers 默认为 false 适配所有域名, 请根据需要修改为 true 并添加服务器域名。";
    echo
fi

# 生成 v5 服务配置文件
# 生成并存储到一个变量中
USER_UUID=$(cat /proc/sys/kernel/random/uuid)
echo "本次生成的 UUID 为: $USER_UUID"

# 生成服务端配置文件
if [ ! -f "$V5RESULT_DIR/v5_conf.json" ]; then
    cat <<EOF > "$V5RESULT_DIR/v5_conf.json"
{
  "inbounds": [  
    {
      "tag": "v5in",
      "port": 10001,
      "listen": "127.0.0.1",
      "protocol": "vless",
      "settings": {
        "clients": [
          {
            "id": "$USER_UUID"
          }
        ],
        "decryption": "none"
      },
      "streamSettings": {
        "network": "ws",
        "security": "none", 
        "wsSettings": {
          "path": "/ws",
          "User-Agent": "ws-client"
        }
      }
    }
  ],
  "outbounds": [{
    "protocol": "freedom",
    "settings": {}
  }]
}
EOF
    echo "已生成 v5-result 服务端默认配置文件 v5_conf.json。";
    echo
fi

# 生成 v5-result-windows 客户端配置文件
if [ ! -f "$V5RESULT_DIR/config.json" ]; then
    cat <<EOF > "$V5RESULT_DIR/config.json"
{
  "inbounds": [
    {
      "tag": "v5in",
      "port": 10808,
      "listen": "127.0.0.1",
      "protocol": "socks",
      "sniffing": {
        "enabled": true,
        "destOverride": [
          "http",
          "tls"
        ],
        "metadataOnly": false
      },
      "settings": {
        "auth": "noauth",
        "udp": true
      }
    }
  ],
  "outbounds": [
    {
      "tag": "cdn-proxy",
      "protocol": "vless",
      "settings": {
        "vnext": [
          {
            "address": "104.18.86.206",
            "port": 443,
            "users": [
              {
                "id": "$USER_UUID",
                "encryption": "none"
              }
            ]
          }
        ]
      },
      "streamSettings": {
        "network": "ws",
        "security": "tls",
        "tlsSettings": {
          "serverName": "yourdomain.com",                       
          "echDohServer": "yourechdohserver.com",
          "allowInsecure": false
        },
        "wsSettings": {
          "path": "/ws",
          "headers": {
            "Host": "yourdomain.com",
            "User-Agent": "ws-client"
          }
        }
      },
      "mux": {
        "enabled": true,
        "concurrency": 4
      }
    },
    {
      "protocol": "freedom",
      "tag": "direct"
    },
    {
      "protocol": "blackhole",
      "tag": "block"
    }
  ],
  "routing": {
    "domainStrategy": "AsIs",
    "domainMatcher": "mph",
    "balancers": [
      {
        "tag": "cdn-balancer-proxy",
        "selector": [
          "cdn-proxy"
        ],
        "strategy": {
          "type": "random"
        }
      }
    ],
    "rules": [
      {
        "type": "field",
        "balancerTag": "cdn-balancer-proxy",
        "domain": [
          "geosite:geolocation-!cn"
        ],
        "inboundTag": [
          "v5in"
        ]
      },
      {
        "type": "field",
        "outboundTag": "direct",
        "inboundTag": [
          "v5in"
        ]
      }
    ]
  }
}
EOF
    echo "已生成 v5-result-windows 客户端默认配置文件 config.json。";
    echo
    echo "请根据需要修改 config.json 中的服务器地址、端口、UUID 和 TLS 设置等参数，以确保客户端能够正确连接到服务器。";
    echo
    echo -e "\e[1;37;44m 特别注意 config.json 中的 "echDohServer" 设置, 如还没设置 CF 解释 ECH 的 WORKERS, 请先屏蔽它。\e[0m";
    echo
fi

# 生成前端服务自动化启动文件, 将服务内容定义为变量
SERVICE_TEMPLATE=$(cat <<EOF
# 直接编辑系统服务文件
# sudo nano /usr/lib/systemd/system/webServer.service

[Unit]
Description=Go Multi-Domain Web Server
After=network.target

[Service]
# Linux 规定绑定 80 端口需要 root 权限
User=root
# 你的程序存放的目录
WorkingDirectory=$WEBSERVER_DIR
# 程序执行路径
ExecStart=$WEBSERVER_DIR/webServer
# 崩溃后自动重启
Restart=always

[Install]
WantedBy=multi-user.target

# 使用复制及重新加载系统服务
# sudo cp $WEBSERVER_DIR/webServer.service /usr/lib/systemd/system/webServer.service
# sudo systemctl daemon-reload

# 启动及停止服务
# sudo systemctl start webServer
# sudo systemctl stop webServer

# 设置开机自启动及取消自启动
# sudo systemctl enable webServer
# sudo systemctl disable webServer

# 查看运行状态
# sudo systemctl status webServer

# 代码编写完成后，使用以下命令构建 Go 程序
# go build -o webServer webServer.go
EOF
)

# 定义需要生成的两个目标路径
FILE_A="$WEBSERVER_DIR/webServer.service"
FILE_B="/usr/lib/systemd/system/webServer.service"

# 分别判断并写入
# 检查第一个路径（你的项目目录）
if [ ! -f "$FILE_A" ]; then
    echo "$SERVICE_TEMPLATE" > "$FILE_A"
    echo
    echo "已在 $FILE_A 生成服务文件"
    echo
else
    echo "文件 $FILE_A 已存在，跳过"
    echo
fi

# 检查第二个路径（系统服务目录）
if [ ! -f "$FILE_B" ]; then
    # 使用 sudo tee 确保有权限写入系统目录
    echo "$SERVICE_TEMPLATE" | sudo tee "$FILE_B" > /dev/null
    echo
    echo "已在 $FILE_B 生成系统服务文件"
    echo
else
    echo "文件 $FILE_B 已存在，跳过"
    echo
fi

# 生成 v5-result 服务自动化启动文件, 将服务内容定义为变量
V5_TEMPLATE=$(cat <<EOF
# 直接编辑系统服务文件
# sudo nano /usr/lib/systemd/system/v5-result.service

[Unit]
Description=Go Multi-Domain v5-result
After=network.target

[Service]
# Linux 规定绑定 80 端口需要 root 权限
User=root
# 你的程序存放的目录
WorkingDirectory=$V5RESULT_DIR
# 程序执行路径
ExecStart=$V5RESULT_DIR/v5-result run -c v5-conf.json
# 崩溃后自动重启
Restart=always

[Install]
WantedBy=multi-user.target

# 使用复制及重新加载系统服务
# sudo cp $V5RESULT_DIR/v5-result.service /usr/lib/systemd/system/v5-result.service
# sudo systemctl daemon-reload

# 启动及停止服务
# sudo systemctl start v5-result
# sudo systemctl stop v5-result

# 设置开机自启动及取消自启动
# sudo systemctl enable v5-result
# sudo systemctl disable v5-result

# 查看运行状态
# sudo systemctl status v5-result

# 代码编写完成后，使用以下命令构建 Go 程序
# go build -o v5-result v5-result.go
EOF
)

# 定义需要生成的两个目标路径
FILE_C="$V5RESULT_DIR/v5-result.service"
FILE_D="/usr/lib/systemd/system/v5-result.service"

# 分别判断并写入
# 检查第一个路径（你的项目目录）
if [ ! -f "$FILE_C" ]; then
    echo "$V5_TEMPLATE" > "$FILE_C"
    echo
    echo "已在 $FILE_C 生成服务文件"
    echo
else
    echo "文件 $FILE_C 已存在，跳过"
    echo
fi

# 检查第二个路径（系统服务目录）
if [ ! -f "$FILE_D" ]; then
    # 使用 sudo tee 确保有权限写入系统目录
    echo "$V5_TEMPLATE" | sudo tee "$FILE_D" > /dev/null
    echo
    echo "已在 $FILE_D 生成系统服务文件"
    echo
else
    echo "文件 $FILE_D 已存在，跳过"
    echo
fi

sudo systemctl daemon-reload
echo "已重新加载系统服务配置";

sudo systemctl enable webServer
sudo systemctl start webServer
sudo systemctl enable v5-result
sudo systemctl start v5-result

echo "------------------------------------------------";
echo "部署完成！";
echo
echo "请根据生成的服务文件说明，使用 systemctl 管理服务的启动、停止和开机自启。脚本运行时已运行相关命令。";
echo
echo "如果需要修改前端服务配置，请编辑 /usr/local/myserver/webServer/server_conf.json 文件。";
echo
echo "如果需要修改 v5-result 服务配置，请编辑 /usr/local/myserver/v5-result/v5_conf.json 文件。";
echo 
echo -e "\e[1;37;44m 请将 /usr/local/myserver/v5-result/v5-result-windows.exe、config.json、geosite.dat 文件复制到 Windows 客户端上，并在 Windows 上使用相应的命令行工具运行它。\e[0m";
echo ""
echo -e "\e[1;37;44m 可以将 v5-result-windows.exe 生成快捷方式，在属性->目标行末添加参数，如引号里的内容 \" run\", 以便在 Windows 上直接双击运行服务。\e[0m";
echo " "
echo "v5-result-windows.exe 的配置文件为同目录下的 config.json。";
echo "------------------------------------------------";

