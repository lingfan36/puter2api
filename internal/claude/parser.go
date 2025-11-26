package claude

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"puter2api/internal/types"
)

// ParseToolCalls 解析文本中的工具调用
func ParseToolCalls(text string) ([]types.ParsedToolCall, string) {
	re := regexp.MustCompile(`(?s)<tool_call>\s*(\{.*?\})\s*</tool_call>`)
	matches := re.FindAllStringSubmatch(text, -1)

	var calls []types.ParsedToolCall
	remainingText := text

	for i, match := range matches {
		var call types.ParsedToolCall
		if err := json.Unmarshal([]byte(match[1]), &call); err == nil {
			if call.ID == "" {
				call.ID = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), i)
			}
			calls = append(calls, call)
			remainingText = strings.Replace(remainingText, match[0], "", 1)
		}
	}

	remainingText = strings.TrimSpace(remainingText)
	return calls, remainingText
}

