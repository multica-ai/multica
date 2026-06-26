# 时间显示与时区处理 Spec

> 本文是 `docs/timezone-architecture-rfc.md` 在**展示层**和 **deadline 语义**上的延伸。
> RFC 定义了「数据怎么存、报表怎么按 tz 切日界」的两轴模型;本 spec 定义「**任意时间在 UI 上怎么显示**」「**locale 从哪取**」「**逾期/deadline 怎么判**」,并给出需要抽象的公共方法 / 组件与落地差距清单。
> 冲突时以 RFC 为准;本文只补 RFC 未覆盖的部分。

## 0. 目标

本 spec 服务于三个产品目标:

1. **locale 统一**:所有时间(及其他本地化文本)的 locale 全部取自 Settings 的 **Language** 配置项,缺省回退 `en`(精确语义见 §7.1 订正)。
2. **显示统一走 Viewing Timezone**:所有「用于显示的瞬时时间」一律按用户的 **Viewing Timezone**(`user.timezone`)转换后展示,覆盖 web、desktop 等所有应用。
3. **tooltip 统一**:web / desktop 上所有时间显示都带 tooltip,展示**完整时间 + 时区信息**。

---

## 1. 时区轴模型(结论)

延续 RFC,系统只有**两个时区轴**,外加一类**不属于任何轴**的浮动日历日。每个字段只回答一个问题。

| 轴 / 类别 | 在回答什么 | 真值来源 | 典型字段 |
|---|---|---|---|
| **Scheduling(调度)** | 「这件事在**哪个 tz 的几点**触发 / 到期」——产出唯一绝对瞬间 | 挂在被调度对象上的 tz | `autopilot_trigger.timezone`;(将来)issue 的 deadline tz |
| **Viewing(查看)** | 「把一个瞬时**渲染给某个查看者**时用哪个 tz」 | 查看者 `user.timezone`(NULL=跟随浏览器) | 一切 TIMESTAMPTZ 的显示 |
| **浮动日历日(非轴)** | 「这是挂历上的**哪一天**」——对所有人恒定,**不带 tz** | `DATE` 字面值 | `issue.start_date` / `issue.due_date` |

**核心判别**:一个字段是「瞬时」还是「日历日」,决定它走 Viewing 还是浮动。

- **瞬时(instant)**:`created_at`、`updated_at`、`completed_at`、`received_at`、Inbox `created_at` 等所有 `TIMESTAMPTZ`。同一时刻在不同时区显示成不同钟点/日期是**正确**的 → 走 Viewing。
- **日历日(floating date)**:`start_date` / `due_date`。「截止 March 1」对所有人就是 March 1,**不转 tz**(转了就是 GH #3618 / MUL-2925,见迁移 112)。其逾期标红是「今天 vs 该日历日」的比较,「今天」取查看者 Viewing tz(见 §4)。

> **不引入 `workspace.timezone` 当 viewing 权威**(RFC §1.3.4 / §3.4)。它只能作为「新成员 Viewing tz 的默认种子」或「Scheduling 轴的默认值」,`user.timezone` 永远 override。

---

## 2. 存储约定(现状已满足,勿回退)

- 所有时间瞬时列均为 `TIMESTAMPTZ`,Go 侧统一 `pgtype.Timestamptz`,API 一律 `RFC3339` 序列化(`util.TimestampToString` / `TimestampToPtr`)。**存储全 UTC,本 spec 不改存储。**
- 日历日列为 `DATE`(`pgtype.Date`),经 `util.ParseCalendarDate` 写入,强制 UTC 午夜边界校验。当前**唯一**的 `DATE` 写入场景是 `issue.start_date` / `issue.due_date`。
- 约束:新增「瞬时」字段必须 `TIMESTAMPTZ`;新增「用户选的纯日期」字段才用 `DATE`,且不得对其做 tz 转换显示。

---

## 3. 显示规则

### 3.1 瞬时 → Viewing Timezone

所有 `TIMESTAMPTZ` 的展示,必须经过统一格式化入口,内部用 `Intl.DateTimeFormat(locale, { timeZone })`,其中 `timeZone` = `useViewingTimezone()`。

- 禁止在组件里直接 `new Date(x).toLocaleString(...)` / `toLocaleDateString(...)`(那是浏览器 tz,绕过设置)。
- 覆盖 web 与 desktop:格式化入口放在 `packages/views`(共享层),两端复用。
- **当年省略年份**:`mode:"date"`(纯日期,如 `<DateTime variant="date">`、`useFormatDateTime().formatDate`)在「该瞬时落在查看者当前日历年(按 Viewing tz 判定)」时**不显示年份**(`Mar 1`),跨年才带(`Mar 1, 2025`),消除同年冗余又保留跨年无歧义。`datetime`/`time` 模式与 tooltip(`formatInstantWithOffset`)**始终带年份**,不受影响。

### 3.2 日历日 → 浮动,不转 tz

`start_date` / `due_date` 继续走 `formatDateOnly`(`packages/core/issues/date.ts`,`timeZone:"UTC"` 锚定),**显示对所有人恒定**。不得接入 Viewing tz。

### 3.3 相对时间 → 子日免疫时区,日级别按 Viewing tz 的日历日,双向对称

- **方向对称**:梯度同时覆盖过去与未来。过去读 `3h ago`,未来读 `in 3h`;同一档位两个方向只差措辞,数值与边界一致。方向由 `relativeTimeBucket` 返回的 `future` 标志承载,`just_now` 无方向。
- **子日(< 24h)**:`just now / Nm ago / Nh ago`(未来 `in Nm / in Nh`),按 `|now - t|` 经过时长计算,**与 tz 无关**。
- **子分钟容差**:双向子分钟(`|now - t| < 1min`)都归 `just now`,顺带吸收时钟偏差——一个略微「未来」的服务器时间戳不会显示成 `in 30s`。
- **日级别(≥ 24h)**:按**查看者 Viewing tz 下的日历日差**计算(`1d ago` = 昨天、`2d ago` = 前天;未来 `in 1d / in 2d`),使「2d ago」与旁边显示的日期一致——避免「相隔 2 个日历日却显示 1d ago」。代价:日级别档位**依赖 Viewing tz**(跨午夜、不同 tz 的查看者可能看到不同档位),这是有意为之。
- **超过 30 个日历日 → 月,满 12 个月 → 年**:`Nmo ago` / `Ny ago`(未来 `in Nmo` / `in Ny`),**永远相对,不回退绝对日期**。月/年差按**查看者 Viewing tz 下的日历月/年**计算(满月计数,非 ÷30 天),与日级别的日历语义一致。
- 单一来源:纯函数 `relativeTimeBucket(thenMs, nowMs, timeZone)`(`packages/core/i18n/relative-time.ts`,mobile 共享)+ 视图 hook `useTimeAgo()`。**原 5 个分叉已收敛**(见 §5/§6)。
- **句中插值**:当时间嵌在已翻译整句里(`Updated {{time}}`),不拆句(会破坏 ja/ko 语序)、不引入 `<Trans>`,而是用 `<InstantTooltip value=…>` 把整条短语包一层 tooltip(见 §5)。
- **Scheduling 轴可借相对时间规避缺失 tz**:相对时间是**纯时长 / 日历差,本身与 tz 无关**,因此能在「拿不到被调度对象 tz」的列表场景安全表达未来触发时间。典型:autopilot 列表的 **next run**——列表接口的 `next_run_at` 是裸 UTC 瞬间且不带 trigger tz,渲染绝对墙钟要么误报(Viewing tz),要么落成无意义的服务器进程偏移;改用 `useTimeAgo()` 显示 `in 3h`,既省空间又无歧义,hover tooltip 再以 **UTC + GMT 偏移**给出精确瞬间。详情页拿得到 trigger,仍按 trigger tz 显示绝对时间(见 §6「不在本清单」)。
- **例外**:runtime「last seen」用 `useFormatLastSeen()`,保留**秒级 + 复合单位**(`2m 30s ago` / `2d 4h ago`)的连接活性精度,不并入 `useTimeAgo`(见 §6)。

### 3.4 locale → Language 设置,`en-US` 兜底

- 所有格式化入口的 `locale` 取自 **Language 设置**;未设置 / 非法时回退 `en`。
- 取消现存的 `"en-US"` 硬编码与「不传 locale 跟随浏览器」两种写法,统一走同一个 locale 来源。
- **current locale accessor** = `const { i18n } = useTranslation(); i18n.language`(短码 `en` / `zh-Hans` / `ko` / `ja`)。这些短码本身即合法 BCP-47,**直接传给 `Intl`,不做映射**(详见 §7.1)。

### 3.5 tooltip → 完整时间 + 时区(web / desktop)

- web / desktop 上**每一处**时间显示(含相对时间、日历日、绝对时间)都挂 tooltip。
- **瞬时**:Viewing tz 渲染 + **GMT 偏移后缀**(已定,见 §7.5),不带 IANA 名、不并列 UTC:
  - en:`Mar 1, 2026, 02:30:45 PM (GMT+8)`
  - zh:`2026年3月1日 14:30:45(GMT+8)`
- **日历日**(`due_date` / `start_date`):纯日期 tooltip,**无时分、无时区**(en `March 1, 2026` / zh `2026年3月1日`)。
- **实现约束(`Intl` 实测坑,务必遵守)**:
  - `timeZoneName` **不能**与 `dateStyle` / `timeStyle` 同用 → date+time 须用显式分量 opts(`year/month/day/hour/minute/second`)。
  - zh-Hans 下内置 `timeZoneName` 会把时区**塞进日期与时间之间** → **偏移后缀手动追加**:`formatToParts` 取 `shortOffset` token 拼到末尾,保证跨语言排序一致。
- mobile 无 hover,**不在本目标内**(如需等价能力另议长按方案)。

---

## 4. Deadline / 逾期 / 通知处理(补 RFC 未覆盖)

**结论:前端逾期标红是一次「日历日比较」,在查看者 Viewing tz 的今天里判定 —— 走 Viewing,与屏幕上看到的 `due_date` 同一真值。**

理由:标红是给「正在看屏的人」的提示,它要回答的是「对**这个查看者**而言,截止日是不是已经过去了」。`due_date` 本身是浮动日历日,但「今天是哪天」必须落在某个 tz 上才有意义;既然卡片上的 `due_date` 是按查看者展示的,标红就用同一个查看者的 Viewing tz,二者永远一致。

### 4.1 标红 / 「逾期」前端判定

- **「今天」锚点取查看者 Viewing tz**(`user.timezone`,NULL 跟随浏览器),与所有其它显示同源。
- `isPastDateOnly(due_date, timeZone)`(`date.ts`):把 `due_date` 与「Viewing tz 下的今天」都归到 UTC 午夜做纯日历日比较;`timeZone` 由调用方传入 `useViewingTimezone()`。非法 tz 回退 UTC,不抛。
- `due_date` 的存储不变:浮动 `DATE`,无时分、无 tz。

### 4.2 过期 → Inbox 通知(将来功能)

逾期一旦要被**后台物化**成一条通知,「**何时发**」就不能再是查看者属性 —— 后台 job 触发时没有「查看者」,必须是全队唯一的绝对瞬间。所以触发与标红是两件事:

1. **触发瞬间** = `end-of-day(due_date) AT TIME ZONE <canonical deadline tz>`,**canonical deadline tz**:
   - **v1 = `UTC`**(零 schema 改动)。
   - 将来如需更贴近设置者,加 `issue.due_timezone`,**创建/设 due_date 时静默捕获设置者当时的 Viewing tz**(仿 `autopilot_trigger.timezone`),默认链 `setter Viewing tz → workspace 默认(若引入) → UTC`。
2. **触发** = Scheduling:后台 job 在上述 canonical 瞬间写入一条 Inbox item,发一次,与查看者无关;assignee 是 agent / 为空也照常。
3. **显示** = Viewing:该 Inbox item 的 `created_at`(TIMESTAMPTZ)按各查看者 `user.timezone` 渲染,与其它时间戳一致。
4. **UI 不加 tz 选项**:create issue 表单只选日期。deadline tz 是后台静默捕获的元数据,不是用户决策(对比:cron 的 tz 是用户决策,因为「9 点」离开 tz 无意义;「截止 March 1」自带绝对感,用户无此意见)。

> **标红(Viewing)与通知触发(canonical)可能不在同一天**:UTC 负偏移的查看者在当地午夜前后,卡片可能还没标红,而 canonical=UTC 的通知已经发出(反之亦然)。这是两个轴各司其职的结果 —— 标红服务「正在看屏的这个人」,通知触发服务「全队唯一一次」。若将来要让两者贴合,走 §4.2.1 的 `issue.due_timezone` 把 canonical 对齐到设置者 tz。

---

## 5. 需要抽象的公共方法 / 组件

| 抽象 | 位置(建议) | 职责 |
|---|---|---|
| `useViewingTimezone()` | 已有 `packages/views/common/use-viewing-timezone.ts` | 读 `user.timezone`,NULL 回退浏览器 tz |
| `useFormatDateTime()` | 新增 `packages/views/common/` | 基于 Viewing tz + Language locale 封装 `formatDateTime` / `formatDate` / `formatTime`;内部 memoized `Intl.DateTimeFormat`。**替换所有走浏览器 tz 的内联 `toLocaleString` 瞬时显示** |
| `<DateTime>` 组件 | 新增 `packages/views/common/`,基于 `@multica/ui/components/ui/tooltip`(已有,§7.2) | 统一「可见文本(相对或绝对)+ tooltip(完整时间+tz)」成对模式;web/desktop 共用 |
| 统一相对时间 hook | `packages/views/i18n/use-time-ago.ts` + 纯函数 `packages/core/i18n/relative-time.ts` | **已落地**:`relativeTimeBucket(thenMs, nowMs, timeZone)` 单一档位逻辑(子日经过时长 + 日级别日历日 + 日历月/年延伸,双向对称、不回退绝对日期),`useTimeAgo()` 注入 Viewing tz + locale。原 5 套分叉(i18n / inbox-list-item / projects `useFormatRelativeDate` / chat `useFormatTimeAgo` / mobile)已收敛 |
| `<InstantTooltip>` | 新增 `packages/views/common/instant-tooltip.tsx` | 句中插值时间(`Updated {{time}}`)无法换成裸 `<DateTime>`(会破坏各 locale 语序)。把整条已翻译短语包一层 tooltip(完整时间 + GMT 偏移),零 i18n 改动、不引入 `<Trans>` |
| `useFormatLastSeen()` | 新增 `packages/views/runtimes/use-format-last-seen.ts` | runtime「last seen」连接活性:保留**秒级 + 复合单位**(原 `runtimes/utils.ts:formatLastSeen` 第 4 套分叉),改为 i18n(runtimes namespace)+ 配 `<InstantTooltip>`,不并入 `useTimeAgo` |
| `formatDateOnly` locale 收口 | 已有 `packages/core/issues/date.ts` | 调用方不再硬编码 `"en-US"`,locale 统一来自 Language 设置 |
| Viewing-tz overdue helper | 已有 `packages/core/issues/date.ts` | `isPastDateOnly(due_date, timeZone)` 在查看者 Viewing tz 的今天里判逾期;`timeZone` 由调用方传 `useViewingTimezone()`(§4.1) |

---

## 6. 现状差距清单(待修)

来自代码审计,按优先级:

1. **约 20 处瞬时显示内联 `new Date().toLocaleString()` 走浏览器 tz** → 全部替换为 `useFormatDateTime()`。**这是 Viewing Timezone 失效的主因。** 覆盖文件:
   - `settings/{lark-tab:258, tokens-tab:164/167/172}`
   - `autopilots/{webhook-deliveries-section:77, autopilots-page:376/948, autopilot-detail-page:71}`
   - `runtimes/cloud-runtime-dialog:393`、`runtimes/utils:855`
   - `common/task-transcript/agent-transcript-dialog:517/795/796`
   - `agents/agents-page:1177`、`squads/squads-page:991`
   - **`issues/{comment-card:526/798, issue-detail:558}` —— 活动/评论 `created_at` 的 tooltip(原审计漏列)**
   - `billing/billing-test-page:732`(仅此 `formatDate` helper;该文件其余 `toLocaleString` 都是数字,非时间)
2. **`isPastDateOnly` 用浏览器 tz** → 改取查看者 Viewing tz(§4.1),`timeZone` 由调用方传 `useViewingTimezone()`。
3. ~~**相对时间 5 个分叉实现** → 合并为单一 hook~~(**已完成**,§5)。审计原列 5 套,落地时另发现 runtime `formatLastSeen`(`runtimes/utils.ts`,秒级复合单位)第 6 套——按「保留秒级活性」处理为 `useFormatLastSeen` hook(i18n + tooltip),不并入 `useTimeAgo`。
   - **遗留**:agents 列表「Last active」是 30 天活跃 sparkline 的**按天桶**(`lastActiveDaysAgo`),agent 上无精确瞬时,故只统一了文案(`Today / Nd ago`)、**挂不了完整时间 tooltip**;要彻底统一需后端给 agent 暴露 `last_active_at` 瞬时。
4. **locale 不统一**:`formatDateOnly` **8 处**硬编码 `"en-US"`(list-row:30 / board-card:32 / issue-detail:183,233,238 / due-date-picker:54 / start-date-picker:58 / inbox-detail-label:41)+ `issues-header:119` 跟随浏览器 → 统一取 Language 设置。
5. **tooltip 缺失**:多数绝对时间无 tooltip,相对时间基本无 tooltip → 由 `<DateTime>` 统一补齐。
6. ~~app 层盲区~~:已扫描,**基本无**(见 §7.3)。产品时间显示全集中在 `packages/views`,app 层只有 landing/changelog 营销日期与 desktop 时长,不在改造范围。

> **明确不在本清单(不要动)**:
> - **Scheduling 轴显示**:`autopilots/pickers/timezone-picker:22`、`trigger-config:65/74`、`autopilot-dialog` 排程预览——用显式 tz 是对的,非 Viewing。autopilot 列表的 **next run** 是特例:列表接口不带 trigger tz,改用**相对时间**(tz 无关)+ UTC 偏移 tooltip 表达,见 §3.3。
> - **日历日 chart 标签**:`issues/gantt-view`、`runtimes/charts/activity-heatmap` 的轴/格子是按天日期标签,走日历日规则(浮动、不转 tz),非瞬时;仅 locale 需随 Language 收口,不接 Viewing tz。
> - **数字**:`runtimes/charts/*`、`billing` 里的 `total.toLocaleString()` 等是千分位数字格式化,与时间无关。

---

## 7. 落地前核实结论(已查实)

> 原标「待核实」的项已通过代码审计确认。除 §7.5 一项产品决策外,其余均已落定。

### 7.1 Language 设置 —— 已具备,可直接接入 ✅

- 存在:`user.language`(migration 060,`VARCHAR(20)` nullable),`PATCH /api/me` 同步(`UpdateMeRequest.language`),客户端 cookie `multica-locale`。
- 后端校验白名单 `supportedLanguages`:`en` / `zh-Hans` / `ko` / `ja`(`server/internal/handler/auth.go:47`,校验在 `:683`)。
- 框架 react-i18next,**current locale accessor** = `useTranslation().i18n.language`(返回短码 `en` / `zh-Hans` / `ko` / `ja`)。
- 短码是**翻译包 key**(语言 + script,故意不含 region:region 不改翻译文案),这是正确表示,不应改成区域限定形式。
- **这四个短码本身即合法 BCP-47,可直接传给 `Intl.DateTimeFormat`**(已实测:`en→Mar 1, 2026`、`zh-Hans→2026年3月1日` 等均正常)。**不需要映射表。**
  - 映射到 `en-US`/`zh-CN`/… 仅用于钉死某地区格式惯例(日期顺序、12/24h),而对这 4 个 locale 无实际差别(`en` 默认即美式)。故**默认直接透传 `i18n.language`**;仅当将来出现「同语言不同地区格式」需求(目前无)时再加可选映射。
- 默认/兜底:`en`(i18next `fallbackLng: "en"`,`create-i18n.ts:16`)。
- **订正**:目标 1 原写「`en-US` 兜底」,精确表述是「locale 取 `i18n.language`,缺省回退 `en`」。

### 7.2 Tooltip 组件 —— 已具备 ✅

- 共享组件:`packages/ui/components/ui/tooltip.tsx`(Base UI),导出 `Tooltip` / `TooltipTrigger` / `TooltipContent` / `TooltipProvider`,经 `@multica/ui/components/ui/tooltip` 引用。
- **已在 `packages/views` 内被使用**(如 `issues/issue-detail:557`、`comment-card:525` 的活动/评论 tooltip),web/desktop 两端都在渲染它 → **renderer 可用性已被现网证明**。`<DateTime>` 直接基于它封装即可。

### 7.3 app 层盲区 —— 基本无,产品时间显示集中在 packages/views ✅

- `apps/web/app/**`、`apps/web/components/**`:无产品时间戳渲染。
- `apps/web/features/**`(landing/changelog 营销页):仅 changelog 发布日(UTC 锚定的浮动日期,合理)、活动热力图(生成数据)、版权年——非产品数据,不在范围。
- `apps/desktop/src/renderer/src/**`:仅 `formatUptime()`(时长,非时间戳),无 `*_at` 渲染。
- 结论:**所有产品时间显示都在 `packages/views`(已盘点),app 层无需额外追查。**

### 7.4 后端渲染时间 —— 不在范围 ✅

- 服务端唯一拼人类可读时间的是 **autopilot 生成的 issue 描述/标题**(`service/autopilot.go:1258,1323`),已用 trigger 时区(Scheduling 轴),正确。
- 其余均为 API JSON 的 `RFC3339` UTC 时间戳,前端按 Viewing tz 渲染;无 HTML / 邮件正文 / CSV / PDF 含格式化时间(邮件仅 SMTP `Date` 头,属协议元数据)。
- 结论:**后端零改动。**

### 7.5 tooltip 精确格式 —— 已定 ✅

- 选定**仅 GMT 偏移**(不带 IANA 名、不并列 UTC):
  - en:`Mar 1, 2026, 02:30:45 PM (GMT+8)`
  - zh:`2026年3月1日 14:30:45(GMT+8)`
- 日历日为纯日期 tooltip(无时分、无时区)。
- 实现细节(`Intl` 坑)见 §3.5,写死在 `<DateTime>`。

---

## 8. 非目标 / 明确不做

- **不**引入 `workspace.timezone` 当 viewing 渲染权威(可作默认种子 / Scheduling 默认值)。
- **不**对日历日(`due_date` / `start_date`)做 tz 转换显示。
- **不**在 create issue 表单为 due_date 增加 tz 选择器。
- **不**为 mobile 做 hover tooltip(无 hover 场景)。
- **不**改动存储层(已全 UTC)。

---

## 9. 一句话总览

- **存**:全 UTC(瞬时 `TIMESTAMPTZ`)/ 纯日期(日历日 `DATE`),不动。
- **显示**:瞬时按 **Viewing tz** + **Language locale** 格式化,日历日浮动不转,web/desktop 一律挂「完整时间+时区」tooltip。
- **逾期/deadline**:前端**标红**按查看者 **Viewing tz** 的今天判定(与屏幕上 `due_date` 一致);将来的后台「逾期 → 通知」**触发**才走 canonical tz(v1=UTC,fire-once 与查看者无关),UI 不加 tz 选项。
