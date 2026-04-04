# Next.js 到 SPA 迁移方案

## 目标

这次不是在现有文档上做小修，而是重新定义迁移方案。

当前方案必须满足以下目标：

- 第一阶段只迁移工作区应用，不一次性迁走整个站点
- `/login` 和所有登录后页面改为单页面应用
- landing `/`、`/about`、`/changelog`、`/homepage` 第一阶段继续保留在 Next.js
- 用户看到的地址不变，继续使用 `/login`、`/issues`、`/issues/:id`、`/settings` 等现有路径
- 工作区单页面应用由 Go 提供访问
- landing 第一阶段继续由现有 Next.js 应用提供
- 路由方案固定为 TanStack Router，不再比较其他方案

## 为什么要重写原方案

原文档不适合直接执行，原因有四个：

1. 目标目录结构与当前仓库不匹配，默认所有页面都会进入一个新的 `src/routes`，但当前仓库里的页面、功能和状态并不是这样组织的。
2. 迁移范围写成了全站一次性切换，但当前 landing 和工作区对 Next.js 的依赖类型完全不同，不能按同一阶段处理。
3. 文档遗漏了现有代码里真实存在的 Next.js 依赖，尤其是地址跳转、查询参数、服务端 cookie/header、图片、字体、页面元信息、开发代理和首页跳转。
4. 文档没有交代第一阶段由谁来提供页面访问，导致 `/login`、`/issues` 和 `/` 谁接管没有答案。

## 第一阶段的明确边界

### 迁入 SPA 的路径

- `/login`
- `/issues`
- `/issues/:id`
- `/board`
- `/inbox`
- `/my-issues`
- `/agents`
- `/agents/:id`
- `/runtimes`
- `/skills`
- `/settings`

### 暂留在 Next.js 的路径

- `/`
- `/about`
- `/changelog`
- `/homepage`

### 第一阶段不做的事

- 不把 landing 一起改成 SPA
- 不在一个运行时里同时继续承载“旧工作区 Next 页面”和“新工作区 SPA 页面”
- 不为了兼容保留两套工作区实现

工作区可以分页面迁移，但 landing 不能继续和工作区混在同一套 Next.js 运行方式里不动。

## 第一阶段目标架构

第一阶段采用“双架构”：

```text
apps/
├── web/                # 现有 Next.js，第一阶段只负责官网页面
└── workspace/          # 新增 Vite + TanStack Router 工作区 SPA

server/
└── cmd/server/router.go
```

职责拆分如下：

| 部分 | 第一阶段职责 |
| --- | --- |
| `apps/web` | 只负责 landing、about、changelog、homepage |
| `apps/workspace` | 负责 `/login` 和全部工作区页面 |
| Go 服务 | 提供 `/api`、`/auth`、`/ws`；提供工作区 SPA；把官网路径转给 Next.js |

## 路径归属

第一阶段的对外入口必须由 Go 统一接管，然后按路径分发：

| 路径 | 归属 |
| --- | --- |
| `/api/*` | Go 现有 API |
| `/auth/*` | Go 现有认证接口 |
| `/ws` | Go 现有实时连接 |
| `/login` | 工作区 SPA |
| `/issues*` | 工作区 SPA |
| `/board` | 工作区 SPA |
| `/inbox*` | 工作区 SPA |
| `/my-issues` | 工作区 SPA |
| `/agents*` | 工作区 SPA |
| `/runtimes` | 工作区 SPA |
| `/skills` | 工作区 SPA |
| `/settings` | 工作区 SPA |
| `/`、`/about`、`/changelog`、`/homepage` | 转给现有 Next.js 官网 |

Go 需要同时承担两件事：

1. 为工作区 SPA 提供静态文件和路由回退
2. 在第一阶段把官网相关路径转发给现有 Next.js

这意味着第一阶段不是“移除 Next.js”，而是“把工作区先从 Next.js 里拆出来”。第二阶段再处理官网并最终移除 Next.js。

## 现有代码里必须替换的依赖

原文档把很多内容写成“基本不动”，这不准确。以下依赖必须在工作区迁移时显式替换。

### 1. 地址跳转和地址参数

当前工作区直接依赖 Next.js 的导航能力：

- `apps/web/app/(dashboard)/layout.tsx`
- `apps/web/app/(auth)/login/page.tsx`
- `apps/web/app/(dashboard)/inbox/page.tsx`
- `apps/web/app/(dashboard)/_components/app-sidebar.tsx`
- `apps/web/features/modals/create-issue.tsx`
- `apps/web/features/issues/components/issue-detail.tsx`
- `apps/web/features/issues/components/list-row.tsx`
- `apps/web/features/issues/components/board-card.tsx`
- `apps/web/features/issues/components/issue-mention-card.tsx`

这些地方分别依赖了：

- `useRouter`
- `usePathname`
- `useSearchParams`
- `next/link`

迁移到 SPA 后，这些调用必须统一替换为 TanStack Router 的跳转、匹配和查询参数能力。

### 2. 服务端 cookie 和 header

当前有两处布局直接读取服务端上下文：

- `apps/web/app/layout.tsx`
- `apps/web/app/(landing)/layout.tsx`

其中包含：

- 根布局用 cookie 决定语言
- landing 布局用 cookie 和 `Accept-Language` 决定初始语言

第一阶段里，工作区不再运行在 Next.js 下，因此工作区自己的 shell 不能再依赖这些服务端接口；landing 仍可继续使用它们，因为 landing 暂时保留在 Next.js。

### 3. Next.js 专属图片、字体和页面元信息

landing 页面当前依赖：

- `next/image`
- `next/font`
- `Metadata`
- `robots.ts`
- `sitemap.ts`

相关文件包括：

- `apps/web/app/layout.tsx`
- `apps/web/app/(landing)/layout.tsx`
- `apps/web/app/(landing)/page.tsx`
- `apps/web/app/(landing)/about/page.tsx`
- `apps/web/app/(landing)/changelog/page.tsx`
- `apps/web/app/robots.ts`
- `apps/web/app/sitemap.ts`
- `apps/web/features/landing/components/landing-hero.tsx`
- `apps/web/features/landing/components/features-section.tsx`

这些能力第一阶段全部留在官网 Next.js 一侧，不进入工作区 SPA 范围。

### 4. Next.js 的请求转发和首页跳转

当前运行方式还依赖：

- `apps/web/next.config.ts` 里的 `/api`、`/auth`、`/ws` 转发
- `apps/web/proxy.ts` 里首页根据登录 cookie 跳到 `/issues`

迁移后：

- 工作区 SPA 不能再依赖 Next.js rewrites
- 首页跳转逻辑也不能继续依赖 `proxy.ts`

第一阶段里，这两类能力要改为：

- 工作区 SPA 直接访问 Go 提供的 `/api`、`/auth`、`/ws`
- `/login` 和受保护页面的跳转由 SPA 路由守卫负责
- `/` 是否跳到 `/issues` 由官网路径策略决定，不再使用 Next.js 首页代理

## 代码组织原则

“薄路由、厚功能层”保留，但要换成 SPA 版本的写法。

### 路由层规则

- TanStack Router 的路由文件只负责挂载页面和声明守卫、查询参数、加载逻辑
- 不在路由文件里继续堆积页面实现

### 功能层规则

- 业务页面、组件、状态和 API 调用继续留在 feature 目录
- 能保持现有 `zustand + features + shared` 模式的，全部保持
- 只替换路由接入层，不重做业务状态模型

### 先抽离再接路由的页面

当前有部分页面实现仍直接写在 Next.js 路由文件内，第一阶段必须先抽到功能层，再接入 TanStack Router。至少包括：

- `apps/web/app/(dashboard)/agents/page.tsx`
- `apps/web/app/(dashboard)/settings/page.tsx`
- `apps/web/app/(dashboard)/inbox/page.tsx`

建议的目标位置：

```text
apps/workspace/src/
├── routes/
├── features/
│   ├── auth/
│   ├── inbox/
│   ├── issues/
│   ├── agents/
│   ├── runtimes/
│   ├── settings/
│   └── ...
├── shared/
├── components/
├── hooks/
└── main.tsx
```

第一阶段不引入新的共享包。工作区相关代码直接迁入 `apps/workspace`，官网需要保留的内容继续留在 `apps/web`，避免为了抽象而扩大改动范围。

## 第一阶段的实施顺序

### 阶段 0：准备和拆分边界

- 保留 `apps/web` 作为临时官网应用
- 新建 `apps/workspace`，使用 Vite + TanStack Router
- 确认 Go 成为外部统一入口
- 列出工作区页面与官网页面的路径归属

### 阶段 1：抽离工作区代码

- 把工作区通用组件、状态、API、hooks、业务页面从 `apps/web` 迁到 `apps/workspace/src`
- 只保留官网需要的 landing 相关内容在 `apps/web`
- 将仍写在 Next.js 路由文件里的大页面抽到功能层

### 阶段 2：接入 TanStack Router

- 建立工作区根路由、登录路由和受保护路由
- 用 TanStack Router 替换工作区里的 `next/link`、`useRouter`、`usePathname`、`useSearchParams`
- 在路由层实现：
  - 未登录访问受保护页面跳到 `/login`
  - 已登录访问 `/login` 跳到 `/issues`
  - `/inbox?issue=...` 等查询参数行为保持不变

### 阶段 3：切换运行方式

- Go 直接提供 `apps/workspace` 的静态打包产物
- Go 对 SPA 路由做 `index.html` 回退
- Go 将 `/`、`/about`、`/changelog`、`/homepage` 转发到 `apps/web`
- 停止依赖 `apps/web/next.config.ts` 为工作区提供 `/api`、`/auth`、`/ws` 转发

### 阶段 4：清理旧工作区壳

- 删除 `apps/web/app/(auth)` 和 `apps/web/app/(dashboard)` 下已迁走的工作区页面
- 删除只为工作区服务的 Next.js 路由接入代码
- 保留官网仍需要的 landing、metadata、robots、sitemap

## 工作区 SPA 的路由设计

第一阶段固定使用 TanStack Router 文件路由，建议结构如下：

```text
apps/workspace/src/routes/
├── __root.tsx
├── login.tsx
├── issues.index.tsx
├── issues.$id.tsx
├── board.tsx
├── inbox.tsx
├── my-issues.tsx
├── agents.index.tsx
├── agents.$id.tsx
├── runtimes.tsx
├── skills.tsx
└── settings.tsx
```

路由职责固定为：

- `__root.tsx`：工作区应用外壳、全局 provider、基础守卫
- `login.tsx`：登录页、CLI 回调参数处理
- 其他路由：只挂载对应 feature 页面

## 环境变量与运行方式

第一阶段文档必须明确以下三类配置。

### 1. 工作区 SPA 访问后端 HTTP 的地址

新增：

- `VITE_API_URL`

用途：

- 工作区 SPA 调用 `/api/*` 和 `/auth/*`

默认策略：

- 生产环境走同域地址，可为空或指向当前域
- 本地开发可指向 Go 服务地址

### 2. 工作区 SPA 访问实时连接的地址

新增：

- `VITE_WS_URL`

用途：

- 工作区 SPA 连接 `/ws`

默认策略：

- 生产环境走同域 `ws(s)://<current-host>/ws`
- 本地开发可显式指向 Go 的 WebSocket 地址

### 3. Go 转给临时官网的地址

新增：

- `MARKETING_SITE_ORIGIN`

用途：

- Go 在第一阶段把官网路径转发到仍在运行的 Next.js 应用

### 第一阶段的实际访问链路

```text
Browser
  -> Go
      -> /api/*, /auth/*, /ws        (Go 直接处理)
      -> /login, /issues*, ...       (Go 返回 workspace SPA)
      -> /, /about, /changelog ...   (Go 转发到 Next.js 官网)
```

## 必须保留的用户行为

以下行为在第一阶段切换后必须保持不变：

- 未登录访问 `/issues` 会进入 `/login`
- 已登录访问 `/login` 会进入 `/issues`
- `/inbox?issue=...` 仍能根据地址参数选中详情
- 从列表进入详情、从弹窗进入新建结果页、从侧边栏切换页面，结果与现在一致
- 退出登录后回到登录页
- `/api` 和 `/ws` 地址不变
- landing 页面继续可以从原地址访问

## 完成标准

这份迁移文档只有在满足以下条件时才算完成：

1. 清楚区分第一阶段和第二阶段，不再把整个站点写成一次性迁移。
2. 明确指出 `/login` 属于第一阶段 SPA 范围。
3. 明确指出 landing 第一阶段继续留在 Next.js。
4. 明确指出 Go 是第一阶段统一入口，负责工作区 SPA 和官网转发。
5. 明确列出当前必须替换的 Next.js 依赖，而不是笼统写“组件基本不用改”。
6. 明确说明工作区代码要保持“薄路由、厚功能层”，并点出需要先抽离的大页面。
7. 明确写出环境变量、访问链路、验收标准和测试范围。

## 测试计划

### 页面级检查

- 登录页加载、发码、验码、登录后跳转
- 未登录访问受保护页面时的跳转
- 已登录访问 `/login` 时的回跳
- Issues 列表到详情的进入和返回
- Create issue 后进入新建结果页
- Inbox 地址参数与选中项同步
- 退出登录后回到 `/login`

### 服务端检查

- Go 对 `/login` 和工作区路径返回 SPA 内容
- Go 对工作区路径进行 `index.html` 回退
- Go 对 `/`、`/about`、`/changelog`、`/homepage` 正确转发到 Next.js
- Go 不会把 `/api`、`/auth`、`/ws` 错误转到前端

### 端到端检查

- 现有认证、导航、Issue 主流程继续通过
- 新增官网冒烟检查：
  - `/` 可访问
  - `/about` 可访问
  - `/changelog` 可访问

## 第二阶段

第二阶段再处理官网，并完成最终的 Next.js 下线：

- 将 landing 从 Next.js 迁出
- 替换 `next/image`、`next/font`、`Metadata`、`robots`、`sitemap`
- 移除官网转发
- 移除 `apps/web`

在第一阶段完成前，不进入这一步。
