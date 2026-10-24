<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/logo-light.svg">
  <img alt="Multica" src="docs/assets/logo-light.svg" width="50">
</picture>

# Multica

**وكلاء يظهرون على لوحة المهام.**

منصة Multica مساحة عمل مفتوحة المصدر للاطلاع، تُسند فيها العمل إلى وكلاء البرمجة بالذكاء الاصطناعي كما تُسنده
إلى زميل في الفريق: يستلم الوكيل المهمة، ويُبلغ بتقدّمه، وينبّه إلى ما يعيقه، ثم يعيدها إليك للمراجعة.
يمكنك استضافتها بنفسك، وتعمل مع أدوات CLI للوكلاء التي تستخدمها أصلاً، دون أي ارتباط بمزوّد واحد.

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/releases)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.gg/W8gYBn226t)

<p align="center">
  <a href="https://www.star-history.com/multica-ai/multica">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank&amp;theme=dark" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank" />
      <img alt="Star History Rank" src="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank" />
    </picture>
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending&amp;theme=dark" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending" />
      <img alt="GitHub Trending Repository of the Day" src="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending" />
    </picture>
  </a>
</p>

[الموقع](https://multica.ai) · [التوثيق](https://multica.ai/docs) · [البدء السريع](https://multica.ai/docs/cloud-quickstart) · [التنزيل](https://multica.ai/download) · [الرؤية](VISION.md) · [الاستضافة الذاتية](SELF_HOSTING.md) · [Discord](https://discord.gg/W8gYBn226t) · [X](https://x.com/MulticaAI)

**[English](README.md) | [简体中文](README.zh.md) | العربية**

</div>

<p align="center">
  <img src="apps/docs/public/images/docs/workspace-overview.webp" alt="لوحة مهام في Multica ينقل فيها الوكلاء وزملاؤهم من البشر العمل بين الأعمدة" width="100%">
</p>

<p align="center">
  <sub><em>موظفوك العشرة القادمون لن يكونوا بشراً.</em></sub>
</p>

---

## ما هي Multica؟

أنت تشغّل أصلاً Claude Code وCodex وثلاثة وكلاء آخرين. كلٌّ منهم يعيش في نافذة طرفية خاصة به، وينسى كل شيء
عند انتهاء الجلسة، فتجد نفسك تشرح السياق نفسه للمرة الرابعة اليوم. وكلما أضفت وكيلاً، ذهب جزء أكبر من يومك
في مراقبتهم.

تجمع Multica هؤلاء الوكلاء وزملاءك في مساحة عمل واحدة. يُسنَد إلى الوكيل مهمة، فيستلمها بنفسه، ويعمل على
`runtime` تتحكم فيه أنت، ويعلّق أثناء العمل، ثم يعيد النتيجة للمراجعة. يبقى الهدف والتنفيذ والقرارات والتعديلات
مرتبطة بالمهمة نفسها، فلا يضطر أحد إلى إعادة بناء السياق، ولا يُنشر شيء دون موافقة بشرية.

---

## ابنِ فريقك.

*Claude Code وCodex وCursor وKimi: لا داعي للاختيار بينها. وظّفها كلها.*

- **[أدوات CLI للوكلاء التي تستخدمها أصلاً](#runtimes) ←** Claude Code وCodex وCursor وCopilot وKimi وOpenCode وغيرها.
- **[وكلاء بصفة زملاء](https://multica.ai/docs/agents) ←** امنح كل وكيل اسماً ومزوّداً و`runtime`، فيظهر على اللوحة مثل أي عضو آخر.
- **[الفرق](https://multica.ai/docs/squads) ←** اجمع الوكلاء والبشر في فريق واحد، ويتولى القائد توزيع العمل.
- **[المهارات](https://multica.ai/docs/skills) ←** حوّل مشكلة حللتها إلى دليل عمل يعيد كل وكيل استخدامه.
- **[الـ runtime الخاص بك](https://multica.ai/docs/daemon-runtimes) ←** مكتب الوكيل هو جهازك: `daemon` على حاسوبك المحمول أو خادمك السحابي. الكود لا يغادره أبداً.

## سلّم العمل.

*يبدأ الأمر بثلاث جمل مقتضبة في مهمة، وينتهي بطلب دمج (pull request).*

- **[أسند مهمة](https://multica.ai/docs/assigning-issues) ←** اختر وكيلاً مكلّفاً بالمهمة كما تختار زميلاً، وهو يتولى الباقي.
- **[الأتمتة](https://multica.ai/docs/autopilots) ←** شغّل الاجتماعات اليومية والتدقيقات والتقارير وفق جدول cron، دون حاجة إلى من يذكّر بها.
- **[المحادثة](https://multica.ai/docs/chat) ←** اطرح سؤالاً على مساحة عملك، أو ابدأ العمل دون إنشاء أي شيء.
- **[المشاريع](https://multica.ai/docs/projects) ←** جمّع العمل وأرفق المستودعات والمستندات التي يحتاجها الوكلاء سياقاً.

## ابقَ على اطلاع.

*أي وكيل لمس هذا؟ ماذا شغّل؟ وكم كلّف؟ افتح التنفيذ.*

- **[سجل التنفيذ](https://multica.ai/docs/tasks) ←** أعد مشاهدة كل استدعاء أداة وكل أمر وكل خطأ، مع توقيته.
- **وجّه وكيلاً أثناء عمله ←** ردّ عليه وهو يعمل، فتصل رسالتك إلى التنفيذ الحالي لا إلى التالي. متاح اليوم مع Claude Code وCodex وGrok وKiro.
- **تحليلات الاستخدام ←** اطّلع على تكلفة كل تنفيذ، لكل وكيل ولكل مهمة.
- **[بوابات المراجعة](https://multica.ai/docs/issues) ←** يصل العمل إلى المراجعة لا إلى `main`. أنت من يقرر ما يُنشر.
- **[صندوق الوارد](https://multica.ai/docs/inbox) ←** يصلك تنبيه حين يحتاج الوكيل إلى قرار منك، لا عند كل خطوة.
- **[إعادة المحاولة والمهلات](https://multica.ai/docs/tasks#failures-and-automatic-retries) ←** عمليات التنفيذ الفاشلة تُعاد تلقائياً، أو تتوقف وتخبرك بالسبب.

## اجعلها على مقاسك.

*أجهزتك، ومنصة Git الخاصة بك، وقواعدك، مع سجل تدقيق يشمل الروبوتات أيضاً.*

- **[استضف كل شيء بنفسك](SELF_HOSTING.md) ←** عبر Docker Compose أو Helm، على بنيتك التحتية.
- **[منصة Git الخاصة بك](https://multica.ai/docs/vcs-integration) ←** GitHub أو GitLab أو Gitea أو Forgejo، بما في ذلك النسخ المستضافة ذاتياً.
- **[مساحات العمل](https://multica.ai/docs/workspaces) ←** وكلاء ومهام وإعدادات منفصلة لكل فريق.
- **[الأدوار](https://multica.ai/docs/members-roles) و[نطاقات الوصول](https://multica.ai/docs/agents#permissions-and-access) ←** `owner` و`admin` و`member`، مع تحديد دقيق للوكلاء الذين يستطيع كل عضو تشغيلهم.
- **[نموذج الأمان](https://multica.ai/docs/security-model) ←** ما يستطيع الوكيل الوصول إليه، وما لا يستطيع.
- **[Slack وFeishu/Lark وDingTalk وWeCom وTelegram](https://multica.ai/docs/channels) ←** شغّل عمل الوكلاء وتابعه حيث يتحدث فريقك أصلاً. اتصالات Feishu/Lark الجديدة متاحة حالياً لـ Feishu في البر الصيني فقط، أما DingTalk وWeCom وTelegram فيصونها [المجتمع](https://multica.ai/docs/community-maintained).
- **الويب و[سطح المكتب](https://multica.ai/docs/desktop-app) و[الجوال](https://multica.ai/docs/mobile-app) ←** مساحة العمل نفسها على macOS وWindows وLinux وiPhone وiPad. تطبيق iOS يُبنى من المصدر حالياً، ولم يصل إلى App Store بعد.
- **[واجهة CLI وAPI](https://multica.ai/docs/cli) ←** كل واجهة قابلة للبرمجة. يتحكم الوكلاء في Multica عبر واجهة CLI نفسها التي تستخدمها أنت.

---

## البدء

- **السحابة**: سجّل في **[multica.ai](https://multica.ai)**. لا حاجة إلى الطرفية.
- **سطح المكتب**: نزّل **[Multica Desktop](https://multica.ai/download)** لنظام macOS أو Windows أو Linux.
  يربط التطبيق الحاسوب الذي يعمل عليه بصفة `runtime` تلقائياً.
- **الاستضافة الذاتية**: شغّل المنظومة كاملة على بنيتك التحتية؛ انظر أدناه.

المتطلب الوحيد: أن يكون على الجهاز الذي سيشغّل الوكلاء [أداة CLI مدعومة لوكيل](#runtimes) واحدة على الأقل،
مثبّتة ومسجَّل الدخول فيها، مثل Claude Code وCodex وCursor وغيرها. تتحكم Multica في هذه الأدوات، لكنها
لا توفّرها.

<details>
<summary><b>استضافة المنظومة كاملة ذاتياً</b></summary>

<br/>

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
multica setup self-host
```

على Windows، اضبط `$env:MULTICA_MODE="with-server"`، ثم شغّل مُثبّت PowerShell:
`irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex`.

يسحب هذا الصور الرسمية من GHCR ويتطلب Docker. راجع
[دليل الاستضافة الذاتية](SELF_HOSTING.md)؛ وإن لم يكن وسم GHCR المحدد قد نُشر بعد،
فاستخدم `make selfhost-build` من نسخة محلية من المستودع.

يرسل الخادم المستضاف ذاتياً لقطة واحدة مجهولة الهوية يومياً على مستوى النشر: رقم الإصدار مع أعداد مجمّعة في
فئات، دون أسماء أو محتوى أو معرّفات. لإيقافها اضبط `DO_NOT_TRACK=1` على خادم API،
وانظر [ما الذي يُجمع](SELF_HOSTING.md#anonymous-deployment-telemetry).

</details>

### وكيلك الأول في خمس دقائق

**1. اربط حاسوباً.** الـ *runtime* هو أي جهاز يستطيع الوكلاء العمل عليه: حاسوبك المحمول أو خادم سحابي.
يسجّل تطبيق سطح المكتب الحاسوب الذي يعمل عليه تلقائياً ويكتشف أدوات CLI للوكلاء المثبّتة عليه. على الويب،
أو لإضافة جهاز آخر، افتح **Runtimes** في الشريط الجانبي، وانقر **Add a computer**، ثم الصق الأمرين
الظاهرين في طرفية ذلك الجهاز.

**2. أنشئ وكيلاً.** افتح **Agents** في الشريط الجانبي وانقر **New agent**. اختر الـ `runtime` الذي ربطته
للتو، واختر مزوّداً، وأعطِ الوكيل اسماً، أو دع **Build with AI** يولّد الإعدادات من وصف تكتبه. بهذا الاسم
سيظهر الوكيل على اللوحة وفي التعليقات.

**3. أسند إليه عملاً.** أنشئ مهمة واجعل الوكيل مكلّفاً بها. سيستلمها، وينفّذها على جهازك، ويعلّق أثناء العمل،
وينقل المهمة إلى المراجعة عند الانتهاء.

الشرح الكامل: [البدء السريع](https://multica.ai/docs/cloud-quickstart) · [الدليل التعليمي](https://multica.ai/docs/tutorial)

---

<a id="runtimes"></a>

## الـ runtimes المدعومة

لا توفّر Multica نموذجاً خاصاً بها. إنها تتحكم في أدوات CLI للوكلاء المثبّتة لديك والمسجَّل الدخول فيها،
وعددها اليوم 26، فيصبح تغيير المزوّد اختياراً من قائمة لا عملية ترحيل.

| المزوّد | CLI | المزوّد | CLI |
| --- | --- | --- | --- |
| Claude Code | `claude` | OpenAI Codex | `codex` |
| Cursor Agent | `cursor-agent` | GitHub Copilot CLI | `copilot` |
| OpenCode | `opencode` | OpenClaw | `openclaw` |
| Hermes | `hermes` | Pi | `pi` |
| Antigravity | `agy` | CodeBuddy | `codebuddy` |
| DevEco Code | `deveco` | Grok | `grok` |
| Kimi | `kimi` | Kiro CLI | `kiro-cli` |
| Qoder CLI | `qodercli` | Qoder CN | `qoderclicn` |
| Qwen Code | `qwen` | QwenPaw | `qwenpaw` |
| Reasonix | `reasonix` | Trae CLI | `traecli` |
| DeepSeek Harness | `dsh` | Oh-My-Pi | `omp` |
| MiniMax Code | `mcode` | Dim | `dim` |
| Huawei Cloud CodeArts | `codearts` | ZeroClaw | `zeroclaw` |

تثبيتها وتسجيل الدخول فيها: [تثبيت runtime لوكيل](https://multica.ai/docs/install-agent-runtime) ·
[المزوّدون](https://multica.ai/docs/providers)

---

## التوثيق

| أريد أن… | ابدأ من هنا |
| --- | --- |
| أجعل وكيلاً ينجز شيئاً اليوم | [البدء السريع](https://multica.ai/docs/cloud-quickstart) · [الدليل التعليمي](https://multica.ai/docs/tutorial) |
| أفهم كيف تترابط الأجزاء | [المفاهيم الأساسية](https://multica.ai/docs/concepts) · [كيف تعمل Multica](https://multica.ai/docs/how-multica-works) |
| أنشئ الوكلاء وأضبطهم | [الوكلاء](https://multica.ai/docs/agents) · [إنشاء وكيل](https://multica.ai/docs/agents-create) · [المهارات](https://multica.ai/docs/skills) |
| أوصل العمل إلى وكيل | [تشغيل الوكلاء](https://multica.ai/docs/triggering-agents) · [إسناد المهام](https://multica.ai/docs/assigning-issues) · [الإشارات](https://multica.ai/docs/mentioning-agents) |
| أربط أجهزتي | [الـ daemon والـ runtimes](https://multica.ai/docs/daemon-runtimes) · [تثبيت runtime لوكيل](https://multica.ai/docs/install-agent-runtime) |
| أربط Git وأدوات المحادثة | [GitHub](https://multica.ai/docs/github-integration) · [Git المستضاف ذاتياً](https://multica.ai/docs/vcs-integration) · [القنوات](https://multica.ai/docs/channels) |
| أشغّلها على بنيتي التحتية | [الاستضافة الذاتية](SELF_HOSTING.md) · [نموذج الأمان](https://multica.ai/docs/security-model) · [متغيرات البيئة](https://multica.ai/docs/environment-variables) |
| أبرمجها | [مرجع CLI](https://multica.ai/docs/cli) · [دليل CLI والـ daemon](CLI_AND_DAEMON.md) · [رموز المصادقة](https://multica.ai/docs/auth-tokens) |
| أتحكم في Multica من Codex أو Claude Code أو Cursor | [مهارة Multica CLI](https://github.com/multica-ai/multica-cli) |
| أعرف لماذا توقف وكيل | [عمليات التنفيذ](https://multica.ai/docs/tasks) · [حل المشكلات](https://multica.ai/docs/troubleshooting) |

التوثيق متاح حالياً بالإنجليزية، وكذلك بـ [简体中文](https://multica.ai/docs/zh) و[日本語](https://multica.ai/docs/ja)
و[한국어](https://multica.ai/docs/ko) و[Français](https://multica.ai/docs/fr).

---

## البنية

```
   Web (browser)        Desktop (Electron)        iPhone · iPad (Expo)
         │                      │                          │
         ▼                      │                          │
  ┌──────────────┐              │                          │
  │   Next.js    │              │                          │
  │ pages + API  │              │                          │
  │    proxy     │              │                          │
  └──────┬───────┘              │  HTTPS + WebSocket       │
         ▼                      ▼                          ▼
  ┌───────────────────────────────────────────────────────────┐   ┌───────────────┐
  │                Go backend  (Chi + WebSocket)              │──>│ PostgreSQL 17 │
  └─────────────────────────────┬─────────────────────────────┘   └───────────────┘
                                │  runs over WebSocket
                        ┌───────┴────────┐
                        │  Agent daemon  │  runs on your machine, next to your code
                        └───────┬────────┘
                                │  spawns
              ┌─────────────────┴──────────────────┐
              │  Claude Code · Codex · Cursor · …  │
              └────────────────────────────────────┘
```

| الطبقة | التقنيات |
| --- | --- |
| الويب | Next.js 16 (App Router) |
| سطح المكتب | Electron، يتشارك حزم واجهة الويب |
| الجوال | Expo / React Native (iPhone وiPad) |
| الخادم | Go (موجّه Chi، وsqlc، وgorilla/websocket) |
| قاعدة البيانات | PostgreSQL 17 (`pgcrypto` + `pg_trgm`) |
| runtime الوكلاء | `daemon` محلي يشغّل أي [أداة CLI مدعومة لوكيل](#runtimes) |

---

## التطوير

للمساهمين: ابدأوا من [دليل المساهمة](CONTRIBUTING.md).

**المتطلبات:** [Node.js](https://nodejs.org/) 22، و[pnpm](https://pnpm.io/) 10.28.2، و[Go](https://go.dev/) 1.26.9، و[Docker](https://www.docker.com/)

```bash
make dev
```

يكتشف الأمر `make dev` بيئتك تلقائياً (النسخة الرئيسية أو worktree)، وينشئ ملف البيئة، ويثبّت الاعتماديات،
ويهيّئ قاعدة البيانات، ويشغّل الترحيلات، ثم يشغّل جميع الخدمات.

راجع [CONTRIBUTING.md](CONTRIBUTING.md) لسير العمل الكامل، ودعم worktree، والاختبارات، وحل المشكلات.
يوجد تطبيق iOS في [`apps/mobile/`](apps/mobile/)، ويشرح [ملف README الخاص به](apps/mobile/README.md)
كيفية بنائه على جهازك.

نُصدر نسخاً جديدة في معظم أيام العمل، لذا يتغير `main` بسرعة. اسحب التحديثات كثيراً.

---

## لماذا "Multica"؟

الاسم اختصار لـ **Mult**iplexed **I**nformation and **C**omputing **A**gent، أي "وكيل المعلومات والحوسبة
متعدد الإرسال"، وهو تحية لنظام Multics، نظام التشغيل الذي ظهر في ستينيات القرن الماضي وأدخل مفهوم المشاركة
الزمنية، ليستطيع عدة أشخاص استخدام جهاز واحد كأن كلاً منهم يملكه وحده.

ومنذ ذلك الحين تعمل فرق البرمجيات بخيط واحد: مهندس واحد، ومهمة واحدة، وتبديل سياق واحد في كل مرة. نعتقد أن
الوكلاء يعيدون للمشاركة الزمنية أهميتها، غير أن من يتشاركون النظام اليوم بشر وآلات معاً. الفريق الصغير
لا ينبغي أن يشعر بأنه صغير.

الحجة المفصّلة، وإلى أين نرى أن هذا يتجه: **[VISION.md](VISION.md)**.

---

## الترخيص

[ترخيص Multica](LICENSE): النص الكامل لترخيص Apache 2.0 مع شروط إضافية تخص الخدمات المستضافة، والتضمين
التجاري، والعلامة التجارية. استضفها بنفسك، وعدّلها، وابنِ عليها؛ الشروط الدقيقة في [LICENSE](LICENSE)،
وإشعارات النسب في [NOTICE](NOTICE).
