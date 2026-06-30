# Web 品牌改造计划：Multica → CoStrict

## 1. 背景与目标

本计划用于指导 Multica Web 应用分阶段切换为 CoStrict 品牌。当前阶段只调整 Web 端的用户可见品牌，不进行仓库级全局改名，也不改变现有客户端、协议、部署配置或法律主体。

目标是让用户在官网和 Web 工作台中看到一致的 CoStrict 品牌，同时保证桌面端、CLI、自部署环境和既有集成不受影响，并为后续 Logo、域名及其他产品线迁移保留清晰边界。

## 2. 已确认范围

### 2.1 本期包含

- `apps/web/` 中公开官网、登录流程、工作台、错误页等页面的用户可见品牌文案。
- Web 页面的浏览器标题、描述、Open Graph、结构化数据和无障碍文本中的品牌名称。
- Web 会使用到的 `packages/views/` 共享页面和国际化文案，但必须通过平台品牌配置隔离，不能导致桌面端同步改名。
- 与本次品牌文案有关的 Web 测试和防回归检查。
- 上线前的商标及法律风险确认。

### 2.2 本期不包含

- `apps/desktop/` 桌面应用及其安装包、应用名称、Bundle ID、协议和自动更新配置。
- `apps/docs/` 文档站。
- 服务端发送的登录邮件及其他事务邮件。
- CLI、Daemon、Docker 镜像、Helm Chart、GitHub 仓库和发布流程。
- Logo、favicon、插图和其他视觉品牌资产；待 CoStrict 品牌资产提供后另行处理。
- 域名、邮箱和社交媒体账号；待 CoStrict 域名方案提供后另行处理。
- `@multica/*` 包作用域、`MULTICA_*` 环境变量、Cookie、Local Storage Key、HTTP Header、API 字段、数据库名称、分析事件名等技术标识。
- 根目录 `LICENSE` 和历史发布版本中的版权、许可文字。

## 3. 改造原则

1. **按语义替换，不做全局替换。** `Multica` 既可能是展示品牌，也可能是法律主体、技术标识或尚未改名的独立产品名称。
2. **Web 与桌面端品牌隔离。** 共享组件不得直接把默认品牌从 Multica 改为 CoStrict；Web 应显式注入 CoStrict，桌面端继续使用 Multica。
3. **链接目标与展示名称分离。** 本期允许页面显示 CoStrict，同时暂时链接到现有 `multica.ai` 或 GitHub 地址；代码中应保留后续集中替换入口。
4. **不伪装尚未改名的产品。** 指向现有桌面应用和 CLI 的下载、唤起及安装文案必须保留其真实产品名称，或使用清楚的过渡说明。
5. **不修改法律主体。** `Multica, Inc.` 只有在公司法定名称或权利归属完成变更并经法务确认后才能修改。
6. **旧品牌残留必须可解释。** 验收时每个保留的 `Multica` 都应属于明确的允许清单，而不是遗漏。

## 4. 名称处理规则

| 内容类型 | 本期处理 | 示例 |
| --- | --- | --- |
| 平台展示品牌 | 替换为 `CoStrict` | 页面标题、首页介绍、登录标题、导航和空状态 |
| Web SEO 品牌 | 替换为 `CoStrict` | title、description、Open Graph `siteName`、JSON-LD `name` |
| 现有域名和邮箱 | 暂时保留 | `multica.ai`、`support@multica.ai` |
| 包名和 import | 保留 | `@multica/core`、`@multica/views` |
| 浏览器存储和协议 | 保留 | `multica-locale`、`multica://` |
| Desktop/CLI 正式名称 | 暂时保留或增加过渡说明 | `Open Multica Desktop`、`Multica CLI` |
| 法律主体和版权 | 保留并交由法务确认 | `© 2025 Multica, Inc.` |
| 已持久化的用户数据 | 不批量改写 | 既有 `Multica Helper` Agent 名称、历史内容和评论 |

`Multica Helper` 不是普通静态文案：它会作为 Agent 名称持久化，并用于查找或复用现有 Agent。本期若希望新 Web 用户看到 `CoStrict Helper`，需要单独设计稳定标识和旧名称识别逻辑，避免重复创建 Agent。该项归入高复杂度任务，不得通过字符串替换完成。

## 5. 优先级与复杂度

| 优先级 | 复杂度 | 工作包 | 主要产出 |
| --- | --- | --- | --- |
| P0 | 中 | 商标与法律门禁 | 商标检索记录、风险结论、可使用地区和类别、法务批准 |
| P0 | 低 | 品牌词分类与允许清单 | “替换 / 保留 / 延后”清单，避免误改技术标识和法律文字 |
| P1 | 低 | `apps/web` 独有静态文案 | 官网、认证页、错误页及 Web 专属文案改为 CoStrict |
| P1 | 低 | Web 元数据 | title、description、Open Graph、JSON-LD 和无障碍名称统一 |
| P1 | 中 | 共享视图品牌隔离 | Web 注入 CoStrict，Desktop 默认继续显示 Multica |
| P1 | 中 | 中英文国际化文案 | Web 展示的英文和简体中文品牌文案同步更新 |
| P1 | 中 | Desktop/CLI 过渡文案 | 下载和唤起页面准确说明仍名为 Multica 的客户端 |
| P2 | 高 | `Multica Helper` 迁移设计 | 稳定身份、旧名称识别、新名称展示和防重复策略 |
| P2 | 中 | 防回归检查 | 面向用户可见内容的旧品牌扫描和允许清单 |
| P2 | 中 | 测试与验收 | Web 单元测试、关键流程测试和人工页面清单 |
| 延后 | — | 视觉资产、域名及其他产品线 | 等待 Logo、域名和后续范围确认 |

### 5.1 实施计划表

> 状态规则：`[ ]` 待开始，`[-]` 进行中，`[x]` 已完成，`[!]` 被阻塞。每个实施批次完成并通过对应验证后，必须在同一个变更中更新本表。

| 状态 | 批次 | 优先级 | 复杂度 | 改动 | 完成标记要求 |
| --- | --- | --- | --- | --- | --- |
| [ ] | 0.1 | P0 | 中 | 完成 CoStrict 商标检索和法务批准 | 附书面结论或内部审批链接；未完成时禁止生产发布 |
| [x] | 1.1 | P1 | 低 | 替换 Web 专属页面文案与全局元数据 | 相关页面和元数据测试通过，域名与技术标识未变化 |
| [x] | 1.2 | P1 | 低 | 替换 Landing Page 中英文营销文案 | 英文和简体中文关键文案测试通过 |
| [x] | 2.1 | P1 | 中 | 增加共享视图品牌配置和 Web 注入 | Web 显示 CoStrict，共享默认及 Desktop 保持 Multica |
| [x] | 2.2 | P1 | 中 | 将共享国际化品牌文案改为可注入占位符 | Auth、Workspace、Onboarding、Chat、Settings 按清单完成 |
| [x] | 3.1 | P1 | 中 | 处理 Desktop/CLI 下载与唤起的过渡文案 | 真实产物名称、URL 和 `multica://` 协议保持不变 |
| [!] | 4.1 | P2 | 高 | 设计并实施 `Multica Helper` 的稳定身份迁移 | 阻塞：涉及服务端持久化数据与 Desktop 并行行为，超出本期仅 Web 展示品牌范围 |
| [x] | 5.1 | P2 | 中 | 添加用户可见旧品牌扫描和允许清单 | 扫描可区分展示文案与技术标识 |
| [!] | 5.2 | P2 | 中 | 完成自动化与人工验收 | 阻塞：缺少 `.env`，且 Views 全量测试存在与本改造无关的既有失败；相关类型检查和目标测试已通过 |

实施纪律：

- 一次只推进一个批次；跨批次改动应拆分，避免状态失真。
- `[-]` 批次不得超过一个；遇到外部依赖时改为 `[!]` 并写明阻塞原因。
- `[x]` 只表示代码、测试和文档状态均已同步，不以“代码已写但未验证”作为完成。
- 0.1 可与开发并行，但在生产发布前必须完成。

## 6. 分阶段实施方案

### 阶段 0：法律与发布门禁（P0）

负责人建议：产品负责人、法务或商标律师。

- 检索 `CoStrict`、`Co Strict`、大小写变体、近似拼写、近似读音及近似图形。
- 覆盖实际经营和计划进入的国家或地区，并重点确认软件、可下载软件和 SaaS 等相关商品或服务。
- 初步可关注尼斯分类第 9 类和第 42 类；最终类别和商品服务描述必须由商标专业人员结合实际业务确认。
- 查询中国国家知识产权局、WIPO 全球品牌数据库以及目标市场官方商标数据库。
- 调查未注册但已经在软件、AI、开发工具或项目管理领域使用的近似名称，避免只依赖注册数据库。
- 明确 CoStrict 是产品品牌还是公司法定名称。若仅为产品品牌，不修改 `Multica, Inc.`、合同主体、版权权利人和隐私政策主体。
- 在注册状态确认前不使用 `®`；是否使用 `™` 由法务根据目标市场决定。

**退出条件：** 形成书面检索记录和风险结论，并由有决策权的负责人批准进入生产发布。数据库中未发现完全相同名称不等于不存在侵权或驳回风险。

官方检索入口：

- [WIPO Global Brand Database](https://www.wipo.int/en/web/global-brand-database)
- [国家知识产权局商标局](https://sbj.cnipa.gov.cn/)
- [USPTO Trademark Search](https://www.uspto.gov/trademarks/search)
- [USPTO：Likelihood of Confusion](https://www.uspto.gov/trademarks/search/likelihood-confusion)

### 阶段 1：Web 专属展示层（P1，低复杂度）

优先处理只被 Web 使用、不会影响其他应用的内容：

- `apps/web/app/layout.tsx` 中默认标题、标题模板、Open Graph 站点名等品牌字段。
- `apps/web/app/(landing)/` 中首页、About、Changelog、Contact Sales、Download 等页面的标题和描述。
- `apps/web/features/landing/` 中首页 Header、Footer、Hero、FAQ、功能演示和中英文营销文案。
- `apps/web/app/not-found.tsx` 等错误和兜底页面。
- Web 登录、认证回调页面中的平台品牌文案。

注意事项：

- `metadataBase`、canonical、sitemap、robots 和下载地址仍依赖旧域名，本期不更改 URL。
- `siteName` 和页面标题可改为 CoStrict；URL 保持旧域名是有意的过渡状态。
- 社交账号、GitHub 地址和下载产物名称暂时保留，不能伪造尚不存在的 CoStrict 地址。

### 阶段 2：共享视图品牌隔离（P1，中复杂度）

`packages/views/` 同时服务 Web 和 Desktop，直接修改共享翻译会使桌面端一起改名。推荐增加轻量的品牌配置能力：

- 定义品牌描述对象，至少包含 `productName`，默认值保持 `Multica`。
- Web 根 Provider 显式传入 `{ productName: "CoStrict" }`。
- Desktop 不传配置或继续传入 `{ productName: "Multica" }`。
- 共享组件和翻译使用品牌占位符，例如 `Sign in to {{productName}}`，不在组件中新增平台判断。
- 配置只承载展示信息，不把域名、API 地址或技术命名空间混入其中。

首批应覆盖的共享文案领域：

- Auth、Workspace 和 Onboarding 的欢迎与登录文案。
- Chat、Settings 和 Git 集成中的产品说明文案。
- Web 工作台内的空状态、提示、通知和无障碍标签。

对 `Multica CLI`、`Multica Desktop`、文档链接和 `Multica Helper` 进行单独分类，不直接套用 `productName` 占位符。

### 阶段 3：过渡产品文案（P1，中复杂度）

Web 目前包含下载桌面应用、唤起 `multica://` 和安装 CLI 的入口。因为这些产品本期不改名，推荐：

- 唤起按钮继续使用 `Open Multica Desktop`，或显示 `Open Multica Desktop (for CoStrict)` 之类经产品确认的过渡文案。
- 下载页明确下载产物仍名为 Multica，文件名和 GitHub Release 地址保持不变。
- CLI 安装命令、二进制名、环境变量和提示中的 `Multica CLI` 保持不变。
- 不修改 `multica://auth/callback`，否则会破坏已安装桌面端的认证回调。

该阶段的目标是避免用户在 CoStrict 网站下载或唤起一个名为 Multica 的程序时误以为遇到钓鱼、错误下载或安装包被篡改。

### 阶段 4：持久化品牌实体（P2，高复杂度）

`Multica Helper` 等名称会进入服务端数据，并参与已有 Agent 查找逻辑。推荐先形成独立设计，再决定是否纳入本期发布：

- 使用不随品牌变化的模板或系统角色标识识别 Helper，不依赖展示名称。
- 查找时兼容已有 `Multica Helper`，新建时可使用 `CoStrict Helper`。
- 不批量改写用户已经重命名或编辑过的 Agent。
- 确保 Web 和 Desktop 并行使用时不会各自创建一个 Helper。
- 对历史 Issue、评论、提示词和已生成内容保留原文，除非另有迁移需求。

在稳定标识上线前，建议保留 `Multica Helper`，并把残留记录在允许清单中。

### 阶段 5：防回归与测试（P2）

- 添加仅针对用户可见内容的旧品牌扫描。扫描规则不能简单禁止仓库出现 `multica`。
- 允许清单至少覆盖 import、包名、URL、邮箱、协议、Cookie、存储 Key、测试数据、Desktop/CLI 名称和法律主体。
- 为英文和简体中文页面分别验证品牌名称。
- 更新现有断言，例如登录标题、Not Found 返回文案、首页元数据和结构化数据。
- 增加一项隔离测试：Web 渲染 CoStrict，Desktop 或共享组件默认渲染 Multica。
- 人工检查公开官网、登录、认证回调、首次进入工作区、设置页、下载页和 404 页面。

## 7. 主要文件清单

以下是实施时的首要检查范围，不代表可以对目录进行批量替换：

- `apps/web/app/layout.tsx`
- `apps/web/app/(landing)/**`
- `apps/web/features/landing/**`
- `apps/web/app/(auth)/**`
- `apps/web/app/auth/callback/**`
- `apps/web/app/not-found.tsx`
- `apps/web/components/web-providers.tsx`
- `packages/views/locales/en/**`
- `packages/views/locales/zh-Hans/**`
- `packages/views/auth/**`
- `packages/views/onboarding/**`
- `packages/views/workspace/**`
- `packages/views/chat/**`
- `packages/views/settings/**`

仓库初步盘点约有 1,209 个受版本控制文件包含 `multica`（不区分展示文案与技术标识），其中 `packages/` 约 535 个、`server/` 约 377 个、`apps/` 约 207 个。这说明全局替换风险极高；本计划只处理上述 Web 展示路径及必要的共享视图适配。

## 8. 法律与合规风险

| 风险 | 影响 | 控制措施 |
| --- | --- | --- |
| CoStrict 与已有商标近似 | 注册被驳回、异议、侵权主张或被迫二次改名 | 上线前进行多地区、多变体、相关商品服务的完整检索并由专业人员复核 |
| 把品牌名误当公司主体 | 版权、合同、隐私政策和付款主体表述不一致 | 保留 `Multica, Inc.`，除非法律主体已经完成变更 |
| 修改定制开源许可证文字 | 旧版本权利、贡献者预期和许可解释产生争议 | 本期不修改 `LICENSE`；如需改名，由法务单独审查新版本许可和历史版本处理方式 |
| 使用 `®` 或误导性权利声明 | 产生虚假或不当商标标识风险 | 注册确认前不使用 `®`，其他标识按目标市场法律意见处理 |
| 品牌与域名、客户端名称不一致 | 用户误认钓鱼网站或错误安装包 | 下载和唤起流程增加明确过渡文案，不虚构新域名或新客户端名称 |
| 遗漏第三方授权范围 | 新 Logo、字体、插图或图标无法合法商用 | 后续资产交付时记录作者、授权条款、地域、期限和可修改范围 |

本节是项目风险控制计划，不构成具体司法辖区的法律意见。最终商标可用性和发布决定应由有资质的法律专业人员确认。

## 9. 验收标准

### 9.1 功能验收

- 官网和 Web 工作台的目标用户可见平台品牌统一显示为 CoStrict。
- 英文和简体中文页面不存在未经允许的 Multica 展示品牌残留。
- 浏览器标题、SEO、Open Graph、JSON-LD 和无障碍名称与 CoStrict 一致。
- Web 登录、工作区切换、下载和桌面认证回调功能保持正常。
- Desktop、CLI、文档站、事务邮件和自部署配置没有因本次改造发生名称或行为变化。

### 9.2 技术验收

- `@multica/*`、`MULTICA_*`、`multica://`、Cookie、存储 Key 和 API 契约未被改名。
- 共享品牌配置有默认值，未包裹 Provider 时不会崩溃。
- Web 与 Desktop 的品牌隔离有自动化测试覆盖。
- 所有保留的用户可见 `Multica` 均记录在允许清单中并注明原因。
- 按仓库要求通过完整验证流水线 `make check` 后才能合并实施代码。

### 9.3 发布验收

- 商标和法律门禁已有书面批准。
- 产品、设计、研发和运营共同确认过渡文案。
- 发布说明明确这是 Web 品牌切换，Desktop、CLI 和域名仍处于后续迁移阶段。
- 具备回滚方案：恢复 Web 品牌配置和元数据，不回滚或迁移用户数据。

## 10. 后续阶段

待 CoStrict Logo 和域名提供后，另建实施任务处理：

1. Logo、favicon、社交分享图和视觉规范。
2. 新域名、canonical、sitemap、robots、OAuth 回调及重定向。
3. 邮箱、社交账号、分析平台和外部服务配置。
4. 文档站和事务邮件。
5. Desktop、CLI、协议、安装包、签名和自动更新迁移。
6. GitHub、容器镜像、Helm、包作用域和其他技术标识。

这些任务必须独立评估兼容周期和回滚策略，不属于本期 Web 展示品牌改造。
