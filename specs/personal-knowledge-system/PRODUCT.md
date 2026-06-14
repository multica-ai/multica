# Personal Knowledge System

## Summary

Personal Knowledge System 是一个个人优先的 Markdown note 知识库。用户可以快速创建、编辑、归档、搜索自己的 note，并在未来把这些 note 作为工作记忆、上下文或 agent 可用知识的来源。

## Problem

当前产品已经有 issue、comment、daily review、daily plan、focus/energy 信号和 agent skill，但它们都不是稳定的个人知识库。用户需要一个低摩擦、个人可见、可搜索、可长期沉淀的 note 系统，先服务人类用户自己的记录和回看，再为后续工作记忆和 agent 上下文提供基础。

## Goals / Non-goals

1. V1 的目标是个人知识库，不是团队 Wiki。
2. V1 的核心对象是 note；note 可以很短，也可以很长。
3. V1 的 note 正文以 Markdown 为主要内容格式。
4. V1 支持手动创建 note，不支持从 issue、comment、daily review、daily plan、focus 或 energy 洞察中自动提取 note。
5. V1 不做文件导入、链接导入、RAG、embedding、语义搜索或知识图谱。
6. V1 不改变 Skill 的定位；Skill 不作为 Knowledge 的主承载体。
7. V1 不定义 AI/agent 自动使用 note 的行为。agent 使用 note 的方式后续单独设计。
8. V1 默认所有 note 只对创建者本人可见；团队可见性后续单独设计。

## Figma

Figma: none provided

## Behavior

1. 用户可以进入一个独立的 Knowledge 页面查看自己的个人知识库。

2. Knowledge 页面在桌面端使用双栏布局：
   - 左侧显示 note 列表、搜索、过滤和创建入口。
   - 右侧显示当前选中 note 的阅读或编辑内容。
   - 当没有选中 note 时，右侧显示空状态，引导用户选择或创建 note。

3. Knowledge 页面在移动端使用单栏布局：
   - 默认显示 note 列表。
   - 用户点开 note 后进入该 note 的详情/编辑视图。
   - 用户可以从详情/编辑视图返回列表。

4. 用户可以通过 Knowledge 页面里的创建入口新建 note。

5. 用户可以通过全局快捷入口新建 note：
   - 顶部 New 菜单中提供新建 note。
   - 全局 Command/Search 中可以通过 `New note` 命令创建 note。
   - Knowledge 导航入口附近提供加号快捷创建。

6. 新建 note 时，系统立即创建一条 draft note，并让用户进入编辑状态。用户不需要先填写弹窗表单才能开始记录。

7. 新建 note 的默认 title 是 `Untitled <timestamp>`，用于避免多个新建 note 在列表中重名。

8. 默认 title 中的 `<timestamp>` 使用用户本地时间，格式为 `YYYY-MM-DD HH:mm:ss`。

9. 默认 title 示例：

   ```text
   Untitled 2026-06-13 14:30:12
   ```

10. 用户第一次编辑 title 时，可以完全替换默认 title，不需要保留 `Untitled` 或 timestamp。

11. Note 的 title 是 V1 唯一的命名字段。

12. 用户可以编辑 note 的 title。

13. 用户编辑 title 时，只改变用户可读标题，不改变 note 的创建时间、更新时间或标签。

14. V1 不生成或展示独立文件名。

15. Markdown 是 note 正文的内容格式，不是用户需要管理的文件属性。

16. Note 列表默认按更新时间从新到旧排序。

17. Note 列表中的每条 note 至少显示：
    - 用户可读 title。
    - 更新时间或上次同步时间。
    - 标签。
    - 归档状态，若当前列表包含 archived note。

18. Note 列表不显示任何派生命名结构。

19. 用户可以编辑 note 的 Markdown 正文。

20. Markdown 编辑体验必须支持：
    - 输入和编辑 Markdown 源文。
    - 阅读或预览 Markdown 内容。
    - 在编辑和阅读/预览之间切换，或用等效方式让用户确认 Markdown 渲染效果。

21. Note 编辑使用自动保存。

22. 自动保存期间，用户可以看到保存状态。

23. 保存状态文案使用英文，至少包含以下状态：
    - `Saving...`
    - `Saved`
    - `Failed to save`
    - `Last synced at <time>`

24. `Last synced at <time>` 表示最近一次成功保存的时间。

25. 如果用户编辑 title、正文或标签，保存状态应从已保存状态变为待保存/保存中状态。

26. 如果自动保存成功，用户看到 `Saved` 和最新的 `Last synced at <time>`。

27. 如果自动保存失败，用户看到 `Failed to save`。

28. 自动保存失败时，用户正在编辑的内容不能被静默丢弃。

29. 自动保存失败后，用户继续编辑时，产品仍应保留最新本地编辑内容，并继续尝试保存。

30. 用户离开当前 note 时，如果存在尚未成功保存的编辑内容，产品允许用户离开，并在后台继续保存。

31. 如果用户离开当前 note 后后台保存成功，用户不需要额外确认。

32. 如果用户离开当前 note 后后台保存失败，产品必须在用户当前所在页面显示全局错误 toast，提示 note 保存失败。

33. 后台保存失败时，用户最新编辑内容不能被静默丢弃。用户回到该 note 时，应能继续看到本地保留的最新编辑内容或明确的保存失败状态。

34. Knowledge note 使用标签组织。

35. 标签支持从已有标签中选择。

36. 标签支持用户自由输入并创建。

37. Issue 和 Knowledge 共用同一套 workspace 标签。
    - 同一个标签可以用于 issue，也可以用于 note。
    - 用户不需要维护两套同名标签。
    - 标签名称、颜色或其他可见属性在 issue 和 note 中应保持一致。

38. Note 可以没有标签。

39. 没有标签的 note 仍应可以创建、保存、搜索和归档。

40. 用户可以按标签过滤 Knowledge note。

41. 用户可以搜索自己的 note。

42. Knowledge 页面内搜索至少匹配：
    - title。
    - Markdown 正文。
    - 标签名称。

43. Knowledge 页面内搜索结果只显示当前用户可见的 note。

44. Knowledge 页面内搜索不要求精确跳转到正文中的字符位置。

45. 当搜索命中正文时，产品可以显示命中片段，但 V1 不要求把编辑器或预览滚动到具体 offset。

46. 全局搜索必须纳入用户自己的 Knowledge note。

47. 全局搜索结果中，note 结果至少显示：
    - title。
    - 命中的正文片段，若命中来自正文。
    - 标签。
    - 更新时间。

48. 全局搜索结果中的 note 点击后打开 Knowledge 页面并选中对应 note。

49. 桌面端从全局搜索打开 note 时，Knowledge 页面右侧显示该 note。

50. 移动端从全局搜索打开 note 时，进入该 note 的详情/编辑视图。

51. 从全局搜索打开 note 时，V1 不要求精确滚动到正文命中位置。

52. 如果全局搜索结果来自正文命中，Knowledge 页面可以保留搜索词并高亮可见命中，但这不是 V1 必须行为。

53. 用户可以归档 note。

54. 归档后的 note 默认从主列表隐藏。

55. 用户可以通过 Archived 过滤查看已归档 note。

56. 已归档 note 仍可以被打开查看。

57. 已归档 note 可以继续编辑。

58. 用户可以取消归档 note，使其重新回到主列表。

59. V1 不提供删除 note 的用户操作。

60. V1 不提供 Trash、Deleted 过滤或恢复入口。

61. 归档不是删除；归档后的 note 仍应保留 title、正文、标签和更新时间。

62. 用户只能看到自己的 personal note。

63. 其他 workspace 成员默认看不到用户的 personal note。

64. Agent 默认不能自动使用用户的 personal note。

65. 除非后续功能明确设计，note 不会自动注入 agent task context。

66. 如果用户没有任何 note，Knowledge 页面显示空状态。

67. 空状态应提供创建 note 的入口。

68. 如果用户搜索后没有结果，Knowledge 页面显示搜索空状态。

69. 搜索空状态应允许用户清除搜索词。

70. 如果用户过滤 archived note 但没有 archived note，页面显示 archived 空状态。

71. 页面加载 note 列表时应显示加载状态。

72. 页面加载 note 详情时应显示加载状态或保持已知内容，避免界面空白闪烁。

73. 如果加载 note 列表失败，用户应看到错误状态，并可以重试。

74. 如果加载 note 详情失败，用户应看到错误状态，并可以返回列表或重试。

75. 如果用户打开的 note 已不存在或不可见，页面应显示“not found / unavailable”状态，并允许用户返回 Knowledge 列表。

76. 如果用户正在编辑 note，而同一 note 已在其他窗口或设备被更新，产品必须提示用户：“This note was updated elsewhere.”

77. 出现多窗口或多设备编辑冲突时，用户必须能选择：
    - 保留本地编辑内容并继续保存。
    - 重新加载远端最新内容。

78. 在用户做出冲突选择前，产品不能静默覆盖当前窗口中用户正在看的本地编辑内容。

79. V1 不要求实时协同编辑。

80. Knowledge note 的 title、正文、标签、归档状态和更新时间必须在刷新页面后保持一致。

81. 用户修改 title 后，note 仍然是同一条 note；列表、详情和搜索结果都应显示新的 title。

82. 用户修改标签后，title 不变。

83. 用户归档或取消归档 note 后，title 不变。

84. 用户搜索 note 时，archived note 默认不出现在普通搜索结果中，除非用户正在查看或过滤 archived note。

85. 全局搜索默认不返回 archived note。

86. V1 不要求从 issue、comment、daily review、daily plan、focus 或 energy 页面直接保存为 note。

87. V1 不要求 note 显示来源字段。

88. V1 不要求 note 关联 issue、comment、review 或 plan。

89. V1 不要求 note 导出为文件。

90. V1 不要求 note 导入文件或链接。

91. V1 不要求 note 转换为 Skill。

92. V1 不要求 Skill 出现在 Knowledge 页面。

93. V1 不要求 Knowledge note 出现在 agent skill 列表中。

94. Keyboard and accessibility:
    - Knowledge 页面中的创建、搜索、标签选择、归档、取消归档和编辑控件必须可通过键盘访问。
    - 搜索输入应有可识别 label 或等效可访问名称。
    - 保存状态应对屏幕阅读器可感知，或至少不依赖颜色作为唯一反馈。
    - 归档操作应有明确可访问名称。

95. 用户在 Knowledge 页面新建 note 后，焦点应进入可编辑区域或 title 输入，使用户可以立即开始记录。

96. 用户通过全局快捷入口新建 note 后，应直接进入新 note 的编辑状态。

97. 用户通过全局搜索打开 note 后，焦点应落在 note 详情区域或可读标题附近，而不是停留在全局搜索框中。

98. Knowledge 页面不应把个人 note 表现为团队共享知识。

99. Knowledge 页面不应暗示 agent 会自动使用这些 note。

100. 当未来增加 agent 使用 note 的能力时，必须让用户明确知道哪些 note 会被使用。

101. 当未来增加团队共享能力时，必须让用户明确知道哪些 note 对团队可见。
