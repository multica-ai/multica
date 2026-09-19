import Logging
import PostgresNIO

// 🎭 The Oracle's Consulate — where PostgresNIO meets the pool's seam.
//
// Two roles live here and nowhere else: the Summoner (which stamps each
// soul's name into its startup robes — the incident's whole moral) and the
// Oracle (which asks questions of the database through the pool). ✨

/// 🔮 A housed Postgres connection.
///
/// `@unchecked Sendable` invariant (stated, per canon): `PostgresConnection`
/// is a channel-backed class; NIO guarantees channel operations are
/// thread-safe, and custody is confined to the pool actor's checkout
/// lifecycle. This is Exhibit-1-legal wrapping of legacy reference state,
/// not a convenience silence.
public struct PostgresSoul: PooledConnection {
    public let connection: PostgresConnection
    public let soulID: Int

    public var isClosed: Bool { connection.isClosed }

    public func dismiss() async {
        try? await connection.close()
    }
}

/// 📯 The Postgres summoner — performs the naming ceremony on every summons.
public struct PostgresSoulSummoner: ConnectionSummoner {
    private let host: String
    private let port: Int
    private let username: String
    private let password: String?
    private let database: String
    public let applicationName: String
    private let logger: Logger

    public init(config: PoolConfig, logger: Logger) {
        self.host = config.host
        self.port = config.port
        self.username = config.username
        self.password = config.password
        self.database = config.database
        self.applicationName = config.applicationName.value
        self.logger = logger
    }

    /// 🌟 The naming ceremony — `application_name` stitched into the startup
    /// robes. Two lines. The incident's entire lesson, at the source.
    public func summon(id: Int) async throws -> PostgresSoul {
        var configuration = PostgresConnection.Configuration(
            host: host,
            port: port,
            username: username,
            password: password,
            database: database,
            tls: .disable
        )
        configuration.options.additionalStartupParameters = [
            ("application_name", applicationName)
        ]
        let connection = try await PostgresConnection.connect(
            configuration: configuration,
            id: id,
            logger: logger
        )
        return PostgresSoul(connection: connection, soulID: id)
    }
}

/// 🏛️ The Oracle — asks questions of the database, always through the pool.
public struct Oracle: Sendable {
    public typealias SoulPool = PoolOfSouls<PostgresSoulSummoner>

    private let pool: SoulPool
    private let logger: Logger

    public init(pool: SoulPool, logger: Logger) {
        self.pool = pool
        self.logger = logger
    }

    public var poolHandle: SoulPool { pool }

    /// 🧪 The Health Incantation — one round-trip, honestly measured.
    public func ping() async throws -> Duration {
        let clock = ContinuousClock()
        let started = clock.now
        _ = try await pool.withConnection { soul in
            try await soul.connection.query("SELECT 1", logger: self.logger)
        }
        return clock.now - started
    }

    /// 📖 The census — ask the oracle's ledger who is connected, and under
    /// what names. (This is the query that would have named the incident in
    /// ten seconds flat.)
    public struct CensusEntry: Sendable, Codable, Equatable {
        public let applicationName: String
        public let souls: Int
    }

    public func census() async throws -> [CensusEntry] {
        try await pool.withConnection { soul in
            let rows = try await soul.connection.query(
                """
                SELECT COALESCE(NULLIF(application_name, ''), '(unnamed)') AS app,
                       count(*)::int4 AS souls
                FROM pg_stat_activity
                GROUP BY 1
                ORDER BY 2 DESC, 1
                """,
                logger: self.logger
            )
            var entries: [CensusEntry] = []
            for try await row in rows {
                let cells = Array(row)
                entries.append(CensusEntry(
                    applicationName: try cells[0].decode(String.self),
                    souls: Int(try cells[1].decode(Int32.self))
                ))
            }
            return entries
        }
    }
}
