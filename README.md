# model-auto-fallback

一个 **OpenAI 兼容协议的智能 Fallback 网关**，让你只需修改一行 baseurl，即可自动获得多 Provider 间的模型可用性探测与透明降级能力。

## 核心特性

- **零代码侵入**：用户只需改 baseurl，无需修改业务代码
- **透明代理转发**：请求/响应完全透明，用户无感知
- **动态模型发现**：通过 `/v1/models` 自动发现各 Provider 模型，无需静态配置
- **智能 Fallback**：三级降级策略（自定义链 > 名称相似度 > 跨家族降级）
- **SSE 流式透传**：完整支持 `stream: true` 模式
- **健康探测**：定时探测各 Provider 模型状态，缓存结果
- **多端点支持**：`/v1/chat/completions`、`/v1/completions`、`/v1/responses`

## 快速开始

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 配置

复制环境变量模板：

```bash
cp .env.example .env
```

编辑 `.env` 文件，填入你的 API Key：

```bash
QWEN_API_KEY=sk-your-qwen-key
ANTHROPIC_API_KEY=sk-ant-your-anthropic-key
DEEPSEEK_API_KEY=your-deepseek-key
```

编辑 `config.yaml`（可选，已有默认配置）：

```yaml
server:
  port: 8080

providers:
  - name: qwen
    baseurl: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${QWEN_API_KEY}

  - name: anthropic
    baseurl: https://api.anthropic.com/v1
    api_key: ${ANTHROPIC_API_KEY}

fallback:
  custom:
    - qwen-max
    - qwen-plus
    - claude-opus-4.6

probe:
  interval: 30s
  timeout: 10s
  cache_ttl: 60s
```

### 3. 启动

```bash
go run main.go
```

或指定配置文件：

```bash
go run main.go -config=/path/to/config.yaml
```

### 4. 使用

将你的 OpenAI SDK baseurl 指向本网关：

```python
# Python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="dummy"  # 网关会使用配置的 Provider API Key
)

response = client.chat.completions.create(
    model="qwen-max",
    messages=[{"role": "user", "content": "Hello"}]
)
```

```javascript
// Node.js
import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: 'http://localhost:8080/v1',
  apiKey: 'dummy'
});

const response = await client.chat.completions.create({
  model: 'qwen-max',
  messages: [{ role: 'user', content: 'Hello' }]
});
```

## 配置说明

### 配置优先级

配置项支持三级优先级（从高到低）：

1. **环境变量** - 直接在 shell 中设置
2. **.env 文件** - 项目根目录的 `.env` 文件
3. **YAML 配置** - `config.yaml` 中的原始值

示例：

```yaml
# config.yaml
providers:
  - name: qwen
    api_key: ${QWEN_API_KEY}  # 优先从环境变量读取，其次 .env，最后用 YAML 原值
```

### 配置字段

| 字段 | 类型 | 说明 | 默认值 |
|------|------|------|--------|
| `server.port` | int | 网关监听端口 | 8080 |
| `providers[].name` | string | Provider 标识 | - |
| `providers[].baseurl` | string | Provider 的 OpenAI 兼容 API 地址 | - |
| `providers[].api_key` | string | API Key，支持 `${ENV_VAR}` 引用 | - |
| `fallback.custom` | []string | 全局扁平降级链（可选） | [] |
| `fallback.global_priority` | []string | 跨家族降级时的 Provider 遍历顺序 | providers 配置顺序 |
| `probe.interval` | duration | 健康探测间隔 | 30s |
| `probe.timeout` | duration | 单次探测请求超时 | 10s |
| `probe.cache_ttl` | duration | 探测结果缓存有效期 | 60s |

## Fallback 机制

### 工作流程

```
用户请求 model: "qwen-max"
        │
        ▼
  查询探测缓存：qwen-max 是否可用？
        │
   ┌────┴────┐
   │  可用    │  不可用
   ▼         ▼
  直接转发   执行 Fallback 决策
  到 qwen     │
             ▼
           按 fallback 链找到第一个可用模型
           重写 request.model 字段
           转发到对应 Provider
```

### 三级 Fallback 策略

**优先级从高到低**：

#### 1. 用户自定义 fallback 链

配置文件中显式指定的全局降级顺序：

```yaml
fallback:
  custom:
    - qwen-max
    - qwen-plus
    - claude-opus-4.6
    - deepseek-v4
```

当用户请求的模型在链中时，从该位置向后查找第一个可用模型。

#### 2. 名称相似度匹配

按模型家族前缀匹配，同家族内版本降级：

```
qwen-max → qwen-plus → qwen-turbo
claude-opus-4.6 → claude-opus-4.5 → claude-sonnet-4.6
deepseek-v4 → deepseek-v3.2 → deepseek-v3
```

#### 3. 跨家族降级

同家族全部不可用时，按 `global_priority` 顺序降级到其他家族：

```yaml
fallback:
  global_priority:
    - anthropic
    - deepseek
    - qwen
```

### /v1/models 端点行为

`/v1/models` 端点**只返回当前可用的模型**（基于最近一次探测结果）：

- ✅ 探测成功（HTTP 200）的模型会出现在列表中
- ❌ 探测失败或超时的模型不会出现
- 🔄 每隔 `probe.interval` 自动刷新

这确保用户看到的模型列表始终是可用的，避免请求不可用模型。

## 健康探测

### 探测流程

```
定时器触发 (默认 30s)
        │
        ▼
  遍历所有配置的 Provider
        │
        ▼
  对每个 Provider 调用 /v1/models 获取模型列表
        │
        ▼
  对每个模型发起轻量探测请求
  (POST /v1/chat/completions, max_tokens=1)
        │
        ▼
  更新缓存：{model: {available: bool, latency: ms, probed_at: timestamp}}
```

### 探测结果缓存

- 探测结果缓存 TTL 默认 60s
- Fallback 决策时直接读缓存，无需等待探测
- 缓存命中时决策耗时 < 1ms

## 支持的端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/models` | GET | 返回所有可用模型列表 |
| `/v1/chat/completions` | POST | Chat Completions API，支持 `stream: true` |
| `/v1/completions` | POST | Completions API |
| `/v1/responses` | POST | Responses API（OpenAI 新协议），支持 `input`、`tools`、`stream` |
| `/v1/responses/{id}` | GET | 获取指定响应记录 |

## 兼容性

### 支持的 Provider

理论上支持所有提供 **OpenAI 兼容 API** 的 Provider：

- ✅ Anthropic (Claude)
- ✅ DeepSeek
- ✅ Qwen (通义千问)
- ✅ Groq
- ✅ OpenRouter
- ✅ 其他 OpenAI 兼容服务

### 支持的 SDK

- ✅ OpenAI Python SDK
- ✅ OpenAI Node.js SDK
- ✅ 任何支持自定义 baseurl 的 OpenAI 兼容客户端

## 架构设计

详细设计文档见 [docs/DESIGN.md](docs/DESIGN.md)

```
用户应用 (OpenAI SDK)
        │
        ▼
┌──────────────────────────────────────┐
│         model-auto-fallback          │
│                                      │
│  ┌────────────────────────────────┐  │
│  │     OpenAI 兼容 API 层          │  │
│  └───────────┬────────────────────┘  │
│              │                       │
│  ┌───────────▼────────────────────┐  │
│  │       Fallback 决策引擎         │  │
│  └───────────┬────────────────────┘  │
│              │                       │
│  ┌───────────▼────────────────────┐  │
│  │       探测管理器 (Prober)       │  │
│  └───────────┬────────────────────┘  │
│              │                       │
│  ┌───────────▼────────────────────┐  │
│  │       请求转发器 (Forwarder)    │  │
│  └────────────────────────────────┘  │
└──────────────────────────────────────┘
        │
        ▼
┌───────────────┐  ┌───────────────┐
│  Anthropic    │  │   DeepSeek    │
│  Provider     │  │   Provider    │
└───────────────┘  └───────────────┘
```

## 性能指标

- 代理引入的额外延迟 < 50ms (P99，不含 fallback 决策时的探测等待)
- 探测结果缓存命中时，fallback 决策耗时 < 1ms
- 支持至少 3 个 Provider 同时配置

## 限制与边界

### 范围内

- ✅ OpenAI 兼容端点透明代理
- ✅ SSE 流式透传
- ✅ 模型可用性探测
- ✅ Fallback 决策
- ✅ 动态模型发现

### 范围外

- ❌ 负载均衡（建议上层 nginx/k8s service）
- ❌ 速率限制（上层网关或 Provider 侧处理）
- ❌ 协议转换（要求所有 Provider 均为 OpenAI 兼容）
- ❌ 计费/用量统计（各 Provider 后台查看）
- ❌ 认证管理（透传用户请求中的 Key）

## 故障排查

### 问题：启动时报错 "failed to load config"

**原因**：配置文件路径错误或格式错误

**解决**：
```bash
# 检查配置文件是否存在
ls config.yaml

# 验证 YAML 格式
go run main.go -config=config.yaml
```

### 问题：所有请求返回 503 "no available model found"

**原因**：所有 Provider 探测失败

**解决**：
1. 检查 API Key 是否正确配置
2. 检查网络连接（能否访问 Provider API）
3. 查看日志中的探测错误信息

### 问题：Fallback 没有生效

**原因**：首选模型实际可用，或 fallback 链配置错误

**解决**：
1. 检查 `/v1/models` 端点，确认首选模型是否在列表中
2. 检查 `fallback.custom` 配置是否包含首选模型
3. 查看日志中的 fallback 决策信息

## 开发

### 构建

```bash
go build -o model-auto-fallback main.go
```

### 运行测试

```bash
go test ./...
```

### 项目结构

```
.
├── main.go                    # 程序入口
├── config.yaml                # 配置文件
├── .env.example               # 环境变量模板
├── internal/
│   ├── config/                # 配置管理
│   ├── prober/                # 模型探测器
│   ├── fallback/              # Fallback 决策引擎
│   ├── forwarder/             # 请求转发器
│   └── proxy/                 # HTTP 路由处理
└── docs/
    └── DESIGN.md              # 详细设计文档
```

## License

MIT
