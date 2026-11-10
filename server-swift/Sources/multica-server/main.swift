import Foundation
import Hummingbird
import Logging
import MulticaServerCore
import NIOCore

// 🎭 The Curtain-Raiser — the composition root, and nothing more.
//
// Every dependency is constructed here, in the open, in order — no service
// locator, no container, no registry. If you want to know what the show
// needs, read this file top to bottom. ✨

// 📜 Act 0 — the scroll is read
let env = Environment()
let logger = Logger(label: "orchestrion")

guard case .success(let config) = PoolConfig.parse(
    urlString: env.get("DATABASE_URL") ?? "",
    env: { env.get($0) }
) else {
    logger.critical("💥 😭 THE SCRIPT IS MISSING — set DATABASE_URL (see PLAN.md / .env)")
    exit(1)
}

// 🎻 Act 1 — the orchestra assembles (constructor injection, in the open)
let summoner = PostgresSoulSummoner(config: config, logger: logger)
let pool = PoolOfSouls(
    summoner: summoner,
    tides: .init(maxConns: config.maxConns, minConns: config.minConns),
    logger: logger
)
await pool.warmUp()

let oracle = Oracle(pool: pool, logger: logger)
let observer = TelemetryObserver(capacity: 120, logger: logger)
let interval = Duration.seconds(Double(env.get("TELEMETRY_INTERVAL_SECONDS").flatMap(Double.init) ?? 15))
await observer.startSampler(pool: pool, oracle: oracle, interval: interval)

// 🎬 Act 2 — the stage is lit
let router = buildRouter(oracle: oracle, observer: observer, logger: logger)
let port = env.get("PORT").flatMap(Int.init) ?? 3640
let app = Application(
    router: router,
    configuration: .init(address: .hostname("127.0.0.1", port: port), serverName: "orchestrion")
)

logger.info("🎪 ✨ THE CURTAIN RISES — orchestrion on 127.0.0.1:\(port) as '\(config.applicationName.value)' (tides \(config.minConns)…\(config.maxConns), telemetry every \(interval.secondsAsDouble)s)")
try await app.runService()
