import ExpoModulesCore
import UIKit
import LarkSSOSDK

// Expo Native Module wrapping LarkSSOSDK for Feishu OAuth.
//
// JS contract (see ../index.ts):
//   await FeishuSSO.register(appId)
//     - one-time. Strips the underscore from `cli_xxx...` → `clixxx...` to
//       derive the URL scheme the SDK callbacks land on, then calls
//       LarkSSO.register(apps:). Safe to call multiple times.
//   const { code } = await FeishuSSO.start(scope)
//     - opens Feishu app (or its H5 fallback) for OAuth, resolves with the
//       authorization code. The code is single-use, ~5min TTL — caller
//       immediately POSTs it to /auth/feishu to exchange for a session
//       token. Rejects with code "FEISHU_SSO_CANCELLED" if the user
//       dismisses the consent screen, otherwise "FEISHU_SSO_ERROR" with the
//       SDK's message.
//
// The SDK requires a host UIViewController for the H5 fallback path; we
// grab the topmost VC via UIApplication. Hard-coded to iOS only — Android
// support, if ever added, needs a separate native module.
public class FeishuSSOModule: Module {
  // The SDK is callback-based; we hold a single in-flight Promise here and
  // resolve it from the LarkSSODelegate forwarder. The SDK does not support
  // concurrent OAuth requests anyway (the user can only be in one consent
  // flow at a time), so a single slot is correct — and a second start()
  // call before the first resolves rejects it with FEISHU_SSO_BUSY.
  private var pending: Promise?
  private let delegate = FeishuSSODelegateForwarder()

  public func definition() -> ModuleDefinition {
    Name("FeishuSSOModule")

    OnCreate {
      self.delegate.module = self
    }

    // One-time SDK registration. Idempotent on the JS side (see
    // lib/feishu-sso.ts) so this gets called multiple times across hot
    // reloads / login retries — LarkSSO.register is itself documented as
    // safe to re-call with the same app id.
    AsyncFunction("register") { (appId: String, lang: String) -> Void in
      // The SDK derives the iOS URL scheme by stripping the `_` from the
      // app id (e.g. cli_XXXX → cliXXXX). The Info.plist CFBundleURLTypes
      // entry uses the same stripped form, patched in via the withFeishuSSO
      // config plugin.
      let scheme = appId.replacingOccurrences(of: "_", with: "")
      LarkSSO.register(apps: [App(server: .feishu, appId: appId, scheme: scheme)])
      // Controls the language of the SDK's H5 fallback login page (shown
      // when the Feishu app isn't installed). Driven from JS so the
      // language can change without recompiling native — see
      // lib/feishu-sso.ts. Empty string = let the SDK pick from system.
      if !lang.isEmpty {
        LarkSSO.setupLang(lang)
      }
    }

    // Start an OAuth flow. Returns { code: String } on success.
    AsyncFunction("start") { (scope: [String], promise: Promise) in
      if self.pending != nil {
        promise.reject("FEISHU_SSO_BUSY", "Another Feishu sign-in is already in progress.")
        return
      }
      guard let vc = self.topViewController() else {
        promise.reject(
          "FEISHU_SSO_NO_VIEW_CONTROLLER",
          "Could not locate a UIViewController to host the Feishu consent screen.")
        return
      }
      self.pending = promise
      let request: SSORequest = .feishu
      if !scope.isEmpty {
        request.scope = scope
      }
      DispatchQueue.main.async {
        LarkSSO.send(request: request, viewController: vc, delegate: self.delegate)
      }
    }
  }

  fileprivate func handleResponse(_ response: SSOResponse) {
    guard let promise = self.pending else { return }
    self.pending = nil
    response.safeHandleResult(success: { code in
      promise.resolve(["code": code])
    }, failure: { error in
      // SSOError is an NSError subclass carrying a typed `.type`
      // (SSOErrorType). type == .cancelled (-3) is the user dismissing
      // the consent screen — surface it as FEISHU_SSO_CANCELLED so the JS
      // layer treats it as a silent reset rather than a banner error.
      // Everything else is a real failure.
      let code = error.type == .cancelled
        ? "FEISHU_SSO_CANCELLED"
        : "FEISHU_SSO_ERROR"
      promise.reject(code, error.localizedDescription)
    })
  }

  private func topViewController(
    base: UIViewController? = UIApplication.shared.connectedScenes
      .compactMap { $0 as? UIWindowScene }
      .flatMap { $0.windows }
      .first(where: { $0.isKeyWindow })?.rootViewController
  ) -> UIViewController? {
    if let nav = base as? UINavigationController {
      return topViewController(base: nav.visibleViewController)
    }
    if let tab = base as? UITabBarController, let selected = tab.selectedViewController {
      return topViewController(base: selected)
    }
    if let presented = base?.presentedViewController {
      return topViewController(base: presented)
    }
    return base
  }
}

// LarkSSODelegate forwarder. The SDK's send() takes a delegate object and
// keeps a strong reference until the response fires; we keep it as a stored
// property on the module instance to satisfy that contract, then bounce
// the callback back into the module's Swift-typed `handleResponse`.
private class FeishuSSODelegateForwarder: NSObject, LarkSSODelegate {
  weak var module: FeishuSSOModule?

  // LarkSSODelegate's sole requirement. The method name is
  // `lkSSODidReceive(response:)`, not the `didReceive(response:)` the
  // public docs show — verified against the SDK's .swiftinterface.
  func lkSSODidReceive(response: SSOResponse) {
    module?.handleResponse(response)
  }
}
