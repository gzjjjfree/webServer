# WebServer (Go)

这是一个基于 Go 语言开发的高性能前端服务器，专门针对 **Cloudflare CDN** 与 **直连模式 (Let's Encrypt 自动证书)** 双轨并行优化定制[cite: 3]。项目集成了 WebSocket 转发、gRPC 支持、多域名动态路由以及静态文件托管功能[cite: 3]。新版本已全面支持根据配置自动识别并动态切换本地长期证书与 Let's Encrypt 自动申请机制。

### 核心特性

- **双轨证书与代理模式**:
  - **CDN 模式**: 充分利用 Cloudflare 的网络加速与安全防护，结合自建 Workers 有效解决 ECH 缓存过期导致的网络连通性问题[cite: 3]。证书由本地 `certs` 目录的源服务器证书提供支持。
  - **直连模式 (Let's Encrypt)**: 完美集成 Let's Encrypt 自动证书申请与续签机制。当开启白名单校验且匹配域名时，服务器自动截获 80 端口流量完成 HTTP-01 验证，下发证书并自动缓存至 `autocerts` 目录。
- **智能协议转发**:
  - **WebSocket**: 针对 `/ws` 路径深度优化，完美支持 V2Ray 等协议的持久连接，通过 `FlushInterval: -1` 实现数据的实时零延迟透传[cite: 3]。
  - **RESTful API**: 精准区分 `/getapi` 和 `/postapi` 并将其高效转发至不同的后端服务端口[cite: 3]。
  - **gRPC**: 完美支持 `/grpcapi` 路径的高性能流量转发[cite: 3]。
  - **SearXNG 搜索**: 自动拦截 `/sxng` 路径并进行本地代理转发。
- **静态文件托管**: 默认将未匹配其他路由的根路径 `/` 映射到本地的 `./html` 目录[cite: 3]。
- **HTTP 自动重定向**: 80 端口默认将普通 HTTP 流量安全地执行 301 永久重定向至 HTTPS，并在 Let's Encrypt 验证时智能让行。

### 运行前置条件

在部署和运行本项目之前，**必须**依次确认并满足以下前置条件[cite: 3]：

**通用条件：**

1. **必须拥有自有域名**: 项目运行必须绑定并依赖自有的独立域名[cite: 3]。
2. **脚本安全审查**: 在将 `deploy.sh` 复制到服务器运行之前，**必须先行查看、逐行审计并完全理解 `deploy.sh` 中的每一行命令**，切勿盲目执行[cite: 3]。

**若选择 CDN 模式 (推荐)：** 3. **域名接入 Cloudflare CDN**: 域名必须已开启 Cloudflare 的 CDN 代理（即小黄云状态），并将解析准确指向服务器的真实 IP[cite: 3]。4. **配置源服务器证书**: 必须在 Cloudflare 后台生成“源服务器证书”（Origin Certificate），并将获取的证书文件放入 `certs` 文件夹下[cite: 3]。

- **自动同步**: 在运行 `deploy.sh` 脚本前，可将 `certs` 文件夹与 `deploy.sh` 放在同一目录下，脚本会自动将证书复制到目标部署目录[cite: 3]。

5. **自建 Cloudflare Workers 服务**: 必须在 Cloudflare 上建立并配置用于解析 ECH 的 Workers 服务。此举是为了解决“先有鸡还是先有蛋”的问题（即当 ECH 过期后无法直接科学上网，导致无法访问官方的 ECH 解析服务，必须通过自建的 Workers 服务来进行中转解析）[cite: 3]。

**若选择直连模式 (非 CDN)：** 6. **直接解析**: 域名必须直接解析至本服务器 IP（不可开启 CDN 代理）。7. **端口开放**: 服务器的 80 和 443 端口必须对外开放，以确保 Let's Encrypt 证书申请（HTTP-01 挑战）流程不被拦截。

### 自动化部署与目录结构

新版交互式部署脚本 `./deploy.sh` 运行流程：

1. 强制确认前期准备工作。
2. 动态询问是否使用 CDN 代理，并据此决定证书策略与客户端连接参数 (域名、IP、ECH 域名配置等)。
3. 自动生成服务端与 Windows 客户端对应的配置文件。

建议将项目统一部署在 `/usr/local/myserver/` 目录下，以确保环境的一致性与规范性[cite: 3]。

```text
/usr/local/myserver/
├── webServer/
│   ├── webServer          # 编译后的 Go 可执行文件
│   ├── server_conf.json   # 动态生成的配置文件
│   ├── html/              # 静态文件根目录及 Windows 客户端下载目录
│   ├── certs/             # Cloudflare 源服务器证书存放目录 (CDN 模式用)
│   └── autocerts/         # Let's Encrypt 自动证书缓存目录 (直连模式自动生成)
└── v5-result/
    ├── v5-result          # 后端核心组件
    └── v5_conf.json       # 服务端配置文件
```

### 配置文件说明 (server_conf.json)

- `"isWs"`: 配置用于 V2Ray 等协议的 WebSocket 路径[cite: 3]。
- `"isServers"` 与 `"servers"` 用于双轨路由策略控制：
  - **CDN 模式 (`"isServers": false`)**: 关闭域名白名单校验，所有 SNI 域名请求均回退使用本地 `certs` 目录下的长期证书。
  - **直连模式 (`"isServers": true`)**: 开启域名白名单机制。如果请求域名存在于 `"servers"` 列表中，系统将自动经由 Let's Encrypt 处理；若请求域名不在列表中，系统会平滑回退，尝试寻找本地长期证书。

配置文件示例：

```json
{
  "v2rayPort": "10001",
  "getApiPort": "10002",
  "postApiPort": "10003",
  "grpcApiPort": "10004",
  "isWs": "/ws",
  "isServers": true,
  "servers": ["yourdomain.com"]
}
```

### 附录：Cloudflare Workers ECH 解析服务脚本

您可以将自建的 Workers 脚本代码配置于此处[cite: 3]。用以通过自建中转节点提供不依赖官方域名的安全 DOH 解析，彻底打通网络初始连通性[cite: 3]。

```javascript
export default {
  async fetch(request) {
    const url = new URL(request.url);
    const domain = url.searchParams.get("domain");

    if (domain) {
      try {
        const dohUrl = `https://1.1.1.1/dns-query?name=${domain}&type=65`;
        const response = await fetch(dohUrl, {
          headers: { accept: "application/dns-json" },
        });
        const json = await response.json();

        const echConfig = extractEch(json);
        if (echConfig) {
          return new Response(echConfig, {
            headers: { "Content-Type": "text/plain" },
          });
        }
        return new Response("ECH not found in record", { status: 404 });
      } catch (e) {
        return new Response("Error: " + e.message, { status: 500 });
      }
    }
    return new Response("Missing domain", { status: 400 });
  },
};

function extractEch(dnsJson) {
  if (!dnsJson.Answer) return null;

  for (const record of dnsJson.Answer) {
    if (record.type === 65) {
      const data = record.data;

      // 情况 A: 已经是易读格式 ech="xxx"
      const match = data.match(/ech="([^"]+)"/);
      if (match) return match[1];

      // 情况 B: 十六进制格式 \# <len> <hex_data>
      if (data.startsWith("\\#")) {
        // 移除前缀 "\# 136 "（具体的长度数字可能不同）
        const parts = data.split(" ");
        // 真正的十六进制数据从索引 2 或 3 开始，我们将所有部分合并
        const hex = parts.slice(2).join("");

        // ECH 的标识符是 0005
        const echIndex = hex.indexOf("0005");
        if (echIndex !== -1) {
          // 0005 后面是 2 字节长度 (4个字符)
          const lenHex = hex.substring(echIndex + 4, echIndex + 8);
          const len = parseInt(lenHex, 16);
          // 提取 ECH 核心数据
          const echHex = hex.substring(echIndex + 8, echIndex + 8 + len * 2);

          // 将 Hex 转换为 Uint8Array，再转为 Base64
          const bytes = new Uint8Array(
            echHex.match(/.{1,2}/g).map((byte) => parseInt(byte, 16)),
          );
          return btoa(String.fromCharCode(...bytes));
        }
      }
    }
  }
  return null;
}
```
