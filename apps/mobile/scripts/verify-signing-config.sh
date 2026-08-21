#!/usr/bin/env bash
# 验证 Android release 签名配置的两个分支都按预期工作。
#
# 为什么单独成脚本而不进 vitest:单测只能断言插件生成的 build.gradle 字符串,
# 断不到 Gradle 真正解析后的行为 —— 「属性缺失时回退 debug」这个语义曾因此
# 带着假绿灯走到交付(release buildType 已指向 signingConfigs.release,空配置
# 得到的是未签名包而非 debug 签名包)。这里用 AGP variant API 探针补上那一层。
# 需要 Android SDK + JDK 17,单次约 1–2 分钟,故不放进默认测试链路。
#
# 用法(在 apps/mobile/ 下):
#   ./scripts/verify-signing-config.sh
#
# 前置:已跑过 expo prebuild(android/ 存在),且构建机 ~/.gradle/gradle.properties
# 里配好了 MULTICA_RELEASE_* 四个属性 —— 「有凭据」分支需要它们才有意义。
set -euo pipefail

cd "$(dirname "$0")/.."
[ -d android ] && [ -f android/gradlew ] || {
  echo "android/ 不存在,先跑 pnpm android:mobile:prod 之类的 prebuild" >&2
  exit 1
}

probe=$(mktemp -t multica-signing-probe.XXXXXX.gradle)
# 无凭据分支靠一个干净的 GRADLE_USER_HOME 实现:软链 caches/wrapper 复用下载,
# 但不含 gradle.properties,于是 MULTICA_RELEASE_* 属性天然缺失。
clean_home=$(mktemp -d -t multica-gradle-home.XXXXXX)
trap 'rm -rf "$probe" "$clean_home"' EXIT

ln -sfn "$HOME/.gradle/caches" "$clean_home/caches"
ln -sfn "$HOME/.gradle/wrapper" "$clean_home/wrapper"

cat > "$probe" <<'GRADLE'
gradle.projectsEvaluated {
  rootProject.subprojects.findAll { it.name == 'app' }.each { p ->
    p.android.applicationVariants.all { v ->
      if (v.name == 'release') {
        println "PROBE storeFile=${v.signingConfig?.storeFile} ready=${v.signingConfig?.isSigningReady()}"
      }
    }
  }
}
GRADLE

# 不能加 -q:那会把 println 一并压掉,探针输出为空,断言拿不到证据反而误报 FAIL。
run_probe() { (cd android && ./gradlew -I "$probe" :app:help --console=plain 2>&1 | grep '^PROBE' || true); }

echo "### 无凭据(期望 storeFile 指向 android/app/debug.keystore, ready=true)"
GRADLE_USER_HOME="$clean_home" run_probe | tee "$clean_home/nocreds.txt"

echo
echo "### 有凭据(期望 storeFile 指向发布 keystore, ready=true)"
run_probe | tee "$clean_home/creds.txt"

echo
fail=0
grep -q 'debug.keystore.*ready=true' "$clean_home/nocreds.txt" || {
  echo "FAIL 无凭据分支未回退到 debug keystore —— 产出的 release 包会是未签名的" >&2
  fail=1
}
if grep -q 'storeFile=null' "$clean_home/creds.txt"; then
  echo "FAIL 有凭据分支 storeFile 为 null —— 检查 ~/.gradle/gradle.properties" >&2
  fail=1
fi
[ $fail -eq 0 ] && echo "OK 两个分支均符合预期"
exit $fail
