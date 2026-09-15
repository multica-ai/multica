/**
 * Xcode 27 (iOS 27 SDK) support for the prebuilt iOS project.
 *
 * Two things break when the app is built with the iOS 27 SDK:
 *
 * 1. UIKit terminates an app at launch unless it has adopted the scene-based
 *    life cycle (`UIApplicationEvaluateRuntimeIssueForNoSceneLifecycleAdoption`
 *    — see Apple TN3187). The Expo prebuild template still starts React Native
 *    from a window created in `application(_:didFinishLaunchingWithOptions:)`,
 *    so this plugin adds a `UIApplicationSceneManifest` to Info.plist and moves
 *    window creation into a `SceneDelegate`, mirroring `ExpoAppSceneDelegate`
 *    (which expo ships from SDK 56 on). Delete the scene half of this plugin
 *    once the app moves to that SDK.
 *
 * 2. Xcode 27 rejects pod targets below iOS 15.0 (iosMath ships 6.0, RNSVG
 *    12.4), and `expo-router`'s iOS code uses iOS 16 APIs without an
 *    availability guard, so the pod targets must be raised to the app's own
 *    deployment target — `ios.deploymentTarget` in app.config.ts.
 *
 * Every mod below is idempotent and throws (instead of silently writing broken
 * Swift) when the prebuild template no longer matches the expected text.
 */
const {
  withAppDelegate,
  withInfoPlist,
  withPodfile,
} = require("expo/config-plugins");

/** Scene delegate class appended to the app delegate file. */
const SCENE_DELEGATE_CLASS = "SceneDelegate";

/** Matches the window bootstrap block in the Expo AppDelegate template. */
const WINDOW_BOOTSTRAP = `#if os(iOS) || os(tvOS)
    window = UIWindow(frame: UIScreen.main.bounds)
    factory.startReactNative(
      withModuleName: "main",
      in: window,
      launchOptions: launchOptions)
#endif`;

const SCENE_DELEGATE_SOURCE = `
/// Scene-based life cycle, which UIKit requires for apps built with the iOS 27
/// SDK. Mirrors \`ExpoAppSceneDelegate\` from Expo SDK 56 until the app can
/// depend on it.
class ${SCENE_DELEGATE_CLASS}: UIResponder, UIWindowSceneDelegate {
  var window: UIWindow?

  func scene(
    _ scene: UIScene,
    willConnectTo session: UISceneSession,
    options connectionOptions: UIScene.ConnectionOptions
  ) {
    guard let windowScene = scene as? UIWindowScene,
      let appDelegate = UIApplication.shared.delegate as? AppDelegate,
      let factory = appDelegate.reactNativeFactory
    else {
      fatalError("SceneDelegate couldn't start React Native: the app delegate exposes no factory.")
    }

    let window = UIWindow(windowScene: windowScene)
    self.window = window

    // Mirror the window onto the app delegate so code that reads
    // \`UIApplication.shared.delegate?.window\` keeps working.
    appDelegate.window = window

    // Under the scene life cycle cold-start URLs and user activities arrive in
    // \`connectionOptions\`, not in \`launchOptions\`.
    factory.startReactNative(
      withModuleName: "main",
      in: window,
      launchOptions: nil)
    SceneDelegate.route(urlContexts: connectionOptions.urlContexts)
    connectionOptions.userActivities.forEach { SceneDelegate.route(userActivity: $0) }
  }

  func sceneDidDisconnect(_ scene: UIScene) {
    window = nil
  }

  // UIKit no longer calls the app delegate equivalents under the scene life
  // cycle, so forward them to the Expo subscribers to keep their behavior.

  func sceneDidBecomeActive(_ scene: UIScene) {
    ExpoAppDelegateSubscriberManager.applicationDidBecomeActive(UIApplication.shared)
  }

  func sceneWillResignActive(_ scene: UIScene) {
    ExpoAppDelegateSubscriberManager.applicationWillResignActive(UIApplication.shared)
  }

  func sceneWillEnterForeground(_ scene: UIScene) {
    ExpoAppDelegateSubscriberManager.applicationWillEnterForeground(UIApplication.shared)
  }

  func sceneDidEnterBackground(_ scene: UIScene) {
    ExpoAppDelegateSubscriberManager.applicationDidEnterBackground(UIApplication.shared)
  }

  func scene(_ scene: UIScene, openURLContexts URLContexts: Set<UIOpenURLContext>) {
    SceneDelegate.route(urlContexts: URLContexts)
  }

  func scene(_ scene: UIScene, continue userActivity: NSUserActivity) {
    SceneDelegate.route(userActivity: userActivity)
  }

  /// Routes URLs to both the Expo subscribers and React Native's linking
  /// manager, which is what feeds expo-router's incoming-link handling.
  private static func route(urlContexts: Set<UIOpenURLContext>) {
    for context in urlContexts {
      var options: [UIApplication.OpenURLOptionsKey: Any] = [:]
      if let sourceApplication = context.options.sourceApplication {
        options[.sourceApplication] = sourceApplication
      }
      if let annotation = context.options.annotation {
        options[.annotation] = annotation
      }
      options[.openInPlace] = context.options.openInPlace

      _ = ExpoAppDelegateSubscriberManager.application(
        UIApplication.shared, open: context.url, options: options)
      _ = RCTLinkingManager.application(UIApplication.shared, open: context.url, options: options)
    }
  }

  /// Routes universal links to the Expo subscribers and React Native's linking manager.
  private static func route(userActivity: NSUserActivity) {
    _ = ExpoAppDelegateSubscriberManager.application(
      UIApplication.shared,
      continue: userActivity,
      restorationHandler: { _ in })
    _ = RCTLinkingManager.application(
      UIApplication.shared,
      continue: userActivity,
      restorationHandler: { _ in })
  }
}
`;

/** Adds the scene manifest so UIKit adopts the scene-based life cycle. */
function withSceneManifest(config) {
  return withInfoPlist(config, (config) => {
    config.modResults.UIApplicationSceneManifest = {
      UIApplicationSupportsMultipleScenes: false,
      UISceneConfigurations: {
        UIWindowSceneSessionRoleApplication: [
          {
            UISceneConfigurationName: "Default",
            UISceneDelegateClassName: `$(PRODUCT_MODULE_NAME).${SCENE_DELEGATE_CLASS}`,
          },
        ],
      },
    };
    return config;
  });
}

/** Moves window creation out of the app delegate and appends the scene delegate. */
function withSceneDelegate(config) {
  return withAppDelegate(config, (config) => {
    if (config.modResults.language !== "swift") {
      throw new Error(
        "withIosXcode27Support expects the Swift app delegate template; found " +
          `"${config.modResults.language}".`,
      );
    }

    let contents = config.modResults.contents;

    if (contents.includes(`class ${SCENE_DELEGATE_CLASS}:`)) {
      return config;
    }
    if (!contents.includes(WINDOW_BOOTSTRAP)) {
      throw new Error(
        "withIosXcode27Support could not find the window bootstrap block in the app delegate " +
          "template. The Expo template changed — update WINDOW_BOOTSTRAP in " +
          "apps/mobile/plugins/withIosXcode27Support.js.",
      );
    }

    contents = contents.replace(
      WINDOW_BOOTSTRAP,
      "    // The window is created by SceneDelegate.scene(_:willConnectTo:); UIKit requires\n" +
        "    // the scene-based life cycle for apps built with the iOS 27 SDK.",
    );
    contents = `${contents.trimEnd()}\n${SCENE_DELEGATE_SOURCE}`;

    config.modResults.contents = contents;
    return config;
  });
}

/**
 * Raises every pod target to the app's deployment target. Xcode 27 rejects
 * targets below iOS 15.0, and modules such as expo-router need iOS 16 APIs.
 */
function withPodDeploymentTarget(config) {
  return withPodfile(config, (config) => {
    let contents = config.modResults.contents;

    if (contents.includes("min_deployment_target")) {
      return config;
    }

    const postInstall = contents.match(
      /post_install do \|installer\|[\s\S]*?\n  end\n/,
    );
    if (!postInstall) {
      throw new Error(
        "withIosXcode27Support could not find the Podfile post_install hook. The Expo Podfile " +
          "template changed — update the anchor in apps/mobile/plugins/withIosXcode27Support.js.",
      );
    }

    const bump = [
      "    # Xcode 27 rejects pod targets below iOS 15.0 (iosMath ships 6.0, RNSVG",
      "    # 12.4) and expo-router's iOS code needs iOS 16 APIs, so raise every pod",
      "    # target to the app's deployment target.",
      "    min_deployment_target = podfile_properties['ios.deploymentTarget'] || '16.0'",
      "    installer.pods_project.targets.each do |target|",
      "      target.build_configurations.each do |config|",
      "        current = config.build_settings['IPHONEOS_DEPLOYMENT_TARGET']",
      "        next unless current.to_s =~ /\\A\\d/",
      "        if Gem::Version.new(current) < Gem::Version.new(min_deployment_target)",
      "          config.build_settings['IPHONEOS_DEPLOYMENT_TARGET'] = min_deployment_target",
      "        end",
      "      end",
      "    end",
      "",
    ].join("\n");

    contents = contents.replace(
      postInstall[0],
      postInstall[0].replace(/\n  end\n$/, `\n${bump}  end\n`),
    );

    config.modResults.contents = contents;
    return config;
  });
}

module.exports = function withIosXcode27Support(config) {
  config = withSceneManifest(config);
  config = withSceneDelegate(config);
  config = withPodDeploymentTarget(config);
  return config;
};
