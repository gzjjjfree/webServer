package main

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/fsnotify/fsnotify"
	"github.com/gzjjjfree/loggz"
)

// # 请在此处填入你的 Server酱 SendKey，微信方糖

// var SEND_KEY = ”your_key_here“

// 请将 SEND_KEY.example.go 改名为 SEND_KEY.go

// WatchConfig 监听文件夹变动
func (cm *CertManager) WatchConfig() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// 监听写入和创建事件
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					loggz.WriteInfoLog("检测到证书文件变动，正在重新加载...")
					go SendToWechat(sendKey, "证书文件变动", "检测到证书文件变动，正在重新加载...")
					cm.LoadCerts()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				loggz.WriteInfoLog(fmt.Sprint("监听错误:", err))
			}
		}
	}()

	err = watcher.Add(cm.certDir)
	if err != nil {
		panic(err)
	}
}

// SendToWechat: 发送消息到微信
func SendToWechat(key string, title string, content string) {
	// 对标题和内容进行编码，处理空格和换行
	safeTitle := url.QueryEscape(title)
	safeContent := url.QueryEscape(content)
	apiURL := fmt.Sprintf("https://sctapi.ftqq.com/%s.send?title=%s&desp=%s", key, safeTitle, safeContent)

	resp, err := http.Get(apiURL)
	if err != nil {
		loggz.WriteInfoLog(fmt.Sprintf("发送失败: %v", err))
		return
	}
	defer resp.Body.Close()
}
