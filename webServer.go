package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gzjjjfree/loggz"
)

func main() {
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
	V2rayPort   string `json:"v2rayPort"`
	GetApiPort  string `json:"getApiPort"`
	PostApiPort string `json:"postApiPort"`
	GrpcApiPort string `json:"grpcApiPort"`
	IsWs        string `json:"isWs"`
}

// 辅助函数：创建反向代理
func newProxy(port string, prefix string) *httputil.ReverseProxy {
	target := fmt.Sprintf("http://127.0.0.1:%s", port)
	url, _ := url.Parse(target)

	proxy := httputil.NewSingleHostReverseProxy(url)

	// 获取原始的 Director
	originalDirector := proxy.Director

	// 自定义 Director 来重写路径
	proxy.Director = func(req *http.Request) {
		// 先执行默认逻辑（设置 Scheme, Host 等）
		originalDirector(req)

		// 去掉路径前缀
		if prefix != "" {
			// 假设访问 /postapi/login -> 转发后变成 /login
			// 如果只访问 /postapi -> 转发后变成 /
			newPath := strings.TrimPrefix(req.URL.Path, prefix)
			if newPath == "" {
				newPath = "/"
			}
			req.URL.Path = newPath
		}
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
	v2rayProxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			targetUrl, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%s", conf.V2rayPort))
			if err != nil {
				log.Fatal("目标地址解析失败:", err)
			}

			pr.SetURL(targetUrl)

			pr.Out.Host = targetUrl.Host

			pr.Out.Header.Set("Upgrade", "websocket")
			pr.Out.Header.Set("Connection", "Upgrade")

			pr.SetXForwarded()
		},

		// --- 性能优化：减少延迟 ---
		// FlushInterval = -1 表示一旦从后端收到数据就立即发送给客户端
		// 这对于 WebSocket 这种实时性极高的协议至关重要
		FlushInterval: -1,

		// --- 响应处理：直接原样返回 ---
		// 如果后端返回 404、500 或其他自定义状态码，
		// ModifyResponse 默认不做干预即会原样透传给用户
		ModifyResponse: func(resp *http.Response) error {
			// 返回 nil 表示对响应不作修改，直接交给用户
			return nil
		},

		// --- 错误处理 (可选) ---
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			loggz.WriteErrLog(fmt.Sprintf("代理转发错误: %v", err))
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	getProxy := newProxy(conf.GetApiPort, "/getapi")
	postProxy := newProxy(conf.PostApiPort, "/postapi")
	grpcProxy := newProxy(conf.GrpcApiPort, "/grpcapi")

	// 静态文件服务器
	fs := http.FileServer(http.Dir("./html"))

	// 3. 核心路由逻辑
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 路径规则匹配
		switch r.URL.Path {
		case conf.IsWs:
			if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
				v2rayProxy.ServeHTTP(w, r)
			} else {
				http.Error(w, "Not a WebSocket request", http.StatusBadRequest)
			}
		case "/getapi":
			getProxy.ServeHTTP(w, r)
		case "/postapi":
			postProxy.ServeHTTP(w, r)
		case "/grpcapi":
			grpcProxy.ServeHTTP(w, r)
		default:
			fs.ServeHTTP(w, r)
		}
	})

	// 开启 80 端口重定向
	go func() {
		loggz.WriteInfoLog("正在启动 80 端口监控并设置 HTTPS 重定向...")

		// 创建一个跳转处理器
		redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. 获取请求的主机名（去掉可能存在的端口）
			host := strings.Split(r.Host, ":")[0]

			// 2. 拼接新的 HTTPS URL
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

		certDir := "certs"
		files, err := os.ReadDir(certDir)
		if err != nil {
			loggz.WriteErrLog(fmt.Sprintf("无法读取证书目录: %v", err))
			return
		}

		var certs []tls.Certificate

		for _, file := range files {
			// 只处理以 .pem 结尾且不是 .key.pem 的文件（避免重复扫描）
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".pem") && !strings.HasSuffix(file.Name(), ".key.pem") {

				// 提取文件名（如 uk.pem -> uk）
				prefix := strings.TrimSuffix(file.Name(), ".pem")

				certPath := filepath.Join(certDir, file.Name())
				// 假设配对的私钥文件名为 域名.key
				keyPath := filepath.Join(certDir, prefix+".key")

				// 尝试加载这一对证书
				pair, err := tls.LoadX509KeyPair(certPath, keyPath)
				if err != nil {
					loggz.WriteErrLog(fmt.Sprintf("跳过证书 [%s]: 加载失败 %v", prefix, err))
					continue
				}

				certs = append(certs, pair)
				loggz.WriteInfoLog(fmt.Sprintf("成功加载证书: %s", prefix))
			}
		}

		if len(certs) == 0 {
			loggz.WriteErrLog("错误: 未发现任何有效的证书对，HTTPS 将无法启动")
			return
		}

		// 3. 将证书切片配置到自定义的 Server 结构体中
		server := &http.Server{
			Addr:    ":443",
			Handler: mainHandler, // 你的主路由
			TLSConfig: &tls.Config{
				// Go 底层会利用 SNI 技术，根据客户端请求的域名自动寻找匹配的证书
				Certificates: certs,
			},
		}

		// 4. 启动服务（由于证书已经在 TLSConfig 中指明，这里的路径参数直接传空字符串）
		err = server.ListenAndServeTLS("", "")
		if err != nil && err != http.ErrServerClosed {
			loggz.WriteErrLog(fmt.Sprintf("HTTPS 启动失败: %v", err))
		}
	}()
}
