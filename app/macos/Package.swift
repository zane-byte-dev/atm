// swift-tools-version: 5.9

import Foundation
import PackageDescription

let packageRoot = URL(fileURLWithPath: #filePath).deletingLastPathComponent().path
let debugInfoPlist = "\(packageRoot)/Resources/DebugInfo.plist"

let package = Package(
    name: "ATMMenuBarApp",
    platforms: [
        // 13.4 rather than 13.0 because sherpa-onnx-spm's prebuilt onnxruntime
        // slices were compiled against 13.4: linking them into a 13.0 target is
        // legal but emits ~690 "built for newer macOS version" warnings on every
        // link, which would bury every warning worth reading. 13.4 shipped in May
        // 2023, so the floor moves by weeks, not versions.
        .macOS("13.4"),
    ],
    products: [
        .executable(name: "ATMMenuBarApp", targets: ["ATMMenuBarApp"]),
    ],
    dependencies: [
        // Speech recognition for 语音输入. Pinned exactly rather than to a range:
        // the package's targets are prebuilt xcframeworks fetched by checksum, so a
        // version bump is a new binary and deserves to be a deliberate change.
        //
        // The xcframeworks hold static archives (upstream's combine-libs.sh merges
        // onnxruntime into libsherpa-onnx.a), so they link into the executable and
        // Scripts/build-app.sh needs no Contents/Frameworks step.
        .package(url: "https://github.com/willwade/sherpa-onnx-spm.git", exact: "1.13.16"),
    ],
    targets: [
        .executableTarget(
            name: "ATMMenuBarApp",
            dependencies: [
                .product(name: "SherpaOnnx", package: "sherpa-onnx-spm"),
            ],
            resources: [
                .process("Resources"),
            ],
            linkerSettings: [
                .unsafeFlags(
                    [
                        "-Xlinker", "-sectcreate",
                        "-Xlinker", "__TEXT",
                        "-Xlinker", "__info_plist",
                        "-Xlinker", debugInfoPlist,
                    ],
                    .when(configuration: .debug)
                ),
            ]
        ),
        .testTarget(
            name: "ATMMenuBarAppTests",
            dependencies: ["ATMMenuBarApp"]
        ),
    ]
)
