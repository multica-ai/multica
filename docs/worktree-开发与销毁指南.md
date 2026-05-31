# worktree 开发与销毁指南

本文档说明在本仓库中如何使用 Git worktree 进行隔离开发，以及开发完成后如何安全销毁对应的本地资源。

## 适用场景

使用 worktree 的典型目标有两个：

1. 在不影响主 checkout 的前提下并行开发多个分支
2. 让每个 worktree 使用独立数据库和独立前后端端口，避免本地环境互相污染

## 先记住三个结论

1. `make worktree-env` 现在会直接完成 worktree 初始化，不再只是生成 `.env.worktree`
2. 每个 worktree 都会绑定自己的数据库、端口和 `.env.worktree`
3. worktree 资源不会跟着 E2E 自动销毁，回收入口是显式命令，不是测试结束自动清理

## worktree 生命周期总览

标准路径如下：

```bash
git worktree add ../multica-my-feature -b aircjm/my-feature develop
cd ../multica-my-feature

make worktree-env
make start-worktree
make check-worktree

make list-worktree-resources
make destroy-worktree FORCE=1
```

如果要从其他 checkout 直接删除整个 worktree，使用：

```bash
make remove-worktree WORKTREE_PATH=../multica-my-feature FORCE=1
```

## 第一步：创建 worktree

在主 checkout 或其他现有 checkout 中执行：

```bash
git worktree add ../multica-my-feature -b aircjm/my-feature develop
```

这一步只会创建 Git worktree 目录，不会创建数据库，也不会启动服务。

## 第二步：初始化 worktree

切换到新建的 worktree 目录后执行：

```bash
make worktree-env
```

这一步现在是高层入口，会直接完成完整初始化，包含：

1. 生成 `.env.worktree`
2. 生成独立数据库名
3. 生成独立前后端端口
4. 安装依赖
5. 启动共享 PostgreSQL 容器（如果未启动）
6. 创建 worktree 对应数据库
7. 执行 migration
8. 校验 worktree 数据库和端口配置是否就绪

### `.env.worktree` 里会保存什么

worktree env 至少会包含这些信息：

```bash
MULTICA_ENV_KIND=worktree
WORKTREE_NAME=multica-my-feature
WORKTREE_ROOT=/absolute/path/to/the/worktree
POSTGRES_DB=multica_multica_my_feature_702
POSTGRES_PORT=5432
PORT=18782
FRONTEND_PORT=13702
DATABASE_URL=postgres://multica:multica@localhost:5432/multica_multica_my_feature_702?sslmode=disable
```

其中：

1. `MULTICA_ENV_KIND=worktree` 用来让命令识别这是 worktree 环境
2. `WORKTREE_NAME` 和 `WORKTREE_ROOT` 用来描述资源归属
3. `POSTGRES_DB` 是该 worktree 独享的数据库
4. `PORT` 和 `FRONTEND_PORT` 是该 worktree 独享的前后端端口

## 第三步：在 worktree 中开发

初始化完成后，日常开发命令和主 checkout 类似，但要使用 worktree 专用入口：

```bash
make start-worktree
make stop-worktree
make check-worktree
```

### 为什么不要直接用 `make start`

仓库现在已经给 worktree 增加了门禁检查。只要当前 env 被识别为 worktree，`start`、`check`、`dev`、`test`、`migrate-up` 等基础命令也会先检查 worktree 是否已完成初始化。

但在协作习惯上，仍然建议优先使用：

```bash
make start-worktree
make check-worktree
```

这样更直观，也更不容易误解当前命令是跑在主 checkout 还是 worktree 上。

## 第四步：验证 worktree

推荐验证入口：

```bash
make check-worktree
```

这条命令会在当前 worktree 环境中执行完整验证。它依赖 `.env.worktree` 指向独立数据库，不会回到主 checkout 的 `multica` 数据库。

如果只是想查看当前 worktree 到底绑定了哪些本地资源，先执行：

```bash
make list-worktree-resources
```

它会输出：

1. worktree env 路径
2. worktree 根目录
3. 当前分支
4. 对应数据库名
5. 数据库是否存在
6. 前后端端口
7. 当前端口上是否仍有监听进程

## 数据库什么时候创建

数据库创建发生在初始化阶段，而不是启动阶段。

也就是说：

1. `git worktree add` 不创建数据库
2. `make worktree-env` 会创建数据库并初始化 schema
3. `make start-worktree` 只负责启动服务，不负责补建数据库

## 数据库什么时候销毁

数据库**不会**在这些时机自动销毁：

1. E2E 跑完之后
2. `make stop-worktree` 之后
3. `make db-down` 之后
4. 直接删除 worktree 目录之后

原因是当前仓库把“测试结束”和“环境回收”分成了两个显式阶段。测试结束不代表你不再需要保留数据进行排查，所以默认不自动 drop 数据库。

## 第五步：销毁当前 worktree 的本地资源

如果你还在该 worktree 目录里，只想回收这个 worktree 对应的数据库、端口和 env 文件，执行：

```bash
make list-worktree-resources
make destroy-worktree FORCE=1
```

### `make destroy-worktree FORCE=1` 会做什么

它会按顺序执行：

1. 读取 `.env.worktree`
2. 检查这个 env 是否真的是 worktree env
3. 拒绝主 checkout 的数据库和端口，避免误删
4. 停掉当前 worktree 占用的前后端监听进程
5. 删除对应 `POSTGRES_DB`
6. 删除 `.env.worktree`

### 为什么必须传 `FORCE=1`

因为这一步会真的 drop 数据库，属于不可逆本地操作。默认不带 `FORCE=1` 时，只会打印待销毁资源摘要，不会真的执行删除。

## 第六步：从其他 checkout 一次性删除整个 worktree

如果你已经回到主 checkout，或者身处另一个 worktree，需要把目标 worktree 连同本地资源一起删掉，执行：

```bash
make remove-worktree WORKTREE_PATH=../multica-my-feature FORCE=1
```

### `make remove-worktree` 会做什么

它会按顺序执行：

1. 定位目标 worktree 的 `.env.worktree`
2. 调用 `destroy-worktree` 回收数据库、端口和 env 文件
3. 执行 `git worktree remove`

### 一个重要限制

不能在目标 worktree 自己的 shell 里删除它自己。也就是说：

1. 如果你在 `../multica-my-feature` 目录里，不能直接对这个目录执行 `make remove-worktree`
2. 你应该先回到主 checkout，或者切到另一个 checkout，再执行删除

如果你当前就在目标 worktree 里，先执行：

```bash
make destroy-worktree FORCE=1
```

然后回到其他 checkout 再执行：

```bash
make remove-worktree WORKTREE_PATH=../multica-my-feature FORCE=1
```

## 低层命令说明

大多数场景只需要高层入口，不需要直接使用低层脚本。

### `make worktree-env`

高层入口。推荐默认使用。会直接完成完整初始化。

### `make setup-worktree`

显式初始化入口。适合你已经有 `.env.worktree`，只是想重新执行 setup 或 migration 的场景。

### `make init-worktree-env`

低层入口。只负责生成 `.env.worktree`，不负责安装依赖、创建数据库或执行 migration。除非你明确知道自己只想生成 env 文件，否则不要单独使用。

## 推荐的完整操作示例

### 例子一：新建一个功能 worktree 并开始开发

```bash
git worktree add ../multica-project-labels -b aircjm/project-labels develop
cd ../multica-project-labels

make worktree-env
make start-worktree
make check-worktree
```

### 例子二：开发完成后回收当前 worktree 的本地资源

```bash
make list-worktree-resources
make destroy-worktree FORCE=1
```

### 例子三：从主 checkout 彻底删除一个旧 worktree

```bash
cd /path/to/main-checkout
make remove-worktree WORKTREE_PATH=../multica-project-labels FORCE=1
```

## 常见问题

### 1. 为什么 E2E 跑完数据库没有自动删除

因为 E2E 结束后你可能还需要排查失败数据、复现 bug 或继续调试。把 drop 数据库绑到测试结束会误删仍在使用的环境。

### 2. `make stop-worktree` 和 `make destroy-worktree` 有什么区别

`make stop-worktree` 只停服务进程，不删数据库，不删 env 文件。  
`make destroy-worktree FORCE=1` 会真正回收 worktree 本地资源。

### 3. `make db-down` 会删除 worktree 数据库吗

不会。`make db-down` 只是停掉 PostgreSQL 容器，数据卷还在，本地数据库仍然保留。

### 4. 如果 `destroy-worktree` 拒绝执行怎么办

优先检查：

1. `.env.worktree` 是否存在
2. `MULTICA_ENV_KIND=worktree` 是否存在
3. `POSTGRES_DB` 是否误指向主库
4. 当前端口是否和主 checkout 冲突

最简单的修复方式通常是重新执行：

```bash
make setup-worktree
make list-worktree-resources
```

确认资源归属正确后，再执行销毁。

## 命令速查

```bash
# 创建并初始化 worktree 环境
make worktree-env

# 启动 / 停止 / 验证当前 worktree
make start-worktree
make stop-worktree
make check-worktree

# 查看当前 worktree 绑定资源
make list-worktree-resources

# 销毁当前 worktree 的本地资源
make destroy-worktree FORCE=1

# 从其他 checkout 删除整个 worktree
make remove-worktree WORKTREE_PATH=../multica-my-feature FORCE=1
```
