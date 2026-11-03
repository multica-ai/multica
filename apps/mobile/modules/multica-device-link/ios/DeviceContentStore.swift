import CryptoKit
import Foundation

/// Files the app keeps about the devices it uses: content hashes of attachments
/// and the conversations it last read. Names are hashed, so any key is a safe
/// file name.
enum DeviceContentStore {
  static let maxSnapshots = 40

  static func sha256(fileAt url: URL) throws -> String {
    let handle = try FileHandle(forReadingFrom: url)
    defer { try? handle.close() }
    var hash = SHA256()
    while let chunk = try handle.read(upToCount: 1 << 20), !chunk.isEmpty { hash.update(data: chunk) }
    return hash.finalize().map { String(format: "%02x", $0) }.joined()
  }

  static func directory(under root: URL) -> URL { root.appendingPathComponent("MulticaSnapshots", isDirectory: true) }

  private static func file(key: String, under root: URL) -> URL {
    let name = SHA256.hash(data: Data(key.utf8)).map { String(format: "%02x", $0) }.joined()
    return directory(under: root).appendingPathComponent(name)
  }

  static func read(key: String, under root: URL) -> String? {
    let url = file(key: key, under: root)
    guard let content = try? String(contentsOf: url, encoding: .utf8) else { return nil }
    try? FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: url.path)
    return content
  }

  static func write(key: String, content: String, under root: URL) throws {
    let directory = directory(under: root)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    try content.write(to: file(key: key, under: root), atomically: true, encoding: .utf8)
    let entries = try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: [.contentModificationDateKey])
    guard entries.count > maxSnapshots else { return }
    let modified = { (url: URL) in (try? url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast }
    for old in entries.sorted(by: { modified($0) < modified($1) }).prefix(entries.count - maxSnapshots) {
      try? FileManager.default.removeItem(at: old)
    }
  }

  static func clear(under root: URL) { try? FileManager.default.removeItem(at: directory(under: root)) }
}
