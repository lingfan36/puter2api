package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"puter2api/internal/claude"
	"puter2api/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// HandleOpenAIChat 处理 /v1/chat/completions 请求 (OpenAI 兼容接口)
func (h *Handler) HandleOpenAIChat(c *gin.Context) {
	startTime := time.Now()

	// 读取原始请求体用于调试
	bodyBytes, _ := c.GetRawData()

	// 重新设置请求体以便后续解析
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req types.OpenAIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().Str("api", "OpenAI").Err(err).Msg("JSON 解析失败")
		c.JSON(400, gin.H{
			"error": gin.H{
				"message": err.Error(),
				"type":    "invalid_request_error",
				"code":    "invalid_request",
			},
		})
		return
	}

	hasTools := len(req.Tools) > 0
	lastMsgLen := len(req.Messages[len(req.Messages)-1].Content)
	log.Info().
		Str("api", "OpenAI").
		Str("model", req.Model).
		Bool("stream", req.Stream).
		Int("messages", len(req.Messages)).
		Bool("hasTools", hasTools).
		Int("last_msg_len", lastMsgLen).
		Msg("收到请求")

	// 从数据库获取可用的 Token
	tokenRecord, err := h.store.GetActiveToken()
	if err != nil {
		log.Error().Str("api", "OpenAI").Err(err).Msg("获取 Token 失败")
		c.JSON(500, gin.H{
			"error": gin.H{
				"message": "failed to get token",
				"type":    "api_error",
				"code":    "internal_error",
			},
		})
		return
	}
	if tokenRecord == nil {
		c.JSON(401, gin.H{
			"error": gin.H{
				"message": "no active token available, please add a token first",
				"type":    "authentication_error",
				"code":    "invalid_api_key",
			},
		})
		return
	}

	token := tokenRecord.Token
	log.Debug().Str("api", "OpenAI").Str("token", tokenRecord.Name).Int64("id", tokenRecord.ID).Msg("使用 Token")

	// 更新 Token 使用时间
	h.store.UpdateTokenUsed(tokenRecord.ID)

	// 转换 OpenAI 消息为 Puter 消息
	systemPrompt, messages := h.convertOpenAIMessages(req)
	puterMessages := claude.ConvertMessages(messages, systemPrompt)

	// 调用 Puter API
	responseText, err := h.puterClient.CallWithModel(puterMessages, token, req.Model)
	if err != nil {
		log.Error().Str("api", "OpenAI").Err(err).Msg("调用 Puter API 失败")
		c.JSON(500, gin.H{
			"error": gin.H{
				"message": err.Error(),
				"type":    "api_error",
				"code":    "internal_error",
			},
		})
		return
	}

	// 解析工具调用
	toolCalls, remainingText := claude.ParseToolCalls(responseText)

	// 发送响应
	if req.Stream {
		h.sendOpenAIStreamResponse(c, req.Model, remainingText, toolCalls)
	} else {
		h.sendOpenAINonStreamResponse(c, req.Model, remainingText, toolCalls)
	}

	// 记录完成日志
	elapsed := time.Since(startTime).Seconds()
	log.Info().
		Str("api", "OpenAI").
		Str("耗时", fmt.Sprintf("%.2fs", elapsed)).
		Int("响应长度", len(responseText)).
		Msg("请求完成")
}

// convertOpenAIMessages 转换 OpenAI 消息格式为内部格式
func (h *Handler) convertOpenAIMessages(req types.OpenAIRequest) (string, []types.ClaudeMessage) {
	var systemPrompt string
	var messages []types.ClaudeMessage

	// 处理工具定义，添加到 system prompt
	if len(req.Tools) > 0 {
		toolPrompt := "\n\n# Tools\n\nYou have access to the following tools. When you need to use a tool, output it in this EXACT format:\n\n<tool_call>\n{\"name\": \"tool_name\", \"input\": {\"param\": \"value\"}}\n</tool_call>\n\nAvailable tools:\n\n"
		for _, tool := range req.Tools {
			toolPrompt += fmt.Sprintf("## %s\n", tool.Function.Name)
			if tool.Function.Description != "" {
				toolPrompt += fmt.Sprintf("%s\n", tool.Function.Description)
			}
			if len(tool.Function.Parameters) > 0 {
				toolPrompt += fmt.Sprintf("Input schema: %s\n", string(tool.Function.Parameters))
			}
			toolPrompt += "\n"
		}
		systemPrompt = toolPrompt
	}

	for _, m := range req.Messages {
		if m.Role == "system" {
			// 提取 system 消息内容
			var content string
			if err := json.Unmarshal(m.Content, &content); err == nil {
				systemPrompt = content + systemPrompt
			} else {
				systemPrompt = string(m.Content) + systemPrompt
			}
			continue
		}

		// 转换消息
		claudeMsg := types.ClaudeMessage{
			Role: m.Role,
		}

		// 处理 tool 角色的消息
		if m.Role == "tool" {
			claudeMsg.Role = "user"
			var content string
			if err := json.Unmarshal(m.Content, &content); err != nil {
				content = string(m.Content)
			}
			toolResult := fmt.Sprintf("\n<tool_result id=\"%s\">\n%s\n</tool_result>\n", m.ToolCallID, content)
			claudeMsg.Content, _ = json.Marshal(toolResult)
		} else if len(m.ToolCalls) > 0 {
			// 处理 assistant 消息中的 tool_calls
			var content string
			if err := json.Unmarshal(m.Content, &content); err == nil && content != "" {
				// 有文本内容
			} else {
				content = ""
			}
			for _, tc := range m.ToolCalls {
				content += fmt.Sprintf("\n<tool_call>\n{\"name\": \"%s\", \"id\": \"%s\", \"input\": %s}\n</tool_call>\n", tc.Function.Name, tc.ID, tc.Function.Arguments)
			}
			claudeMsg.Content, _ = json.Marshal(content)
		} else {
			// 普通消息
			claudeMsg.Content = m.Content
		}

		messages = append(messages, claudeMsg)
	}

	return systemPrompt, messages
}

// sendOpenAIStreamResponse 发送 OpenAI 格式的流式响应
func (h *Handler) sendOpenAIStreamResponse(c *gin.Context, model string, text string, toolCalls []types.ParsedToolCall) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	msgID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	// 发送角色信息
	firstChunk := types.OpenAIResponse{
		ID:      msgID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []types.OpenAIChoice{
			{
				Index: 0,
				Delta: &types.OpenAIResponseMsg{
					Role: "assistant",
				},
				FinishReason: nil,
				Logprobs:     nil,
			},
		},
	}
	h.writeSSEChunk(c, firstChunk)

	// 发送文本内容（一次性发送）
	if text != "" {
		chunk := types.OpenAIResponse{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []types.OpenAIChoice{
				{
					Index: 0,
					Delta: &types.OpenAIResponseMsg{
						Content: &text,
					},
					FinishReason: nil,
					Logprobs:     nil,
				},
			},
		}
		h.writeSSEChunk(c, chunk)
	}

	// 发送工具调用
	if len(toolCalls) > 0 {
		for i, tc := range toolCalls {
			idx := i
			// 发送工具调用开始
			toolCallChunk := types.OpenAIResponse{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
				Choices: []types.OpenAIChoice{
					{
						Index: 0,
						Delta: &types.OpenAIResponseMsg{
							ToolCalls: []types.OpenAIToolCall{
								{
									Index: &idx,
									ID:    tc.ID,
									Type:  "function",
									Function: types.OpenAIToolCallFunction{
										Name:      tc.Name,
										Arguments: "",
									},
								},
							},
						},
						FinishReason: nil,
						Logprobs:     nil,
					},
				},
			}
			h.writeSSEChunk(c, toolCallChunk)

			// 一次性发送参数
			argsStr := string(tc.Input)
			argChunk := types.OpenAIResponse{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
				Choices: []types.OpenAIChoice{
					{
						Index: 0,
						Delta: &types.OpenAIResponseMsg{
							ToolCalls: []types.OpenAIToolCall{
								{
									Index: &idx,
									Function: types.OpenAIToolCallFunction{
										Arguments: argsStr,
									},
								},
							},
						},
						FinishReason: nil,
						Logprobs:     nil,
					},
				},
			}
			h.writeSSEChunk(c, argChunk)
		}
	}

	// 发送结束标记
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	finalChunk := types.OpenAIResponse{
		ID:      msgID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []types.OpenAIChoice{
			{
				Index:        0,
				Delta:        &types.OpenAIResponseMsg{},
				FinishReason: &finishReason,
				Logprobs:     nil,
			},
		},
	}
	h.writeSSEChunk(c, finalChunk)

	// 发送 [DONE]
	c.Writer.Write([]byte("data: [DONE]\n\n"))
	c.Writer.Flush()
}

// sendOpenAINonStreamResponse 发送 OpenAI 格式的非流式响应
func (h *Handler) sendOpenAINonStreamResponse(c *gin.Context, model string, text string, toolCalls []types.ParsedToolCall) {
	msgID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	var openaiToolCalls []types.OpenAIToolCall
	for _, tc := range toolCalls {
		openaiToolCalls = append(openaiToolCalls, types.OpenAIToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: types.OpenAIToolCallFunction{
				Name:      tc.Name,
				Arguments: string(tc.Input),
			},
		})
	}

	var contentPtr *string
	if text != "" {
		contentPtr = &text
	}

	resp := types.OpenAIResponse{
		ID:      msgID,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []types.OpenAIChoice{
			{
				Index: 0,
				Message: &types.OpenAIResponseMsg{
					Role:      "assistant",
					Content:   contentPtr,
					ToolCalls: openaiToolCalls,
				},
				FinishReason: &finishReason,
				Logprobs:     nil,
			},
		},
		Usage: &types.OpenAIUsage{
			PromptTokens:     0,
			CompletionTokens: len(text),
			TotalTokens:      len(text),
		},
	}

	c.JSON(200, resp)
}

// writeSSEChunk 写入 SSE 数据块
func (h *Handler) writeSSEChunk(c *gin.Context, data any) {
	jsonData, _ := json.Marshal(data)
	c.Writer.Write([]byte("data: "))
	c.Writer.Write(jsonData)
	c.Writer.Write([]byte("\n\n"))
	c.Writer.Flush()
}

// getModelProvider 根据模型 ID 判断提供商
func getModelProvider(id string) string {
	if strings.HasPrefix(id, "openrouter:") {
		return "openrouter"
	}
	if strings.HasPrefix(id, "togetherai:") {
		return "togetherai"
	}
	if strings.HasPrefix(id, "claude-") {
		return "anthropic"
	}
	if strings.HasPrefix(id, "gpt-") || strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") || strings.HasPrefix(id, "o4") {
		return "openai"
	}
	if strings.HasPrefix(id, "gemini-") {
		return "google"
	}
	if strings.HasPrefix(id, "grok-") {
		return "xai"
	}
	if strings.HasPrefix(id, "deepseek-") {
		return "deepseek"
	}
	if strings.HasPrefix(id, "mistral-") || strings.HasPrefix(id, "ministral-") || strings.HasPrefix(id, "open-mistral-") || strings.HasPrefix(id, "pixtral-") || strings.HasPrefix(id, "codestral-") || strings.HasPrefix(id, "devstral-") || strings.HasPrefix(id, "magistral-") {
		return "mistral"
	}
	return "other"
}

// HandleModels 处理 /v1/models 请求
func (h *Handler) HandleModels(c *gin.Context) {
	modelList := h.modelList

	models := make([]map[string]any, 0, len(modelList))
	for _, id := range modelList {
		models = append(models, map[string]any{
			"id":       id,
			"object":   "model",
			"created":  1700000000,
			"owned_by": getModelProvider(id),
		})
	}

	c.JSON(200, gin.H{
		"object": "list",
		"data":   models,
	})
}

// HandleImageGeneration 处理 /v1/images/generations 请求
func (h *Handler) HandleImageGeneration(c *gin.Context) {
	startTime := time.Now()

	var req struct {
		Model          string `json:"model"`
		Prompt         string `json:"prompt"`
		N              int    `json:"n"`
		Size           string `json:"size"`
		ResponseFormat string `json:"response_format"` // "url" or "b64_json"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	if req.Model == "" {
		req.Model = "dall-e-3"
	}
	if req.N == 0 {
		req.N = 1
	}
	if req.ResponseFormat == "" {
		req.ResponseFormat = "b64_json"
	}

	log.Info().Str("api", "ImageGen").Str("model", req.Model).Str("prompt", req.Prompt).Msg("收到请求")

	// 获取 Token
	tokenRecord, err := h.store.GetActiveToken()
	if err != nil || tokenRecord == nil {
		c.JSON(500, gin.H{"error": gin.H{"message": "no active token", "type": "api_error"}})
		return
	}
	h.store.UpdateTokenUsed(tokenRecord.ID)

	// 调用 Puter 图片生成
	respBytes, err := h.puterClient.CallImageGeneration(req.Prompt, req.Model, tokenRecord.Token)
	if err != nil {
		log.Error().Str("api", "ImageGen").Err(err).Msg("图片生成失败")
		c.JSON(500, gin.H{"error": gin.H{"message": err.Error(), "type": "api_error"}})
		return
	}

	// Puter 返回的可能是图片二进制数据或 JSON
	// 尝试解析为 JSON
	var puterResp map[string]any
	if err := json.Unmarshal(respBytes, &puterResp); err == nil {
		// JSON 响应，可能包含 url 或 base64
		if url, ok := puterResp["url"].(string); ok {
			c.JSON(200, gin.H{
				"created": time.Now().Unix(),
				"data": []gin.H{
					{"url": url},
				},
			})
			elapsed := time.Since(startTime).Seconds()
			log.Info().Str("api", "ImageGen").Str("耗时", fmt.Sprintf("%.2fs", elapsed)).Msg("完成")
			return
		}
		if b64, ok := puterResp["b64_json"].(string); ok {
			c.JSON(200, gin.H{
				"created": time.Now().Unix(),
				"data": []gin.H{
					{"b64_json": b64},
				},
			})
			elapsed := time.Since(startTime).Seconds()
			log.Info().Str("api", "ImageGen").Str("耗时", fmt.Sprintf("%.2fs", elapsed)).Msg("完成")
			return
		}
	}

	// 二进制图片数据 -> base64
	b64 := base64.StdEncoding.EncodeToString(respBytes)
	c.JSON(200, gin.H{
		"created": time.Now().Unix(),
		"data": []gin.H{
			{"b64_json": b64},
		},
	})

	elapsed := time.Since(startTime).Seconds()
	log.Info().Str("api", "ImageGen").Str("耗时", fmt.Sprintf("%.2fs", elapsed)).Msg("完成")
}

// HandleVideoGeneration 处理 /v1/videos/generations 请求
func (h *Handler) HandleVideoGeneration(c *gin.Context) {
	startTime := time.Now()

	var req struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		FPS    int    `json:"fps"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	if req.Prompt == "" {
		c.JSON(400, gin.H{"error": gin.H{"message": "prompt is required", "type": "invalid_request_error"}})
		return
	}
	if req.Model == "" {
		req.Model = "togetherai:minimax/hailuo-02"
	}

	log.Info().Str("api", "VideoGen").Str("model", req.Model).Str("prompt", req.Prompt).Msg("收到请求")

	// 获取 Token
	tokenRecord, err := h.store.GetActiveToken()
	if err != nil || tokenRecord == nil {
		c.JSON(500, gin.H{"error": gin.H{"message": "no active token", "type": "api_error"}})
		return
	}
	h.store.UpdateTokenUsed(tokenRecord.ID)

	// 调用 Puter 视频生成
	respBytes, err := h.puterClient.CallVideoGeneration(req.Prompt, req.Model, tokenRecord.Token, req.Width, req.Height, req.FPS)
	if err != nil {
		log.Error().Str("api", "VideoGen").Err(err).Msg("视频生成失败")
		c.JSON(500, gin.H{"error": gin.H{"message": err.Error(), "type": "api_error"}})
		return
	}

	// 尝试解析为 JSON（可能含 video_url 等）
	var puterResp map[string]any
	if err := json.Unmarshal(respBytes, &puterResp); err == nil {
		// 返回 Puter 的原始 JSON 响应，包装为统一格式
		c.JSON(200, gin.H{
			"created": time.Now().Unix(),
			"data":    puterResp,
		})
	} else {
		// 二进制视频数据，返回 base64
		b64 := base64.StdEncoding.EncodeToString(respBytes)
		c.JSON(200, gin.H{
			"created": time.Now().Unix(),
			"data": gin.H{
				"b64_video": b64,
			},
		})
	}

	elapsed := time.Since(startTime).Seconds()
	log.Info().Str("api", "VideoGen").Str("耗时", fmt.Sprintf("%.2fs", elapsed)).Msg("完成")
}
