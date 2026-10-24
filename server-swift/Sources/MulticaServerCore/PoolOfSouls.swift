import Logging

// 🎭 The Pool of Souls — the imperative shell around a pure policy.
//
// The actor owns *custody* (who holds which soul); every *decision* is
// delegated to `Admission.decide` and every derived mood to `Pressure`.
// The seam (`ConnectionSummoner`) lets tests stage the whole theater with
// scripted souls — no oracle, no database, no flake. 🎪

/// 🔮 What every hirable soul must promise: it can be asked if it still
/// breathes, and dismissed with thanks.
public protocol PooledConnection: Sendable {
    var isClosed: Bool { get }
    func dismiss() async
}

/// 📯 The summoner's oath — a factory the pool can beg for fresh souls.
public protocol ConnectionSummoner: Sendable {
    associatedtype Connection: PooledConnection
    func summon(id: Int) async throws -> Connection
    var applicationName: String { get }
}

/// 🌊 The pool itself — an actor, because custody is shared mutable state
/// and actors are how Swift 6 says "serialized, provably."
public actor PoolOfSouls<Summoner: ConnectionSummoner> {

    private let summoner: Summoner
    private let config: PoolConfigTides
    private let logger: Logger

    // 🌙 custody state — private to the actor, always consistent
    private var dreaming: [Summoner.Connection] = []
    private var totalSouls = 0
    private var onStage = 0
    private var waiting: [CheckedContinuation<Summoner.Connection, any Error>] = []
    private var acquireCount = 0
    private var emptyAcquireCount = 0
    private var highWater = 0
    private var nextID = 1

    /// 📜 Just the tides the pool needs — a focused view of the contract.
    public struct PoolConfigTides: Sendable {
        public let maxConns: Int
        public let minConns: Int
        public init(maxConns: Int, minConns: Int) {
            self.maxConns = maxConns
            self.minConns = minConns
        }
    }

    public init(summoner: Summoner, tides: PoolConfigTides, logger: Logger) {
        self.summoner = summoner
        self.config = tides
        self.logger = logger
    }

    /// 🌱 The Warm-Up Overture — wake the minimum choir before the curtain.
    public func warmUp() async {
        var awakened = 0
        while totalSouls < config.minConns {
            do {
                let soul = try await summonSoul()
                dreaming.append(soul)
                totalSouls += 1
                awakened += 1
            } catch {
                logger.warning("🌙 ⚠️ Warm-up soul declined the summons: \(error.localizedDescription)")
                break
            }
        }
        logger.info("🌱 ✨ CHOIR WARMED — \(awakened) souls dreaming (tides \(config.minConns)…\(config.maxConns)), all named '\(summoner.applicationName)'")
    }

    /// 🎪 The Summoning — borrow a soul, perform, return it to the deep.
    /// Policy is one pure call; this shell just obeys the verdict.
    public func withConnection<T: Sendable>(
        _ performance: @Sendable (Summoner.Connection) async throws -> T
    ) async throws -> T {
        acquireCount += 1
        let soul: Summoner.Connection

        // ✂️ The dead do not take the stage
        retireDeparted()

        switch Admission.decide(idleCount: dreaming.count, totalConns: totalSouls, maxConns: config.maxConns) {
        case .reuseIdle:
            soul = dreaming.removeLast()
        case .summonNew:
            soul = try await summonSoul()
            totalSouls += 1
        case .enqueueWaiter:
            emptyAcquireCount += 1
            soul = try await withCheckedThrowingContinuation { continuation in
                waiting.append(continuation)
            }
        }

        onStage += 1
        highWater = max(highWater, onStage)

        do {
            let treasure = try await performance(soul)
            returnToTheDeep(soul)
            return treasure
        } catch {
            // 💔 A soul that died mid-performance is retired, not re-dreamt
            returnToTheDeep(soul)
            throw error
        }
    }

    /// 💤 A soul returns — the line gets first refusal, then the deep.
    private func returnToTheDeep(_ soul: Summoner.Connection) {
        onStage -= 1
        if soul.isClosed {
            totalSouls -= 1
            return
        }
        if let nextPatron = waiting.first, !waiting.isEmpty {
            waiting.removeFirst()
            onStage += 1
            highWater = max(highWater, onStage)
            nextPatron.resume(returning: soul)
        } else {
            dreaming.append(soul)
        }
    }

    /// 🔮 Delegate to the summoner's oath; ids are the pool's ledger numbers.
    private func summonSoul() async throws -> Summoner.Connection {
        defer { nextID += 1 }
        return try await summoner.summon(id: nextID)
    }

    /// 🕯️ Quietly retire souls that departed without a farewell.
    private func retireDeparted() {
        let before = dreaming.count
        dreaming.removeAll { $0.isClosed }
        totalSouls -= before - dreaming.count
    }

    // 📊 The evening's ledger, read aloud — a pure snapshot crossing the boundary.
    public func stats() -> PoolStats {
        PoolStats(
            applicationName: summoner.applicationName,
            nameSource: "pool",
            maxConns: config.maxConns,
            minConns: config.minConns,
            totalConns: totalSouls,
            idleConns: dreaming.count,
            checkedOut: onStage,
            waiters: waiting.count,
            acquireCount: acquireCount,
            emptyAcquireCount: emptyAcquireCount,
            highWater: highWater
        )
    }

    /// 🌑 Final curtain — thank every soul; excuse every waiter honestly.
    public func shutdown() async {
        let farewell = dreaming
        dreaming = []
        totalSouls = 0
        onStage = 0
        for patron in waiting { patron.resume(throwing: CancellationError()) }
        waiting = []
        for soul in farewell { await soul.dismiss() }
        logger.info("🌑 🎬 FINAL CURTAIN — \(farewell.count) souls thanked and dismissed")
    }
}
