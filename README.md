# model-auto-fallback

[English](#english) | [中文](#中文)

---

## English

An **OpenAI-compatible intelligent fallback gateway** that automatically detects model availability and transparently falls back across multiple providers. Just change one line of baseurl, and your AI application gets cross-vendor high availability with zero code changes.

### Why You Need This

**Scenario 1: Free Token Management**

Many AI platforms offer free tokens for new users:
- Alibaba Cloud DashScope: 1M free tokens for qwen-max
- DeepSeek: Free trial credits
- Multiple providers with promotional quotas

**The Problem**: When your free tokens run out on one provider, your application stops working. You have to:
1. Manually switch API keys in your code
2. Update baseurl configurations
3. Redeploy your application
4. Hope you remember to do this before your app breaks

**The Solution**: This gateway automatically detects when a model is unavailable (quota exceeded, rate limited, or down) and seamlessly switches to your backup provider. Your application keeps running without interruption.

**Scenario 2: Cost Optimization**

Different providers have different pricing:
- Use cheap/free models first (qwen-turbo, deepseek-chat)
- Automatically fallback to premium models when needed
- Maximize your free tier usage across multiple platforms

**Scenario 3: High Availability**

Production applications can't afford downtime:
- Primary provider has an outage? Automatically switch to backup
- Rate limited? Fallback to alternative provider
- Model deprecated? Seamlessly migrate to newer version

### Real-World Example

```python
# Your existing code - NO CHANGES NEEDED
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080",  # Just point to the gateway
    api_key="dummy"  # Gateway handles provider keys
)

# This request will automatically:
# 1. Try qwen-max (free tier)
# 2. If quota exceeded → fallback to qwen-plus
# 3. If still unavailable → fallback to deepseek-v4
# 4. Your app never knows the difference!
response = client.chat.completions.create(
    model="qwen-max",
    messages=[{"role": "user", "content": "Hello"}]
)
```

### Key Features

- **Zero Code Changes**: Just change baseurl, that's it
- **Transparent Proxy**: Requests and responses are completely transparent
- **Smart Fallback**: 3-tier fallback strategy (custom chain → name similarity → cross-family)
- **Dynamic Discovery**: Automatically discovers models via `/v1/models`, no static config needed
- **Health Monitoring**: Periodic health checks with cached results
- **SSE Streaming**: Full support for `stream: true` mode
- **Multi-Endpoint**: Supports `/v1/chat/completions`, `/v1/completions`, `/v1/responses`

### Quick Start

#### 1. Install Dependencies

```bash
go mod tidy
```

#### 2. Configure

Copy environment template:

```bash
cp .env.example .env
```

Edit `.env` with your API keys:

```bash
# Use your free trial keys from different platforms
QWEN_API_KEY=sk-your-qwen-key
DEEPSEEK_API_KEY=your-deepseek-key
ANTHROPIC_API_KEY=sk-ant-your-anthropic-key
```

Edit `config.yaml` (optional):

```yaml
server:
  port: 8080

providers:
  # Free tier provider (try first)
  - name: qwen
    baseurl: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${QWEN_API_KEY}

  # Backup provider
  - name: deepseek
    baseurl: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}

fallback:
  # Custom fallback chain (optional)
  custom:
    - qwen-max        # Try free tier first
    - qwen-plus       # Then paid tier
    - deepseek-v4     # Finally backup provider

probe:
  interval: 30s       # Check health every 30s
  timeout: 10s
  cache_ttl: 60s
```

#### 3. Start Gateway

```bash
go run main.go
```

#### 4. Use in Your Application

**Python:**
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080",
    api_key="dummy"
)

response = client.chat.completions.create(
    model="qwen-max",
    messages=[{"role": "user", "content": "Hello"}]
)
```

**Node.js:**
```javascript
import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: 'http://localhost:8080',
  apiKey: 'dummy'
});

const response = await client.chat.completions.create({
  model: 'qwen-max',
  messages: [{ role: 'user', content: 'Hello' }]
});
```

**cURL:**
```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen-max",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### Configuration Priority

Configuration supports 3-tier priority (high to low):

1. **Environment Variables** - Set directly in shell
2. **.env File** - Project root `.env` file
3. **YAML Config** - Original values in `config.yaml`

### Fallback Mechanism

#### How It Works

```
User requests model: "qwen-max"
        │
        ▼
  Check probe cache: is qwen-max available?
        │
   ┌────┴────┐
   │ Yes     │ No
   ▼         ▼
  Forward    Execute Fallback
  directly   │
             ▼
           Find first available model in fallback chain
           Rewrite request.model field
           Forward to corresponding provider
```

#### 3-Tier Fallback Strategy

**Priority (high to low):**

##### 1. Custom Fallback Chain

Explicitly specified in config:

```yaml
fallback:
  custom:
    - qwen-max
    - qwen-plus
    - claude-opus-4.6
    - deepseek-v4
```

##### 2. Name Similarity Matching

Match by model family prefix, version downgrade within same family:

```
qwen-max → qwen-plus → qwen-turbo
claude-opus-4.6 → claude-opus-4.5 → claude-sonnet-4.6
deepseek-v4 → deepseek-v3.2 → deepseek-v3
```

##### 3. Cross-Family Fallback

When entire family unavailable, fallback by `global_priority`:

```yaml
fallback:
  global_priority:
    - qwen
    - deepseek
    - anthropic
```

### Use Cases

#### Use Case 1: Maximize Free Tiers

```yaml
providers:
  - name: qwen
    baseurl: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${QWEN_API_KEY}  # 1M free tokens

  - name: deepseek
    baseurl: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}  # Free trial

fallback:
  custom:
    - qwen-max      # Use free tier first
    - deepseek-chat # Fallback to another free tier
```

**Result**: Your app uses qwen-max's 1M free tokens first. When exhausted, automatically switches to deepseek's free tier. No manual intervention needed!

#### Use Case 2: Cost Optimization

```yaml
fallback:
  custom:
    - qwen-turbo      # Cheapest ($0.3/M tokens)
    - qwen-plus       # Mid-tier ($4/M tokens)
    - qwen-max        # Premium ($20/M tokens)
```

**Result**: Always use cheapest model first, only upgrade when unavailable.

#### Use Case 3: Production High Availability

```yaml
providers:
  - name: primary
    baseurl: https://api.primary-provider.com/v1
    api_key: ${PRIMARY_KEY}

  - name: backup1
    baseurl: https://api.backup1.com/v1
    api_key: ${BACKUP1_KEY}

  - name: backup2
    baseurl: https://api.backup2.com/v1
    api_key: ${BACKUP2_KEY}

fallback:
  global_priority:
    - primary
    - backup1
    - backup2
```

**Result**: Primary provider down? Automatically switch to backup. Your users never notice.

### Supported Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/models` | GET | List all available models |
| `/v1/chat/completions` | POST | Chat Completions API, supports `stream: true` |
| `/v1/completions` | POST | Completions API |
| `/v1/responses` | POST | Responses API (OpenAI new protocol) |
| `/v1/responses/{id}` | GET | Get specific response record |

### Compatible Providers

Theoretically supports all **OpenAI-compatible API** providers:

- ✅ Alibaba Cloud Qwen (DashScope)
- ✅ DeepSeek
- ✅ Anthropic (Claude)
- ✅ Groq
- ✅ OpenRouter
- ✅ Together AI
- ✅ Any other OpenAI-compatible service

### Performance

- Proxy overhead < 50ms (P99, excluding fallback probe wait)
- Fallback decision < 1ms when cache hit
- Supports at least 3 providers simultaneously

### Troubleshooting

#### Problem: All requests return 503 "no available model found"

**Cause**: All providers failed health check

**Solution**:
1. Check if API keys are correctly configured
2. Check network connectivity (can you reach provider APIs?)
3. View probe errors in gateway logs

#### Problem: Fallback not working

**Cause**: Preferred model is actually available, or fallback chain misconfigured

**Solution**:
1. Check `/v1/models` endpoint to confirm if preferred model is in list
2. Check if `fallback.custom` config includes preferred model
3. View fallback decision info in gateway logs

### Development

#### Build

```bash
go build -o model-auto-fallback main.go
```

#### Run Tests

```bash
cd test
go test -v
```

#### Project Structure

```
.
├── main.go                    # Entry point
├── config.yaml                # Configuration
├── .env.example               # Environment template
├── internal/
│   ├── config/                # Config management
│   ├── prober/                # Model health checker
│   ├── fallback/              # Fallback decision engine
│   ├── forwarder/             # Request forwarder
│   └── proxy/                 # HTTP routing
└── docs/
    └── DESIGN.md              # Detailed design doc
```

### License

MIT

---

## 中文

一个 **OpenAI 兼容协议的智能 Fallback 网关**，自动检测模型可用性并在多个供应商之间透明降级。只需修改一行 baseurl，你的 AI 应用就能获得跨厂商高可用保障，无需任何代码改动。

### 为什么需要这个项目

**场景 1：免费额度管理**

许多 AI 平台为新用户提供免费额度：
- 阿里云百炼（DashScope）：qwen-max 100 万 tokens 免费额度
- DeepSeek：免费试用额度
- 多个平台的促销配额

**问题**：当某个平台的免费额度用完后，你的应用就停止工作了。你必须：
1. 手动在代码中切换 API Key
2. 更新 baseurl 配置
3. 重新部署应用
4. 希望在应用崩溃前记得做这件事

**解决方案**：本网关自动检测模型不可用（配额耗尽、限流或宕机），无缝切换到备用供应商。你的应用持续运行，不会中断。

**场景 2：成本优化**

不同供应商的定价不同：
- 优先使用便宜/免费的模型（qwen-turbo、deepseek-chat）
- 需要时自动降级到高级模型
- 最大化利用多个平台的免费额度

**场景 3：高可用性**

生产环境的应用不能承受停机：
- 主供应商故障？自动切换到备用
- 被限流？降级到替代供应商
- 模型下线？无缝迁移到新版本

### 真实案例

```python
# 你现有的代码 - 无需任何改动
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080",  # 只需指向网关
    api_key="dummy"  # 网关处理供应商密钥
)

# 这个请求会自动：
# 1. 尝试 qwen-max（免费额度）
# 2. 如果配额耗尽 → 降级到 qwen-plus
# 3. 如果仍不可用 → 降级到 deepseek-v4
# 4. 你的应用完全无感知！
response = client.chat.completions.create(
    model="qwen-max",
    messages=[{"role": "user", "content": "你好"}]
)
```

### 核心特性

- **零代码改动**：只需改 baseurl，就这么简单
- **透明代理**：请求和响应完全透明
- **智能降级**：三级降级策略（自定义链 → 名称相似度 → 跨家族）
- **动态发现**：通过 `/v1/models` 自动发现模型，无需静态配置
- **健康监控**：定期健康检查，结果缓存
- **SSE 流式**：完整支持 `stream: true` 模式
- **多端点**：支持 `/v1/chat/completions`、`/v1/completions`、`/v1/responses`

### 快速开始

#### 1. 安装依赖

```bash
go mod tidy
```

#### 2. 配置

复制环境变量模板：

```bash
cp .env.example .env
```

编辑 `.env` 填入你的 API Key：

```bash
# 使用不同平台的免费试用密钥
QWEN_API_KEY=sk-your-qwen-key
DEEPSEEK_API_KEY=your-deepseek-key
ANTHROPIC_API_KEY=sk-ant-your-anthropic-key
```

编辑 `config.yaml`（可选）：

```yaml
server:
  port: 8080

providers:
  # 免费额度供应商（优先尝试）
  - name: qwen
    baseurl: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${QWEN_API_KEY}

  # 备用供应商
  - name: deepseek
    baseurl: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}

fallback:
  # 自定义降级链（可选）
  custom:
    - qwen-max        # 先尝试免费额度
    - qwen-plus       # 然后付费版
    - deepseek-v4     # 最后备用供应商

probe:
  interval: 30s       # 每 30 秒检查健康状态
  timeout: 10s
  cache_ttl: 60s
```

#### 3. 启动网关

```bash
go run main.go
```

#### 4. 在应用中使用

**Python:**
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080",
    api_key="dummy"
)

response = client.chat.completions.create(
    model="qwen-max",
    messages=[{"role": "user", "content": "你好"}]
)
```

**Node.js:**
```javascript
import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: 'http://localhost:8080',
  apiKey: 'dummy'
});

const response = await client.chat.completions.create({
  model: 'qwen-max',
  messages: [{ role: 'user', content: '你好' }]
});
```

**cURL:**
```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen-max",
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

### 配置优先级

配置支持三级优先级（从高到低）：

1. **环境变量** - 直接在 shell 中设置
2. **.env 文件** - 项目根目录的 `.env` 文件
3. **YAML 配置** - `config.yaml` 中的原始值

### Fallback 机制

#### 工作流程

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
             │
             ▼
           按 fallback 链找到第一个可用模型
           重写 request.model 字段
           转发到对应 Provider
```

#### 三级 Fallback 策略

**优先级从高到低：**

##### 1. 用户自定义降级链

配置文件中显式指定：

```yaml
fallback:
  custom:
    - qwen-max
    - qwen-plus
    - claude-opus-4.6
    - deepseek-v4
```

##### 2. 名称相似度匹配

按模型家族前缀匹配，同家族内版本降级：

```
qwen-max → qwen-plus → qwen-turbo
claude-opus-4.6 → claude-opus-4.5 → claude-sonnet-4.6
deepseek-v4 → deepseek-v3.2 → deepseek-v3
```

##### 3. 跨家族降级

同家族全部不可用时，按 `global_priority` 降级：

```yaml
fallback:
  global_priority:
    - qwen
    - deepseek
    - anthropic
```

### 使用场景

#### 场景 1：最大化免费额度

```yaml
providers:
  - name: qwen
    baseurl: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${QWEN_API_KEY}  # 100 万 tokens 免费

  - name: deepseek
    baseurl: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}  # 免费试用

fallback:
  custom:
    - qwen-max      # 先用免费额度
    - deepseek-chat # 降级到另一个免费额度
```

**效果**：应用先使用 qwen-max 的 100 万免费 tokens。用完后自动切换到 deepseek 的免费额度。无需手动干预！

#### 场景 2：成本优化

```yaml
fallback:
  custom:
    - qwen-turbo      # 最便宜（¥2/百万 tokens）
    - qwen-plus       # 中等（¥28/百万 tokens）
    - qwen-max        # 高级（¥140/百万 tokens）
```

**效果**：始终优先使用最便宜的模型，只在不可用时才升级。

#### 场景 3：生产环境高可用

```yaml
providers:
  - name: primary
    baseurl: https://api.primary-provider.com/v1
    api_key: ${PRIMARY_KEY}

  - name: backup1
    baseurl: https://api.backup1.com/v1
    api_key: ${BACKUP1_KEY}

  - name: backup2
    baseurl: https://api.backup2.com/v1
    api_key: ${BACKUP2_KEY}

fallback:
  global_priority:
    - primary
    - backup1
    - backup2
```

**效果**：主供应商宕机？自动切换到备用。用户完全无感知。

### 支持的端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/models` | GET | 列出所有可用模型 |
| `/v1/chat/completions` | POST | Chat Completions API，支持 `stream: true` |
| `/v1/completions` | POST | Completions API |
| `/v1/responses` | POST | Responses API（OpenAI 新协议） |
| `/v1/responses/{id}` | GET | 获取指定响应记录 |

### 兼容的供应商

理论上支持所有 **OpenAI 兼容 API** 的供应商：

- ✅ 阿里云通义千问（百炼 DashScope）
- ✅ DeepSeek
- ✅ Anthropic (Claude)
- ✅ Groq
- ✅ OpenRouter
- ✅ Together AI
- ✅ 其他任何 OpenAI 兼容服务

### 性能指标

- 代理额外延迟 < 50ms（P99，不含 fallback 探测等待）
- 缓存命中时 fallback 决策 < 1ms
- 支持至少 3 个供应商同时配置

### 故障排查

#### 问题：所有请求返回 503 "no available model found"

**原因**：所有供应商探测失败

**解决**：
1. 检查 API Key 是否正确配置
2. 检查网络连接（能否访问供应商 API）
3. 查看网关日志中的探测错误

#### 问题：Fallback 没有生效

**原因**：首选模型实际可用，或 fallback 链配置错误

**解决**：
1. 检查 `/v1/models` 端点，确认首选模型是否在列表中
2. 检查 `fallback.custom` 配置是否包含首选模型
3. 查看网关日志中的 fallback 决策信息

### 开发

#### 构建

```bash
go build -o model-auto-fallback main.go
```

#### 运行测试

```bash
cd test
go test -v
```

#### 项目结构

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

### 许可证

MIT
