// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "VoxCaret",
    platforms: [.macOS("13.4")],
    products: [.executable(name: "VoxCaret", targets: ["VoxCaret"])],
    dependencies: [.package(url: "https://github.com/willwade/sherpa-onnx-spm.git", exact: "1.13.16")],
    targets: [
        .executableTarget(name: "VoxCaret", dependencies: [.product(name: "SherpaOnnx", package: "sherpa-onnx-spm")]),
        .testTarget(name: "VoxCaretTests", dependencies: ["VoxCaret"]),
    ]
)
