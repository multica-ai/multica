// swift-tools-version: 6.1
import PackageDescription

/// 🎭 The Orchestrion — MultiCa's Swift rebirth
///
/// "One language from HUD to database pool; the incident's lessons
///  woven in at birth, not bolted on after." ✨
let package = Package(
    name: "MulticaServer",
    platforms: [.macOS(.v14)],
    dependencies: [
        // Pinned to Andromeda's resolved version (Package.resolved parity, 2026-08-26)
        .package(url: "https://github.com/hummingbird-project/hummingbird", from: "2.25.1"),
        .package(url: "https://github.com/vapor/postgres-nio", from: "1.22.0"),
    ],
    targets: [
        // 🌟 The heart of the machine — all logic testable, no main() hostage-taking
        .target(
            name: "MulticaServerCore",
            dependencies: [
                .product(name: "Hummingbird", package: "hummingbird"),
                .product(name: "PostgresNIO", package: "postgres-nio"),
            ]
        ),
        // 🎬 The curtain-raiser — wiring only
        .executableTarget(
            name: "multica-server",
            dependencies: ["MulticaServerCore"]
        ),
        // 🧪 The quality-assurance ritual
        .testTarget(
            name: "MulticaServerCoreTests",
            dependencies: ["MulticaServerCore"]
        ),
    ]
)
