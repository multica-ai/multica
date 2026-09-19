import Foundation
import Logging

// 🎭 The enchanted Observatory — samples the pool's pulse, keeps a ring of
// evenings, and reads each sample's mood aloud in emoji.
//
// Pure core: RingBuffer + PoolSample. Shell: the sampler task. ✨

/// 💎 A ring of fixed capacity — value semantics, oldest verse overwritten.
public struct RingBuffer<Element>: Sendable where Element: Sendable {
    private var storage: [Element?]
    private var head = 0
    private var count = 0
    public let capacity: Int

    public init(capacity: Int) {
        precondition(capacity > 0, "a ring must have room for at least one verse")
        self.capacity = capacity
        self.storage = Array(repeating: nil, count: capacity)
    }

    /// 🌟 Append, overwriting the eldest when full.
    public mutating func append(_ element: Element) {
        storage[head] = element
        head = (head + 1) % capacity
        count = min(count + 1, capacity)
    }

    /// 📖 The verses, eldest first.
    public var recent: [Element] {
        let start = (head - count + capacity) % capacity
        return (0..<count).map { storage[(start + $0) % capacity]! }
    }

    public var isEmpty: Bool { count == 0 }
}

/// 🕰️ One heartbeat of the pool, as the gallery will see it.
public struct PoolSample: Sendable, Codable, Equatable {
    public var at: Date
    public var stats: PoolStats
    public var delta: PoolStatsDelta
    public var pressure: String
    public var pressureSigil: String
    public var pingMs: Double?
}

/// 🔭 The observatory itself — an actor, for the ring is shared state.
///
/// Two audiences: the in-process ring (history) and live subscribers (SSE
/// fan-out). Subscribers are `AsyncStream`s — back-pressure honest, and
/// termination removes them from the ledger automatically.
public actor TelemetryObserver {
    private var ring: RingBuffer<PoolSample>
    private var lastStats: PoolStats?
    private var sampler: Task<Void, Never>?
    private var subscribers: [UUID: AsyncStream<PoolSample>.Continuation] = [:]
    private let logger: Logger

    public init(capacity: Int, logger: Logger) {
        self.ring = .init(capacity: capacity)
        self.logger = logger
    }

    /// 📊 The full evening's memory, eldest first.
    public func history() -> [PoolSample] { ring.recent }

    /// 📡 Subscribe to live heartbeats — SSE's daily bread.
    /// Buffering newest: a slow watcher loses stale verses, never blocks the show.
    public func subscribe() -> AsyncStream<PoolSample> {
        let id = UUID()
        return AsyncStream(bufferingPolicy: .bufferingNewest(8)) { continuation in
            continuation.onTermination = { _ in
                Task { await self.unsubscribe(id) }
            }
            Task { await self.enroll(id, continuation: continuation) }
        }
    }

    private func enroll(_ id: UUID, continuation: AsyncStream<PoolSample>.Continuation) {
        subscribers[id] = continuation
    }

    private func unsubscribe(_ id: UUID) {
        subscribers[id] = nil
    }

    /// 🌟 Record one heartbeat — pure derivation inside, custody outside.
    public func record(stats: PoolStats, pingMs: Double?) {
        let delta = lastStats.map { PoolStatsDelta.diff($0, stats) } ?? .init(acquires: 0, emptyAcquires: 0)
        let pressure = Pressure.verdict(for: stats, delta: delta)
        let sample = PoolSample(
            at: Date(),
            stats: stats,
            delta: delta,
            pressure: pressure.rawValue,
            pressureSigil: pressure.sigil,
            pingMs: pingMs
        )
        ring.append(sample)
        lastStats = stats

        // 📡 Fan-out to every live watcher — stale watchers are pruned by
        // their own termination; the show never waits for one slow patron.
        for continuation in subscribers.values {
            continuation.yield(sample)
        }

        // 📜 The verse is read aloud — serene hums INFO, storms WARN
        let line = "\(pressure.sigil) db pool \(pressure.caption) — total \(stats.totalConns)/\(stats.maxConns), on stage \(stats.checkedOut), in line \(stats.waiters), acquires Δ\(delta.acquires), empty Δ\(delta.emptyAcquires)"
        if pressure == .serene {
            logger.info("🌙 \(line)")
        } else {
            logger.warning("🌊 \(line)")
        }
    }

    /// 🎬 Start the sampler — one conductor, started once.
    public func startSampler(
        pool: Oracle.SoulPool,
        oracle: Oracle,
        interval: Duration = .seconds(15)
    ) {
        guard sampler == nil else { return }
        sampler = Task { [weak self] in
            while true {
                try? await Task.sleep(for: interval)
                guard let self else { return }
                var pingMs: Double?
                if let ping = try? await oracle.ping() { pingMs = ping.millisecondsAsDouble }
                let stats = await pool.statsSnapshotForObservatory()
                await self.record(stats: stats, pingMs: pingMs)
            }
        }
    }
}

// 🎁 Small kindness: the pool already speaks stats(); give it a gentle alias
// so the observatory reads like prose.
extension PoolOfSouls {
    public func statsSnapshotForObservatory() async -> PoolStats { stats() }
}
