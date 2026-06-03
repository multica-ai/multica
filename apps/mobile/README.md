# Multica Mobile 开发者文档

`apps/mobile` 是 Multica 的移动端 app，基于 Expo、React Native 和 TypeScript 构建。包名是 `@multica/mobile`，应用名是 `Multicam`，Android package 和 iOS bundle identifier 都是 `com.wujieai.multica`。

这份文档面向仓库开发者，覆盖本地运行、构建、发布和 OTA 更新流程。

## 前置条件

- 已在仓库根目录安装依赖：`pnpm install`
- 本地开发需要 Expo CLI 能通过 workspace 脚本启动
- Android 原生运行需要 Android Studio、Android SDK、模拟器或真机
- iOS 原生运行需要 macOS、Xcode、模拟器或真机
- EAS 云构建、提交和 OTA 更新需要登录 Expo 账号，并具备当前 EAS project 的权限

## 本地运行

推荐从仓库根目录启动 mobile app：

```bash
pnpm dev:mobile
```

也可以直接运行 mobile package 的脚本：

```bash
pnpm --filter @multica/mobile dev
pnpm --filter @multica/mobile android
pnpm --filter @multica/mobile ios
```

脚本含义：

| 命令 | 用途 |
| --- | --- |
| `dev` | 启动 Expo dev server |
| `android` | 通过 `expo run:android` 构建并安装 Android 原生开发包 |
| `ios` | 通过 `expo run:ios` 构建并安装 iOS 原生开发包 |

这个 app 使用了 `expo-secure-store`、`expo-document-picker`、Google Sign-In 等原生能力。只用 Expo Go 时可能无法覆盖全部功能；涉及原生模块、权限或 plugin 变化时，需要重新构建并安装 dev client 或原生包。

## 运行时配置

`app.config.js` 会读取环境变量，并写入 Expo `extra`。运行时通过 `src/runtime/env.ts` 读取这些值。

| 环境变量 | 用途 | 默认值来源 |
| --- | --- | --- |
| `EXPO_PUBLIC_API_BASE_URL` | API base URL | `app.json` 的 `expo.extra.apiBaseUrl` |
| `EXPO_PUBLIC_WS_URL` | WebSocket URL | `app.json` 的 `expo.extra.wsUrl` |
| `EXPO_PUBLIC_WEB_BASE_URL` | Web 入口 URL | 默认跟随 API base URL |
| `GOOGLE_IOS_CLIENT_ID` | iOS Google 登录 client ID | 空字符串，EAS profile 中有默认配置 |
| `GOOGLE_IOS_URL_SCHEME` | iOS Google 登录 URL scheme | 空字符串，EAS profile 中有默认配置 |

当前 `app.json` 默认连接 Multica Cloud：

```text
https://multica.wujieai.com
wss://multica.wujieai.com/ws
```

连接本地后端时要注意：手机或模拟器里的 `localhost` 通常不是开发机。请按设备环境使用开发机局域网 IP、Android emulator 的 `10.0.2.2`，或其他可从设备访问的地址。

示例：

```bash
EXPO_PUBLIC_API_BASE_URL=http://10.0.2.2:8080 \
EXPO_PUBLIC_WS_URL=ws://10.0.2.2:8080/ws \
pnpm --filter @multica/mobile dev
```

## 构建

Mobile app 使用 EAS Build。构建配置在 `eas.json`。

```bash
pnpm --filter @multica/mobile build:android
pnpm --filter @multica/mobile build:android:apk
pnpm --filter @multica/mobile build:ios
```

脚本含义：

| 命令 | EAS profile | 产物 |
| --- | --- | --- |
| `build:android` | `production` | Android App Bundle (`.aab`) |
| `build:android:apk` | `production-apk` | Android APK |
| `build:ios` | `production` | iOS production build |

`eas.json` 里的 profile：

| Profile | 用途 |
| --- | --- |
| `development` | dev client，internal distribution，Android 产物为 APK |
| `preview` | internal distribution，适合发布前验证，Android 产物为 APK |
| `production-apk` | 继承 `production`，但 Android 产物为 APK |
| `production` | production channel；Android 产物为 app bundle，iOS/Android 都启用自动递增构建号 |

`production` profile 会自动递增：

- Android `versionCode`
- iOS `buildNumber`

发布前仍然需要确认 `app.json` 中的 `version` 和 `runtimeVersion` 是否符合当前 release 预期。当前项目使用 bare workflow，`runtimeVersion` 需要手动维护为固定字符串；当 native runtime 发生不兼容变化时，应随新包一起递增。

### iOS APNs 和 TestFlight

iOS 远程推送使用 Go 服务端直连 APNs。移动端通过 `expo-notifications` 获取 APNs device token；APNs token auth 凭证只配置在服务端环境变量中，不提交到仓库。

TestFlight 使用 production APNs。构建或提交 iOS production build 前确认：

- Apple Developer 中 App ID `com.wujieai.multica` 已启用 Push Notifications capability。
- EAS iOS credentials / provisioning profile 是在启用 Push capability 后生成或更新的。
- `app.config.js` 已包含 `expo-notifications` config plugin；本项目不启用 background remote notifications，因为当前发送的是普通 alert push。
- 后端已配置 APNs production token auth 环境变量：
  - `APNS_TEAM_ID`
  - `APNS_KEY_ID`
  - `APNS_BUNDLE_ID=com.wujieai.multica`
  - `APNS_AUTH_KEY_P8` 或 `APNS_AUTH_KEY_PATH`
  - 可选：`APNS_BASE_URL` / `APNS_ENV`

production/TestFlight 环境默认不设置 `APNS_BASE_URL`，也不设置 sandbox。`APNS_AUTH_KEY_P8` 应放在部署平台 Secret/Env 中；如果使用 `APNS_AUTH_KEY_PATH`，`.p8` 文件由部署环境挂载，不提交到仓库。

真实验收需要安装 TestFlight 包并在真机登录。登录后服务端应看到 `provider=apns`、`platform=ios` 的 mobile push registration；模拟器不作为远程 APNs 推送验收目标。

### Android 本地正式构建

需要在本机产出正式 APK 或 AAB 时，使用 EAS local build。它在本机执行 Gradle 构建，但仍会连接 Expo project，并使用 Expo 平台上配置的 remote credentials；不要把 Android credentials 改成本地文件，除非明确要脱离 Expo 凭证管理。

先确认登录状态：

```bash
pnpm --filter @multica/mobile exec eas whoami
```

未登录时先登录：

```bash
pnpm --filter @multica/mobile exec eas login
```

构建前建议确认依赖和 Expo 配置：

```bash
pnpm install
pnpm --filter @multica/mobile list react-native-reanimated react-native-worklets --depth 0
pnpm --filter @multica/mobile exec expo config --type public
```

构建正式 APK：

```bash
pnpm --filter @multica/mobile exec eas build \
  --platform android \
  --profile production-apk \
  --local \
  --output ./multica-production.apk
```

构建应用商店使用的 AAB：

```bash
pnpm --filter @multica/mobile exec eas build \
  --platform android \
  --profile production \
  --local \
  --output ./multica-production.aab
```

本地构建注意事项：

- `production` 和 `production-apk` 会自动递增 Android `versionCode`；构建后检查 `apps/mobile/app.json` 的 diff，确认版本号变化符合 release 预期。
- EAS local build 不会自动注入 Expo Secret 环境变量；如果某次构建依赖 Secret，需要在本机 shell 中手动 export。
- 如果设置过 `EAS_LOCAL_BUILD_WORKINGDIR`，失败后重试建议换一个空目录或取消该环境变量，避免复用上次失败构建的 Gradle/CMake 缓存。
- `.easignore` 会保留已提交的 `apps/mobile/android/gradlew` 和原生源码，但排除 `android/build`、`.cxx`、`.gradle` 等生成缓存；不要把这些缓存作为修复手段提交。

## 发布

### iOS

提交 iOS production build：

```bash
pnpm --filter @multica/mobile submit:ios
```

这个命令使用 `eas.json` 中 `submit.production.ios` 的 App Store Connect 配置，包括 `ascAppId`、`appleId` 和 `appleTeamId`。

### Android

当前仓库只配置了 Android 构建脚本，没有配置 Play Store submit 脚本。Android 发布流程以 EAS 构建产物为起点：

```bash
pnpm --filter @multica/mobile build:android
```

需要 APK 内部分发时使用：

```bash
pnpm --filter @multica/mobile build:android:apk
```

## OTA 更新

Mobile app 已接入 `expo-updates`。`app.json` 中的 `updates.url` 指向当前 EAS project：

```text
https://u.expo.dev/a4140277-7ff1-4282-b21e-5fc2fd9e5eea
```

`runtimeVersion` 当前手动设置为固定字符串。OTA 更新只会发送给相同 runtime 的安装包；当 native module、config plugin、权限、scheme、icon、Expo SDK 或其他 native runtime 发生不兼容变化时，需要先递增 `app.json` 中的 `runtimeVersion` 并重新构建安装包。旧 runtime 的安装包不会接收新 runtime 的 OTA 更新。

EAS channel 来自 `eas.json`：

| Channel | 来源 profile | 用途 |
| --- | --- | --- |
| `development` | `development` | dev client 验证 |
| `preview` | `preview` | 发布前内部验证 |
| `production` | `production` / `production-apk` | production 安装包 |

发布 OTA 更新：

```bash
pnpm --filter @multica/mobile exec eas update --channel preview --message "Describe the update"
pnpm --filter @multica/mobile exec eas update --channel production --message "Describe the update"
```

推荐先发布到 `preview` channel，并用 preview/internal build 验证。确认没有问题后，再发布到 `production` channel。

OTA 适合发布 JavaScript 和资源变更，例如界面文案、业务逻辑、小型样式调整、图片资源更新。以下变更必须重新构建安装包，不能只发 OTA：

- 新增、删除或升级原生模块
- 修改 Expo plugin 配置
- 修改 iOS permission、Android permission 或原生配置
- 修改 Android package、iOS bundle identifier、scheme
- 修改 app icon、启动图等需要原生工程参与的配置
- 需要新的 dev client 或 store build 才能生效的能力

## 常用校验

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile test
pnpm --filter @multica/mobile lint
```

这些命令分别执行 TypeScript 类型检查、Vitest 单元测试和 ESLint 检查。

## 常见问题

### 设备访问不了本地后端

不要直接把 `EXPO_PUBLIC_API_BASE_URL` 写成 `http://localhost:8080` 后给真机使用。真机需要访问开发机局域网 IP；Android emulator 通常使用 `10.0.2.2` 访问宿主机。

### WebSocket 连接失败

确认 API 和 WebSocket 协议匹配。`https` API 通常应搭配 `wss` WebSocket；`http` API 通常搭配 `ws` WebSocket。

### 文件选择器不可用

如果看到文件选择器不可用的提示，通常说明当前安装包没有包含 `expo-document-picker` 对应的原生能力。重新构建并安装 mobile app。

### iOS Google 登录失败

确认构建时设置了 `GOOGLE_IOS_CLIENT_ID` 和 `GOOGLE_IOS_URL_SCHEME`。EAS profile 已配置默认值；本地原生构建如需 Google 登录，也要提供对应环境变量。

### EAS 构建或提交失败

确认已登录 Expo 账号，并且账号有当前 project 的权限。iOS 提交还需要 App Store Connect 配置和凭证可用。
