#!/bin/bash
# chmod +x deploy.sh

# sed -i 's/\r//' ./deploy.sh

# ==============================================================
# 问题1：运行开始后的前期准备提醒
# ==============================================================
echo "======================================================="
echo "请先进行前期准备："
echo "1、必须先申请域名。不然后期自行配置。"
echo "2、域名强烈建议由 coudflare 进行 CDN 代理，如果是 CDN 代理的话，请先申请长期证书放在 deploy.sh 同目录的 certs 目录下，并在 coudflare 上建立好 works 应用，代码是本项目的 ECH.works 文件，works 必须使用自定义域名。"
echo "======================================================="
read -p "是否已完成以上准备工作？(输入 y 继续运行，输入 n 去做准备工作): " READY_CHOICE
if [[ "$READY_CHOICE" != "y" && "$READY_CHOICE" != "Y" ]]; then
    echo "请完成准备工作后再运行本脚本，脚本已退出。"
    exit 0
fi
echo

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
mkdir -p "$WEBSERVER_DIR/html/windows/result"
mkdir -p "$V5RESULT_DIR"

# --- 下载函数 ---
download_file() {
    local repo=$1
    local pattern=$2
    local output=$3
    local url=$4 

    # 判断是 GitHub API 还是直接下载链接
    if [[ "$url" == *"api.github.com"* ]]; then
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

# 定义 API 模板
API_URL='https://api.github.com/repos/$repo/releases/latest'

# 1. 部署 webServer
download_file "$REPO_WEBSERVER" "webServer" "$WEBSERVER_DIR/webServer" "$API_URL"
echo "webServer 已部署";
echo

# 2. 部署 v5-result
download_file "$REPO_V5RESULT" "linux-$TARGET_ARCH" "$V5RESULT_DIR/v5-result" "$API_URL"
echo "v5-result 已部署";
echo

# 3. 部署 v5-result-windows
download_file "$REPO_V5RESULT" "windows-$TARGET_ARCH" "$WEBSERVER_DIR/html/windows/v5-result-windows.exe" "$API_URL"
echo "v5-result-windows 已部署";
echo

# 3. 部署 v5-result-android
download_file "$REPO_V5RESULT" "android-arm64" "$WEBSERVER_DIR/html/windows/v5-result-android" "$API_URL"
echo "v5-result-android 已部署";
echo

# 4. 下载 geosite.dat
RAW_URL="https://raw.githubusercontent.com/gzjjjfree/v5-result/v5-result/geosite.dat"
download_file "$REPO_V5RESULT" "geosite" "$WEBSERVER_DIR/html/windows/geosite.dat" "$RAW_URL"
echo "geosite.dat 已下载";
echo

# 5. 下载 result.json
RAW_RESULT_URL="https://raw.githubusercontent.com/gzjjjfree/webServer/result/result.json"
download_file "$REPO_WEBSERVER" "result" "$WEBSERVER_DIR/html/windows/result/result.json" "$RAW_RESULT_URL"
echo "result.json 已下载";
echo


# ==============================================================
# 问题2：生成前端服务配置文件前的 CDN 询问逻辑
# ==============================================================
read -p "是否使用 CDN 代理？(y/n): " IS_CDN_INPUT
if [[ "$IS_CDN_INPUT" == "y" || "$IS_CDN_INPUT" == "Y" ]]; then
    USE_CDN=true
    SERVER_CONF_IS_SERVERS="false"
    SERVER_CONF_SERVERS="[]"
else
    USE_CDN=false
    SERVER_CONF_IS_SERVERS="true"
    read -p "请输入域名 (多个域名请用英文逗号 ',' 分开): " USER_DOMAINS
    # 替换逗号为 JSON 格式所需的 ","
    FORMATTED_DOMAINS=$(echo "$USER_DOMAINS" | sed 's/,/","/g')
    SERVER_CONF_SERVERS="[\"$FORMATTED_DOMAINS\"]"
    # 提取第一个域名用于后面的客户端配置备用
    FIRST_DOMAIN=$(echo "$USER_DOMAINS" | cut -d',' -f1)
fi

# 生成前端服务配置文件
if [ ! -f "$WEBSERVER_DIR/server_conf.json" ]; then
    cat <<EOF > "$WEBSERVER_DIR/server_conf.json"
{
  "v2rayPort": "10001",
  "getApiPort": "10002",
  "postApiPort": "10003",
  "grpcApiPort": "10004",
  "isWs": "/ws",
  "isServers": $SERVER_CONF_IS_SERVERS,
  "servers":
  $SERVER_CONF_SERVERS
}
EOF
    echo "已生成默认 server_conf.json。";
    echo
fi

# 生成 v5 服务配置文件
USER_UUID=$(cat /proc/sys/kernel/random/uuid)
echo -e "本次生成的 UUID 为:  \e[1;37;44m$USER_UUID\e[0m";
echo
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
    echo -e "已生成 v5-result 服务端默认配置文件 \e[1;37;44mv5_conf.json\e[0m。";    
fi


# ==============================================================
# 问题3：生成 v5-result-windows 客户端配置文件前的逻辑
# ==============================================================
if [ "$USE_CDN" = true ]; then
    read -p "请输入主域名 (domain): " CLIENT_DOMAIN
    read -p "请输入 ECH 的自定义域名 (echdomain) 注: 不要带 "https://" 头: " CLIENT_ECH_DOMAIN
    CLIENT_ADDRESS="104.18.86.206"
    CLIENT_ECH_CONF="\"echDohServer\": \"$CLIENT_ECH_DOMAIN\","
    ROUTING_RULE_TAG="\"balancerTag\": \"cdn-balancer-proxy\","
else
    # 非 CDN 模式，如果前面已经获取过域名，直接复用
    CLIENT_DOMAIN="$FIRST_DOMAIN"
    if [ -z "$CLIENT_DOMAIN" ]; then
        read -p "请输入主域名 (domain): " CLIENT_DOMAIN
    fi
    read -p "请输入服务器 IP 地址 (addrIP): " CLIENT_ADDRESS
    CLIENT_ECH_CONF="\"echDohServer\": \"\","
    ROUTING_RULE_TAG="\"outboundTag\":  \"cdn-proxy\","
fi

# 生成 v5-result-windows 客户端配置文件
if [ ! -f "$WEBSERVER_DIR/html/windows/config.json" ]; then
    cat <<EOF > "$WEBSERVER_DIR/html/windows/config.json"
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
            "address": "$CLIENT_ADDRESS",
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
          "serverName": "$CLIENT_DOMAIN",                       
          $CLIENT_ECH_CONF
          "allowInsecure": false
        },
        "wsSettings": {
          "path": "/ws",
          "headers": {
            "Host": "$CLIENT_DOMAIN",
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
        $ROUTING_RULE_TAG
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
    echo -e "在客户端请根据需要修改 \e[1;37;44mconfig.json\e[0m 中的服务器地址、端口、UUID 和 TLS 设置等参数，以确保客户端能够正确连接到服务器。";    
    echo -e "特别注意 config.json 中的 \e[1;37;44mechDohServer\e[0m 设置, 如还没设置 CF 解释 ECH 的 WORKERS, 请先屏蔽它。具体查看 \e[1;37;44mv5-result\e[0m 项目。";
    echo
fi

# ==============================================================
# 后续原有的服务生成与启动逻辑
# ==============================================================

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
EOF
)

FILE_A="$WEBSERVER_DIR/webServer.service"
FILE_B="/usr/lib/systemd/system/webServer.service"

if [ ! -f "$FILE_A" ]; then
    echo "$SERVICE_TEMPLATE" > "$FILE_A"    
    echo "已在 $FILE_A 生成服务文件"    
else
    echo "文件 $FILE_A 已存在，跳过"    
fi

if [ ! -f "$FILE_B" ]; then
    echo "$SERVICE_TEMPLATE" | sudo tee "$FILE_B" > /dev/null   
    echo "已在 $FILE_B 生成系统服务文件"    
else
    echo "文件 $FILE_B 已存在，跳过"    
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
WorkingDirectory=/usr/local/myserver/v5-result
# 程序执行路径
ExecStart=/usr/local/myserver/v5-result/v5-result run -c v5_conf.json
# 崩溃后自动重启
Restart=always

[Install]
WantedBy=multi-user.target
EOF
)

FILE_C="$V5RESULT_DIR/v5-result.service"
FILE_D="/usr/lib/systemd/system/v5-result.service"

if [ ! -f "$FILE_C" ]; then
    echo "$V5_TEMPLATE" > "$FILE_C"    
    echo "已在 $FILE_C 生成服务文件"   
else
    echo "文件 $FILE_C 已存在，跳过"   
fi

if [ ! -f "$FILE_D" ]; then
    echo "$V5_TEMPLATE" | sudo tee "$FILE_D" > /dev/null   
    echo "已在 $FILE_D 生成系统服务文件"    
else
    echo "文件 $FILE_D 已存在，跳过"    
fi

echo
echo "正在设置系统服务...";
sudo systemctl daemon-reload

sudo systemctl enable webServer
sudo systemctl start webServer
sudo systemctl enable v5-result
sudo systemctl start v5-result

echo "------------------------------------------------";
echo "部署完成！";
echo
echo -e "请根据生成的服务文件说明，使用 systemctl 管理服务的启动、停止和开机自启。脚本运行时\e[1;37;44m已运行\e[0m相关命令启动。";
echo
echo -e "如果需要修改前端服务配置，请编辑 /usr/local/myserver/webServer/\e[1;37;44mserver_conf.json\e[0m 文件。";
echo -e "如果需要修改 v5-result 服务配置，请编辑 /usr/local/myserver/v5-result/\e[1;37;44mv5_conf.json\e[0m 文件。";
echo 
echo -e "请将 /usr/local/myserver/webServer/html/windows/\e[1;37;44mv5-result-windows.exe、config.json、geosite.dat 文件复制到 Windows 客户端\e[0m上, 并在 Windows 上使用相应的命令行工具运行它。";
echo -e "下载文件可以通过指向服务器的域名访问 \e[1;37;44mhttp://yourdomain.com/windows/\e[0m 来获取, 也可以直接从服务器上复制。";
echo -e "在 Windows 上运行 v5-result-windows.exe 的命令示例: \e[1;37;44mv5-result-windows.exe run -c config.json\e[0m";
echo -e "可以将 v5-result-windows.exe \e[1;37;44m生成快捷方式\e[0m, 在属性->目标行末添加参数\e[1;37;44m run\e[0m, 以便在 Windows 上直接双击运行服务。";
echo "v5-result-windows.exe 的配置文件为同目录下的 config.json。";
echo "------------------------------------------------";
sudo cp server_conf.json /usr/local/myserver/webServer/server_conf.json
sudo cp v5_conf.json /usr/local/myserver/v5-result/v5_conf.json
sudo cp -r certs /usr/local/myserver/webServer/
sudo systemctl restart webServer
sudo systemctl restart v5-result