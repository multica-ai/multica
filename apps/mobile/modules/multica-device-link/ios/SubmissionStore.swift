import Foundation
import CryptoKit
import Darwin

/// Reliable messages live in Application Support, independently of snapshot eviction.
enum SubmissionStore {
  private static func file(_ key: String, under root: URL) throws -> URL {
    let directory = root.appendingPathComponent("submissions", isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let name = SHA256.hash(data: Data(key.utf8)).map { String(format: "%02x", $0) }.joined()
    return directory.appendingPathComponent(name + ".json")
  }

  private static func synchronize(_ url: URL) throws {
    let handle = try FileHandle(forWritingTo: url)
    defer { try? handle.close() }
    try handle.synchronize()
    let directory = open(url.deletingLastPathComponent().path, O_RDONLY)
    guard directory >= 0 else { throw POSIXError(.EIO) }
    defer { close(directory) }
    guard fsync(directory) == 0 else { throw POSIXError(.EIO) }
  }

  static func read(_ key: String, under root: URL) throws -> String? {
    let url = try file(key, under: root)
    guard FileManager.default.fileExists(atPath: url.path) else { return nil }
    return try String(contentsOf: url, encoding: .utf8)
  }

  static func write(_ key: String, content: String, under root: URL) throws {
    let url = try file(key, under: root)
    try Data(content.utf8).write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    try synchronize(url)
  }

  static func retainAsset(_ key: String, uri: String, under root: URL) throws -> String {
    guard let source = URL(string: uri), source.isFileURL else { throw POSIXError(.EINVAL) }
    let attributes=try FileManager.default.attributesOfItem(atPath:source.path)
    guard let size=attributes[.size] as? NSNumber,size.int64Value<=25*1024*1024 else {throw POSIXError(.EFBIG)}
    let url = try file("asset:" + key, under: root).appendingPathExtension("asset")
    if FileManager.default.fileExists(atPath: url.path) { return url.absoluteString }
    try Data(contentsOf: source).write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    try synchronize(url)
    return url.absoluteString
  }

  static func removeAsset(_ key: String, under root: URL) throws {
    let url = try file("asset:" + key, under: root).appendingPathExtension("asset")
    if FileManager.default.fileExists(atPath: url.path) { try FileManager.default.removeItem(at: url) }
  }
}
