package puter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"puter2api/internal/types"
)

const (
	apiURL = "https://api.puter.com/drivers/call"
)

// Client Puter API 客户端
type Client struct {
	httpClient *http.Client
	userAgent  string
}

// DriverInfo 驱动信息
type DriverInfo struct {
	Interface string
	Driver    string
	Model     string // 实际传给 Puter 的模型名
	Method    string
}

// NewClient 创建新的客户端
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				DisableKeepAlives:   false,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36",
	}
}

// ResolveDriver 根据模型 ID 确定 Puter 的 interface/driver/model
func ResolveDriver(modelID string) DriverInfo {
	// openrouter: 前缀
	if strings.HasPrefix(modelID, "openrouter:") {
		return DriverInfo{
			Interface: "puter-chat-completion",
			Driver:    "openrouter",
			Model:     strings.TrimPrefix(modelID, "openrouter:"),
			Method:    "complete",
		}
	}
	// togetherai: 前缀
	if strings.HasPrefix(modelID, "togetherai:") {
		innerModel := strings.TrimPrefix(modelID, "togetherai:")
		if isImageModel(innerModel) {
			return DriverInfo{
				Interface: "puter-image-generation",
				Driver:    "together-ai-image-generation",
				Model:     innerModel,
				Method:    "generate",
			}
		}
		if isVideoModel(innerModel) {
			return DriverInfo{
				Interface: "puter-video-generation",
				Driver:    "together",
				Model:     innerModel,
				Method:    "generate",
			}
		}
		return DriverInfo{
			Interface: "puter-chat-completion",
			Driver:    "together-ai",
			Model:     innerModel,
			Method:    "complete",
		}
	}
	// Claude 系列
	if strings.HasPrefix(modelID, "claude-") {
		return DriverInfo{
			Interface: "puter-chat-completion",
			Driver:    "claude",
			Model:     modelID,
			Method:    "complete",
		}
	}
	// OpenAI GPT / o 系列
	if strings.HasPrefix(modelID, "gpt-") || strings.HasPrefix(modelID, "o1") || strings.HasPrefix(modelID, "o3") || strings.HasPrefix(modelID, "o4") {
		return DriverInfo{
			Interface: "puter-chat-completion",
			Driver:    "openai-completion",
			Model:     modelID,
			Method:    "complete",
		}
	}
	// Gemini 系列
	if strings.HasPrefix(modelID, "gemini-") {
		return DriverInfo{
			Interface: "puter-chat-completion",
			Driver:    "gemini",
			Model:     modelID,
			Method:    "complete",
		}
	}
	// Grok 系列
	if strings.HasPrefix(modelID, "grok-") {
		return DriverInfo{
			Interface: "puter-chat-completion",
			Driver:    "xai",
			Model:     modelID,
			Method:    "complete",
		}
	}
	// DeepSeek 系列
	if strings.HasPrefix(modelID, "deepseek-") {
		return DriverInfo{
			Interface: "puter-chat-completion",
			Driver:    "deepseek",
			Model:     modelID,
			Method:    "complete",
		}
	}
	// Mistral 系列 (包括 codestral, devstral, pixtral, magistral, ministral)
	if strings.HasPrefix(modelID, "mistral-") || strings.HasPrefix(modelID, "ministral-") ||
		strings.HasPrefix(modelID, "open-mistral-") || strings.HasPrefix(modelID, "pixtral-") ||
		strings.HasPrefix(modelID, "codestral-") || strings.HasPrefix(modelID, "devstral-") ||
		strings.HasPrefix(modelID, "magistral-") {
		return DriverInfo{
			Interface: "puter-chat-completion",
			Driver:    "mistral",
			Model:     modelID,
			Method:    "complete",
		}
	}
	// 默认走 openai-completion
	return DriverInfo{
		Interface: "puter-chat-completion",
		Driver:    "openai-completion",
		Model:     modelID,
		Method:    "complete",
	}
}

// isImageModel 判断是否是图片生成模型
func isImageModel(model string) bool {
	lower := strings.ToLower(model)
	imageKeywords := []string{"flux", "stable-diffusion", "dall-e", "imagen", "hidream", "ideogram",
		"seedream", "juggernaut", "dreamshaper", "flash-image", "gemini-3-pro-image"}
	for _, kw := range imageKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isVideoModel 判断是否是视频生成模型
func isVideoModel(model string) bool {
	lower := strings.ToLower(model)
	videoKeywords := []string{"sora", "veo", "kling", "seedance", "wan2", "hailuo", "vidu", "pixverse"}
	for _, kw := range videoKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// Call 调用 Puter API 并返回完整响应文本
func (c *Client) Call(messages []types.PuterMessage, authToken string) (string, error) {
	return c.CallWithModel(messages, authToken, "claude-opus-4-5-20251001")
}

// CallWithModel 调用 Puter API 并返回完整响应文本（指定模型）
func (c *Client) CallWithModel(messages []types.PuterMessage, authToken string, model string) (string, error) {
	driver := ResolveDriver(model)

	puterReq := types.PuterRequest{
		Interface: driver.Interface,
		Driver:    driver.Driver,
		TestMode:  false,
		Method:    driver.Method,
		Args: types.PuterArgs{
			Messages: messages,
			Model:    driver.Model,
			Stream:   true,
		},
		AuthToken: authToken,
	}

	body, _ := json.Marshal(puterReq)
	startTime := time.Now()
	log.Printf("[Puter] 开始请求, model=%s, driver=%s, interface=%s, messages=%d", driver.Model, driver.Driver, driver.Interface, len(messages))

	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[Puter] 创建请求失败: %v", err)
		return "", err
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[Puter] 请求失败: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	// 检查 HTTP 状态码
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[Puter] API 错误: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
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
	elapsed := time.Since(startTime)
	log.Printf("[Puter] 请求完成, 耗时: %v, 响应: %d 字符", elapsed, len(responseText))

	return responseText, nil
}

// CallImageGeneration 调用 Puter 图片生成 API
func (c *Client) CallImageGeneration(prompt string, model string, authToken string) ([]byte, error) {
	driver := ResolveDriver(model)
	// 强制图片生成接口
	if driver.Interface != "puter-image-generation" {
		driver.Interface = "puter-image-generation"
		driver.Driver = "openai-image-generation"
		driver.Method = "generate"
	}

	reqBody := map[string]any{
		"interface": driver.Interface,
		"driver":    driver.Driver,
		"test_mode": false,
		"method":    driver.Method,
		"args": map[string]any{
			"prompt": prompt,
			"model":  driver.Model,
		},
		"auth_token": authToken,
	}

	body, _ := json.Marshal(reqBody)
	startTime := time.Now()
	log.Printf("[Puter] 图片生成请求, model=%s, driver=%s", driver.Model, driver.Driver)

	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		log.Printf("[Puter] 图片生成错误: status=%d, body=%s", resp.StatusCode, string(respBytes))
		return nil, fmt.Errorf("puter image API error: status=%d, body=%s", resp.StatusCode, string(respBytes))
	}

	elapsed := time.Since(startTime)
	log.Printf("[Puter] 图片生成完成, 耗时: %v, 响应: %d bytes", elapsed, len(respBytes))

	return respBytes, nil
}

// CallVideoGeneration 调用 Puter 视频生成 API
func (c *Client) CallVideoGeneration(prompt string, model string, authToken string, width, height, fps int) ([]byte, error) {
	driver := ResolveDriver(model)
	// 强制视频生成接口
	if driver.Interface != "puter-video-generation" {
		driver.Interface = "puter-video-generation"
		driver.Driver = "together"
		driver.Method = "generate"
	}

	args := map[string]any{
		"prompt": prompt,
		"model":  driver.Model,
	}
	if width > 0 {
		args["width"] = width
	}
	if height > 0 {
		args["height"] = height
	}
	if fps > 0 {
		args["fps"] = fps
	}

	reqBody := map[string]any{
		"interface":  driver.Interface,
		"driver":     driver.Driver,
		"test_mode":  false,
		"method":     driver.Method,
		"args":       args,
		"auth_token": authToken,
	}

	body, _ := json.Marshal(reqBody)
	startTime := time.Now()
	log.Printf("[Puter] 视频生成请求, model=%s, driver=%s", driver.Model, driver.Driver)

	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		log.Printf("[Puter] 视频生成错误: status=%d, body=%s", resp.StatusCode, string(respBytes))
		return nil, fmt.Errorf("puter video API error: status=%d, body=%s", resp.StatusCode, string(respBytes))
	}

	elapsed := time.Since(startTime)
	log.Printf("[Puter] 视频生成完成, 耗时: %v, 响应: %d bytes", elapsed, len(respBytes))

	return respBytes, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "text/plain;actually=json")
	req.Header.Set("DNT", "1")
	req.Header.Set("Origin", "https://docs.puter.com")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Referer", "https://docs.puter.com/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("sec-ch-ua", `"Chromium";v="142", "Google Chrome";v="142", "Not_A Brand";v="99"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-ch-ua-full-version-list", `"Chromium";v="142.0.7355.4", "Google Chrome";v="142.0.7355.4", "Not_A Brand";v="99.0.0.0"`)
	req.Header.Set("sec-ch-ua-arch", `"arm"`)
	req.Header.Set("sec-ch-ua-bitness", `"64"`)
	req.Header.Set("sec-ch-ua-model", `""`)
	req.Header.Set("sec-ch-ua-platform-version", `"15.0.0"`)
}
