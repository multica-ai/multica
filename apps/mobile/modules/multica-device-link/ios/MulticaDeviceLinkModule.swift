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
  private let contentQueue = DispatchQueue(label: "ai.multica.mobile.device-content")
  private func snapshotRoot() throws -> URL { try FileManager.default.url(for: .cachesDirectory, in: .userDomainMask, appropriateFor: nil, create: true) }
  private var backgroundTask: UIBackgroundTaskIdentifier = .invalid

  private func deviceLinkStorage() throws -> URL {
    try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
      .appendingPathComponent("MulticaDeviceLink", isDirectory: true)
  }

  // Opens before any link does, so what happens before one starts is kept. Logs are
  // diagnostics of this install, so they stay out of device backups.
  private lazy var logs: MobilelinkLogs? = {
    guard let storage = try? deviceLinkStorage() else { return nil }
    var error: NSError?
    guard let opened = MobilelinkOpenLogs(storage.path, &error) else { return nil }
    var directory = storage.appendingPathComponent("logs", isDirectory: true)
    var values = URLResourceValues()
    values.isExcludedFromBackup = true
    try? directory.setResourceValues(values)
    return opened
  }()

  private func closeClient() {
    self.client?.close()
    self.client = nil
  }

  private func endBackgroundTask() {
    if self.backgroundTask != .invalid {
      UIApplication.shared.endBackgroundTask(self.backgroundTask)
      self.backgroundTask = .invalid
    }
  }

  public func definition() -> ModuleDefinition {
    Name("MulticaDeviceLink")

    // Opened here because the JS thread and the content queue both use it next.
    OnCreate { _ = self.logs }

    AsyncFunction("start") { (configuration: String) -> String in
      self.invalidateFiles()
      self.endBackgroundTask()
      self.closeClient()
      guard var values = try JSONSerialization.jsonObject(with: Data(configuration.utf8)) as? [String: Any] else {
        throw NSError(domain: "MulticaDeviceLink", code: 2)
      }
      values["storagePath"] = try self.deviceLinkStorage().path
      let data = try JSONSerialization.data(withJSONObject: values)
      var error: NSError?
      guard let next = MobilelinkStart(String(decoding: data, as: UTF8.self), &error) else {
        throw error ?? NSError(domain: "MulticaDeviceLink", code: 1)
      }
      self.client = next
      return next.endpoint()
    }.runOnQueue(queue)

    // Synchronous so an error logged as the app dies still reaches the disk.
    Function("appendLogLines") { (source: String, lines: String) in
      self.logs?.appendLines(source, lines: lines)
    }

    Function("clearLogs") {
      self.logs?.clear()
    }

    Function("remoteLogReadAllowed") { () -> Bool in
      self.logs?.remoteReadAllowed() ?? false
    }

    Function("setRemoteLogReadAllowed") { (allowed: Bool) in
      self.logs?.setRemoteReadAllowed(allowed)
    }

    Function("setLogRuntime") { (runtime: String) in
      self.logs?.setRuntime(runtime)
    }

    AsyncFunction("exportLogs") { (sinceMinutes: Int) -> String in
      guard let logs = self.logs else { throw NSError(domain: "MulticaDeviceLink", code: 5) }
      var error: NSError?
      let exported = logs.exportFile(Int64(sinceMinutes), error: &error)
      if let error { throw error }
      return exported
    }.runOnQueue(self.contentQueue)

    AsyncFunction("clearLogExports") {
      self.logs?.clearExports()
    }.runOnQueue(self.contentQueue)

    // Only a sign-out the user chose: the device list drops this phone at once.
    AsyncFunction("unregisterDevice") {
      self.client?.unregister()
    }.runOnQueue(queue)

    AsyncFunction("stop") {
      self.invalidateFiles()
      self.endBackgroundTask()
      self.closeClient()
    }.runOnQueue(queue)

    AsyncFunction("updateToken") { (token: String) in
      self.client?.updateToken(token)
    }.runOnQueue(queue)

    AsyncFunction("networkChanged") {
      self.client?.networkChanged()
    }.runOnQueue(queue)

    // The link is never closed for being in the background: it runs for as long as
    // iOS lets the app run. Expiry only returns the task, which iOS requires.
    AsyncFunction("enterBackground") { () in
      guard self.client != nil else { return }
      self.client?.backgrounded()
      self.endBackgroundTask()
      self.backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "MulticaDeviceLink") { [weak self] in
        self?.queue.async { self?.endBackgroundTask() }
      }
    }.runOnQueue(queue)

    AsyncFunction("enterForeground") { () -> Bool in
      self.endBackgroundTask()
      self.client?.foregrounded()
      return self.client != nil
    }.runOnQueue(queue)

    AsyncFunction("sha256File") { (uri: String) -> String? in
      guard let url = URL(string: uri), url.isFileURL else { return nil }
      return try DeviceContentStore.sha256(fileAt: url)
    }.runOnQueue(self.contentQueue)

    AsyncFunction("retainSubmissionAsset") { (key: String, uri: String) -> String in
      try SubmissionStore.retainAsset(key, uri: uri, under: self.deviceLinkStorage())
    }.runOnQueue(self.contentQueue)
    AsyncFunction("removeSubmissionAsset") { (key: String) in
      try SubmissionStore.removeAsset(key, under: self.deviceLinkStorage())
    }.runOnQueue(self.contentQueue)

    AsyncFunction("readSubmissions") { (key: String) -> String? in
      try SubmissionStore.read(key, under: self.deviceLinkStorage())
    }.runOnQueue(self.contentQueue)

    AsyncFunction("writeSubmissions") { (key: String, content: String) in
      try SubmissionStore.write(key, content: content, under: self.deviceLinkStorage())
    }.runOnQueue(self.contentQueue)

    AsyncFunction("readSnapshot") { (key: String) -> String? in
      DeviceContentStore.read(key: key, under: try self.snapshotRoot())
    }.runOnQueue(self.contentQueue)

    AsyncFunction("writeSnapshot") { (key: String, content: String) in
      try DeviceContentStore.write(key: key, content: content, under: try self.snapshotRoot())
    }.runOnQueue(self.contentQueue)

    AsyncFunction("clearSnapshots") {
      DeviceContentStore.clear(under: try self.snapshotRoot())
    }.runOnQueue(self.contentQueue)

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
        self.endBackgroundTask()
        self.closeClient()
      }
    }
  }
}
