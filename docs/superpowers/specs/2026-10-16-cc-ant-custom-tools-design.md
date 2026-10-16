# Design Doc: Chat Completions 与 Anthropic Messages 的 Custom Tools 支持 (v2.3.1)

Status: Approved by User
Target Version: v2.3.1 (Patch release)
IR Document Contract: 0.2.0 (unchanged, backwards-compatible with 0.1.0)

## 1. 目标与背景

在 oxa v2.2.0 中，我们完成了 OpenAI Responses 端到端 Custom Tools 原生支持（Spec 13）。在 v2.3.0 中，我们完成了 OpenAI Responses Apply Patch 端到端支持（Spec 14）。
在此之前，Chat Completions 和 Anthropic Messages 对 Custom Tools 均采用显式 `unsupported-semantic` loss 降级丢弃。

本设计目标为：按照 `spec/13 §6` 的规划，在 **Chat Completions** 和 **Anthropic Messages** 两大协议面上全面补齐 Custom Tools 的端到端对等能力（非流式与流式全部覆盖），跨越 Go、TypeScript、Python、Rust、C++ 五门参考语言，消除跨协议转换（如 Responses ⇄ Chat Completions）时的降级损耗。

注意：根据决策树共识，**Apply Patch**（Spec 14）在 Chat Completions 与 Anthropic Messages 上继续保持规范降级（`unsupported-semantic` loss）。

---

## 2. OpenAI Chat Completions 原生 Custom Tools 契约 (N-CC-CUSTOM)

Chat Completions 官方 SDK 规范采用类似 `function` 工具的子对象嵌套结构。

### 2.1 工具声明 (`tools[]`)
```json
{
  "type": "custom",
  "custom": {
    "name": "grep",
    "description": "Search regular expressions in files",
    "format": {
      "type": "text"
    }
  }
}
```
- `type` 为 `"custom"`。
- `custom.name` 必填字符串，不能为空。
- `custom.description` 可选字符串。
- `custom.format` 必填格式定义对象（`{"type": "text"}` 或 `{"type": "grammar", "syntax": "lark"|"regex", "definition": "..."}`）。

### 2.2 工具选择 (`tool_choice`)
```json
{
  "type": "custom",
  "custom": {
    "name": "grep"
  }
}
```
- 对应 IR `ToolChoice{Mode: "tool", ToolType: "custom", Name: "grep"}`。
- `tool_choice: "auto"`, `"required"`, `"none"` 原样保持既有逻辑。当 `tool_choice: "required"` 时，即使所有工具均为 custom 工具也保留为 `"required"`（不再作为 unsupported loss 丢弃）。

### 2.3 模型调用 (`message.tool_calls[]`)
在 assistant 消息中：
```json
{
  "id": "call_1",
  "type": "custom",
  "custom": {
    "name": "grep",
    "input": "pattern: [a-z]+\r\npath: main.go"
  }
}
```
- `type` 为 `"custom"`。
- `id` 必填非空字符串，映射到 IR `CustomToolUseBlock.ID`。
- `custom.name` 必填字符串，映射到 IR `CustomToolUseBlock.Name`。
- `custom.input` 必填不透明文本字符串，映射到 IR `CustomToolUseBlock.Input`（以 JSON 字符串外壳存储，不按 JSON 解析）。

### 2.4 工具结果 (`role: "tool"`)
在 tool 结果消息中：
```json
{
  "role": "tool",
  "tool_call_id": "call_1",
  "content": "matched 3 lines"
}
```
- `role: "tool"` 沿用标准 tool 结果角色。
- `tool_call_id` 对应调用的 `id`。
- 当 IR `CustomToolResultBlock.Output` 为字符串时，映射为 `content: "..."`。
- 当 IR 为多模态 block 列表时，映射为 `content: [{"type": "text", ...}, {"type": "image_url", ...}]`。

### 2.5 流式增量 (`ChoiceDelta.tool_calls[]`)
在流式 chunk 中：
```json
{
  "index": 0,
  "id": "call_1",
  "type": "custom",
  "custom": {
    "name": "grep",
    "input": "pattern: "
  }
}
```
- 首次发射带有 `index`, `id`, `type: "custom"`, `custom.name` 及首段 `input`。
- 后续 delta chunk 只携带 `index` 和 `custom.input` 片段增量。
- chunk `finish_reason: "tool_calls"` 正常闭合。

---

## 3. Anthropic Messages 结构化包装契约 (N-AN-CUSTOM)

Anthropic Messages 原生要求所有工具输入均为合法的 JSON Schema 对象。因此采用**单字段结构化对象包装**，在保留纯协议数据映射的同时达成语义等价。

### 3.1 工具声明 (`tools[]`)
IR `Tool{Type: "custom", Name: "grep", Description: "...", Format: ...}` 转换为 Anthropic 工具：
```json
{
  "name": "grep",
  "description": "Search regular expressions in files",
  "input_schema": {
    "type": "object",
    "properties": {
      "input": {
        "type": "string",
        "description": "Raw custom tool input text"
      }
    },
    "required": ["input"],
    "additionalProperties": false
  }
}
```
- `name` 与 `description` 原样保留。
- 生成规范的单属性 `input_schema`。
- 当从 Anthropic 解码到 IR 时：若工具的 `input_schema` 具有该单字段 `input` 结构特征，则识别并解码为 IR `Tool{Type: "custom", ...}`。

### 3.2 模型调用 (`content[]` 中的 `tool_use`)
在 Anthropic assistant 消息中：
```json
{
  "type": "tool_use",
  "id": "toolu_1",
  "name": "grep",
  "input": {
    "input": "pattern: [a-z]+\r\npath: main.go"
  }
}
```
- `input` 统一包装为 `{"input": "<raw_text>"}`。
- 解码到 IR 时，提取 `input.input` 纯文本，构建 IR `CustomToolUseBlock`。
- 识别规则：请求上下文有匹配的 custom 工具声明优先识别；单独 Response 解码时，若 `input` 仅含单字段 `input` 且为字符串，提升为 `CustomToolUseBlock`。

### 3.3 工具结果 (`content[]` 中的 `tool_result`)
在 Anthropic user 消息中：
```json
{
  "type": "tool_result",
  "tool_use_id": "toolu_1",
  "content": "matched 3 lines"
}
```
- 纯文本结果直接作为字符串 `content`。
- 多模态结果作为 block 数组（`[TextBlock, ImageBlock]`），Anthropic 原生支持。
- 解码到 IR 时，若对应的调用属于 Custom Tool，则解码为 IR `CustomToolResultBlock`。

### 3.4 流式生命周期与缓冲解包
- **解码器 (Anthropic → IR)**：
  - 遇到包装的 custom `tool_use` 时，暂存开启项。
  - 在接收 `input_json_delta` 时在内存中累加完整的 JSON 字符串。
  - 在 `content_block_stop` 闭合时，从 JSON 解析出 `{"input": "<raw_text>"}`，提取出完整的 `<raw_text>`。
  - 发射 IR `ContentBlockStart{Block: CustomToolUseBlock}`, `ContentBlockDelta{Delta: CustomToolInputDelta{Text: "<raw_text>"}}`, `ContentBlockStop`。
- **编码器 (IR → Anthropic)**：
  - 将 IR `CustomToolUseBlock` / `CustomToolInputDelta` 包装为标准的 Anthropic `tool_use` / `input_json_delta` 事件发射。

---

## 4. 规范更新与黄金向量规划

1. **规范文件更新**：
   - `spec/10-mapping-openai-chat-completions.md`：增加 Chat Completions 原生 Custom Tools 规范。
   - `spec/12-mapping-anthropic-messages.md`：增加 Anthropic Messages 结构化 Custom Tools 规范。
   - `spec/13-custom-tools.md`：更新 §5 与 §6，将 CC 与 Anthropic 从不支持目标调整为支持目标。
   - `spec/20-streaming-semantics.md`：更新 CC 与 Anthropic 的 custom 工具流式事件规则。
   - `spec/CHANGELOG.md` & `spec/README.md`：更新版本与目录。

2. **黄金向量**：
   - 新增 Chat Completions Custom Tools 向量（非流式请求/响应双向、流式双向）。
   - 新增 Anthropic Messages Custom Tools 向量（非流式请求/响应双向、流式双向）。
   - 新增 Cross 向量：`responses-to-chatcompletions-custom-tool`（零 loss 双向往返！）。
   - 刷新 `vectors/manifest.json`。

3. **参考实现**：
   - 五语言（Go、TypeScript、Python、Rust、C++）全套双向与流式实现。
   - 保证 100% 黄金向量与本地测试绿灯，最后提交 PR 并通过 GitHub Actions CI 18 项 Checks。
EOF
