# Anthropic 原生工具声明保真设计

## 已确认的目标与边界

用户确认 2.4.5 采用保留声明的方案：合法的 Anthropic 原生工具不因缺少 function input_schema 而导致整个请求失败；同协议恢复声明，目标协议无法表达时产生可归因的 unsupported-semantic loss。本次仅支持请求 tools 声明，不扩展 Advisor 的 server_tool_use、advisor_tool_result、流式或多轮历史映射。OXA 不执行工具、不处理 HTTP beta header、不验证模型能力。

## 实现选择

以下为实现方在既有架构内选择的具体表达，不是另行扩大用户确认范围：

- IR Tool 增加 type=native 变体，字段为 name、source、definition。source 初始支持 anthropic；definition 是原始声明 JSON 对象的文本字符串。保留现有 function/custom/apply_patch 形状和 0.2.0 IR contract 标记。
- 不把版本化 type 等同于服务端执行。Anthropic 声明的非空 type（除 function/custom）采用来源限定的原生变体；普通工具仍走既有 schema 校验。仅支持带非空 name 的工具声明；无 name 的 toolset 不在本补丁范围。
- definition 是明确限定在 Tool 声明边界的 opaque payload，而不是通用未知字段逃生舱。不扩展 NativeAnnotation 的允许字段或未知配置透传规则。
- 从原始 JSON 解码时保留整个声明的原始对象文本；来源内未知扩展字段属于完整原生声明，不重复报字段损失。普通工具的未知字段仍按既有审计规则报损。
- 同来源编码恢复完整声明；不修改嵌套 model，不做能力/版本白名单，也不重写缓存或嵌套输入。modelmap 仍只处理既有顶层映射点。
- 跨来源编码按原 IR 下标逐声明省略并报一条 loss；指向被省略原生声明的强制 tool_choice 也省略并报一条 loss。any 在所有声明被省略时沿用既有保护逻辑。
- 新工具类型要求消费方穷尽匹配增加 native 分支；不改变既有类型的意义。五语言以相同 schema、向量和 loss 文本实现。

## 验收

Advisor 最小声明无 decode error 或 loss；混合 function/native 工具顺序正确；保留 model、max_uses、caching、cache_control 和未知嵌套配置；原生客户端工具和未知版本 type 同样保留。普通工具缺 schema、非对象 schema，以及无效 discriminator/name/IR payload 仍为结构错误。同协议原始声明恢复，跨协议不伪造成 function、不输出悬空 choice。五语言通过新旧全部共享向量和各自回归测试。

## 发布边界

准备 2.4.5 的版本文件和变更说明；不 commit、push、创建 PR、合并、打 tag 或发布 registry。保留隔离 worktree 供用户检查。
