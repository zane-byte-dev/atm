import CryptoKit
import Foundation

enum LegacyVoiceImport {
    static let versionKey = "VoiceLegacyImportVersion"
    static let domains = ["dev.zanebyte.atm.menubar", "dev.zanebyte.atm.menubar.dev"]
    static let preferenceKeys = [
        "ATMVoiceInputHotKeyEnabled", "ATMVoiceInputHotKey", "ATMVoiceInputEngine",
        "ATMVoiceInputLanguage", "ATMVoiceInputOnDeviceOnly", "ATMVoiceInputRemoveTrailingPeriod",
        "ATMVoiceInputDictionary",
    ]

    enum ImportError: LocalizedError {
        case incomplete, changed, invalidDestination
        var errorDescription: String? {
            switch self {
            case .incomplete: return "旧模型不完整，请在独立语音 App 中重新下载。旧数据未改动。"
            case .changed: return "复制后模型校验失败，请重试。旧数据未改动。"
            case .invalidDestination: return "目标模型目录已有未完成文件，请先在模型设置中移除后重试。"
            }
        }
    }

    static var legacyModelDirectory: URL {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("ATM/VoiceModels/SenseVoiceSmall-int8-2024-07-17", isDirectory: true)
    }

    /// Read only when the user explicitly clicks Import. No transcript/history keys are accepted.
    static func preferences(in domain: String, defaults: UserDefaults = .standard) -> [String: Any] {
        guard domains.contains(domain) else { return [:] }
        return validated(defaults.persistentDomain(forName: domain) ?? [:])
    }

    static func validated(_ values: [String: Any]) -> [String: Any] {
        var result: [String: Any] = [:]
        for key in preferenceKeys {
            guard let value = values[key] else { continue }
            switch key {
            case "ATMVoiceInputHotKeyEnabled", "ATMVoiceInputOnDeviceOnly", "ATMVoiceInputRemoveTrailingPeriod":
                if let number = value as? NSNumber, CFGetTypeID(number) == CFBooleanGetTypeID() { result[key] = number }
            case "ATMVoiceInputHotKey":
                if let text = value as? String, let hotKey = ATMHotKey(storageValue: text), hotKey.isValid { result[key] = text }
            case "ATMVoiceInputEngine":
                if let text = value as? String, ATMVoiceRecognitionEngine(rawValue: text) != nil { result[key] = text }
            case "ATMVoiceInputLanguage":
                if let text = value as? String, ATMVoiceInputLanguage(rawValue: text) != nil { result[key] = text }
            case "ATMVoiceInputDictionary":
                if let text = value as? String, text.utf8.count <= 128 * 1024 { result[key] = text }
            default: break
            }
        }
        return result
    }

    /// Copy to the destination volume, verify each byte by SHA256, then publish by rename.
    /// Source and destination never share a mutable model; a failure leaves the old app intact.
    static func copyModel(from source: URL, to destination: URL,
                          validate: (ATMSenseVoiceModelFiles) -> Bool = ATMSenseVoiceModelManager.isComplete) throws -> Bool {
        let fm = FileManager.default
        guard fm.fileExists(atPath: source.path) else { return false }
        let names = ["model.int8.onnx", "tokens.txt"]
        for name in names {
            let values = try source.appendingPathComponent(name).resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
            guard values.isRegularFile == true, values.isSymbolicLink != true else { throw ImportError.incomplete }
        }
        let sourceFiles = ATMSenseVoiceModelFiles(model: source.appendingPathComponent(names[0]), tokens: source.appendingPathComponent(names[1]))
        guard validate(sourceFiles) else { throw ImportError.incomplete }
        if fm.fileExists(atPath: destination.path) {
            let existing = ATMSenseVoiceModelFiles(model: destination.appendingPathComponent(names[0]), tokens: destination.appendingPathComponent(names[1]))
            guard validate(existing) else { throw ImportError.invalidDestination }
            return false
        }
        let parent = destination.deletingLastPathComponent()
        try fm.createDirectory(at: parent, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let staging = parent.appendingPathComponent(".import-\(UUID().uuidString)", isDirectory: true)
        try fm.createDirectory(at: staging, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        defer { try? fm.removeItem(at: staging) }
        for name in names {
            let original = source.appendingPathComponent(name)
            let copied = staging.appendingPathComponent(name)
            try fm.copyItem(at: original, to: copied)
            guard try digest(original) == digest(copied) else { throw ImportError.changed }
        }
        let copiedFiles = ATMSenseVoiceModelFiles(model: staging.appendingPathComponent(names[0]), tokens: staging.appendingPathComponent(names[1]))
        guard validate(copiedFiles) else { throw ImportError.incomplete }
        try fm.moveItem(at: staging, to: destination)
        return true
    }

    private static func digest(_ url: URL) throws -> SHA256.Digest {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        var hash = SHA256()
        while let data = try handle.read(upToCount: 4 * 1024 * 1024), !data.isEmpty { hash.update(data: data) }
        return hash.finalize()
    }
}
