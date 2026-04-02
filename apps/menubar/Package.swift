// swift-tools-version: 5.8
import PackageDescription

let package = Package(
    name: "MulticaMenuBar",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "MulticaMenuBar",
            path: "Sources/MulticaMenuBar"
        )
    ]
)
