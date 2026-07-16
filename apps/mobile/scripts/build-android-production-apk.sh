#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MOBILE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$MOBILE_DIR/../.." && pwd)"
ANDROID_DIR="$MOBILE_DIR/android"
TARGET_PACKAGE="${EXPO_ANDROID_PACKAGE_PROD:-ai.multica.mobile}"
APK_NAME="${APK_NAME:-multica-production-release.apk}"
LOCAL_MAVEN_DIR="$ANDROID_DIR/.local-maven"
INIT_SCRIPT="$ANDROID_DIR/.local-release-init.gradle"

export APP_ENV="${APP_ENV:-production}"
export NODE_ENV="${NODE_ENV:-production}"
export EXPO_PUBLIC_API_URL="${EXPO_PUBLIC_API_URL:-https://api.multica.ai}"
export EXPO_PUBLIC_WEB_URL="${EXPO_PUBLIC_WEB_URL:-https://multica.ai}"
export EXPO_ANDROID_PACKAGE_PROD="$TARGET_PACKAGE"

if [[ -z "${JAVA_HOME:-}" ]]; then
  if [[ -d "/Library/Java/JavaVirtualMachines/jdk-18.0.1.1.jdk/Contents/Home" ]]; then
    export JAVA_HOME="/Library/Java/JavaVirtualMachines/jdk-18.0.1.1.jdk/Contents/Home"
  elif [[ -d "/Applications/Android Studio.app/Contents/jbr/Contents/Home" ]]; then
    export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
  elif [[ -x /usr/libexec/java_home ]]; then
    export JAVA_HOME="$(/usr/libexec/java_home -v 17 2>/dev/null || /usr/libexec/java_home)"
  fi
fi

mkdir -p "$LOCAL_MAVEN_DIR/com/github/gregcockroft/AndroidMath/v1.1.0"

download_if_missing() {
  local url="$1"
  local destination="$2"

  if [[ ! -s "$destination" ]]; then
    curl --fail --location --silent --show-error "$url" --output "$destination"
  fi
}

download_if_missing \
  "https://www.jitpack.io/com/github/gregcockroft/AndroidMath/v1.1.0/AndroidMath-v1.1.0.pom" \
  "$LOCAL_MAVEN_DIR/com/github/gregcockroft/AndroidMath/v1.1.0/AndroidMath-v1.1.0.pom"

download_if_missing \
  "https://www.jitpack.io/com/github/gregcockroft/AndroidMath/v1.1.0/AndroidMath-v1.1.0.aar" \
  "$LOCAL_MAVEN_DIR/com/github/gregcockroft/AndroidMath/v1.1.0/AndroidMath-v1.1.0.aar"

cd "$MOBILE_DIR"
pnpm exec expo prebuild --platform android --no-install

# Clear the app build directory so Gradle's createBundleReleaseJsAndAssets
# task can never reuse a stale JS bundle from a previous variant (staging
# vs production). Without this, Gradle sees the task as UP-TO-DATE when
# only EXPO_PUBLIC_* env vars changed and silently bundles the wrong API URL.
rm -rf "$ANDROID_DIR/app/build"

cat > "$INIT_SCRIPT" <<'GRADLE'
allprojects {
  buildscript {
    repositories {
      maven { url new File(rootDir, ".local-maven").toURI() }
      google()
      mavenCentral()
    }
  }
  repositories {
    maven { url new File(rootDir, ".local-maven").toURI() }
    google()
    mavenCentral()
  }
}

gradle.projectsEvaluated {
  def appProject = gradle.rootProject.findProject(":app")
  if (appProject == null) {
    return
  }

  def targetPackage = System.getenv("EXPO_ANDROID_PACKAGE_PROD") ?: "ai.multica.mobile"
  def fixReactNativeEntryPoint = {
    def entryPoint = new File(appProject.buildDir, "generated/autolinking/src/main/java/com/facebook/react/ReactNativeApplicationEntryPoint.java")
    if (entryPoint.exists()) {
      def text = entryPoint.getText("UTF-8")
      def fixed = text
        .replace("ai.multica.mobile.dev.BuildConfig", "${targetPackage}.BuildConfig")
        .replace("ai.multica.mobile.staging.BuildConfig", "${targetPackage}.BuildConfig")
      if (fixed != text) {
        entryPoint.setText(fixed, "UTF-8")
      }
    }
  }

  appProject.tasks.matching { it.name == "generateReactNativeEntryPoint" }.configureEach {
    it.doLast {
      fixReactNativeEntryPoint()
    }
  }

  appProject.tasks.matching { it.name == "compileReleaseJavaWithJavac" }.configureEach {
    it.doFirst {
      fixReactNativeEntryPoint()
    }
  }
}
GRADLE

export GRADLE_USER_HOME="${GRADLE_USER_HOME:-$ANDROID_DIR/.gradle-release}"

"$ANDROID_DIR/gradlew" \
  --no-daemon \
  --max-workers="${GRADLE_MAX_WORKERS:-2}" \
  --init-script "$INIT_SCRIPT" \
  -p "$ANDROID_DIR" \
  :app:assembleRelease \
  -Pandroid.compileSdkVersion=36 \
  -Pandroid.targetSdkVersion=36 \
  -x lintVitalAnalyzeRelease \
  -x lintVitalRelease

APK_PATH="$ANDROID_DIR/app/build/outputs/apk/release/app-release.apk"
FINAL_APK_PATH="$ANDROID_DIR/app/build/outputs/apk/release/$APK_NAME"
cp "$APK_PATH" "$FINAL_APK_PATH"

printf 'Production APK: %s\n' "$FINAL_APK_PATH"
