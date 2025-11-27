package puter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"puter2api/internal/types"
)

const (
	apiURL       = "https://api.puter.com/drivers/call"
	defaultModel = "claude-opus-4-5"
	logFile      = "log.log"
)

// Client Puter API 客户端
type Client struct {
	httpClient *http.Client
}

// NewClient 创建新的客户端
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// Call 调用 Puter API 并返回完整响应文本
func (c *Client) Call(messages []types.PuterMessage, authToken string) (string, error) {
	puterReq := types.PuterRequest{
		Interface: "puter-chat-completion",
		Driver:    "claude",
		TestMode:  false,
		Method:    "complete",
		Args: types.PuterArgs{
			Messages: messages,
			Model:    defaultModel,
			Stream:   true,
		},
		AuthToken: authToken,
	}

	body, _ := json.Marshal(puterReq)
	log.Printf("发送请求到 Puter API, messages=%d", len(messages))

	// 写入 curl 日志
	c.logCurl(body)

	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 检查 HTTP 状态码
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("puter API error: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	// 收集完整响应
	var fullText strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk types.PuterStreamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err == nil && chunk.Text != "" {
			fullText.WriteString(chunk.Text)
		}
	}

	responseText := fullText.String()
	log.Printf("收到响应: %d 字符", len(responseText))

	return responseText, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9")
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("content-type", "text/plain;actually=json")
	req.Header.Set("origin", "https://docs.puter.com")
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("referer", "https://docs.puter.com/")
	req.Header.Set("sec-ch-ua", `"Chromium";v="142", "Google Chrome";v="142", "Not_A Brand";v="99"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-site")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
}

// logCurl 将请求以 curl 格式写入日志文件
func (c *Client) logCurl(body []byte) {
	// 转义单引号
	bodyStr := strings.ReplaceAll(string(body), "'", "'\\''")

	curl := fmt.Sprintf(`# %s
curl --location '%s' \
--header 'accept: */*' \
--header 'accept-language: zh-CN,zh;q=0.9' \
--header 'cache-control: no-cache' \
--header 'content-type: text/plain;actually=json' \
--header 'origin: https://docs.puter.com' \
--header 'pragma: no-cache' \
--header 'priority: u=1, i' \
--header 'referer: https://docs.puter.com/' \
--header 'sec-ch-ua: "Chromium";v="142", "Google Chrome";v="142", "Not_A Brand";v="99"' \
--header 'sec-ch-ua-mobile: ?0' \
--header 'sec-ch-ua-platform: "macOS"' \
--header 'sec-fetch-dest: empty' \
--header 'sec-fetch-mode: cors' \
--header 'sec-fetch-site: same-site' \
--header 'user-agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36' \
--data '%s'

`, time.Now().Format("2006-01-02 15:04:05"), apiURL, bodyStr)

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("写入日志文件失败: %v", err)
		return
	}
	defer f.Close()

	f.WriteString(curl)
}
