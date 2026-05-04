package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/crypto/acme/autocert"
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
	V2rayPort   string   `json:"v2rayPort"`
	GetApiPort  string   `json:"getApiPort"`
	PostApiPort string   `json:"postApiPort"`
	GrpcApiPort string   `json:"grpcApiPort"`
	IsWs        string   `json:"isWs"`
	IsServers   string   `json:"isServers"`
	Servers     []string `json:"servers"`
}

// 辅助函数：创建反向代理
func newProxy(port string) *httputil.ReverseProxy {
	target := fmt.Sprintf("http://127.0.0.1:%s", port)
	url, _ := url.Parse(target)
	return httputil.NewSingleHostReverseProxy(url)
}

func RegisterHostRouter() {
	// 读取并解析配置文件
	confFile, err := os.ReadFile("server_conf.json")
	if err != nil {
		log.Fatalf("无法读取配置文件: %v", err)
	}

	var conf Config
	if err := json.Unmarshal(confFile, &conf); err != nil {
		log.Fatalf("配置文件解析失败: %v", err)
	}

	// 定义动态 Host 策略
	// 这个函数会在接收到新的 HTTPS 请求时被调用，决定是否为该域名申请证书
	dynamicHostPolicy := func(ctx context.Context, host string) error {
		if conf.IsServers != "true" {
			return nil // 如果不开启限制，允许所有指向本机的域名申请
		}

		for _, s := range conf.Servers {
			if host == s {
				return nil // 在白名单内，准许申请
			}
		}
		return fmt.Errorf("acme/autocert: host %q not configured in server_conf.json", host)
	}

	// --------- 配置证书管理器 -------------
	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: dynamicHostPolicy, // 使用我们定义的动态策略
		Cache:      autocert.DirCache("certs"),
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
			log.Printf("代理转发错误: %v", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	getProxy := newProxy(conf.GetApiPort)
	postProxy := newProxy(conf.PostApiPort)
	grpcProxy := newProxy(conf.GrpcApiPort)

	// 静态文件服务器
	fs := http.FileServer(http.Dir("./html"))

	// 3. 核心路由逻辑
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 域名校验：检查当前访问的 Host 是否在配置的 servers 列表中
		// 备注：如果希望所有指向该 IP 的域名都可用，可以跳过此检查
		if conf.IsServers == "true" {
			isValidServer := false
			requestHost := strings.Split(r.Host, ":")[0] // 去掉端口号
			for _, s := range conf.Servers {
				if requestHost == s {
					isValidServer = true
					break
				}
			}

			if !isValidServer {
				http.Error(w, "Access Denied: Unrecognized Host", http.StatusForbidden)
				return
			}
		}

		// 路径规则匹配
		switch {
		case r.URL.Path == conf.IsWs:
			if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
				v2rayProxy.ServeHTTP(w, r)
			} else {
				http.Error(w, "Not a WebSocket request", http.StatusBadRequest)
			}
		case r.URL.Path == "/getapi":
			getProxy.ServeHTTP(w, r)
		case r.URL.Path == "/postapi":
			postProxy.ServeHTTP(w, r)
		case r.URL.Path == "/grpcapi":
			grpcProxy.ServeHTTP(w, r)
		default:
			fs.ServeHTTP(w, r)
		}
	})

	// 开启 80 端口重定向
	go func() {
		log.Println("正在监听 80 端口进行证书验证和跳转...")
		// 这里的第二个参数填 nil 表示自动处理跳转，或者填你的 router 兼容非加密访问
		err := http.ListenAndServe(":80", certManager.HTTPHandler(nil))
		if err != nil {
			log.Fatalf("80端口启动失败: %v", err)
		}
	}()

	log.Println("服务器正在启动，监听 80 端口...")

	// 启动 HTTPS 服务 (端口 443)
	server := &http.Server{
		Addr:      ":443",
		Handler:   mainHandler,
		TLSConfig: certManager.TLSConfig(),
	}

	log.Println("HTTPS 服务器启动成功，监听 443 端口...")
	// ListenAndServeTLS 第二个和第三个参数留空，因为 certManager 会提供证书
	go func() {
		log.Println("HTTPS 服务器启动中...")
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTPS 启动失败: %v", err)
		}
	}()
}
