---
name: obita-coding-versioning
description: 所有和代码相关的变更操作，必须读一下这个技能中的规则并严格遵守。使用此技能版本管理方法，修改变更记录。
---

# obita-coding-versioning

## 指令说明
* 使用方法：当识别到用户意图为编码实现设计、代码变更时，会自动触发此技能并加载 REFERENCE 文件夹中的一个或几个相关的参考文件作为规范约束。
* 参考文件：在文件夹`{.Codex} 或 {.codex}/skills/obita-coding-versioning/REFERENCE`下，采用分层设计的规范文件，具体说明如下：
  - `版本管理办法.md`：用于检查需求编码实现质量，包括功能逻辑实现、代码设计、代码规范、代码质量等。
  - `设计文档更新.md`：用于更新`docs/system-design` 文件夹下的设计文档。
* 版本事实源：执行版本更新前，先读取 `docs/versioning/README.md`，确认当前活跃主版本目录和版本记录文件路径。

## 规划模式（写实现计划时）

在 `docs/...` 或 `openspec/...` 中的计划文件里必须明确：
1. 新版本号
2. 需要新增/更新的 version 文件
3. 版本变更类型（一句话摘要）
