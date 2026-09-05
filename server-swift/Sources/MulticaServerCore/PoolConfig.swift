import Foundation

// 🎭 The Casting Department — where a raw DATABASE_URL scripture becomes a
// signed cosmic contract, and no soul leaves the registry nameless.
//
// Functional core: pure parse, injected env, Result-typed evidence.
// The actor and the wire never touch this file's logic — it is all
// table-tested truth. ✨

/// 🏷️ The provenance of our name — a rich enum, because *where* a name came
/// from is finite, meaningful, and deserves compiler-checked derivations
/// (never `rawValue` math — see canon anti-patterns, Exhibit 6).
public enum ApplicationName: Equatable, Sendable {
    /// The operator's hand-written sigil in the URL query.
    case url(String)
    /// The `MULTICA_APP_NAME` env scroll.
    case env(String)
    /// The blessed fallback — "never again a nameless soul" (incident 2026-08-26).
    case `default`

    /// The name the world will know us by in `pg_stat_activity`.
    public var value: String {
        switch self {
        case .url(let name): name
        case .env(let name): name
        case .default: "multica-swift-server"
        }
    }

    /// How the telemetry gallery credits this name. Exhaustive switch — a new
    /// case forces a decision here, loudly.
    public var sourceLabel: String {
        switch self {
        case .url: "url"
        case .env: "env"
        case .default: "default"
        }
    }
}

/// 💥 The finite ways a scripture can refuse to become a contract.
/// (Associated values carry the evidence — no evidence-destroying `nil`s.)
public enum DatabaseURLError: Error, Equatable, Sendable {
    case notAURL(String)
    case unsupportedScheme(String)
    case missingHost(String)
}

/// 📜 The Cosmic Contract.
public struct PoolConfig: Sendable, Equatable {
    public var host: String
    public var port: Int
    public var username: String
    public var password: String?
    public var database: String
    public var applicationName: ApplicationName
    public var maxConns: Int
    public var minConns: Int

    /// 🌟 The Great Distillation — a *pure* transform from URL + env scroll
    /// to contract. Total function: evidence in the error, never a guess.
    ///
    /// Name precedence: URL sigil > env scroll > blessed default.
    /// Tide defaults are host-sized (8/2), not the Go prod-sized 25-conn wall.
    public static func parse(
        urlString: String,
        env: @Sendable (String) -> String? = { _ in nil }
    ) -> Result<PoolConfig, DatabaseURLError> {
        guard urlString.hasPrefix("postgres://") || urlString.hasPrefix("postgresql://") else {
            return .failure(.unsupportedScheme(urlString))
        }
        guard let parts = URLComponents(string: urlString), let rawHost = parts.host, !rawHost.isEmpty else {
            return .failure(.notAURL(urlString))
        }

        let urlName = parts.queryItems?
            .first { $0.name == "application_name" }?
            .value
            .flatMap { $0.isEmpty ? nil : $0 }

        let name: ApplicationName = switch (urlName, env("MULTICA_APP_NAME")) {
        case (let sigil?, _): .url(sigil)
        case (nil, let scroll?): .env(scroll)
        case (nil, nil): .default
        }

        let db = String(parts.path.dropFirst())
        let config = PoolConfig(
            host: rawHost,
            port: parts.port ?? 5432,
            username: parts.user ?? "postgres",
            password: parts.password,
            database: db.isEmpty ? "postgres" : db,
            applicationName: name,
            maxConns: tide("DATABASE_MAX_CONNS", env: env, default: 8),
            minConns: tide("DATABASE_MIN_CONNS", env: env, default: 2)
        )
        return .success(config.normalized())
    }

    private init(
        host: String, port: Int, username: String, password: String?,
        database: String, applicationName: ApplicationName,
        maxConns: Int, minConns: Int
    ) {
        self.host = host
        self.port = port
        self.username = username
        self.password = password
        self.database = database
        self.applicationName = applicationName
        self.maxConns = maxConns
        self.minConns = minConns
    }

    /// 🌊 The tides, read from the scroll — floor at 1, swallow bad runes.
    private static func tide(
        _ name: String,
        env: @Sendable (String) -> String?,
        default def: Int
    ) -> Int {
        max(1, env(name).flatMap(Int.init) ?? def)
    }

    /// ⚖️ The impossible is unrepresentable: min can never exceed max.
    public func normalized() -> PoolConfig {
        var copy = self
        copy.minConns = min(copy.minConns, copy.maxConns)
        return copy
    }
}
