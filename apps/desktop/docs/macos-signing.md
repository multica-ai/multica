# macOS 代码签名 & 公证(Notarization)

> 让用户下载安装 Multica Desktop 后双击直接打开,而不是先撞 Gatekeeper 的"无法验证开发者"警告 ——
> 谁出哪一步、CI 里怎么接、证书到期/换人时怎么处理。
>
> 仅限 Lilith fork(`CopilotDemo/multica` GitHub mirror + GitLab 原仓)。上游 `multica-ai`
> 用自己的 release.yml,不受这套配置影响。

---

## TL;DR

- CI 跑在 GitHub Actions 上(GitHub-hosted ephemeral macOS runner),靠 5 个 GitHub Secret
  做签名 + 公证;**全配齐 → 签名+公证版**,**任意一个缺 → 自动回退到 ad-hoc 未签名**(不会把
  发布搞挂)。
- 签名证书是 **Developer ID Application**(团队 `8DCHGYMM27`,Shanghai Lilith Computer Technology
  Company Limited)。这张证书 Apple 规定**只有账户持有人**能签发/导出(Admin 也不行),
  目前账户持有人是 **xuanang yu**。所以"换证书"这一步永远绕不开他。
- 一旦 `.p12` 拿到手,**其余配置任何团队成员都能自助完成**:5 个 secret 中的另 3 个(`APPLE_ID`、
  `APPLE_APP_SPECIFIC_PASSWORD`、`APPLE_TEAM_ID`)都不需要账户持有人。

---

## 背景:为什么必须签名 + 公证

macOS 自 10.15 起,**任何从网络下载的应用**都会被 Gatekeeper 扫描,要不弹"无法验证开发者"
("can't be opened because Apple cannot check it for malicious software"),要不直接拒绝运行。
解掉这个警告的唯一办法是:

1. 用 **Developer ID Application** 证书签名 app bundle(电子标识"这是 Lilith 出的");
2. 把签好的包提交 Apple **notary service**(notarytool)做公证;
3. 把公证票据 **staple** 到包上,让客户端离线也能验证。

只签名不公证 → 仍被 Gatekeeper 拦。只公证不签名 → 提交不上去(公证服务只接受 Developer ID 签名)。
两步缺一不可。

> macOS 上没有 iOS 那种"企业内部分发(In-House)"机制。即使应用纯内网分发,要"双击直接开",
> 也只能走 Developer ID + 公证。Apple Development / iOS Distribution 等其他证书类型一律不行。

---

## 5 个 GitHub Secret

全部加在 **github.com/CopilotDemo/multica → Settings → Secrets and variables → Actions → New repository secret**
(不是 Variables,是 Secrets)。名称大小写必须一字不差。

| Secret 名 | 内容 | 出处 |
|---|---|---|
| `CSC_LINK` | `.p12` 的 base64 字符串(单行) | 账户持有人导出 .p12 后,本地 `base64 -i your.p12 \| pbcopy` 生成 |
| `CSC_KEY_PASSWORD` | 导出 .p12 时设的密码 | 账户持有人 |
| `APPLE_ID` | Apple ID 邮箱(必须是 team `8DCHGYMM27` 成员) | 自己 |
| `APPLE_APP_SPECIFIC_PASSWORD` | 上面那个 Apple ID 的 **App 专用密码**(不是登录密码) | 自己在 appleid.apple.com 生成 |
| `APPLE_TEAM_ID` | `8DCHGYMM27` | 直接抄 |

公证用的 Apple ID **不需要**是账户持有人,任意一个 team 成员都行(Admin 完全够用)。
所以个人开发者把自己的 Apple ID + App 专用密码塞进 secret 即可,不必让 xuanang yu 出。

---

## 一次性建立流程

### Step 1 — 跟账户持有人要 `.p12`(唯一卡脖子的一步)

⚠️ Apple 后台("Certificates, Identifiers & Profiles" → Create Certificate)对 Developer ID
Application / Installer 这两行**只对账户持有人开放**,Admin 也是灰的。Xcode 的
"Settings → Accounts → Manage Certificates → +" 同样按角色过滤;非账户持有人下拉里只有
Apple Development 和 iOS Distribution 两项。所以 `.p12` 必须由账户持有人在他自己那台
**有对应私钥**的 Mac 上导出。

**给账户持有人的精确指令(可直接转发)**:

> 1. 钥匙串访问(Keychain Access)→ 左侧选「登录」→ 类别「我的证书」。
> 2. 找到名字**以 `Developer ID Application:` 开头**(完整应为
>    `Developer ID Application: Shanghai Lilith Computer Technology Company Limited (8DCHGYMM27)`)
>    的那一条。**注意不是 `Apple Development:` 开头的那张** —— 那是你的个人开发证书,
>    用不了。
> 3. 点开它左边的三角,**确认下面挂着私钥**。能看到私钥,这台 Mac 才能导出;看不到说明
>    私钥在另一台机器上,要去那台导。
> 4. 右键这条证书 →「导出"Developer ID Application…"」→ 格式选**个人信息交换 (.p12)**
>    → 设一个**强密码**(随机生成,记到密码管理器)→ 保存为 `.p12` 文件。
> 5. 把 `.p12` 文件 + 那个导出密码发给请求人。**文件私发,密码另走一条渠道**
>    (别同时贴在群里)。

如果账户持有人的 Mac 上**找不到这张证书或私钥**:让他在 Xcode 里现建一张 ——
`Xcode → Settings → Accounts → 选 team → Manage Certificates → 左下角 + → Developer ID Application`。
对账户持有人来说这个下拉里**会**有 Developer ID Application 选项(对其他角色没有)。
新建出来的证书私钥自然就落在他这台 Mac 的钥匙串里,接着按上面 5 步导出。

### Step 2 — 自己生成 App 专用密码

1. 浏览器打开 [appleid.apple.com](https://appleid.apple.com),登录你想用来公证的 Apple ID。
2. **登录与安全 → App 专用密码 → 生成密码**,label 写个 "Multica notarize" 之类的。
3. **Apple 只会显示一次**,生成后立刻保存(密码管理器 / 加密笔记)。这个就是
   `APPLE_APP_SPECIFIC_PASSWORD`。

注意:必须是 **App 专用密码**,**不是你的 Apple ID 登录密码** —— notarytool 拒收登录密码,
而且有 2FA 也卡不进去。

### Step 3 — 把 `.p12` 转 base64

GitHub Secret 只能存文本,所以 `.p12` 不能原文件上传,要先 base64 单行编码:

```bash
base64 -i your.p12 | pbcopy
```

macOS 的 `base64` 默认就输出单行(Linux 的会按 76 列折行,折行的 GitHub 也能接受但单行更稳)。
跑完直接 ⌘V 粘进 secret 值框。

### Step 4 — 配 5 个 Secret

去仓库 Settings → Secrets and variables → Actions → New repository secret,按上面的表逐一加。
**核对清单**:

- [ ] 5 个 secret 名字一字不差、大小写一致
- [ ] `CSC_LINK` 里**只有 base64 字符**(`A-Z a-z 0-9 + / =`),开头不是 `-----BEGIN`
- [ ] 没把 `.p12` 文件直接当 secret 上传(那是错的,要 base64)
- [ ] 两个密码字段没带前后空格(粘贴时容易夹带)
- [ ] `APPLE_TEAM_ID` 就是 `8DCHGYMM27`,10 位,没有 `(Enterprise)` 或其他前缀
- [ ] 全在 Secrets 标签里加(**不在 Variables 标签** —— 那个是明文的)

### Step 5 — 打 tag 发版

```bash
git tag 0.x.y
git push origin 0.x.y
```

CI 会自动出签名+公证版。盯进度看 OSS 公开端点:

```bash
curl -s https://multica.lilithgames.com/api/downloads/latest-mac.yml | grep -E "^(version|path)"
```

`version` 翻到新 tag 就是整条链路(GitLab 镜像 → GitHub Actions → OSS)全绿。

---

## CI workflow 怎么接

入口是 `.github/workflows/lilith-desktop-release.yml`,build job 的 `env:` 块里有 6 个条件化
env(`CSC_LINK / CSC_KEY_PASSWORD / CSC_IDENTITY_AUTO_DISCOVERY / APPLE_ID /
APPLE_APP_SPECIFIC_PASSWORD / APPLE_TEAM_ID`),每个都用 `matrix.target == 'mac'` 表达式
门控:

- 矩阵不是 mac(win / linux)→ 这些 env 全部渲染成空字符串,electron-builder 看不到 Apple
  凭证,不会误尝试 mac 签名。
- 矩阵是 mac 但 secret 没配 → env 也是空,electron-builder 走 ad-hoc 签名分支
  (`CSC_IDENTITY_AUTO_DISCOVERY=false` 阻止它扫钥匙串报错)。
- 矩阵是 mac 且 secret 都齐 → `CSC_IDENTITY_AUTO_DISCOVERY` 翻成 `true`,electron-builder
  把 `.p12` 导进运行时临时钥匙串、用它签名;`APPLE_TEAM_ID` 非空使
  `apps/desktop/scripts/package.mjs:366` 的 `disableMacNotarize` 分支不生效,公证启用。

**为什么这样设计**:半配置状态(配了 3 个忘了 2 个)不会把发布搞挂 —— 要不全签要不 ad-hoc,
没有中间态。windows / linux 完全无感。

**`mac.notarize: true`** 写在 `apps/desktop/electron-builder.yml`,electron-builder 看到
APPLE_TEAM_ID 就走 notarytool 流程(Apple ID + App-specific password 方式);看不到就跳过。

**Hardened Runtime 默认开**(electron-builder mac 默认 `hardenedRuntime: true`),entitlements
用 `apps/desktop/build/entitlements.mac.plist`,里头加好了 Electron 必需的 `allow-jit` /
`allow-unsigned-executable-memory` 和给 bundled Go CLI 准备的 `disable-library-validation`。
公证所需的前置条件这些都齐了,不用动。

---

## 验证 `.p12` 是不是对的(在配 Secret 之前!)

**典型错误**:账户持有人导出时点错那一行,把 **Apple Development**(他个人的开发证书)
当成了 Developer ID Application —— 这两张在钥匙串里挨着、名字都带 "Developer",很容易选错。
Apple Development 类的 .p12 公证根本提交不上去,CI 会一路绿到最后 notarize 那步爆出
模糊错误。

**不用密码也能预判证书类型**(读 .p12 的 plaintext metadata):

```bash
python3 - <<'PY'
data = open('your.p12','rb').read()
oid = bytes.fromhex('06092A864886F70D010914')  # PKCS#9 friendlyName
i = data.find(oid); j = i + len(oid)
while data[j] != 0x1E: j += 1
ln = data[j+1]
n, off = (ln, j+2) if ln < 0x80 else (int.from_bytes(data[j+2:j+2+(ln&0x7F)],'big'), j+2+(ln&0x7F))
print(data[off:off+n].decode('utf-16-be'))
PY
```

期望输出包含 `Developer ID Application`(可能带 `Mac ` 前缀 —— 是 Xcode 在本地给 cert 起
label 时的命名习惯,不影响 cert 本体)。如果输出是 `Apple Development:` 开头,**这张 .p12
就是错的**,回到 Step 1 让账户持有人重导。

**有密码后**做一次确认性检查(密码不进 CI 日志):

```bash
openssl pkcs12 -in your.p12 -info -nokeys -passin pass:'你的密码' 2>/dev/null \
  | grep -E "^(subject|issuer)="
```

正确的输出:

```
subject= ... /CN=Developer ID Application: Shanghai Lilith Computer Technology Company Limited (8DCHGYMM27)/...
issuer=  /CN=Developer ID Certification Authority/OU=Apple Certification Authority/O=Apple Inc./C=US
```

CN 里必须有 **`Developer ID Application`** + **`(8DCHGYMM27)`** 后缀,issuer 必须是
Apple 的 **Developer ID Certification Authority**。任一不符就不对。

### 可选:上 CI 前本地试签

```bash
# 5 个变量先 export 到 shell
export CSC_LINK=$(base64 -i your.p12)
export CSC_KEY_PASSWORD='your-p12-password'
export APPLE_ID='you@example.com'
export APPLE_APP_SPECIFIC_PASSWORD='your-app-specific-password'
export APPLE_TEAM_ID='8DCHGYMM27'

pnpm --filter @multica/desktop package -- --mac --arm64
```

构建完成后:

```bash
# 签名身份检查
codesign -dvv apps/desktop/dist/mac-arm64/Multica.app 2>&1 | grep -E "Authority|TeamIdentifier"
# 应该看到 Authority=Developer ID Application: Shanghai Lilith… 和 TeamIdentifier=8DCHGYMM27

# 公证票据检查
xcrun stapler validate apps/desktop/dist/mac-arm64/Multica.app
# 应该看到 The validate action worked!
```

两个都过 = 上 CI 必稳。

---

## Troubleshooting

### CI 报 `Mac verify error: invalid password` / `Cannot find certificate`

`CSC_KEY_PASSWORD` 不对,或者 `CSC_LINK` base64 编错(被截断、夹了换行、复制时漏字符)。
重新跑 `base64 -i your.p12 | pbcopy` 并粘贴。

### Notarization 失败:`Invalid credentials` / `Authentication failed`

- 检查 `APPLE_APP_SPECIFIC_PASSWORD` 是不是 **App 专用密码**(`xxxx-xxxx-xxxx-xxxx` 格式),
  不是登录密码。
- 检查 `APPLE_ID` 是不是 team `8DCHGYMM27` 的成员 —— 用这个 Apple ID 登 developer.apple.com,
  确认能看到团队。

### Notarization 失败:`The signature does not include a secure timestamp`

签名时缺了 `--timestamp`。electron-builder 默认会带,如果出这个错可能是本地 codesign 路径有
异常 —— 检查 macOS 系统版本和 Xcode Command Line Tools 是否最新。

### 公证报 `Hardened Runtime is not enabled`

`apps/desktop/electron-builder.yml` 里 `mac.hardenedRuntime` 必须为 true(默认就是,不要
显式设成 false)。

### CSR 文件丢了 / 钥匙串里看不到私钥

CSR(`.certSigningRequest`)是申请证书时上传给 Apple 的请求单,**它本身不能签名**。它对应的
**私钥**才是关键 —— 生成 CSR 时私钥同时落在生成那台 Mac 的钥匙串里。CSR 文件没了不要紧
(随时能再生成一份);**私钥没了等于这张证书废了**,只能让账户持有人到 Apple 后台 revoke
然后重新建。

### 账户持有人换人了 / 离职了

让组织管理员到 developer.apple.com **转让账户持有人角色**(Membership Details 页有
"Transfer Account Holder")。新账户持有人 → 重新创建 Developer ID Application 证书
→ 重新出 .p12 → 更新 `CSC_LINK` / `CSC_KEY_PASSWORD` 两个 secret。其余 3 个 secret
不变(notarization Apple ID 是任意 team 成员就行)。

---

## 维护:到期 / 续期 / 轮换

| 资源 | 当前到期 | 影响 | 怎么续 |
|---|---|---|---|
| 当前 Developer ID Application 证书 | 2027/02/01 | 到期后新打的包都是无效签名 | 账户持有人在 Xcode 里新建一张 → 重出 .p12 → 更新 `CSC_LINK`/`CSC_KEY_PASSWORD` |
| Apple Developer (Enterprise) Program 会员资格 | 比证书早,具体见 developer.apple.com → 会员资格详细信息 | 失效后**新构建无法公证**(已公证的旧版不受影响,客户端继续能开) | 账户持有人到 developer.apple.com 续订;续订需要的资料 Apple 自己会引导 |
| App 专用密码 | 不会到期,但被吊销/Apple ID 改密码后失效 | notarization 报 Invalid credentials | 重新生成,更新 `APPLE_APP_SPECIFIC_PASSWORD` |

**轮换 .p12 的最小操作**(证书没换、只是密码或 base64 重生成):

```bash
base64 -i your.p12 | pbcopy
# 把 CSC_LINK secret 的值替换掉。CSC_KEY_PASSWORD 如果没变就不用动。
```

---

## 安全注意

- `.p12` 文件 + 它的导出密码 = **完整的签名身份**。任何拿到这两样的人都能以 Lilith 名义
  签发恶意 macOS 包(且公证服务都信任它)。**必须**:
  - 文件和密码**分两条渠道传**(比如文件走飞书私聊、密码走密码管理器分享);
  - 收到后立刻配进 GitHub Secret,**别留本地工作区**;**不留也别再传**;
  - 仓库根目录的 `.gitignore` 已经把 `*.p12 *.pfx *.p8 *.cer *.certSigningRequest
    *.mobileprovision *.keychain*` 全拦了,但还是别养成把这些放进工作区的习惯;
  - 万一泄露:账户持有人立刻 revoke 旧证书 + 出新的 → 更新 `CSC_LINK`/`CSC_KEY_PASSWORD` secret。
- `APPLE_APP_SPECIFIC_PASSWORD` 单独泄露,攻击者能用你的 Apple ID 调用 notarytool,但**不能签名**
  (没 .p12 私钥)。危害较小,但还是马上 revoke 重生成。

---

## 参考(代码位置)

- `.github/workflows/lilith-desktop-release.yml` —— CI workflow,签名 + 公证 env 注入
- `apps/desktop/electron-builder.yml` —— `mac.notarize: true`、`entitlementsInherit`
- `apps/desktop/build/entitlements.mac.plist` —— hardened-runtime entitlements
- `apps/desktop/scripts/package.mjs` —— `disableMacNotarize` 开关(line 366 起)
- 项目根 `CLAUDE.md` 的 *Release (Lilith fork)* 一节 —— 发版流水线全景
