import Foundation

// 🎭 The Rules of the House — pure pool policy, no I/O, no clocks, no actors.
// Every verdict here is a table the tests can recite from memory.
//
// "Policy distilled into enums is policy the compiler defends." ✨

/// 🎪 The admission verdict — finite, meaningful, payload-free on purpose:
/// the *actor* owns the numbers; this owns the *decision*.
public enum Admission: Equatable, Sendable {
    /// A dreaming soul wakes and takes the stage.
    case reuseIdle
    /// The theater has empty seats — summon a fresh, pre-named soul.
    case summonNew
    /// Full house. Join the patient line (pgxpool's "empty acquire").
    case enqueueWaiter

    /// 🌟 The verdict, decided — a pure function of the pool's shape.
    public static func decide(idleCount: Int, totalConns: Int, maxConns: Int) -> Admission {
        if idleCount > 0 { return .reuseIdle }
        if totalConns < maxConns { return .summonNew }
        return .enqueueWaiter
    }

    /// The stagehand's instruction — exhaustive, so a new verdict demands a
    /// new instruction (never a silent fallthrough).
    public var stageInstruction: String {
        switch self {
        case .reuseIdle: "wake a dreamer"
        case .summonNew: "summon a fresh soul"
        case .enqueueWaiter: "join the line"
        }
    }
}

/// 📊 The evening's ledger snapshot — pgxpool `Stat()` harmony, Swift voice.
public struct PoolStats: Sendable, Equatable, Codable {
    public var applicationName: String
    public var nameSource: String
    public var maxConns: Int
    public var minConns: Int
    public var totalConns: Int
    public var idleConns: Int
    public var checkedOut: Int
    public var waiters: Int
    public var acquireCount: Int
    public var emptyAcquireCount: Int
    public var highWater: Int
}

/// 📈 The space between two snapshots — a pure diff, computed aloud.
public struct PoolStatsDelta: Sendable, Equatable, Codable {
    public var acquires: Int
    public var emptyAcquires: Int

    /// 🌟 Pure arithmetic on two ledgers — nothing more, nothing hidden.
    public static func diff(_ earlier: PoolStats, _ later: PoolStats) -> PoolStatsDelta {
        PoolStatsDelta(
            acquires: later.acquireCount - earlier.acquireCount,
            emptyAcquires: later.emptyAcquireCount - earlier.emptyAcquireCount
        )
    }
}

/// 🌩️ The evening's mood — derived, never stored.
public enum Pressure: String, Sendable, Codable {
    case serene
    case gathering
    case pinnedAtMax

    /// 🌟 A pure verdict from the current shape of the house.
    public static func verdict(for stats: PoolStats, delta: PoolStatsDelta) -> Pressure {
        if stats.totalConns >= stats.maxConns && stats.waiters > 0 {
            return .pinnedAtMax
        }
        if delta.emptyAcquires > 0 || stats.waiters > 0 {
            return .gathering
        }
        return .serene
    }

    /// The gallery's sigil — exhaustive switch, compiler-defended.
    public var sigil: String {
        switch self {
        case .serene: "🌙"
        case .gathering: "🌊"
        case .pinnedAtMax: "🌩️"
        }
    }

    /// The gallery's caption.
    public var caption: String {
        switch self {
        case .serene: "serene"
        case .gathering: "pressure gathering"
        case .pinnedAtMax: "pinned at max"
        }
    }
}

/// ⏱️ A kindness for turning durations into numbers the JSON gallery reads.
extension Duration {
    public var secondsAsDouble: Double {
        Double(components.seconds) + Double(components.attoseconds) / 1e18
    }
    public var millisecondsAsDouble: Double { secondsAsDouble * 1_000 }
}
