package handler

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"time"

	"puter2api/internal/claude"
	"puter2api/internal/puter"
	"puter2api/internal/storage"
	"puter2api/internal/types"

	"github.com/gin-gonic/gin"
)

// Handler HTTP 处理器
type Handler struct {
	puterClient *puter.Client
	store       *storage.Storage
}

// NewHandler 创建处理器
func NewHandler(store *storage.Storage) *Handler {
	return &Handler{
		puterClient: puter.NewClient(),
		store:       store,
	}
}

// HandleMessages 处理 /v1/messages 请求
func (h *Handler) HandleMessages(c *gin.Context) {
	// 读取原始请求体用于调试
	bodyBytes, _ := c.GetRawData()
	log.Printf("原始请求体: %s", string(bodyBytes))

	// 重新设置请求体以便后续解析
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req types.ClaudeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("JSON 解析失败: %v", err)
		c.JSON(400, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": err.Error()},
		})
		return
	}

	hasTools := len(req.Tools) > 0
	log.Printf("请求: stream=%v, messages=%d, hasTools=%v", req.Stream, len(req.Messages), hasTools)

	// 从数据库获取可用的 Token
	tokenRecord, err := h.store.GetActiveToken()
	if err != nil {
		log.Printf("获取 Token 失败: %v", err)
		c.JSON(500, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": "failed to get token"},
		})
		return
	}
	if tokenRecord == nil {
		c.JSON(401, gin.H{
			"type":  "error",
			"error": gin.H{"type": "authentication_error", "message": "no active token available, please add a token first"},
		})
		return
	}

	token := tokenRecord.Token
	log.Printf("使用 Token: %s (ID: %d)", tokenRecord.Name, tokenRecord.ID)

	// 更新 Token 使用时间
	h.store.UpdateTokenUsed(tokenRecord.ID)

	// 构建 system prompt 和转换消息
	systemPrompt := claude.BuildSystemPrompt(req.System, req.Tools)
	messages := claude.ConvertMessages(req.Messages, systemPrompt)

	// 调用 Puter API
	responseText, err := h.puterClient.Call(messages, token)
	if err != nil {
		log.Printf("调用 Puter API 失败: %v", err)
		c.JSON(500, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": err.Error()},
		})
		return
	}

	// 解析工具调用
	toolCalls, remainingText := claude.ParseToolCalls(responseText)

	// 发送 SSE 响应
	h.sendSSEResponse(c, remainingText, toolCalls, len(responseText))
}

func (h *Handler) sendSSEResponse(c *gin.Context, text string, toolCalls []types.ParsedToolCall, totalLen int) {
	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	sse := claude.NewSSEWriter(c)

	// 1. message_start
	sse.SendMessageStart(msgID, "claude-opus-4-5")

	blockIndex := 0

	// 2. 发送文本块 (即使为空也要发送，否则 Claude Code 会报错)
	if text != "" || len(toolCalls) == 0 {
		sse.SendTextBlockStart(blockIndex)
		if text != "" {
			sse.SendTextDelta(blockIndex, text)
		}
		sse.SendBlockStop(blockIndex)
		blockIndex++
	}

	// 3. 发送工具调用块
	for _, call := range toolCalls {
		sse.SendToolUseBlockStart(blockIndex, call.ID, call.Name)
		sse.SendInputJSONDelta(blockIndex, string(call.Input))
		sse.SendBlockStop(blockIndex)
		blockIndex++
	}

	// 4. 确定 stop_reason
	stopReason := "end_turn"
	if len(toolCalls) > 0 {
		stopReason = "tool_use"
	}

	// 5. message_delta & message_stop
	sse.SendMessageDelta(stopReason, totalLen)
	sse.SendMessageStop()
}
