# 13 — Custom Tools（规范 2.2）

Status: normative. 本文扩展 spec/01、spec/11 和 spec/20 的 Custom Tools 规则；不包含专用 Apply Patch。

## 1. IR 契约 — N-C-1

IR 文档 contract 保持 `specVersion: "0.2.0"`，继续兼容接受 `0.1.0`。根据项目版本演化准则，大的架构重构升级高位版本，而本项作为同一框架下的能力补充，升级规范中间版本（2.2.0），IR contract 维持 0.2.0（与 2.1.0 引入 response_format 相同）。

`Tool.type` 可选，缺省 function。function 声明沿用 name、description、input_schema，MUST NOT 携带 format；custom 声明携带 `type:custom`、name、可选 description/format，MUST NOT 携带 input_schema。format 缺省与显式 text MUST 保留区别：

```json
{"type":"text"}
```

```json
{"type":"grammar","syntax":"lark","definition":"start: \"hello\""}
```

syntax 也可以为 regex；definition 是不透明字符串，MUST NOT 被执行、编译或改写。未知 format/syntax 为 unsupported-semantic，不得静默改为 text。

`ToolChoice` 的 `mode:tool` 新增可选 `tool_type:function|custom`，缺省 function；其他 mode MUST NOT 携带 tool_type。名称不能用于推断工具类别。

## 2. 内容与增量 — N-C-2

| 类型 | JSON 形状 |
|---|---|
| CustomToolUseBlock | `{type:custom_tool_use,id,name,input:string}` |
| CustomToolResultBlock | `{type:custom_tool_result,tool_use_id,output:string或内容数组}` |
| CustomToolInputDelta | `{type:custom_tool_input_delta,text:string}` |

id/name/tool_use_id MUST 非空；input/text 可为空。输入及 delta 为任意文本，不是 JSON 参数；即使文本恰好是 JSON，也 MUST NOT 解析或重序列化。INV-1 的不透明字符串保真纪律扩展到这两个字段；JSON 外层必须合法转义，语义内容逐字节保留，不做 Unicode、换行或空白归一化。

output 数组仅接受 TextBlock/ImageBlock，可为空；MUST NOT 包含嵌套 tool/thinking。字符串与数组 MUST 保留区别；本类型无 is_error。

INV-3/4 同时适用于 function/custom 调用及结果：同一 assistant turn 可混合两类调用，下一 user turn 中的结果按 call_id 和类别一一对应并保持相对顺序。孤立、重复或类别错配结果无效。INV-5 中 custom_tool_use 只接受 custom_tool_input_delta；其他块不能接受该 delta。

## 3. Responses — N-C-3

- tools[].type=custom ⇄ custom Tool，保留 name、description、format。
- tool_choice `{type:custom,name}` ⇄ `{mode:tool,name,tool_type:custom}`。
- custom_tool_call ⇄ CustomToolUseBlock，call_id ⇄ id，name/input 原样。
- custom_tool_call_output ⇄ CustomToolResultBlock，call_id ⇄ tool_use_id。
- 请求 assistant/result runs 同时接受 function/custom，保持工具之间共享顺序；其他规范化沿用 N-R-5/6/10。
- completed 响应存在任意保留工具调用时 StopReason 为 tool_use。
- 编码非流式 custom 输出项时合成 `id:ctc_abc123`；call_id 不得合成或替换。

output 字符串直接映射。数组 input_text 映射 TextBlock；input_image 的 https/data URL 映射 ImageBlock。input_file、file_id-only 图片等不能表达的内容 MUST 按项记录 unsupported-semantic；图片 detail 等未建模属性记录 unmapped-field。数组项目被全部丢弃时仍保存 output:[] 和关联标识。未知字段必须按 spec/02 的存在性和来源顺序审计；整体不支持的项目不再递归产生字段 loss。

SDK 的 async/caller/namespace/allowed_callers 等未纳入当前 IR 的属性，不得静默忽略；按字段审计，不作为无损 envelope（Anthropic 工具 defer_loading 属性自规范 2.4.7 起由 spec/15 NativeAnnotation 显式建模）。

## 4. 流式 — N-C-4

采用 M7 的缓存/回放模型。custom_tool_call 的 output_item.added 开启原生 item；custom_tool_call_input.delta 按遇到顺序保存 text；custom_tool_call_input.done.input 是完整快照，MUST NOT 再次追加。response.output_item.done 闭合调用并回放 start/delta*/stop，start.input 为完整最终输入。

有 delta 时，其拼接 MUST 等于完整快照；done 可选，有多个快照时必须一致。无 delta 时完整快照生成一个合成 delta（输入可为空）。不得把字段缺失误判为空输入。item_id/output_index/call_id/name 不一致、重复闭合、错配 delta、无终止和无 item 闭合均为结构错误。保持 INV-5/6，不把原生索引直接泄漏到 IR。

反向编码产生 custom output_item.added、input.delta、input.done、output_item.done；无 IR delta 时合成一个完整输入 delta。流式合成 item id 为 `ctc_<顺序号>`，从 0 起。不能产生 function_call_arguments 事件。重复快照不构成额外 IR delta。

## 5. 跨协议支持与目标映射 — N-C-5

自规范 2.3.1 起，三大协议面均支持 Custom Tools：
- **OpenAI Responses**：原生 `custom_tool_call` 与 `custom_tool_call_output`；
- **OpenAI Chat Completions**：原生 `type: "custom"` 嵌套结构（`custom: {name, description?, format}`、`tool_calls[type=custom].custom.{name, input}`、`role: "tool"`）。format 缺失保持缺失，未知 format 记录 unsupported-semantic；custom result 内容数组编码为 ContentPart 列表，与纯文本结果严格区分；
- **Anthropic Messages**：采用单属性结构化包装契约（`input_schema: {"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`、`tool_use.input: {"input": "..."}`、`tool_result`）。若声明带有 grammar format 则记录 unsupported-semantic loss；流式累加文本增量并在 content_block_stop 发射单个合法紧凑 `input_json_delta`；同请求中已被声明为 function 的工具在遇到单属性 input 时不发生类别误判。

若目标协议存在未知不支持变体，丢弃后空消息编码为包含空文本的合法消息。若原 stop_reason=tool_use 但无保留调用，改为 end_turn 并在 stop_reason 记录 degraded。若 any/required 的全部声明都被丢弃，则省略该 choice 并记录 unsupported-semantic。流式一个块一次 loss，吸收该块 delta/stop，压紧保留的原生索引；保持终止和 usage。

## 6. 版本发布历史

- **2.2.0**：首发支持 OpenAI Responses 的 Custom Tools 原生映射；Chat Completions 与 Anthropic Messages 暂时通过显式 `unsupported-semantic` loss 进行确定性降级。
- **2.3.1**：全面补齐 OpenAI Chat Completions（原生 custom wire 结构）与 Anthropic Messages（单属性结构化包装契约）的非流式与流式双向映射，消除跨协议转换降级损耗。

## 7. 固定参考

OpenAI Python SDK commit `e71d781ac5e7b3f365ac130bf338baaf13ae720d`，src/openai/types/responses/{custom_tool_param,tool_choice_custom_param,response_custom_tool_call,response_custom_tool_call_output_param,response_custom_tool_call_input_delta_event,response_custom_tool_call_input_done_event}.py，以及 shared_params/custom_tool_input_format.py。
