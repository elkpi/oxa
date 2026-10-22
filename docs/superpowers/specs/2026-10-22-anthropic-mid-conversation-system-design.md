# Anthropic 会话中间 System 消息映射与保真设计 (v2.4.6)

## 已确认的目标与边界

用户确认升级小版本号为 v2.4.6，并在本次修复中同步覆盖 Go、TypeScript、Python、Rust、C++ 全部 5 门语言。
问题核心在于：Anthropic Messages API 在当前主流模型上已将会话中间（mid-conversation）`role: "system"` 消息作为 GA 特性支持，Claude Code 等官方客户端在 `/v1/messages` 中普遍发送此结构；OXA 目前在各语言解码器中直接将其作为未知角色拒绝（`unknown role "system"`），导致下游网关无法走 OXA 路径。

本次修订在不改变 IR `0.2.0` contract 的前提下，采用折叠至请求级 `Request.System[]` 并记录 `degraded` 损失的标准方案：
- 严格遵循 Anthropic 官方文档的位置约束与内容约束；
- 将消息内的文本内容（字符串或 text block 数组）追加至 `Request.System`，并保留文本块上的 `cache_control` 原生注解；
- 由于 IR 请求级 system 位于消息序列前缀，无法表达指令生效的中间位置，每次解码折叠记录一条 `degraded` 损失；
- 保持 IR 消息角色封闭集（`user` | `assistant`）不变，无需重构 IR 契约或下游多协议转换器；
- 同步补齐规范定义（`spec/12` N-AN-15、`spec/02` 损失目录）、共享黄金向量及 5 门语言实现与版本元数据。

## 规范与契约选择

1. **协议角色扩展**：Anthropic Messages Wire 的 `Message.Role` 扩展为已知集合 `user` | `assistant` | `system`。
2. **位置校验规则 (Placement Validation)**：
   - 规则 A：`messages[0]` 严禁出现 `role: "system"`，首个指令必须使用顶层 `system` 字段；违规属结构错误：`anthropic: messages[0]: system message cannot appear at messages[0]`。
   - 规则 B：连续的 system 消息块合并评估；其前驱消息必须为 `user` 角色（或以 server tool 结束的 assistant 角色）。
   - 规则 C：system 消息必须是 `messages` 数组的末尾项，或者紧随其后的必须是 `assistant` 角色消息。若 system 消息后直接跟随 `user` 消息，属结构错误：`anthropic: messages[i]: system message must be followed by an assistant turn or be the last message`。
3. **内容校验规则 (Content Validation)**：
   - system 消息内容支持纯文本字符串或仅含 `type: "text"` 的 content block 数组。
   - 若出现非文本块（如 `image`、`tool_use`、`tool_result` 等），属结构错误：`anthropic: messages[i]: system content must be text`。
4. **IR 映射与损失定义**：
   - 将解码出的文本块依次追加到 `ir.Request.System` 末尾（在顶层 `system` 解码块之后）。
   - 若文本块携带合法的 `cache_control`，按既有 `spec/15` 规则保留为该 `SystemBlock` 上的 `NativeAnnotation`。
   - 每一条被折叠的 mid-conversation system 消息，产生且仅产生一条 `degraded` 损失：
     - `path`: `messages[i].role`
     - `field`: `role`
     - `reason`: `degraded`
     - `detail`: `"Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved."`
5. **编码 (IR → Messages)**：
   - 无需变动。IR 的 `Request.System` 统一渲染至 Anthropic wire 的顶层 `system` 数组；IR `Messages` 保持纯粹的 `user` / `assistant` 对话。

## 验收标准

1. 规范 `spec/12-mapping-anthropic-messages.md` 增加 N-AN-15 规则与损失目录条目，`spec/README.md` 版本更新至 2.4.6。
2. 黄金向量库新增 `anthropic.nonstream.mid-conversation-system-to-ir` 向量，总向量数达到 347，`veccheck -check-manifest` 严格校验通过。
3. Go、TypeScript、Python、Rust、C++ 五门语言的单元测试及全量 347 个黄金向量 100% 运行通过。
4. 语言包与元数据文件统一更新至 v2.4.6。
