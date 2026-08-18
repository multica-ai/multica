# Extension 资源源码查看

## 目标

在 Extensions 页的“资源清单”中，用户可查看导入版本的内部 Agent 与内部 Skill 的原始文件内容，而不改变 Extension 版本资源。

## 数据边界

`platform_extension_release.manifest` 已持久化规范化后的完整 Bundle：

- Agent 的 `key`、描述和 Markdown prompt；
- Skill 的 `key`、`SKILL.md` 与所有嵌套文件；
- 二进制 Skill 文件以 base64 内容和 `encoding: "base64"` 标识保存。

前端复用现有 Extension detail API 返回的 `manifest`，不新增文件上传、下载、写入或变更 API。

## 交互

- “资源清单”中的每个内部 Agent 与每个 Skill 为可点击只读资源项。
- 点击后打开宽弹框。左侧为文件树；右侧为所选文件内容。
- Agent 合成一个文件：`agents/<agent-key>.md`。默认选中该文件。
- Skill 展开为 `skills/<skill-key>/SKILL.md` 和其余嵌套路径。默认选中 `SKILL.md`。
- 文本文件以保留空白的等宽代码区域显示；二进制文件显示“二进制文件”及其字节大小，不尝试文本渲染。
- 弹框与文件内容均只读；关闭后不改变当前资源清单或导入映射状态。

## 兼容与测试

- 无 manifest 或缺少某项内容时显示只读空状态，不能使 Extension 页崩溃。
- 普通 Command 映射与运行时选择不受影响。
- 测试覆盖 Agent 文件、嵌套 Skill 文件、base64 文件、默认选择和只读行为。
