# model-auto-fallback 设计文档

## 一、项目目标

构建一个 **OpenAI 兼容协议的智能 Fallback 网关**，让用户只需将 baseurl 指向本项目，即可自动获得多 Provider 间的模型可用性探测与透明降级能力。

**核心价值**：用户改一行 baseurl，零代码侵入，即可获得跨厂商的模型高可用保障。

## 二、方案

### 2.1 整体架构

```
用户应用 (OpenAI SDK / HTTP Client)
        │
        │  baseurl = http://localhost:8080
        ▼
┌──────────────────────────────────────┐
│         model-auto-fallback          │
│                                      │
│  ┌────────────────────────────────┐  │
│  │     OpenAI 兼容 API 层          │  │
│  │  /v1/models                    │  │
│  │  /v1/chat/completions          │  │
│  │  /v1/completions               │  │
│  │  /v1/responses                 │  │
│  └───────────┬────────────────────┘  │
│              │                       │
│  ┌───────────▼────────────────────┐  │
│  │       Fallback 决策引擎         │  │
│  │  ┌──────────────────────────┐  │  │
│  │  │ 1. 用户自定义 fallback 链  │  │  │
│  │  │ 2. 默认名称相似度匹配      │  │  │
│  │  └──────────────────────────┘  │  │
│  └───────────┬────────────────────┘  │
│              │                       │
│  ┌───────────▼────────────────────┐  │
│  │       探测管理器 (Prober)       │  │
│  │  - 定时健康探测                 │  │
│  │  - 探测结果缓存                 │  │
│  │  - 并发探测多 Provider          │  │
│  └───────────┬────────────────────┘  │
│              │                       │
│  ┌───────────▼────────────────────┐  │
│  │       请求转发器 (Forwarder)    │  │
│  │  - 透传请求/响应                │  │
│  │  - SSE 流式透传                 │  │
│  │  - 错误透传                     │  │
│  └────────────────────────────────┘  │
└──────────────────────────────────────┘
        │
        ▼
┌───────────────┐  ┌───────────────┐  ┌───────────────┐
│  Anthropic    │  │   DeepSeek    │  │     Qwen      │
│  Provider     │  │   Provider    │  │   Provider    │
└───────────────┘  └───────────────┘  └───────────────┘
```

### 2.2 核心流程

**Chat Completions 请求处理流程**：

```
用户请求 /v1/chat/completions  { model: "claude-opus-4.6", messages: [...] }
        │
        ▼
  解析请求，提取 model 字段
        │
        ▼
  查询探测缓存：claude-opus-4.6 是否可用？
        │
   ┌────┴────┐
   │  可用    │  不可用
   ▼         ▼
  直接转发   执行 Fallback 决策
  到对应      │
  Provider    ▼
            按 fallback 链找到第一个可用模型
            重写 request.Body 中的 model 字段
            转发到对应 Provider
        │
        ▼
  透传响应给用户（含 SSE stream）
```

**Responses 请求处理流程**：

```
用户请求 /v1/responses  { model: "claude-opus-4.6", input: "...", tools: [...] }
        │
        ▼
  解析请求，提取 model 字段
        │
        ▼
  查询探测缓存：claude-opus-4.6 是否可用？
        │
   ┌────┴────┐
   │  可用    │  不可用
   ▼         ▼
  直接转发   执行 Fallback 决策
  到对应      │
  Provider    ▼
            按 fallback 链找到第一个可用模型
            重写 request.Body 中的 model 字段
            转发到对应 Provider
        │
        ▼
  透传响应给用户（含 SSE stream）
```

> Chat Completions 和 Responses 共享同一套探测缓存和 Fallback 决策引擎。Fallback 时仅替换 `model` 字段，其余字段（`messages`、`input`、`tools`、`instructions` 等）原样透传，不做协议转换。

**探测流程**：

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
  (chat/completions, max_tokens=1)
        │
        ▼
  更新缓存：{model: {available: bool, latency: ms, probed_at: timestamp}}
```

### 2.3 Fallback 策略

**优先级**（从高到低）：

1. **用户自定义 fallback 链**：配置文件中显式指定的全局模型降级顺序，按列表顺序依次尝试
2. **默认名称相似度匹配**：按模型家族前缀匹配，同家族内版本降级
   ```
   claude-opus-4.6 → claude-opus-4.5 → claude-sonnet-4.6 → claude-sonnet-4.5
   deepseek-v4 → deepseek-v3.2 → deepseek-v3
   qwen3-max → qwen3-plus → qwen3-turbo
   ```
3. **跨家族降级**：同家族全部不可用时，按配置的全局优先级降级到其他家族
   ```
   Claude 系列 → DeepSeek 系列 → Qwen 系列 → ...
   ```

**Fallback 决策流程**：

```
输入：用户请求的 model
        │
        ▼
┌──────────────────────────────────────┐
│ 1. 检查自定义 fallback 链              │
│    如果当前 model 在 custom 列表中，    │
│    从该位置向后查找第一个可用模型        │
│    找到 → 返回                         │
│    如果当前 model 不在列表中，跳过此步   │
└──────────────┬───────────────────────┘
               │ 未命中
               ▼
┌──────────────────────────────────────┐
│ 2. 名称相似度匹配                      │
│    提取模型家族前缀，                   │
│    在同家族内按版本降级查找              │
│    找到 → 返回                         │
└──────────────┬───────────────────────┘
               │ 未命中
               ▼
┌──────────────────────────────────────┐
│ 3. 跨家族降级                          │
│    按 global_priority 顺序             │
│    遍历各 Provider 的模型列表           │
│    返回第一个可用模型                   │
└──────────────────────────────────────┘
```

### 2.4 配置设计

```yaml
server:
  port: 8080

providers:
  - name: anthropic
    baseurl: https://api.anthropic.com/v1
    api_key: ${ANTHROPIC_API_KEY}

  - name: deepseek
    baseurl: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}

  - name: qwen
    baseurl: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${QWEN_API_KEY}

fallback:
  # 用户自定义降级链（可选，全局扁平列表，优先级最高）
  # 请求的 model 如果在链中，从该位置向后查找第一个可用模型
  # 请求的 model 如果不在链中，跳过自定义链，走默认策略
  custom:
    - claude-opus-4.6
    - claude-opus-4.5
    - claude-sonnet-4.6
    - deepseek-v4

  # 跨家族降级顺序（可选，默认按 providers 配置顺序）
  global_priority:
    - anthropic
    - deepseek
    - qwen

probe:
  interval: 30s        # 探测间隔
  timeout: 10s         # 单次探测超时
  cache_ttl: 60s       # 探测结果缓存 TTL
```

**配置说明**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `server.port` | int | 网关监听端口 |
| `providers[].name` | string | Provider 标识，用于日志和 `global_priority` 引用 |
| `providers[].baseurl` | string | Provider 的 OpenAI 兼容 API 地址 |
| `providers[].api_key` | string | API Key，支持 `${ENV_VAR}` 环境变量引用 |
| `fallback.custom` | []string | 全局扁平降级链，按顺序 fallback |
| `fallback.global_priority` | []string | 跨家族降级时的 Provider 遍历顺序，引用 `providers[].name` |
| `probe.interval` | duration | 健康探测间隔 |
| `probe.timeout` | duration | 单次探测请求超时 |
| `probe.cache_ttl` | duration | 探测结果缓存有效期 |

### 2.5 模型发现机制

不在配置中静态声明模型列表，而是通过各 Provider 的 `/v1/models` 端点动态发现：

```
启动时 / 定时刷新
        │
        ▼
  并发请求所有 Provider 的 /v1/models
        │
        ▼
  汇总模型列表，建立 model → provider 映射
        │
        ▼
  缓存：{model_name: {provider, available, probed_at}}
```

**优势**：
- Provider 新增模型无需改配置，自动感知
- 模型下线自动从可用列表移除
- 配置更简洁，只需声明 Provider 连接信息

**模型名冲突处理**：当多个 Provider 返回同名模型时，按 `providers` 配置顺序，先配置的 Provider 优先。日志中 warn 冲突情况。

### 2.6 OpenAI Responses API 兼容

#### 2.6.1 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/responses` | POST | 创建响应，支持 `input` 多模态输入、`tools` 工具调用、`stream` 流式输出 |
| `/v1/responses/{response_id}` | GET | 获取指定响应记录 |

#### 2.6.2 与 Chat Completions 的关键差异

| 维度 | Chat Completions | Responses |
|------|-----------------|-----------|
| 输入字段 | `messages: [{role, content}]` | `input: string \| InputItem[]` |
| 工具定义 | `tools` 在请求体中 | `tools` 在请求体中（结构兼容） |
| 流式事件 | `data: {choices[0].delta...}` | `data: {type: "response.output_text.delta"...}` |
| 输出结构 | `choices[0].message.content` | `output: OutputItem[]` |
| 检索/搜索 | 不支持 | 支持 `tools: [{type: "file_search"}]` 等内置工具 |

#### 2.6.3 Fallback 处理

Fallback 时仅替换请求体中的 `model` 字段，其余字段完全透传：

```json
// 原始请求
{ "model": "claude-opus-4.6", "input": "hello", "tools": [...] }

// Fallback 后（假设降级到 deepseek-v4）
{ "model": "deepseek-v4", "input": "hello", "tools": [...] }
```

**不做的事**：
- 不做 `input` ↔ `messages` 格式互转
- 不做 `output` ↔ `choices` 格式互转
- 不做 `response.created` 等事件类型重写

**前提**：上游 Provider 的 `/v1/responses` 端点实现与 OpenAI 规范兼容。如果某 Provider 不支持 Responses API，该 Provider 的模型不会出现在 Responses 请求的候选模型中。

#### 2.6.4 流式透传

Responses API 的 SSE 事件类型比 Chat Completions 更丰富，透传时不做解析和改写：

```
event: response.created
data: {...}

event: response.in_progress
data: {...}

event: response.output_text.delta
data: {"delta": "Hello"}

event: response.completed
data: {...}
```

网关逐块读取上游 SSE 流，原样写入下游响应，保证事件边界完整。

## 三、边界

### 3.1 范围内（In Scope）

| 能力 | 说明 |
|------|------|
| OpenAI 兼容端点 | `/v1/models`、`/v1/chat/completions`、`/v1/completions`、`/v1/responses` |
| 透明代理转发 | 请求体和响应体透传，用户无感知 |
| SSE 流式透传 | Chat Completions 和 Responses 的 `stream: true` 模式完整支持 |
| 模型可用性探测 | 定时探测各 Provider 模型状态 |
| Fallback 决策 | 用户自定义链 > 名称相似度匹配 > 跨家族降级 |
| 请求级模型重写 | Fallback 时自动替换 request body 中的 model 字段 |
| 动态模型发现 | 通过 `/v1/models` 自动发现各 Provider 模型，无需静态配置 |
| 配置文件驱动 | YAML 配置 Provider、API Key、Fallback 规则 |
| 环境变量注入 | API Key 等敏感信息通过环境变量引用 |

### 3.2 范围外（Out of Scope）

| 能力 | 说明 | 替代方案 |
|------|------|----------|
| 负载均衡 | 不按负载分发请求到多实例 | 上层 nginx/k8s service |
| 速率限制 | 不限流 | 上层网关/Provider 侧自带 |
| 协议转换 | 不做 Anthropic ↔ OpenAI、Chat Completions ↔ Responses 等格式互转 | 要求所有 Provider 均为 OpenAI 兼容 |
| 计费/用量统计 | 不记录 token 用量 | 各 Provider 后台查看 |
| 认证管理 | 不生成/管理 API Key，透传用户请求中的 Key | 用户自行管理 |
| 多租户 | 单实例服务所有请求 | — |
| 模型 Benchmark | 只判断可用/不可用，不评估质量/速度 | — |
| Responses 语义校验 | 不校验 `input`/`tools`/`instructions` 等字段的合法性 | 上游 Provider 返回错误时透传 |

### 3.3 前提假设

1. **所有上游 Provider 均提供 OpenAI 兼容 API**（`/v1/models`、`/v1/chat/completions`）
2. **Responses API 兼容性**：需要走 Responses 降级的 Provider 需实现 `/v1/responses` 端点，且与 OpenAI 规范兼容
3. **用户请求格式遵循 OpenAI Chat Completions 或 Responses 规范**
4. **用户持有各 Provider 的有效 API Key**
5. **网络可达**：本项目能访问所有配置的 Provider
6. **模型名称全局唯一**：不同 Provider 的模型名称不重复；若重复，按 `providers` 配置顺序取第一个

## 四、验收标准

### 4.1 功能验收

- [ ] **AC-1**：用户仅修改 baseurl 指向本项目，无需修改任何业务代码即可接入
- [ ] **AC-2**：首选模型可用时，Chat Completions 请求透明转发到对应 Provider，响应原样返回
- [ ] **AC-3**：首选模型不可用时，自动按 fallback 链选择第一个可用模型，请求中 model 字段被正确替换
- [ ] **AC-4**：SSE 流式响应完整透传（Chat Completions + Responses），用户端无断流、无丢帧
- [ ] **AC-5**：`/v1/models` 返回所有 Provider 可用模型的并集
- [ ] **AC-6**：支持用户通过配置文件自定义全局 fallback 链
- [ ] **AC-7**：未配置自定义 fallback 时，按默认名称相似度规则降级
- [ ] **AC-8**：API Key 支持环境变量引用，不明文写在配置文件中
- [ ] **AC-9**：`/v1/responses` 端点完整支持，包括 `input`、`tools`、`instructions`、`stream` 等字段的透传与 Fallback
- [ ] **AC-10**：模型列表通过 `/v1/models` 动态发现，Provider 新增/下线模型无需修改配置重启
- [ ] **AC-11**：`/v1/responses/{response_id}` GET 端点透传

### 4.2 非功能验收

- [ ] **AC-12**：代理引入的额外延迟 < 50ms（P99，不含 fallback 决策时的探测等待）
- [ ] **AC-13**：探测结果缓存命中时，fallback 决策耗时 < 1ms
- [ ] **AC-14**：支持至少 3 个 Provider 同时配置
- [ ] **AC-15**：配置文件热加载（修改配置后无需重启）

### 4.3 兼容性验收

- [ ] **AC-16**：兼容 OpenAI Python SDK、Node.js SDK 直接接入（Chat Completions + Responses）
- [ ] **AC-17**：兼容常见 OpenAI 兼容 Provider（Anthropic、DeepSeek、Qwen、Groq 等）
