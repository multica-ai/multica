import { describe, expect, it } from "vitest";
import plugin from "./with-android-release-signing.js";

const { addReleaseSigning } = plugin as {
  addReleaseSigning: (contents: string) => string;
};

/**
 * 上游 expo prebuild 生成的 app/build.gradle 里与签名相关的最小片段。
 * 插件的两个正则锚点都打在这段结构上,所以测试也只保留这段 —— 上游模板一旦
 * 改动导致锚点失配,这里会先红。
 */
const UPSTREAM = `android {
    namespace 'ai.multica.mobile.dev'
    signingConfigs {
        debug {
            storeFile file('debug.keystore')
            storePassword 'android'
            keyAlias 'androiddebugkey'
            keyPassword 'android'
        }
    }
    buildTypes {
        debug {
            signingConfig signingConfigs.debug
        }
        release {
            // Caution! In production, you need to generate your own keystore file.
            // see https://reactnative.dev/docs/signed-apk-android.
            signingConfig signingConfigs.debug
            minifyEnabled enableMinifyInReleaseBuilds
        }
    }
}
`;

describe("addReleaseSigning", () => {
  it("把 release buildType 从 debug 签名切到 release 签名", () => {
    const out = addReleaseSigning(UPSTREAM);

    expect(out).toContain("signingConfig signingConfigs.release");
    // debug buildType 必须原样保留 —— 它仍然该用 debug key。
    expect(out).toMatch(/debug \{\n\s*signingConfig signingConfigs\.debug/);
    // release 块里不该再残留 debug 签名。
    const releaseBlock = out.slice(out.indexOf("release {", out.indexOf("buildTypes")));
    expect(releaseBlock).not.toContain("signingConfigs.debug");
  });

  it("替换后的 signingConfig 行保持 buildTypes 内的 12 空格缩进", () => {
    const out = addReleaseSigning(UPSTREAM);

    // 曾踩过的坑:锚点若不从行首起匹配,原缩进会与捕获组叠成双份。
    expect(out).toContain("\n            signingConfig signingConfigs.release");
    expect(out).not.toMatch(/\n {13,}signingConfig signingConfigs\.release/);
  });

  it("注入的 release signingConfig 从 Gradle 属性取凭据,不含任何字面量密码", () => {
    const out = addReleaseSigning(UPSTREAM);

    expect(out).toContain("storeFile file(MULTICA_RELEASE_STORE_FILE)");
    expect(out).toContain("storePassword MULTICA_RELEASE_STORE_PASSWORD");
    expect(out).toContain("keyAlias MULTICA_RELEASE_KEY_ALIAS");
    expect(out).toContain("keyPassword MULTICA_RELEASE_KEY_PASSWORD");
    // 除了上游自带的 debug keystore 口令,不得引入新的硬编码密码。
    const quoted = out.match(/(storePassword|keyPassword) '[^']*'/g) ?? [];
    expect(quoted).toEqual(["storePassword 'android'", "keyPassword 'android'"]);
  });

  it("显式开启 v3 签名(密钥轮换的前提),关闭冗余的 v1", () => {
    const out = addReleaseSigning(UPSTREAM);

    // v3 无法对已发布的 APK 追签 —— 首个 release 就必须带上。
    expect(out).toContain("enableV3Signing true");
    expect(out).toContain("enableV2Signing true");
    expect(out).toContain("enableV1Signing false");
  });

  it("属性缺失时显式回退 debug 签名,而不是留下空配置", () => {
    const out = addReleaseSigning(UPSTREAM);

    expect(out).toContain(
      "if (project.hasProperty('MULTICA_RELEASE_STORE_FILE'))",
    );
    // 这条 else 是评审实测出的 P0 修复:release buildType 已无条件指向
    // signingConfigs.release,只写 if 的话属性缺失时得到 storeFile=null 的
    // 空配置,AGP 产出未签名 APK(装不上),而非回退 debug。
    expect(out).toContain("} else {");
    expect(out).toContain("initWith project.android.signingConfigs.debug");
    // else 必须在 release signingConfig 内部,不能漏到别处。
    const cfgBlock = out.slice(
      out.indexOf("release {"),
      out.indexOf("buildTypes"),
    );
    expect(cfgBlock).toContain("initWith project.android.signingConfigs.debug");
  });

  it("幂等 —— prebuild 重跑不会重复注入", () => {
    const once = addReleaseSigning(UPSTREAM);
    const twice = addReleaseSigning(once);

    expect(twice).toBe(once);
    // 注入块里 STORE_FILE 恰好出现两次:hasProperty 判据 + storeFile 取值。
    expect(once.match(/MULTICA_RELEASE_STORE_FILE/g)?.length).toBe(2);
  });

  it("上游模板结构变化导致锚点失配时抛错,而不是静默产出 debug 签名的 release 包", () => {
    // 去掉 signingConfigs 块。
    const noSigningConfigs = UPSTREAM.replace(
      /signingConfigs \{[\s\S]*?\n    \}\n/,
      "",
    );
    expect(() => addReleaseSigning(noSigningConfigs)).toThrow(
      /定位 signingConfigs 块/,
    );

    // signingConfigs 在,但 release buildType 的 debug 签名行没了。
    const noReleaseLine = UPSTREAM.replace(
      /\s*\/\/ Caution! In production[\s\S]*?signingConfig signingConfigs\.debug\n(?=\s*minifyEnabled)/,
      "\n",
    );
    expect(() => addReleaseSigning(noReleaseLine)).toThrow(
      /定位[\s\S]*signingConfigs\.debug/,
    );
  });
});
