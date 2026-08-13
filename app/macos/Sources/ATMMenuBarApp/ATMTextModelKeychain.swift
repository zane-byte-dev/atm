import Foundation
import Security

enum ATMTextModelKeychainError: LocalizedError {
    case unexpectedData
    case operationFailed(OSStatus)

    var errorDescription: String? {
        switch self {
        case .unexpectedData:
            return "DeepSeek API Key 在钥匙串中的格式无法读取"
        case .operationFailed(let status):
            let detail = SecCopyErrorMessageString(status, nil) as String? ?? "OSStatus \(status)"
            return "钥匙串操作失败：\(detail)"
        }
    }
}

/// The App owns credential persistence; the Go CLI only ever sees the key in
/// its child-process environment. Endpoint and model are ordinary config, but
/// the credential never enters config.json, command arguments, or ATM logs.
enum ATMTextModelKeychain {
    static let service = "org.zane-byte-dev.atm.text-model"
    static let account = "deepseek-api-key"

    static func readAPIKey() throws -> String? {
        var item: CFTypeRef?
        let status = SecItemCopyMatching(
            baseQuery.merging([
                kSecReturnData as String: true,
                kSecMatchLimit as String: kSecMatchLimitOne,
            ]) { _, new in new } as CFDictionary,
            &item
        )
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess else { throw ATMTextModelKeychainError.operationFailed(status) }
        guard let data = item as? Data,
              let value = String(data: data, encoding: .utf8),
              !value.isEmpty else {
            throw ATMTextModelKeychainError.unexpectedData
        }
        return value
    }

    static func saveAPIKey(_ rawValue: String) throws {
        let value = rawValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else {
            try deleteAPIKey()
            return
        }
        let data = Data(value.utf8)
        let status = SecItemUpdate(
            baseQuery as CFDictionary,
            [kSecValueData as String: data] as CFDictionary
        )
        if status == errSecSuccess { return }
        guard status == errSecItemNotFound else {
            throw ATMTextModelKeychainError.operationFailed(status)
        }
        var item = baseQuery
        item[kSecValueData as String] = data
        item[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        let addStatus = SecItemAdd(item as CFDictionary, nil)
        guard addStatus == errSecSuccess else {
            throw ATMTextModelKeychainError.operationFailed(addStatus)
        }
    }

    static func deleteAPIKey() throws {
        let status = SecItemDelete(baseQuery as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw ATMTextModelKeychainError.operationFailed(status)
        }
    }

    private static var baseQuery: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }
}
