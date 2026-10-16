# Responses Custom Tools 设计

## 目标与边界

实施已批准的方案 A：Responses custom 工具声明、选择、请求历史、响应、流式 delta/done 双向转换。任意输入文本不得转换为 function 参数。Apply Patch 不在本阶段；Chat Completions 的原生 custom 映射另行实现，本阶段明确报告当前 profile 不支持；Anthropic 无等价语义时报告 loss。oxa 不执行工具、grammar、代码或文件操作。

## 锁定的 IR JSON 契约

新文档输出 `specVersion: "0.3.0"`，继续读取 `0.1.0`、`0.2.0`；既有向量不改语义。规范下一主版本为 3.0.0（未发布）；包版本与 Go module path 不在本次自动发布或迁移。

- `Tool`：新增可选 `type`（缺省 function；function 编码维持旧形状），`type:custom` 不允许 `input_schema`；可选 `format`，形状为 `{type:text}` 或 `{type:grammar,syntax:lark|regex,definition:string}`。不提供 format 与显式 text 不合并。
- `ToolChoice`：仅 `mode:tool` 可有 `tool_type:function|custom`，缺省 function；custom 编码显式保留 `tool_type:custom`。
- `CustomToolUseBlock`：`{type:custom_tool_use,id,name,input:string}`；id/name 非空，input 可为空。Go 内存沿用 JSON 字符串 token 的 `json.RawMessage` 存储纪律，禁止把字符串内容当 JSON。
- `CustomToolResultBlock`：`{type:custom_tool_result,tool_use_id,output:string|[TextBlock|ImageBlock]}`；保留字符串/列表区别，列表可为空，禁止递归 tool/thinking 内容；无 is_error。
- `CustomToolInputDelta`：`{type:custom_tool_input_delta,text:string}`；text 可为空；仅匹配 custom_tool_use；Go Text 为 JSON 字符串 token，拼接时只解开字符串外壳。

## Responses 映射

`tools[type:custom]` 与上述声明双向映射；tool_choice custom 对应 mode tool + tool_type custom；`custom_tool_call` 对应 custom_tool_use，call_id 对应 id；`custom_tool_call_output` 对应 custom_tool_result，call_id 对应 tool_use_id。请求中的 assistant run 和 result run 同时接受 function/custom，保持工具之间的原始顺序，保留既有文本优先规范化。正常完成且存在任一种调用时 stop_reason 为 tool_use。

output 字符串原样保存；内容列表 input_text 映射 text、input_image 的 https/data URL 映射现有 image；input_file、file_id-only 图片按项 unsupported-semantic；图片 detail 等未建模字段按 unmapped-field。全部内容被丢弃时保留 output:[] 与关联 id。未知字段含 null/false/空值，按来源顺序审计，不递归 opaque 文本；整个不支持的内容项只报告一次 loss。

INV-3/4 同时适用于两类调用/结果：请求第一消息 user；结果位于紧随调用的 user turn；按 call_id 和调用类别一一对应，禁止孤立、重复或类别不匹配；结果保持调用的相对顺序。独立 response 不要求带结果。

## 流式

沿用 M7：一个原生调用缓存到 response.output_item.done，之后回放完整 start、原始 delta 片段（含空片段）、stop。start.input 为最终完整输入，不应再次与其 delta 累加。input.done 是可选的完整性校验快照；如未出现 delta，使用完整快照生成一个片段。input.done 与 item.done 的输入必须一致；有 delta 时片段拼接必须一致，不能重复追加。output_index、item_id、call_id/name 生命周期冲突为结构错误。无输入字段与空字符串区分；缺少原生闭合或终止为结构错误。

编码器为每个块产生 custom_tool_call 的 output_item.added、每个输入 delta、input.done、output_item.done；无 delta 时合成完整输入 delta。生成 id 为 `ctc_abc123`（非流式）或 `ctc_<顺序号>`（流式，从 0 起），call_id 始终使用 IR id；不写入 function arguments。IR 语法与连续索引保持不变。

## 不支持目标的降级

每个 custom 声明、指定 custom 的 choice、custom 调用、custom 结果各有一个 unsupported-semantic，path 指向对应 IR 项。声明与 choice 不得作为 function 输出。被丢弃内容导致空消息时保留空 text；响应没有保留调用时若原 stop_reason 为 tool_use，归一化为 end_turn 并在 stop_reason 记录 degraded。若 any/required 的唯一工具全部被丢弃，省略该 choice 并记录 unsupported-semantic。流式按块一次 loss 并吸收该块 delta/stop，维持保留块的连续索引；不得生成 function 输入。

## 验收

先新增规范与共享向量，再实现。五语言共同覆盖 text/Lark/regex/缺省 format、custom choice、非 JSON/空/Unicode/CRLF 输入、混合调用和结果、文本/图片/文件 loss、流式 delta/done/无 delta 回退、错误 id/快照/Delta 类型、未知字段审计、两个不支持目标、旧版本读取和 0.3.0 输出。现有全部向量须继续通过。

## 工作单元与资源

清单在仓库外 `/tmp/oxa-custom-tools.weNMAt/manifest.json`。WU-01 负责规范/schema/向量；WU-02 负责 Go；WU-03 至 WU-06 分别负责 TS/Rust/Python/C++，各自只发布唯一语言 patch（先 partial，校验后原子重命名）；WU-07 为唯一集成/汇总单元。每个实现单元最多 60 分钟，10 分钟无进展或输入契约发生冲突时停止；未校验 patch 不应用。最终保留有改动的 worktree，删除临时工件，不提交、不发布。
