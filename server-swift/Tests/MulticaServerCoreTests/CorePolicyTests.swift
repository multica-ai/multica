import Foundation
import Testing
@testable import MulticaServerCore

// 🧪 The Quality-Assurance Ritual — pure tables, recited from memory.
// No database, no clock, no network: the functional core stands alone.

@Suite("PoolConfig.parse — the Great Distillation")
struct PoolConfigParseTests {

    private static let baseURL = "postgres://multica:pw@localhost:5442/multica?sslmode=disable"

    @Test("defaults: blessed name, host-sized tides")
    func defaults() {
        guard case .success(let config) = PoolConfig.parse(urlString: Self.baseURL) else {
            Issue.record("base URL must parse"); return
        }
        #expect(config.applicationName == .default)
        #expect(config.applicationName.value == "multica-swift-server")
        #expect(config.maxConns == 8)
        #expect(config.minConns == 2)
        #expect(config.host == "localhost")
        #expect(config.port == 5442)
        #expect(config.database == "multica")
    }

    @Test("URL sigil outranks env scroll outranks default")
    func namePrecedence() {
        // URL wins over env
        guard case .success(let urlWins) = PoolConfig.parse(
            urlString: Self.baseURL + "&application_name=hand-written",
            env: { $0 == "MULTICA_APP_NAME" ? "env-scroll" : nil }
        ) else { Issue.record("must parse"); return }
        #expect(urlWins.applicationName == .url("hand-written"))

        // env wins when URL is silent
        guard case .success(let envWins) = PoolConfig.parse(
            urlString: Self.baseURL,
            env: { $0 == "MULTICA_APP_NAME" ? "env-scroll" : nil }
        ) else { Issue.record("must parse"); return }
        #expect(envWins.applicationName == .env("env-scroll"))
        #expect(envWins.applicationName.sourceLabel == "env")
    }

    @Test("tides: env scrolls resize, and min can never exceed max")
    func tides() {
        guard case .success(let resized) = PoolConfig.parse(
            urlString: Self.baseURL,
            env: { name in
                switch name {
                case "DATABASE_MAX_CONNS": "25"
                case "DATABASE_MIN_CONNS": "5"
                default: nil
                }
            }
        ) else { Issue.record("must parse"); return }
        #expect(resized.maxConns == 25)
        #expect(resized.minConns == 5)

        guard case .success(let clamped) = PoolConfig.parse(
            urlString: Self.baseURL,
            env: { name in
                switch name {
                case "DATABASE_MAX_CONNS": "3"
                case "DATABASE_MIN_CONNS": "9"   // impossible demand
                default: nil
                }
            }
        ) else { Issue.record("must parse"); return }
        #expect(clamped.minConns == 3)   // the impossible is unrepresentable
    }

    @Test("bad scriptures fail with evidence, not nil")
    func failures() {
        guard case .failure(let scheme) = PoolConfig.parse(urlString: "mysql://nope") else {
            Issue.record("must reject"); return
        }
        #expect(scheme == .unsupportedScheme("mysql://nope"))
    }
}

@Suite("PoolPolicy — the Rules of the House")
struct PoolPolicyTests {

    @Test("Admission verdicts", arguments: [
        (idle: 2, total: 5, max: 8, expected: Admission.reuseIdle),
        (idle: 0, total: 5, max: 8, expected: Admission.summonNew),
        (idle: 0, total: 8, max: 8, expected: Admission.enqueueWaiter),
    ])
    func admission(case input: (idle: Int, total: Int, max: Int, expected: Admission)) {
        #expect(Admission.decide(idleCount: input.idle, totalConns: input.total, maxConns: input.max) == input.expected)
    }

    @Test("Pressure moods derive from shape + delta")
    func pressure() {
        let serene = PoolStats(applicationName: "a", nameSource: "pool", maxConns: 8, minConns: 2,
                               totalConns: 3, idleConns: 3, checkedOut: 0, waiters: 0,
                               acquireCount: 10, emptyAcquireCount: 0, highWater: 1)
        #expect(Pressure.verdict(for: serene, delta: .init(acquires: 4, emptyAcquires: 0)) == .serene)

        var gathering = serene
        gathering.waiters = 2
        #expect(Pressure.verdict(for: gathering, delta: .init(acquires: 9, emptyAcquires: 0)) == .gathering)

        gathering.totalConns = 8
        #expect(Pressure.verdict(for: gathering, delta: .init(acquires: 9, emptyAcquires: 3)) == .pinnedAtMax)
        #expect(Pressure.pinnedAtMax.sigil == "🌩️")
    }

    @Test("deltas are honest arithmetic")
    func deltas() {
        var early = PoolStats(applicationName: "a", nameSource: "pool", maxConns: 8, minConns: 2,
                              totalConns: 1, idleConns: 1, checkedOut: 0, waiters: 0,
                              acquireCount: 100, emptyAcquireCount: 5, highWater: 0)
        var late = early
        late.acquireCount = 127
        late.emptyAcquireCount = 6
        #expect(PoolStatsDelta.diff(early, late) == .init(acquires: 27, emptyAcquires: 1))
        early.maxConns = 0 // silence the unused-mutation god
    }
}

@Suite("RingBuffer — the enchanted circle")
struct RingBufferTests {

    @Test("wraps like a good ring should")
    func wraps() {
        var ring = RingBuffer<Int>(capacity: 3)
        (1...5).forEach { ring.append($0) }
        #expect(ring.recent == [3, 4, 5])
    }

    @Test("empty rings tell the truth")
    func empty() {
        let ring = RingBuffer<String>(capacity: 2)
        #expect(ring.isEmpty)
        #expect(ring.recent.isEmpty)
    }
}
