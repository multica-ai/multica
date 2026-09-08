## 目标
扫描过去 24 小时与以下领域相关的外部信号：
- AI agent, task management, developer tools, team collaboration
- 竞品 site: 检索（brief 或 registry 中登记的 3 个域名）

## 输入
- 公开新闻、竞品、社媒、GitHub Trending（相关类目）
- 仓库 `docs/intel/` 与 `.delivery/` brief（若存在）
- 权威方案：`.ai-company/docs/35-product-intel-lounge.md`

## 输出
- 创建 Issue，标题 `intel/YYYY-MM-DD-daily`（UTC 日期）
- Issue 正文结构化（热点列表 + 今日建议 1 条）
- 飞书情报卡四块格式
- `docs/intel/YYYY-MM-DD-daily.md` 摘要
- Issue 评论 @product-analyst

## 约束
- 不编造来源；不改代码；不自动开工程票
