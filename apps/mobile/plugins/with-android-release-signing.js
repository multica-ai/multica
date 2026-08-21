/**
 * Expo config plugin — 给 Android release 变体接上真实签名配置。
 *
 * 为什么必须是 plugin 而不是直接改 android/app/build.gradle:
 * android/ 是 `expo prebuild` 的产物且被 .gitignore 忽略,手改的 signingConfigs
 * 会在下一次 prebuild(或换台机器 clone)时整个消失。写成 plugin 才能让"release
 * 用发布 keystore"成为可复现的仓库行为。
 *
 * 凭据来源:四个 Gradle 属性,只从构建机的 ~/.gradle/gradle.properties 或环境变量
 * (ORG_GRADLE_PROJECT_* / -P) 读取,永远不进仓库:
 *   MULTICA_RELEASE_STORE_FILE / _KEY_ALIAS / _STORE_PASSWORD / _KEY_PASSWORD
 *
 * 属性缺失时不报错,显式回退到上游默认的 debug 签名 —— 贡献者跑 `android:prod` 只是
 * 想装个能跑的包,不该被"你没有发布 keystore"卡住;真正的发布构建由 CI/发布人负责
 * 提供属性。要确认某个 APK 用的是哪把 key,`apksigner verify --print-certs` 一看
 * 便知(debug key 的 CN 是 "Android Debug")。
 *
 * 注意 else 分支不能省:buildTypes.release 已被无条件改写为 signingConfigs.release,
 * 只写 if 的话属性缺失时得到的是一个 storeFile=null 的空 signingConfig,AGP 会产出
 * **未签名**的 release APK(装不上),而不是回退 debug。这是评审实测出的 P0。
 */
const { withAppBuildGradle } = require("@expo/config-plugins");

// 插入到 signingConfigs { ... } 内部的 release 配置块。
const RELEASE_SIGNING_CONFIG = `
        release {
            // 由 with-android-release-signing.js 注入。属性缺失时走 else,
            // 逐字段复制 debug 签名(见插件头部注释)。
            if (project.hasProperty('MULTICA_RELEASE_STORE_FILE')) {
                storeFile file(MULTICA_RELEASE_STORE_FILE)
                storePassword MULTICA_RELEASE_STORE_PASSWORD
                keyAlias MULTICA_RELEASE_KEY_ALIAS
                keyPassword MULTICA_RELEASE_KEY_PASSWORD
                // AGP 默认只开 v1+v2。显式要 v3:密钥轮换(APK Signature Scheme
                // v3 的 proof-of-rotation)只在 v3 块里存在,等到需要换 key 时
                // 才发现当初没签 v3 就来不及了 —— 已发布的包无法追签。
                // v1 (JAR signing) 关掉:minSdk 24 起 v2 即为强制校验路径,
                // v1 只会拖慢构建并留下可被剥离的弱签名。
                enableV1Signing false
                enableV2Signing true
                enableV3Signing true
            } else {
                // 没有发布凭据:逐字段复制 debug 签名。不能只留空块 —— release
                // buildType 已指向本配置,storeFile 为 null 会让 AGP 产出未签名
                // 的 release APK,adb install 直接拒绝。
                initWith project.android.signingConfigs.debug
            }
        }`;

/** 幂等标记 —— prebuild 可能在已改写过的 build.gradle 上重跑。 */
const MARKER = "MULTICA_RELEASE_STORE_FILE";

function addReleaseSigning(contents) {
  if (contents.includes(MARKER)) return contents;

  // 1) 在 signingConfigs 块里追加 release 配置。锚点是上游模板里紧跟着的
  //    debug 块闭合 + signingConfigs 闭合,匹配失败就抛错而不是静默产出一个
  //    仍用 debug key 签名的 "release" APK。
  const signingConfigsAnchor = /(signingConfigs\s*\{[\s\S]*?\n)(    \})/;
  if (!signingConfigsAnchor.test(contents)) {
    throw new Error(
      "[with-android-release-signing] 未能在 app/build.gradle 中定位 signingConfigs 块;" +
        "上游模板可能已变更,请更新本插件的锚点。",
    );
  }
  contents = contents.replace(
    signingConfigsAnchor,
    `$1${RELEASE_SIGNING_CONFIG}\n$2`,
  );

  // 2) 把 release buildType 的 signingConfig 从 debug 改到 release。
  //    上游模板里这一行带着一条"生产环境请自行生成 keystore"的注释,一并替换掉。
  //    锚点从注释所在行的行首起匹配(含前导缩进),否则替换只吃掉注释文本本身、
  //    把行首缩进留在原地,和 `$1` 叠成双份缩进。
  const releaseSigningLine =
    /^[ \t]*\/\/ Caution! In production[^\n]*\n[ \t]*\/\/ see https:\/\/reactnative\.dev\/docs\/signed-apk-android\.\n([ \t]*)signingConfig signingConfigs\.debug/m;
  if (!releaseSigningLine.test(contents)) {
    throw new Error(
      "[with-android-release-signing] 未能在 release buildType 中定位 " +
        "`signingConfig signingConfigs.debug`;上游模板可能已变更。",
    );
  }
  contents = contents.replace(
    releaseSigningLine,
    "$1signingConfig signingConfigs.release",
  );

  return contents;
}

module.exports = function withAndroidReleaseSigning(config) {
  return withAppBuildGradle(config, (cfg) => {
    if (cfg.modResults.language !== "groovy") {
      throw new Error(
        "[with-android-release-signing] 仅支持 Groovy 版 build.gradle。",
      );
    }
    cfg.modResults.contents = addReleaseSigning(cfg.modResults.contents);
    return cfg;
  });
};

// 供单测直接验证字符串改写逻辑,不必跑整套 prebuild。
module.exports.addReleaseSigning = addReleaseSigning;
