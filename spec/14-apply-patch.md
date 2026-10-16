# 14 — Apply Patch（规范 2.3）

Status: normative，未发布。扩展 spec/01、spec/11、spec/20；IR contract 保持 `0.2.0`，兼容读取 `0.1.0`。本规范只转换数据，MUST NOT 读取文件、执行补丁、清理路径或验证 diff 能否应用。

## 1. 专用声明与选择 — N-AP-1

Tool 新增专用变体 `{type:apply_patch}`，canonical IR 只能携带 type，MUST NOT 携带 name、description、input_schema 或 format。function/custom 的既有约束不变。

ToolChoice 使用 `{mode:tool,tool_type:apply_patch}`，name MUST 缺失。function/custom（包括缺省 function）的 mode=tool 仍要求非空 name；其他 mode 仍禁止 name/tool_type。Responses 对应声明与 choice 都是 `{type:apply_patch}`，不合成虚假工具名。

`{type:custom,name:apply_patch}` 仍属于 generic Custom Tools，MUST NOT 按名称升级为专用工具。两个类别可以同时存在。

## 2. Operation 与内容块 — N-AP-2

Operation 是三个扁平判别变体：

| type | path | diff |
|---|---|---|
| create_file | 必填 string，可为空 | 必填 string，可为空 |
| delete_file | 必填 string，可为空 | MUST 缺失 |
| update_file | 必填 string，可为空 | 必填 string，可为空 |

Path/Diff 是不透明文本。MUST 保留字符、Unicode、CRLF/LF、尾部空白/换行和控制字符，MUST NOT 解释为 JSON 参数、解析补丁语法、补充 header、resolve/clean 路径或改变斜杠。JSON 外壳合法转义；保真比较针对解码后的字符串内容。

新增独立 Block：

- `ApplyPatchToolUseBlock`：`{type:apply_patch_tool_use,id,status,operation}`。id 非空，对应 wire call_id；status 必填，仅 in_progress/completed；operation 必填。
- `ApplyPatchToolResultBlock`：`{type:apply_patch_tool_result,tool_use_id,status,output?}`。tool_use_id 非空，对应 wire call_id；status 必填，仅 completed/failed；output 为可选 string，空串有效；无 is_error。

调用与结果使用不同状态类型。调用 completed 不表示补丁执行成功；结果 failed 不等于顶层 response failed/refusal。

原生结果 output 缺失/null 规范化为 IR 缺失，loss-free；原生空字符串 MUST 保留。编码缺失 output 时省略，不造空文本或 null。Canonical IR 不允许 null，也不允许列表形式的 Apply Patch output。

## 3. 四方向映射与关联 — N-AP-3

- request input 的 apply_patch_call ⇄ assistant turn 的 ApplyPatchToolUseBlock。
- request input 的 apply_patch_call_output ⇄ user turn 的 ApplyPatchToolResultBlock。
- response output 的 apply_patch_call ⇄ Response.Content 的专用调用块。
- response output 的 apply_patch_call_output ⇄ Response.Content 的专用结果块。
- 三类 function/custom/apply_patch 调用共享既有 assistant run 规范化，三类结果共享 user result run；工具之间保持相对顺序，不按类别排序。

INV-3/4 扩展至专用类型：请求中调用/结果须按类别、关联 id 和相对顺序一一对应；同 turn 的调用 id 唯一，孤立、重复、缺失或错类结果无效。先检验来源关联，再进行语义过滤；单独 response 的结果允许关联外部上下文，不要求补出请求或 tools 声明。

Source-to-IR 的未来未知 Operation/status 无法形成合法块时，记录对应 loss。请求内关联的另一半亦省略并记录 unsupported-semantic，保持 source loss 顺序；不留下孤立结果、不伪造调用。不能以过滤掩盖本来就错误的关联。

服务端 output item id 与 call_id 不同：前者为 envelope，后者为关联身份。客户端 input 允许省略/null item id，编码客户端 input 时省略；响应编码分别为调用/结果合成唯一 `apc_0`、`apco_0` 等 id，两种计数各从零开始。MUST NOT 重新编号或合成 call_id。

顶层状态沿用 N-R-11：completed 且保留任一模型调用时 StopReason 为 tool_use；仅有文本/结果时为 end_turn；incomplete/error 映射不变。同一输出含调用和结果时保留二者；tool_use 不是重执行补丁的授权，调用者须按 call_id 区分已回答的调用。

## 4. 原子项流式 — N-AP-4

固定 SDK 无 Apply Patch 专用 diff-delta 事件。本规范不新增 Delta，不复用 InputJSONDelta/CustomToolInputDelta。

解码 added 时检查身份、暂存快照，不发 IR 块；done 时验证完整最终快照，回放恰好一个 ContentBlockStart 和一个 ContentBlockStop。两类 Apply Patch Block 均可出现在响应 ContentBlockStart，MUST 不接受任何 Delta。INV-5/6 保持不变。

Added 的 Operation/status/output 可与 done 不同；done 才是最终载荷。MUST NOT 要求初末 diff 相等、将两个快照拼接，或靠 added 补齐 done 缺失的必填字段。稳定身份为 type、output_index、item id、call_id；身份改变、重复 added/done、缺少开启/闭合/终止、提前 Flush 或错 Delta 均为结构错误。

终止 response.output 若携带已经闭合的专用项，检查其最终 status、Operation/output 与身份一致，不重复发块。item id 的必填性按入口区分，客户端的可选性不能泛化到服务端输出。

编码每个原子块生成一组 added/done，关闭之前的 message/reasoning 项。调用 added 的 status 为 in_progress，done 保留 IR 调用状态；结果 added/done 均保留 completed/failed，不合成 in_progress 结果。终止 response.output 包含按原生顺序完成的所有项。

原生 sequence_number、SSE framing 与其他工具事件沿用既有 profile，不改变 sse 包职责。

## 5. 错误、审计与目标降级 — N-AP-5

- 非法 JSON、已知必填字段缺失/null/类型错误、IR 未知变体和非法条件字段：error，无部分结果。
- wire 未知 operation.type：单位级 unsupported-semantic，不解析其未知 Operation 子字段。
- wire 未来未知 status 字符串：unmapped-value，不猜为 completed/failed。
- wire delete Operation 的额外 diff：按存在记录 unmapped-field，不解析它的值；canonical IR delete 带 diff 则非法。
- allowed_callers/caller/created_by 等未建模属性按存在记录 unmapped-field，包括 null/false/空值。按来源顺序合并 loss，整个被丢弃单位不重复报告内部属性；Path/Diff/output 的字符串内容不递归审计。

Chat Completions/Anthropic 当前 profile 无专用等价映射，每个专用声明、choice、调用、结果各一个 unsupported-semantic。MUST NOT 将 Operation 包为 function 参数、普通代码、同名 custom 工具或提示词注入。

过滤后只在整个 turn 为空时保留合法空文本；仍有 function 结果时不增加额外空 user turn。所有声明均被丢弃时省略 any/required 并报告 unsupported-semantic。确实丢弃调用、没有保留调用且原 stop_reason=tool_use 时才归一化 end_turn 并报告 degraded，不改变其他合法停止原因。

流式每个跳过块一次 loss，仍校验其原子语法；不消耗目标原生块/工具索引，保持保留项连续索引、最终 usage 与终止。两类结果/调用之间不能靠同名推断。

## 6. 固定来源

OpenAI Python SDK commit `e71d781ac5e7b3f365ac130bf338baaf13ae720d`：responses/apply_patch_tool_param.py、tool_choice_apply_patch_param.py、response_input_item_param.py（ApplyPatchCall/ApplyPatchCallOutput）、response_apply_patch_tool_call.py、response_apply_patch_tool_call_output.py、response_output_item.py、response_stream_event.py 与 response_output_item_done_event.py。
