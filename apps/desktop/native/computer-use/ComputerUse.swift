import AppKit
import ApplicationServices
import CoreImage
import ScreenCaptureKit

struct Failure: Error { let message: String; init(_ message: String) { self.message = message } }

final class Capture: NSObject, SCStreamOutput {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<CGImage, Error>?
    private var captured: CGImage?
    func image() async throws -> CGImage {
        try await withCheckedThrowingContinuation { pending in
            lock.lock()
            if let image = captured { lock.unlock(); pending.resume(returning: image) }
            else { continuation = pending; lock.unlock() }
        }
    }
    func stream(_ stream: SCStream, didOutputSampleBuffer sample: CMSampleBuffer, of type: SCStreamOutputType) {
        guard type == .screen, let buffer = CMSampleBufferGetImageBuffer(sample) else { return }
        guard let image = CIContext().createCGImage(CIImage(cvPixelBuffer: buffer), from: CGRect(x: 0, y: 0, width: CVPixelBufferGetWidth(buffer), height: CVPixelBufferGetHeight(buffer))) else { return }
        lock.lock()
        if captured != nil { lock.unlock(); return }
        captured = image
        let pending = continuation; continuation = nil
        lock.unlock()
        pending?.resume(returning: image)
    }
}

@MainActor final class ComputerService {
    struct Snapshot {
        let id: String
        let pid: pid_t
        let window: AXUIElement
        let bounds: CGRect
        let refs: [String: AXUIElement]
    }
    var apps: [String: URL] = [:]
    var snapshots: [String: Snapshot] = [:]
    var windowRefs: [String: [String: AXUIElement]] = [:]
    var selectedWindows: [String: AXUIElement] = [:]
    let blocked = ["ai.multica.desktop", "com.openai.codex", "com.apple.Terminal", "com.googlecode.iterm2", "com.apple.systempreferences", "com.apple.loginwindow", "com.apple.SecurityAgent"]

    func attribute(_ element: AXUIElement, _ name: String) -> CFTypeRef? {
        var value: CFTypeRef?
        return AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success ? value : nil
    }
    func bounds(_ element: AXUIElement) -> CGRect? {
        guard let p = attribute(element, kAXPositionAttribute), let s = attribute(element, kAXSizeAttribute),
              CFGetTypeID(p) == AXValueGetTypeID(), CFGetTypeID(s) == AXValueGetTypeID() else { return nil }
        var point = CGPoint.zero, size = CGSize.zero
        guard AXValueGetValue(p as! AXValue, .cgPoint, &point), AXValueGetValue(s as! AXValue, .cgSize, &size) else { return nil }
        return CGRect(origin: point, size: size)
    }
    func rect(_ r: CGRect) -> [String: CGFloat] { ["x": r.minX, "y": r.minY, "width": r.width, "height": r.height] }
    func check(_ id: String) throws {
        guard !blocked.contains(where: { id == $0 || id.hasPrefix($0 + ".") }), !id.localizedCaseInsensitiveContains("multica"), id != Bundle.main.bundleIdentifier else {
            throw Failure("app_forbidden: This application cannot be automated")
        }
    }
    func running(_ id: String) throws -> NSRunningApplication {
        try check(id)
        guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: id).first, !app.isTerminated else { throw Failure("app_closed: Open the application again") }
        return app
    }
    func window(_ app: NSRunningApplication) throws -> AXUIElement {
        let root = AXUIElementCreateApplication(app.processIdentifier)
        AXUIElementSetMessagingTimeout(root, 2)
        if let id = app.bundleIdentifier, let selected = selectedWindows[id] {
            guard let windows = attribute(root, kAXWindowsAttribute) as? [AXUIElement], windows.contains(where: { CFEqual($0, selected) }), (attribute(selected, kAXMinimizedAttribute) as? Bool) != true else { throw Failure("window_unavailable: Select a current visible window") }
            return selected
        }
        if let focused = attribute(root, kAXFocusedWindowAttribute), CFGetTypeID(focused) == AXUIElementGetTypeID(), (attribute(focused as! AXUIElement, kAXMinimizedAttribute) as? Bool) != true { return focused as! AXUIElement }
        guard let windows = attribute(root, kAXWindowsAttribute) as? [AXUIElement], let window = windows.first(where: { (attribute($0, kAXMinimizedAttribute) as? Bool) != true }) else { throw Failure("window_unavailable: The application needs a visible window") }
        return window
    }
    func resolve(_ value: String) throws -> [String: Any] {
        let active = NSWorkspace.shared.runningApplications.first { $0.bundleIdentifier == value || $0.localizedName?.caseInsensitiveCompare(value) == .orderedSame }
        let roots = ["/Applications", "/System/Applications", "/System/Applications/Utilities", NSHomeDirectory() + "/Applications"]
        let named = roots.map { URL(fileURLWithPath: $0).appendingPathComponent(value + ".app") }.first { FileManager.default.fileExists(atPath: $0.path) }
        guard let url = active?.bundleURL ?? NSWorkspace.shared.urlForApplication(withBundleIdentifier: value) ?? named,
              let bundle = Bundle(url: url), let id = bundle.bundleIdentifier else { throw Failure("app_not_found: Supply an installed app name or bundle ID") }
        try check(id)
        apps[id] = url
        return ["id": id, "name": bundle.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String ?? bundle.object(forInfoDictionaryKey: "CFBundleName") as? String ?? value]
    }
    func screenshot(_ app: NSRunningApplication, _ frame: CGRect, _ title: String?) async throws -> [String: Any] {
        let content = try await SCShareableContent.excludingDesktopWindows(true, onScreenWindowsOnly: true)
        let candidates = content.windows.filter { $0.owningApplication?.processID == app.processIdentifier && $0.frame == frame && (title == nil || $0.title == title) }
        guard candidates.count == 1, let target = candidates.first else { throw Failure("window_unavailable: No unique capturable window; move overlapping windows apart") }
        let filter = SCContentFilter(desktopIndependentWindow: target)
        let config = SCStreamConfiguration()
        let scale = min(1, 1400 / max(frame.width, frame.height))
        config.width = max(1, Int(frame.width * scale)); config.height = max(1, Int(frame.height * scale))
        config.showsCursor = false; config.queueDepth = 1
        let capture = Capture()
        let stream = SCStream(filter: filter, configuration: config, delegate: nil)
        try stream.addStreamOutput(capture, type: .screen, sampleHandlerQueue: DispatchQueue(label: "multica.computer.capture"))
        try await stream.startCapture()
        let image = try await capture.image()
        try await stream.stopCapture()
        let bitmap = NSBitmapImageRep(cgImage: image)
        for quality in [0.7, 0.5, 0.3, 0.15] {
            if let data = bitmap.representation(using: .jpeg, properties: [.compressionFactor: quality]), data.count <= 500 * 1024 {
                return ["mimeType": "image/jpeg", "data": data.base64EncodedString(), "width": image.width, "height": image.height]
            }
        }
        throw Failure("image_too_large: Narrow the target window")
    }
    func observe(_ id: String) async throws -> [String: Any] {
        guard AXIsProcessTrusted(), CGPreflightScreenCaptureAccess() else { throw Failure("permission_required: Enable Screen Recording and Accessibility for Multica Computer Use") }
        let app = try running(id), win = try window(app)
        guard let frame = bounds(win), frame.width > 0, frame.height > 0 else { throw Failure("window_unavailable") }
        let observation = UUID().uuidString
        var refs: [String: AXUIElement] = [:], rows: [[String: Any]] = [], visited = Set<CFHashCode>()
        var truncated = false
        func walk(_ element: AXUIElement, _ depth: Int) {
            guard rows.count < 300, depth <= 12 else { truncated = true; return }
            guard visited.insert(CFHash(element)).inserted else { return }
            let ref = observation + ":e\(rows.count)", role = attribute(element, kAXRoleAttribute) as? String ?? "unknown"
            let secure = (attribute(element, kAXSubroleAttribute) as? String) == kAXSecureTextFieldSubrole
            var row: [String: Any] = ["ref": ref, "role": role, "depth": depth]
            for (key, name) in [("title", kAXTitleAttribute), ("description", kAXDescriptionAttribute), ("value", kAXValueAttribute)] {
                if key == "value" && secure { continue }
                if let value = attribute(element, name) as? String { row[key] = String(value.prefix(160)) }
            }
            if !secure, let value = attribute(element, kAXValueAttribute) as? NSNumber { row["value"] = value }
            if let enabled = attribute(element, kAXEnabledAttribute) as? Bool { row["enabled"] = enabled }
            if let b = bounds(element) { row["bounds"] = rect(b.offsetBy(dx: -frame.minX, dy: -frame.minY)) }
            row["secure"] = secure
            refs[ref] = element; rows.append(row)
            for child in (attribute(element, kAXChildrenAttribute) as? [AXUIElement] ?? []) { walk(child, depth + 1) }
        }
        walk(win, 0)
        while (try JSONSerialization.data(withJSONObject: rows)).count > 120 * 1024 { rows.removeLast(); truncated = true }
        let image = try await screenshot(app, frame, attribute(win, kAXTitleAttribute) as? String)
        guard bounds(win) == frame else { throw Failure("stale_observation: The window moved during capture") }
        snapshots[id] = Snapshot(id: observation, pid: app.processIdentifier, window: win, bounds: frame, refs: refs)
        return ["appId": id, "observationId": observation, "window": rect(frame), "elements": rows, "image": image,
                "coordinateSpace": "window-logical-points", "screenshotSize": ["width": image["width"] as? Int ?? 0, "height": image["height"] as? Int ?? 0], "untrustedContent": true, "truncated": truncated]
    }
    func perform(_ input: [String: Any]) async throws -> [String: Any] {
        guard let op = input["op"] as? String else { throw Failure("invalid_request") }
        if op == "status" { return ["available": true, "platform": "darwin", "mode": "foreground", "accessibility": AXIsProcessTrusted(), "screenRecording": CGPreflightScreenCaptureAccess()] }
        if op == "request_permissions" {
            let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
            _ = AXIsProcessTrustedWithOptions(options); _ = CGRequestScreenCaptureAccess()
            return try await perform(["op": "status"])
        }
        if op == "clear" { snapshots.removeAll(); windowRefs.removeAll(); selectedWindows.removeAll(); return ["status": "released"] }
        guard let id = input["app"] as? String else { throw Failure("invalid_request: app required") }
        if op == "get_app" { return try resolve(id) }
        try check(id)
        if op == "launch" {
            guard let url = apps[id] else { throw Failure("app_not_found") }
            if NSRunningApplication.runningApplications(withBundleIdentifier: id).isEmpty {
                _ = try await NSWorkspace.shared.openApplication(at: url, configuration: NSWorkspace.OpenConfiguration())
            }
            return ["id": id]
        }
        if op == "observe" { return try await observe(id) }
        let app = try running(id)
        if op == "windows" {
            guard AXIsProcessTrusted() else { throw Failure("permission_required") }
            let root = AXUIElementCreateApplication(app.processIdentifier)
            let windows = attribute(root, kAXWindowsAttribute) as? [AXUIElement] ?? []
            var refs: [String: AXUIElement] = [:], result: [[String: Any]] = []
            for win in windows.prefix(50) {
                let key = UUID().uuidString
                refs[key] = win
                var info: [String: Any] = ["id": key, "title": String((attribute(win, kAXTitleAttribute) as? String ?? "").prefix(160)), "minimized": attribute(win, kAXMinimizedAttribute) as? Bool ?? false]
                if let frame = bounds(win) { info["bounds"] = rect(frame) }
                result.append(info)
            }
            windowRefs[id] = refs
            return ["windows": result, "truncated": windows.count > 50]
        }
        if op == "select_window" {
            guard let windowId = input["windowId"] as? String, let win = windowRefs[id]?[windowId], let windows = attribute(AXUIElementCreateApplication(app.processIdentifier), kAXWindowsAttribute) as? [AXUIElement], windows.contains(where: { CFEqual($0, win) }) else { throw Failure("window_unavailable: List the app's windows again") }
            selectedWindows[id] = win; snapshots.removeValue(forKey: id)
            return ["status": "selected", "requiresObservation": true]
        }
        guard AXIsProcessTrusted(), let snap = snapshots.removeValue(forKey: id), snap.id == input["observationId"] as? String, snap.pid == app.processIdentifier, CFEqual(try window(app), snap.window),
              let frame = bounds(snap.window), frame == snap.bounds else { throw Failure("stale_observation: Observe the application again") }
        app.unhide(); _ = app.activate(options: [.activateIgnoringOtherApps])
        if selectedWindows[id] != nil { _ = AXUIElementPerformAction(snap.window, kAXRaiseAction as CFString) }
        for _ in 0..<10 {
            if NSWorkspace.shared.frontmostApplication?.processIdentifier == app.processIdentifier,
               let focused = attribute(AXUIElementCreateApplication(app.processIdentifier), kAXFocusedWindowAttribute), CFEqual(focused, snap.window) { break }
            try await Task.sleep(nanoseconds: 50_000_000)
        }
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == app.processIdentifier else { throw Failure("focus_failed: Application did not become active") }
        guard let focused = attribute(AXUIElementCreateApplication(app.processIdentifier), kAXFocusedWindowAttribute), CFEqual(focused, snap.window) else { throw Failure("focus_failed: Target window is not focused") }
        func point(_ xKey: String = "x", _ yKey: String = "y") throws -> CGPoint {
            guard let x = input[xKey] as? Double, let y = input[yKey] as? Double, x.isFinite, y.isFinite, x >= 0, y >= 0, x < frame.width, y < frame.height else { throw Failure("invalid_coordinate") }
            return CGPoint(x: frame.minX + x, y: frame.minY + y)
        }
        func element() throws -> AXUIElement {
            guard let ref = input["ref"] as? String, let e = snap.refs[ref], (attribute(e, kAXSubroleAttribute) as? String) != kAXSecureTextFieldSubrole else { throw Failure("stale_or_secure_control") }
            return e
        }
        func mouse(_ type: CGEventType, _ p: CGPoint) { CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: p, mouseButton: .left)?.post(tap: .cghidEventTap) }
        if op == "type" || op == "press" {
            let root = AXUIElementCreateApplication(app.processIdentifier)
            if let focused = attribute(root, kAXFocusedUIElementAttribute), CFGetTypeID(focused) == AXUIElementGetTypeID(),
               (attribute(focused as! AXUIElement, kAXSubroleAttribute) as? String) == kAXSecureTextFieldSubrole {
                throw Failure("secure_control: Hand password entry to the user")
            }
        }
        switch op {
        case "click":
            if input["ref"] != nil {
                let e = try element()
                if AXUIElementPerformAction(e, kAXPressAction as CFString) == .success { break }
                guard let b = bounds(e), frame.contains(CGPoint(x: b.midX, y: b.midY)) else { throw Failure("stale_control") }
                mouse(.leftMouseDown, CGPoint(x: b.midX, y: b.midY)); mouse(.leftMouseUp, CGPoint(x: b.midX, y: b.midY))
            } else { let p = try point(); mouse(.leftMouseDown, p); mouse(.leftMouseUp, p) }
        case "set_value":
            guard let value = input["value"] as? String, value.count <= 10000, AXUIElementSetAttributeValue(try element(), kAXValueAttribute as CFString, value as CFString) == .success else { throw Failure("set_value_failed") }
        case "type":
            guard let text = input["text"] as? String, !text.isEmpty, text.utf16.count <= 10000 else { throw Failure("invalid_text") }
            for scalar in text.unicodeScalars {
                let units = Array(String(scalar).utf16)
                for down in [true, false] {
                    let event = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: down)
                    units.withUnsafeBufferPointer { event?.keyboardSetUnicodeString(stringLength: units.count, unicodeString: $0.baseAddress!) }
                    event?.post(tap: .cghidEventTap)
                }
            }
        case "press":
            guard let chord = input["key"] as? String else { throw Failure("invalid_key") }
            var keys = chord.split(separator: "+").map(String.init), flags: CGEventFlags = []
            guard let key = keys.popLast() else { throw Failure("invalid_key") }
            for modifier in keys {
                switch modifier { case "Meta", "Command": flags.insert(.maskCommand); case "Control", "Ctrl": flags.insert(.maskControl); case "Alt", "Option": flags.insert(.maskAlternate); case "Shift": flags.insert(.maskShift); default: throw Failure("invalid_modifier") }
            }
            let codes: [String: CGKeyCode] = ["a":0,"s":1,"d":2,"f":3,"h":4,"g":5,"z":6,"x":7,"c":8,"v":9,"b":11,"q":12,"w":13,"e":14,"r":15,"y":16,"t":17,"1":18,"2":19,"3":20,"4":21,"6":22,"5":23,"9":25,"7":26,"8":28,"0":29,"o":31,"u":32,"i":34,"p":35,"l":37,"j":38,"k":40,"n":45,"m":46,"enter":36,"return":36,"tab":48,"space":49,"backspace":51,"escape":53,"arrowleft":123,"arrowright":124,"arrowdown":125,"arrowup":126]
            guard let code = codes[key.lowercased()] else { throw Failure("unsupported_key") }
            for down in [true,false] { let event = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: down); event?.flags = flags; event?.post(tap: .cghidEventTap) }
        case "scroll":
            let p = try point()
            guard let dy = input["dy"] as? Int32, abs(Int64(dy)) <= 3000 else { throw Failure("invalid_scroll") }
            let event = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 1, wheel1: -dy, wheel2: 0, wheel3: 0)
            event?.location = p; event?.post(tap: .cghidEventTap)
        case "drag":
            let from = try point(), to = try point("toX", "toY")
            mouse(.leftMouseDown, from)
            for n in 1...10 { mouse(.leftMouseDragged, CGPoint(x: from.x + (to.x-from.x)*CGFloat(n)/10, y: from.y + (to.y-from.y)*CGFloat(n)/10)) }
            mouse(.leftMouseUp, to)
        default: throw Failure("unsupported_action")
        }
        return ["status": "applied", "requiresObservation": true]
    }
}

@main struct Main {
    @MainActor static func main() async {
        let service = ComputerService()
        while let line = readLine() {
            var response: [String: Any] = [:]
            do {
                guard line.utf8.count <= 65536, let data = line.data(using: .utf8), let message = try JSONSerialization.jsonObject(with: data) as? [String: Any], let id = message["id"] as? String, let action = message["action"] as? [String: Any] else { throw Failure("invalid_request") }
                response["id"] = id
                response["result"] = try await service.perform(action)
            } catch { response["error"] = (error as? Failure)?.message ?? "native_operation_failed" }
            if let data = try? JSONSerialization.data(withJSONObject: response, options: [.sortedKeys]) { FileHandle.standardOutput.write(data); FileHandle.standardOutput.write(Data([10])) }
        }
    }
}
