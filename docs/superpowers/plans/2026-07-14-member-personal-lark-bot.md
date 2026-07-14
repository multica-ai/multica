# 成员 PersonalAgent 飞书 Bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让每个 workspace 成员自助创建自己的飞书 PersonalAgent，并以该成员身份处理飞书入站与收件箱私信通知。

**Architecture:** 在 `lark_installation` 中增加成员安装类型和成员归属；将通知事件策略从 notification 安装记录迁移到 workspace 级策略；成员 Bot 走既有设备流、Hub、Dispatcher 和绑定表，通知器按收件人查成员安装。

**Tech Stack:** Go、PostgreSQL、sqlc、Chi、React、TanStack Query、Vitest。

---

### Task 1: 数据库与 sqlc

- [ ] 新增成员 Bot 与 workspace 通知策略迁移。
- [ ] 为成员安装、成员通知投递和策略读取/更新增加 sqlc 查询并生成代码。
- [ ] 通过 DB 迁移与生成代码编译检查。

### Task 2: 后端安装、入站与通知

- [ ] 增加成员自助设备流入口，并确保当前成员只能安装自己的 Bot。
- [ ] 将安装成功后的绑定与成员归属原子写入。
- [ ] 让通知器按收件人解析成员 Bot；保留旧 notification Bot 对历史引用回复的兼容。
- [ ] 覆盖权限、唯一性、入站成员身份和私信投递测试。

### Task 3: 前端设置

- [ ] 在飞书设置中添加成员自己的连接、扫码、状态和断开控件。
- [ ] 将管理员事件选择器改为 workspace 级策略接口。
- [ ] 更新 schema、API 客户端、多语言文案和组件测试。

### Task 4: 验证与上线手册

- [ ] 跑聚焦 Go、Core、Views 测试与类型检查。
- [ ] 更新本地数据库、重启服务并验证成员 Bot 安装和通知路径。
- [ ] 生成上线手册，涵盖迁移、灰度、飞书成员自助绑定、验收和回滚。
