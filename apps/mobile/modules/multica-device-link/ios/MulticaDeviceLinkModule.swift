import ExpoModulesCore
import MulticaLink
import UIKit
import QuickLook

public final class MulticaDeviceLinkModule: Module {
  private var client: MobilelinkClient?
  private var filePresenter: DeviceFilePresenter?
  private weak var fileController: UIViewController?
  private let fileLock = NSLock()
  private var fileEpoch = 0
  private func invalidateFiles() { fileLock.lock(); fileEpoch += 1; fileLock.unlock() }
  private func currentFileEpoch() -> Int { fileLock.lock(); defer { fileLock.unlock() }; return fileEpoch }
  private let queue = DispatchQueue(label: "ai.multica.mobile.device-link")
  private var backgroundTask: UIBackgroundTaskIdentifier = .invalid
  private var backgroundClose: DispatchWorkItem?

  private func closeClient() {
    self.client?.close()
    self.client = nil
  }

  private func endBackgroundGrace() {
    self.backgroundClose?.cancel()
    self.backgroundClose = nil
    if self.backgroundTask != .invalid {
      UIApplication.shared.endBackgroundTask(self.backgroundTask)
      self.backgroundTask = .invalid
    }
  }

  public func definition() -> ModuleDefinition {
    Name("MulticaDeviceLink")

    AsyncFunction("start") { (configuration: String) -> String in
      self.invalidateFiles()
      self.endBackgroundGrace()
      self.closeClient()
      guard var values = try JSONSerialization.jsonObject(with: Data(configuration.utf8)) as? [String: Any] else {
        throw NSError(domain: "MulticaDeviceLink", code: 2)
      }
      let storage = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        .appendingPathComponent("MulticaDeviceLink", isDirectory: true)
      values["storagePath"] = storage.path
      let data = try JSONSerialization.data(withJSONObject: values)
      var error: NSError?
      guard let next = MobilelinkStart(String(decoding: data, as: UTF8.self), &error) else {
        throw error ?? NSError(domain: "MulticaDeviceLink", code: 1)
      }
      self.client = next
      return next.endpoint()
    }.runOnQueue(queue)

    AsyncFunction("stop") {
      self.invalidateFiles()
      self.endBackgroundGrace()
      self.closeClient()
    }.runOnQueue(queue)

    AsyncFunction("updateToken") { (token: String) in
      self.client?.updateToken(token)
    }.runOnQueue(queue)

    AsyncFunction("networkChanged") {
      self.client?.networkChanged()
    }.runOnQueue(queue)

    // The close runs natively because JavaScript timers cannot be relied on once
    // the app is in the background.
    AsyncFunction("enterBackground") { (graceSeconds: Double) in
      guard self.client != nil else { return }
      self.endBackgroundGrace()
      let close = DispatchWorkItem { [weak self] in
        self?.client?.closeInBackground()
        self?.client = nil
        self?.endBackgroundGrace()
      }
      self.backgroundClose = close
      self.backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "MulticaDeviceLink") { [weak self] in
        self?.queue.async { if !close.isCancelled { close.perform() } }
      }
      if self.backgroundTask == .invalid {
        close.perform()
      } else {
        self.queue.asyncAfter(deadline: .now() + graceSeconds, execute: close)
      }
    }.runOnQueue(queue)

    AsyncFunction("enterForeground") { () -> Bool in
      self.endBackgroundGrace()
      return self.client != nil
    }.runOnQueue(queue)

    AsyncFunction("clearFiles") {
      self.invalidateFiles()
      self.fileController?.dismiss(animated: false)
      self.fileController = nil
      self.filePresenter = nil
      if let entries = try? FileManager.default.contentsOfDirectory(at: FileManager.default.temporaryDirectory, includingPropertiesForKeys: nil) {
        for entry in entries where entry.lastPathComponent.hasPrefix("MulticaFiles-") { try? FileManager.default.removeItem(at: entry) }
      }
    }.runOnQueue(.main)

    AsyncFunction("openFile") { (address: String, headers: [String: String], name: String, share: Bool, promise: Promise) in
      let epoch = self.currentFileEpoch()
      Task {
        do {
          guard let url = URL(string: address), url.scheme == "http", url.host == "127.0.0.1", url.path.hasPrefix("/devices/") else {
            throw NSError(domain: "MulticaDeviceLink", code: 3)
          }
          var request = URLRequest(url: url, timeoutInterval: 30)
          for (key, value) in headers { request.setValue(value, forHTTPHeaderField: key) }
          let session = URLSession(configuration: .ephemeral)
          defer { session.finishTasksAndInvalidate() }
          let (data, response) = try await session.data(for: request)
          guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode), data.count <= 32 * 1024 * 1024 else {
            throw NSError(domain: "MulticaDeviceLink", code: 4)
          }
          let cache = FileManager.default.temporaryDirectory.appendingPathComponent("MulticaFiles-" + UUID().uuidString, isDirectory: true)
          try FileManager.default.createDirectory(at: cache, withIntermediateDirectories: true)
          let filename = (name.isEmpty ? response.suggestedFilename ?? "attachment" : name) as NSString
          let file = cache.appendingPathComponent(filename.lastPathComponent)
          try data.write(to: file, options: [.atomic, .completeFileProtection])
          await MainActor.run {
            guard self.currentFileEpoch() == epoch else { try? FileManager.default.removeItem(at: cache); promise.reject("session_ended", "The connection has changed"); return }
            guard let controller = DeviceFilePresenter.currentController() else { promise.reject("preview_unavailable", "No active window"); return }
            if share || !QLPreviewController.canPreview(file as NSURL) {
              let activity = UIActivityViewController(activityItems: [file], applicationActivities: nil)
              activity.popoverPresentationController?.sourceView = controller.view
              activity.popoverPresentationController?.sourceRect = controller.view.bounds
              activity.completionWithItemsHandler = { _, _, _, _ in try? FileManager.default.removeItem(at: cache) }
              self.fileController = activity
              controller.present(activity, animated: true)
            } else {
              let presenter = DeviceFilePresenter(file: file)
              self.filePresenter = presenter
              presenter.onDismiss = { [weak self] in self?.filePresenter = nil; try? FileManager.default.removeItem(at: cache) }
              let preview = QLPreviewController()
              preview.dataSource = presenter
              preview.delegate = presenter
              self.fileController = preview
              controller.present(preview, animated: true)
            }
            promise.resolve(nil)
          }
        } catch { promise.reject("preview_failed", error.localizedDescription) }
      }
    }

    OnDestroy {
      self.queue.async {
        self.endBackgroundGrace()
        self.closeClient()
      }
    }
  }
}
