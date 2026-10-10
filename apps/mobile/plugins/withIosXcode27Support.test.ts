// @vitest-environment node
//
// Regression tests for the prebuild plugin that makes the iOS app build and
// launch on Xcode 27 (iOS 27 SDK). Covers the three generated artifacts —
// scene manifest, SceneDelegate/AppDelegate transform, Podfile deployment
// target bump — for generation, idempotency, and template-drift errors.
//
// The cold-start deep-link semantics are load-bearing: under the scene life
// cycle a link that cold-starts the app arrives in `connectionOptions`, and
// only a reconstructed launch-options dictionary makes `Linking.getInitialURL()`
// return it. The tests pin that behavior (see "cold-start deep links" below).
import { createRequire } from "node:module";
import { describe, expect, it } from "vitest";

const require = createRequire(import.meta.url);

// The plugin is CommonJS (loaded by the Expo CLI during prebuild); require it
// directly instead of relying on ESM interop.

const plugin = require("./withIosXcode27Support.js") as {
  applySceneManifest: (
    infoPlist: Record<string, unknown>,
  ) => Record<string, unknown>;
  applySceneDelegate: (appDelegateContents: string) => string;
  applyPodDeploymentTarget: (podfileContents: string) => string;
};

/** The window bootstrap block from the Expo AppDelegate prebuild template. */
const WINDOW_BOOTSTRAP = `#if os(iOS) || os(tvOS)
    window = UIWindow(frame: UIScreen.main.bounds)
    factory.startReactNative(
      withModuleName: "main",
      in: window,
      launchOptions: launchOptions)
#endif`;

/** Minimal replica of the Swift AppDelegate prebuild template. */
const APP_DELEGATE_TEMPLATE = `internal import Expo
import React
import ReactAppDependencyProvider

@main
class AppDelegate: ExpoAppDelegate {
  var window: UIWindow?

  var reactNativeDelegate: ExpoReactNativeFactoryDelegate?
  var reactNativeFactory: RCTReactNativeFactory?

  public override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
  ) -> Bool {
    let delegate = ReactNativeDelegate()
    let factory = ExpoReactNativeFactory(delegate: delegate)
    delegate.dependencyProvider = RCTAppDependencyProvider()

    reactNativeDelegate = delegate
    reactNativeFactory = factory

${WINDOW_BOOTSTRAP}

    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  // Linking API
  public override func application(
    _ app: UIApplication,
    open url: URL,
    options: [UIApplication.OpenURLOptionsKey: Any] = [:]
  ) -> Bool {
    return super.application(app, open: url, options: options) || RCTLinkingManager.application(app, open: url, options: options)
  }
}
`;

/** Minimal replica of the Expo Podfile prebuild template. */
const PODFILE_TEMPLATE = `platform :ios, '16.0'
install! 'cocoapods', :deterministic_uuids => true

target 'Multica' do
  use_expo_runtime!

  use_react_native!(
    :path => config[:reactNativePath]
  )

  post_install do |installer|
    react_native_post_install(
      installer,
      config[:reactNativePath],
      :mac_catalyst_enabled => false
    )
  end
end
`;

describe("applySceneManifest", () => {
  it("generates the single-scene manifest pointing at SceneDelegate", () => {
    const manifest = plugin.applySceneManifest({});

    expect(manifest.UIApplicationSceneManifest).toEqual({
      UIApplicationSupportsMultipleScenes: false,
      UISceneConfigurations: {
        UIWindowSceneSessionRoleApplication: [
          {
            UISceneConfigurationName: "Default",
            UISceneDelegateClassName: "$(PRODUCT_MODULE_NAME).SceneDelegate",
          },
        ],
      },
    });
  });

  it("is idempotent", () => {
    const once = plugin.applySceneManifest({});
    const twice = plugin.applySceneManifest(once);

    expect(twice).toEqual(once);
  });
});

describe("applySceneDelegate", () => {
  it("moves window creation out of the app delegate and appends the scene delegate", () => {
    const contents = plugin.applySceneDelegate(APP_DELEGATE_TEMPLATE);

    expect(contents).not.toContain("UIWindow(frame: UIScreen.main.bounds)");
    expect(contents).toContain(
      "The window is created by SceneDelegate.scene(_:willConnectTo:)",
    );
    expect(contents).toContain(
      "class SceneDelegate: UIResponder, UIWindowSceneDelegate",
    );
  });

  it("is idempotent", () => {
    const once = plugin.applySceneDelegate(APP_DELEGATE_TEMPLATE);
    const twice = plugin.applySceneDelegate(once);

    expect(twice).toBe(once);
  });

  it("throws with a pointer to the plugin when the template changed", () => {
    const drifted = APP_DELEGATE_TEMPLATE.replace(
      WINDOW_BOOTSTRAP,
      "    window = drifted()",
    );

    expect(() => plugin.applySceneDelegate(drifted)).toThrow(
      /apps\/mobile\/plugins\/withIosXcode27Support\.js/,
    );
  });

  it("throws instead of silently keeping a SceneDelegate from an older plugin version", () => {
    // expo prebuild re-runs the mods against an existing ios/ directory; the
    // stale generation must fail loudly instead of freezing old behavior.
    const stale = `${APP_DELEGATE_TEMPLATE}
class SceneDelegate: UIResponder, UIWindowSceneDelegate {
  var window: UIWindow?
}
`;

    expect(() => plugin.applySceneDelegate(stale)).toThrow(
      /older version of the plugin/,
    );
  });

  describe("cold-start deep links", () => {
    const contents = plugin.applySceneDelegate(APP_DELEGATE_TEMPLATE);

    it("rebuilds launch options from connectionOptions instead of passing nil", () => {
      expect(contents).toContain("launchOptions: SceneDelegate.launchOptions(");
      expect(contents).not.toContain("launchOptions: nil");
      // Marks the generation as current; the idempotency guard keys off it.
      expect(contents).toContain("Generated by withIosXcode27Support v3");
    });

    it("rebuilds the exact keys that Linking.getInitialURL() reads", () => {
      expect(contents).toContain('"UIApplicationLaunchOptionsURLKey"');
      expect(contents).toContain(
        '"UIApplicationLaunchOptionsUserActivityDictionaryKey"',
      );
      expect(contents).toContain("NSUserActivityTypeBrowsingWeb");
    });

    it("returns nil launch options when no link cold-started the app", () => {
      expect(contents).toContain(
        "return launchOptions.isEmpty ? nil : launchOptions",
      );
    });

    it("starts React Native before routing the cold-start link", () => {
      // Routing fires before JS listeners exist; only the reconstructed launch
      // options deliver a cold-start link reliably, so the bridge must be
      // started with them first.
      const startReactNative = contents.indexOf("factory.startReactNative(");
      const firstRoute = contents.indexOf("SceneDelegate.route(");

      expect(startReactNative).toBeGreaterThan(-1);
      expect(firstRoute).toBeGreaterThan(startReactNative);
    });

    it("forwards cold-start URLs and user activities to RCTLinkingManager", () => {
      expect(contents).toContain("connectionOptions.urlContexts.forEach");
      expect(contents).toContain("connectionOptions.userActivities.forEach");
      expect(contents).toContain("RCTLinkingManager.application(");
    });
  });

  it("forwards scene events through the running ExpoAppDelegate, not the subscriber manager", () => {
    const contents = plugin.applySceneDelegate(APP_DELEGATE_TEMPLATE);

    // Upstream SceneEventForwarder forwards through ExpoAppDelegate, so
    // overrides on it keep firing; direct subscriber-manager calls bypass
    // them. The generated source must not name the manager at all.
    expect(contents).not.toContain("ExpoAppDelegateSubscriberManager");
    expect(contents).toContain(
      "UIApplication.shared.delegate as? ExpoAppDelegate",
    );
    expect(contents).toContain(
      "expoAppDelegate?.applicationDidBecomeActive(UIApplication.shared)",
    );
  });

  it("declares willContinueUserActivityWithType with the scene callback's Void signature", () => {
    const contents = plugin.applySceneDelegate(APP_DELEGATE_TEMPLATE);

    // A Bool return stops Swift from matching the optional UISceneDelegate
    // requirement, so UIKit never calls the method.
    expect(contents).toContain(
      "func scene(_ scene: UIScene, willContinueUserActivityWithType userActivityType: String) {",
    );
    expect(contents).not.toContain(
      "willContinueUserActivityWithType userActivityType: String) -> Bool",
    );
  });

  it("delivers each link to React Native exactly once", () => {
    const contents = plugin.applySceneDelegate(APP_DELEGATE_TEMPLATE);

    // RCTLinkingManager announces handled links via this notification; the
    // observer skips the explicit call when a subscriber already delivered.
    expect(contents).toContain('Notification.Name("RCTOpenURLNotification")');
    expect(contents).toContain("observer.wasNotified");
  });

  it("forwards quick actions from cold start and while running", () => {
    const contents = plugin.applySceneDelegate(APP_DELEGATE_TEMPLATE);

    expect(contents).toContain("connectionOptions.shortcutItem");
    expect(contents).toContain(
      "performActionFor shortcutItem: UIApplicationShortcutItem",
    );
  });
});

describe("applyPodDeploymentTarget", () => {
  it("raises every pod target to the app deployment target inside post_install", () => {
    const contents = plugin.applyPodDeploymentTarget(PODFILE_TEMPLATE);

    expect(contents).toContain(
      "min_deployment_target = podfile_properties['ios.deploymentTarget'] || '16.0'",
    );
    const postInstall = contents.indexOf("post_install do |installer|");
    const bump = contents.indexOf("min_deployment_target");

    expect(postInstall).toBeGreaterThan(-1);
    expect(bump).toBeGreaterThan(postInstall);
    expect(contents).toContain("IPHONEOS_DEPLOYMENT_TARGET");
  });

  it("is idempotent", () => {
    const once = plugin.applyPodDeploymentTarget(PODFILE_TEMPLATE);
    const twice = plugin.applyPodDeploymentTarget(once);

    expect(twice).toBe(once);
  });

  it("throws with a pointer to the plugin when the post_install hook is missing", () => {
    const drifted = PODFILE_TEMPLATE.replace(/post_install[\s\S]*?end\n/, "");

    expect(() => plugin.applyPodDeploymentTarget(drifted)).toThrow(
      /apps\/mobile\/plugins\/withIosXcode27Support\.js/,
    );
  });
});
