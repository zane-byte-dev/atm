// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "ATMCompanion",
    platforms: [.macOS("13.4")],
    products: [.executable(name: "ATMCompanion", targets: ["ATMCompanion"])],
    targets: [
        .executableTarget(name: "ATMCompanion", resources: [.process("Resources")]),
        .testTarget(name: "ATMCompanionTests", dependencies: ["ATMCompanion"]),
    ]
)
