```
curl 'https://puter.com/puterai/chat/models' \
  -H 'Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7' \
  -H 'Accept-Language: zh-CN,zh;q=0.9' \
  -H 'Cache-Control: no-cache' \
  -H 'Connection: keep-alive' \
  -b '_clck=trp08g%5E2%5Eg1c%5E0%5E2156; __stripe_mid=1ae8b1e7-34aa-4d9c-a0bc-e00348bd4e36e7b24e; __stripe_sid=937ef572-58cc-46d3-bc2e-dbac15385aa77e545a; puter_auth_token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ0IjoicyIsInYiOiIwLjAuMCIsInUiOiJaMmZNZ2FzOFJSeXhSS3M3S1FuWnpnPT0iLCJ1dSI6Im02UmZFbkEzU0VpUjk0TVZadnJYZkE9PSIsImlhdCI6MTc2NDE3MTc0OH0.2pC0C8jAvpFiUpOcVza1V7uCnnfVza9kBLX2p5PcFyw; _clsk=1ydouus%5E1764171905602%5E6%5E1%5Ei.clarity.ms%2Fcollect' \
  -H 'Pragma: no-cache' \
  -H 'Sec-Fetch-Dest: document' \
  -H 'Sec-Fetch-Mode: navigate' \
  -H 'Sec-Fetch-Site: none' \
  -H 'Sec-Fetch-User: ?1' \
  -H 'Upgrade-Insecure-Requests: 1' \
  -H 'User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36' \
  -H 'sec-ch-ua: "Chromium";v="142", "Google Chrome";v="142", "Not_A Brand";v="99"' \
  -H 'sec-ch-ua-mobile: ?0' \
  -H 'sec-ch-ua-platform: "macOS"'
```

## 对 Puter.com 的请求

### 1. 获取模型列表
- **URL**: `https://puter.com/puterai/chat/models`
- **方法**: GET
- **响应**: 
```json
{
  "models": ["model-id-1", "model-id-2", ...]
}
```

### 2. 调用 AI 驱动
- **URL**: `https://api.puter.com/drivers/call`
- **方法**: POST
- **请求头**:
  - `Host`: api.puter.com
  - `Authorization`: Bearer {JWT_TOKEN}
  - `Content-Type`: application/json;charset=UTF-8
  - `Origin`: https://docs.puter.com
  - `Referer`: https://docs.puter.com/

- **请求体**:
```json
{
  "interface": "puter-chat-completion",
  "driver": "openai-completion|deepseek|xai|claude|mistral",
  "test_mode": false,
  "method": "complete",
  "args": {
    "messages": [...],
    "model": "model-name",
    "stream": true|false
  }
}
```

- **响应** (非流式):
```json
{
  "result": {
    "message": {
      "content": "text" | [{"type": "text", "text": "..."}]
    },
    "usage": {
      "input_tokens": 0,
      "output_tokens": 0
    }
  }
}
```

- **响应** (流式): 每行一个 JSON 对象
```json
{"text": "content"}
```
或
```json
{"result": {"message": {"content": "text" | [...]}}}
```

## API 端点

### GET /v1/models
获取可用模型列表

**响应格式**:
```json
{
  "object": "list",
  "data": [
    {
      "id": "model-id",
      "object": "model",
      "created": 1234567890,
      "owned_by": "openai|deepseek|xai|anthropic|mistral|unknown"
    }
  ]
}
```

### POST /v1/chat/completions
创建聊天补全

**请求头**:
- `Authorization`: Bearer {AUTH_TOKEN}

**请求体**:
```json
{
  "model": "model-name",
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "stream": false
}
```

**响应格式** (非流式):
```json
{
  "id": "chatcmpl-1234567890",
  "object": "chat.completion",
  "created": 1234567890,
  "model": "model-name",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "响应内容"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 20,
    "total_tokens": 30
  }
}
```

**响应格式** (流式 - SSE):
```
data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":xxx,"model":"xxx","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":xxx,"model":"xxx","choices":[{"index":0,"delta":{"content":"内容"},"finish_reason":null}]}

data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":xxx,"model":"xxx","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

## 环境变量

- `JWT_TOKEN`: Puter API 的 JWT 令牌（多个用逗号分隔）
- `AUTH_TOKEN`: 本服务的认证令牌（多个用逗号分隔）
- `PORT`: 服务端口（默认 8001）

## 驱动映射

- `deepseek` → deepseek
- `grok` → xai
- `claude` → claude
- `mistral/codestral` → mistral
- 其他 → openai-completion

