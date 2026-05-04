# WebServer (Go)

这是一个基于 Go 语言开发的高性能前端服务器，集成了 **自动 HTTPS 证书管理 (ACME)**、**WebSocket 转发**、**gRPC 支持** 以及 **多域名动态路由** 功能。

### 核心特性

- **自动证书管理**: 集成 `autocert` (Let's Encrypt)，支持根据配置文件中的域名列表动态申请、验证并自动续期 SSL 证书。
- **智能协议转发**:
  - **WebSocket**: 专门针对 `/ws` 路径优化，支持 V2Ray 等协议的持久连接，通过设置 `FlushInterval: -1` 确保数据实时透传。
  - **RESTful API**: 区分 `/getapi` 和 `/postapi` 转发到不同的后端服务端口。
  - **gRPC**: 支持 `/grpcapi` 路径的流量转发。
- **静态文件托管**: 默认根路径 `/` 映射到本地 `./html` 目录。
- **自动 HTTPS 跳转**: 监听 80 端口，自动处理 ACME 验证挑战并将普通 HTTP 请求重定向至 443 端口。
- **多域名白名单**: 只有在配置文件白名单中的域名才会被允许访问及申请证书（当 `isServers` 为 `true` 时）。

### 部署建议

建议将项目部署在 `/usr/local/webServer` 目录下，以符合 Linux 软件安装规范。

```bash
# 创建部署目录
sudo mkdir -p /usr/local/webServer/html

# 将编译好的文件和配置文件移动到该目录
sudo cp webServer /usr/local/webServer/
sudo cp server_conf.json /usr/local/webServer/

# 进入目录并运行，自动运行请查看 webServer.service
cd /usr/local/webServer
sudo ./webServer
```

### 配置文件说明 (server_conf.json)

#### "isWs" 路径为 v2ray ws 路径，

#### "isServers" 为 "false" 时，可验证任何域名，

```json
{
  "v2rayPort": "10001",
  "getApiPort": "10002",
  "postApiPort": "10003",
  "grpcApiPort": "10004",
  "isWs": "/ws",
  "isServers": "true",
  "servers": ["yourdomain1.com", "yourdomain2.com"]
}
```

### 项目结构

```text
/usr/local/webServer/
├── webServer           # 编译后的可执行文件
├── server_conf.json    # 配置文件
├── html/               # 静态文件根目录 (需手动创建并放入 index.html)
└── certs/              # 自动生成的证书存放目录 (程序启动后自动创建)
```

### 注意事项

1.  **权限**: 监听 80 和 443 端口需要以 root 权限运行程序（`sudo`）。
2.  **防火墙**: 请确保服务器防火墙已开放 80 (TCP) 和 443 (TCP) 端口。
3.  **ACME 挑战**: 域名首次访问时会触发证书申请，可能存在几秒钟的初始化延迟。
4.  **持久化运行**: 建议使用 `nohup` 或编写 `systemd` 服务单元来保持程序在后台持续运行。
