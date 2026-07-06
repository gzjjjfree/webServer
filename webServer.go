package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/gzjjjfree/loggz"
)

func main() {
	//if SEND_KEY != "" {
	//	sendKey = SEND_KEY
	//}
	RegisterHostRouter()

	// 保持程序运行
	{
		// osSignals: 声明了一个无缓冲的 channel，其类型为 os.Signal，用于接收操作系统信号
		// 创建一个容量为 1 的 channel。容量为 1 表示该 channel 最多只能存储一个信号
		osSignals := make(chan os.Signal, 1)
		// signal.Notify: 这个函数的作用是将指定的信号注册到指定的 channel 上
		// osSignals: 上面创建的 channel，用于接收信号
		// os.Interrupt 和 syscall.SIGTERM: 两个常见的操作系统信号，分别表示用户中断 (Ctrl+C) 和终止进程
		signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
		// <-osSignals: 从 osSignals channel 接收一个信号。当程序执行到这一行时，会阻塞，直到有信号被发送到该 channel
		<-osSignals
	}
}

// Config 对应 JSON 配置文件结构
type Config struct {
	V2rayPort   string   `json:"v2rayPort"`
	GetApiPort  string   `json:"getApiPort"`
	PostApiPort string   `json:"postApiPort"`
	GrpcApiPort string   `json:"grpcApiPort"`
	IsWs        string   `json:"isWs"`
	IsServers   bool     `json:"isServers"`
	Servers     []string `json:"servers"`
}

type CertManager struct {
	certDir string
	mu      sync.RWMutex
	certs   []tls.Certificate
}

var sendKey string // 用于发送数据到微信方糖的密钥，用于方糖给你发送提醒信息

// 辅助函数：创建反向代理
func newProxy(port string, prefix string) *httputil.ReverseProxy {
	target := fmt.Sprintf("http://127.0.0.1:%s", port)
	targetURL, _ := url.Parse(target)

	// 在 Go 1.20+ 中，推荐直接初始化 ReverseProxy 结构体或使用 Rewrite 逻辑
	proxy := &httputil.ReverseProxy{
		// 💡 针对 V2Ray 和 gRPC 的流式传输优化，关闭内部缓冲
		FlushInterval: -1,

		Rewrite: func(r *httputil.ProxyRequest) {
			// 设置目标地址
			// SetURL 会自动处理目标主机的 Scheme 和 Host，并保留查询参数
			r.SetURL(targetURL)

			// 确保 V2Ray 收到的是真实域名而不是 127.0.0.1
			r.Out.Host = r.In.Host

			// 路径重写逻辑
			if prefix != "" && prefix != "/ws" {
				// 此时 r.Out.URL.Path 已经是基于 targetURL 拼接后的路径
				// 我们直接对发往后端的 Out 请求进行路径修剪
				newPath := strings.TrimPrefix(r.Out.URL.Path, prefix)
				if newPath == "" || !strings.HasPrefix(newPath, "/") {
					newPath = "/" + newPath
				}
				r.Out.URL.Path = newPath
			}

			// 安全增强：设置 X-Forwarded-For, X-Forwarded-Host, X-Forwarded-Proto
			// 它会自动移除客户端伪造的头部，并填入真实的代理链路信息
			r.SetXForwarded()
		},
	}

	return proxy
}

func RegisterHostRouter() {
	// 读取并解析配置文件
	confFile, err := os.ReadFile("server_conf.json")
	if err != nil {
		loggz.WriteErrLog(fmt.Sprintf("无法读取配置文件: %v", err))
	}

	var conf *Config
	if err := json.Unmarshal(confFile, &conf); err != nil {
		loggz.WriteErrLog(fmt.Sprintf("配置文件解析失败: %v", err))
	}

	// 初始化反向代理
	v2rayProxy := newProxy(conf.V2rayPort, "/ws")
	getProxy := newProxy(conf.GetApiPort, "/getapi")
	postProxy := newProxy(conf.PostApiPort, "/postapi")
	grpcProxy := newProxy(conf.GrpcApiPort, "/grpcapi")

	// 注册搜索代理路由
	searxProxy := newProxy("8080", "/sxng")

	// 静态文件服务器
	fs := http.FileServer(http.Dir("./html"))

	// 核心路由逻辑
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// 优先判断 WebSocket (V2Ray)
		if path == conf.IsWs && path != "" && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
			loggz.WriteInfoLog("WebSocket 转发: " + r.RequestURI)
			v2rayProxy.ServeHTTP(w, r)
			return
		}

		// 判断 API 路径 (前缀匹配)
		if strings.HasPrefix(path, "/getapi") {
			loggz.WriteInfoLog("GetAPI 转发: " + r.RequestURI)
			getProxy.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(path, "/postapi") {
			loggz.WriteInfoLog("PostAPI 转发: " + r.RequestURI)
			postProxy.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(path, "/grpcapi") {
			loggz.WriteInfoLog("PostAPI 转发: " + r.RequestURI)
			grpcProxy.ServeHTTP(w, r)
			return
		}

		// 拦截搜索主接口
		if strings.HasPrefix(path, "/sxng") {
			loggz.WriteInfoLog("SearXNG 搜索转发: " + r.RequestURI)
			searxProxy.ServeHTTP(w, r)
			return
		}

		// 静态文件
		loggz.WriteInfoLog("静态资源请求: " + r.RequestURI)
		fs.ServeHTTP(w, r)
	})

	// 开启 80 端口重定向
	go func() {
		loggz.WriteInfoLog("正在启动 80 端口监控并设置 HTTPS 重定向...")

		// 创建一个跳转处理器
		redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 获取请求的主机名（去掉可能存在的端口）
			host := strings.Split(r.Host, ":")[0]

			// 拼接新的 HTTPS URL
			// 使用 301 (Permanent Redirect) 对 SEO 友好
			target := "https://" + host + r.URL.Path
			if len(r.URL.RawQuery) > 0 {
				target += "?" + r.URL.RawQuery
			}

			loggz.WriteInfoLog(fmt.Sprintf("[Redirect] HTTP -> HTTPS: %s", target))
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		})

		// 监听 80 端口并应用跳转逻辑
		if err := http.ListenAndServe(":80", redirectHandler); err != nil {
			loggz.WriteErrLog(fmt.Sprintf("80端口监听失败: %v", err))
		}
	}()

	loggz.WriteInfoLog("服务器正在启动，监听 80 端口...")

	loggz.WriteInfoLog("HTTPS 服务器启动成功，监听 443 端口...")
	// ListenAndServeTLS 第二个和第三个参数留空，因为 certManager 会提供证书
	go func() {
		loggz.WriteInfoLog("HTTPS 服务器启动中，正在扫描证书文件夹...")

		// 1. 初始化管理器
		cm := &CertManager{certDir: "certs"}

		// 2. 初始加载
		if err := cm.LoadCerts(); err != nil {
			panic(fmt.Sprintf("初始加载证书失败: %v", err))
		}

		// 3. 开启异步监听
		//cm.WatchConfig()

		// 4. 将证书切片配置到自定义的 Server 结构体中
		server := &http.Server{
			Addr:      ":443",
			Handler:   mainHandler, // 你的主路由
			TLSConfig: cm.GetConfig(conf),
		}

		// 5. 启动服务（由于证书已经在 TLSConfig 中指明，这里的路径参数直接传空字符串）
		err = server.ListenAndServeTLS("", "")
		if err != nil && err != http.ErrServerClosed {
			loggz.WriteErrLog(fmt.Sprintf("HTTPS 启动失败: %v", err))
		}
	}()
}

// LoadCerts 封装你原有的扫描逻辑
func (cm *CertManager) LoadCerts() error {
	files, err := os.ReadDir(cm.certDir)
	if err != nil {
		return err
	}

	var tempCerts []tls.Certificate
	for _, file := range files {
		// 匹配逻辑：*.pem 且存在 *.key
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".pem") && !strings.HasSuffix(file.Name(), ".key.pem") {
			prefix := strings.TrimSuffix(file.Name(), ".pem")
			certPath := filepath.Join(cm.certDir, file.Name())
			keyPath := filepath.Join(cm.certDir, prefix+".key")

			pair, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err != nil {
				fmt.Printf("跳过证书 [%s]: %v\n", prefix, err)
				continue
			}
			tempCerts = append(tempCerts, pair)
			loggz.WriteInfoLog(fmt.Sprint("成功加载/更新证书: \n", prefix))
		}
	}

	cm.mu.Lock()
	cm.certs = tempCerts
	cm.mu.Unlock()
	return nil
}

// GetConfig 获取用于 http.Server 的 TLSConfig
func (cm *CertManager) GetConfig(conf *Config) *tls.Config {
	return &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// 如果开启了服务器域名白名单校验
			if conf != nil && conf.IsServers {
				allowed := false
				// 遍历配置中的白名单列表
				for _, domain := range conf.Servers {
					if hello.ServerName == domain {
						allowed = true
						break
					}
				}

				// 如果当前请求的 SNI 域名不在白名单内，直接拦截并返回 nil
				if !allowed {
					loggz.WriteInfoLog("域名不在白名单列表内，拒绝提供证书: " + hello.ServerName)
					// 返回 nil, nil 代表让 TLS 握手失败，不会向客户端发送任何证书
					return nil, nil
				}
			}

			cm.mu.RLock()
			defer cm.mu.RUnlock()

			// 自动匹配 SNI 域名
			for _, cert := range cm.certs {
				if err := hello.SupportsCertificate(&cert); err == nil {
					loggz.WriteInfoLog("提供证书域名为: " + hello.ServerName)
					return &cert, nil
				}
			}
			loggz.WriteInfoLog("未找到匹配的证书: " + hello.ServerName)
			return nil, fmt.Errorf("未找到匹配的证书: %s", hello.ServerName)
		},
	}
}

// SearchProxyHandler 处理搜索转发
//func SearchProxyHandler(w http.ResponseWriter, r *http.Request) {
//	// 1. 鉴权逻辑（可选）：只有登录用户才能搜索
//	// if !checkUserLogin(r) {
//	//     http.Error(w, "请先登录", http.StatusUnauthorized)
//	//     return
//	// }
//
//	// 2. 获取前端传来的关键词
//	query := r.URL.Query().Get("q")
//	if query == "" {
//		http.Error(w, "搜索内容不能为空", http.StatusBadRequest)
//		return
//	}
//
//	// 3. 构造请求 SearXNG 的 URL
//	// 注意：我们将 format 锁定为 html，直接让 SearXNG 渲染好界面返回
//	searxngURL := fmt.Sprintf("http://127.0.0.1:8080/search?q=%s&format=html", url.QueryEscape(query))
//
//	// 4. 创建新的请求
//	req, err := http.NewRequest("GET", searxngURL, nil)
//	if err != nil {
//		http.Error(w, "创建请求失败", http.StatusInternalServerError)
//		return
//	}
//
//	// 伪装 User-Agent，防止被下游引擎识别为爬虫
//	req.Header.Set("User-Agent", r.Header.Get("User-Agent"))
//
//	// 5. 发起请求
//	client := &http.Client{}
//	resp, err := client.Do(req)
//	if err != nil {
//		http.Error(w, "无法连接到搜索引擎", http.StatusBadGateway)
//		return
//	}
//	defer resp.Body.Close()
//
//	// 6. 复制 SearXNG 的 Content-Type（通常是 text/html）
//	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
//	w.WriteHeader(resp.StatusCode)
//
//	// 7. 将 SearXNG 的响应流式传输回前端 iframe
//	io.Copy(w, resp.Body)
//}
