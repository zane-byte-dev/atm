// swift-tools-version: 5.9

import Foundation
import PackageDescription

let packageRoot = URL(fileURLWithPath: #filePath).deletingLastPathComponent().path
let debugInfoPlist = "\(packageRoot)/Resources/DebugInfo.plist"

let package = Package(
    name: "ATMMenuBarApp",
    platforms: [
        .macOS(.v13),
    ],
    products: [
        .executable(name: "ATMMenuBarApp", targets: ["ATMMenuBarApp"]),
    ],
    targets: [
        .executableTarget(
            name: "ATMMenuBarApp",
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
