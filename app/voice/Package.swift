// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "ATMVoice",
    platforms: [.macOS("13.4")],
    products: [.executable(name: "ATMVoice", targets: ["ATMVoice"])],
    dependencies: [.package(url: "https://github.com/willwade/sherpa-onnx-spm.git", exact: "1.13.16")],
    targets: [
        .executableTarget(name: "ATMVoice", dependencies: [.product(name: "SherpaOnnx", package: "sherpa-onnx-spm")]),
        .testTarget(name: "ATMVoiceTests", dependencies: ["ATMVoice"]),
    ]
)
