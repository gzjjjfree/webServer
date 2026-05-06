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
	"sync"
	"syscall"
	"time"

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

	var conf *Config
	if err := json.Unmarshal(confFile, &conf); err != nil {
		log.Fatalf("配置文件解析失败: %v", err)
	}

	// --------- 配置证书管理器 -------------
	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: GetRateLimitedHostPolicy(conf), // 使用我们定义的动态策略
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

// 记录域名上一次申请/校验的时间
var lastRequestTime sync.Map

func GetRateLimitedHostPolicy(conf *Config) autocert.HostPolicy {
	return func(ctx context.Context, host string) error {
		normalizedHost := strings.ToLower(strings.TrimSpace(host))

		// 1. 基本白名单校验 (你原有的逻辑)
		if conf.IsServers == "true" {
			isValid := false
			for _, s := range conf.Servers {
				if normalizedHost == strings.ToLower(s) {
					isValid = true
					break
				}
			}
			if !isValid {
				return fmt.Errorf("acme/autocert: host %q not allowed", host)
			}
		}

		// 2. 频率限制逻辑 (10分钟限制)
		now := time.Now()
		if val, ok := lastRequestTime.Load(normalizedHost); ok {
			lastTime := val.(time.Time)
			if now.Sub(lastTime) < 10*time.Minute {
				// 如果距离上次申请不足10分钟，拒绝触发 ACME 流程
				return fmt.Errorf("acme/autocert: rate limit exceeded for %q (10 min limit)", host)
			}
		}

		// 校验通过，更新该域名的最后访问时间
		lastRequestTime.Store(normalizedHost, now)
		return nil
	}
}
