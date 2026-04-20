# Multica 用户使用说明（中文）

本文档面向最终用户，介绍如何连接公司部署的 Multica 服务、如何登录、如何连接本地 Runtime，以及如何把任务分配给 Agent。

## 1. Multica 是什么

Multica 是一个多 Agent 协作平台。

你可以把它理解为：

- 用 Web 页面管理任务
- 用本地 Runtime 执行 Claude / Codex / OpenClaw 等 Agent
- 把 Issue 分配给 Agent，让 Agent 像同事一样接单、执行、反馈进度

## 2. 你需要准备什么

开始前，请确认你已经具备：

- 一个可访问的 Multica Web 地址
- 一个可访问的 Multica Server 地址
- 本机已安装至少一种 Agent CLI，例如：
  - `claude`
  - `codex`
  - `openclaw`
  - `opencode`
- 本机已安装 `multica` CLI

## 3. 如何配置远程服务地址

### 3.1 配置 Web 地址

```bash
multica config set app_url <前端地址>
```

示例：

```bash
multica config set app_url http://127.0.0.1:13030
```

### 3.2 配置 Server 地址

```bash
multica config set server_url <后端 ws 地址>
```

示例：

```bash
multica config set server_url ws://127.0.0.1:13080/ws
```

### 3.3 查看当前配置

```bash
multica config list
```

如果当前版本没有 `config list`，可以直接查看 CLI 配置文件，或重新执行 `multica setup self-host`。

## 4. 如何登录 Multica

### 4.1 浏览器登录

在浏览器打开 Web 地址，例如：

```text
http://127.0.0.1:13030
```

然后根据当前部署状态选择：

1. **邮箱验证码登录**：输入邮箱后点击继续，再输入验证码
2. **飞书登录**：如果页面显示 `Continue with Feishu`，点击后会跳转到飞书授权页；如果看不到这个按钮，通常说明当前环境还没有配置飞书应用参数

### 4.2 当前环境的验证码说明

如果当前部署还没有接入正式邮件服务或企业鉴权，开发环境可以使用：

```text
888888
```

注意：

- 这是开发/测试环境兜底验证码
- 正式环境接入飞书企业登录后，登录流程会切换为企业身份认证

### 4.3 CLI 登录

如果你希望本地 `multica` CLI 与远端服务打通，执行：

```bash
multica login
```

执行后通常会：

- 自动打开浏览器
- 跳转到 Multica 登录页
- 登录完成后回传 token 给本地 CLI

## 5. 一条命令完成初始化

如果服务地址已经固定，推荐直接执行：

```bash
multica setup self-host --server-url <后端地址> --app-url <前端地址>
```

示例：

```bash
multica setup self-host --server-url ws://127.0.0.1:13080/ws --app-url http://127.0.0.1:13030
```

这个命令通常会帮你完成：

- 写入远端地址
- 打开浏览器登录
- 拉取工作区信息
- 启动本地 daemon

## 6. 如何启动本地 Runtime

### 6.1 启动 daemon

```bash
multica daemon start
```

### 6.2 查看 daemon 状态

```bash
multica daemon status
```

### 6.3 daemon 的作用

daemon 会在你的电脑上做三件事：

- 向 Multica 报告你的机器在线
- 检测本机可用的 Agent CLI
- 当 Agent 被派单时，在你的电脑上执行任务

## 7. 如何确认本机已连接成功

进入 Web 页面后：

1. 打开对应工作区
2. 进入 **设置 → Runtimes**
3. 看到你的机器在线，说明连接成功

如果没有看到：

- 检查本机 `multica daemon status`
- 检查 `server_url` 是否正确
- 检查 Web 页面是否登录到了正确工作区

## 8. 如何创建 Agent

在 Web 页面中：

1. 进入 **设置 → Agents**
2. 点击创建 Agent
3. 选择你的 Runtime
4. 选择 provider（Claude / Codex / OpenClaw / OpenCode）
5. 填写 Agent 名称和说明

创建完成后，这个 Agent 就可以被分配任务。

## 9. 如何给 Agent 分配任务

### 9.1 创建 Issue

你可以在 Web 页面里新建 Issue，也可以用 CLI：

```bash
multica issue create --title "修复登录页异常" --description "请排查验证码登录失败问题"
```

### 9.2 指派给 Agent

在看板或 Issue 详情里，把负责人改成对应 Agent。

### 9.3 执行后会发生什么

Agent 会：

- 自动领取任务
- 在绑定的 Runtime 上执行
- 通过评论、状态变化、消息流反馈执行过程

## 10. 常见使用场景

### 10.1 让 Agent 修 bug

- 创建一个 bug issue
- 指派给某个 Agent
- Agent 在本地 Runtime 上执行

### 10.2 让 Agent 做小功能

- 写清楚目标和验收标准
- 指派给 Agent
- 观察执行进度和反馈

### 10.3 多个 Agent 分工

- 一个 Agent 做前端
- 一个 Agent 做后端
- 一个 Agent 做验证

Multica 的优势就是把多个 Agent 组织成团队协作方式。

## 11. 当前部署的访问方式说明

### 11.1 如果你是通过 SSH 隧道访问

你可能会看到这样的本地地址：

- `http://127.0.0.1:13030`
- `ws://127.0.0.1:13080/ws`

这说明当前服务还没有直接暴露到公网，而是通过隧道访问。

### 11.2 如果后续切换到正式域名

管理员会给你新的地址，例如：

- `https://multica.company.com`
- `wss://multica-api.company.com/ws`

这时只需要重新设置：

```bash
multica config set app_url <新前端地址>
multica config set server_url <新后端地址>
```

然后重新执行：

```bash
multica login
multica daemon restart
```

## 12. 常见问题

### 12.1 登录页打不开

请先确认：

- 你的前端地址是否正确
- 管理员是否已启动前端服务
- 你是否连着 SSH 隧道

### 12.2 登录后没有工作区

可能原因：

- 你还未被邀请加入工作区
- 登录到了错误账号
- 企业身份尚未绑定

### 12.3 Runtime 不在线

排查顺序：

1. `multica daemon status`
2. 检查本机是否安装了可用 Agent CLI
3. 检查 `server_url` 是否正确
4. 检查网络是否能访问后端 WebSocket 地址

### 12.4 Agent 接单了但不执行

重点检查：

- 本机 Agent CLI 是否可执行
- daemon 是否有报错
- 任务是否被指派给了正确的 Runtime

## 13. 推荐的首次使用流程

对于新用户，建议严格按下面顺序：

1. 获取前端地址和后端地址
2. 执行 `multica config set ...`
3. 执行 `multica login`
4. 执行 `multica daemon start`
5. 进入 Web 页面检查 Runtime 是否在线
6. 创建第一个 Agent
7. 创建一个测试 Issue 并指派给 Agent

## 14. 后续会发生的变化

当前登录方式仍可能是：

- 邮箱验证码登录
- Google 登录

后续公司版本计划新增：

- **飞书企业登录**
- 与企业身份体系打通
- 基于飞书用户信息自动识别成员身份

届时用户登录体验会更接近公司统一 SSO。