import Foundation

/// The app's side of `atm _ipc`.
///
/// Every other read this app performs is an ordinary CLI command, which means
/// each one is a separate unversioned promise: renaming a flag breaks one screen,
/// at runtime, with no warning anywhere. Everything reached through `_ipc` moves
/// together under a single protocol version instead, and a version this build has
/// not been written against is refused rather than decoded hopefully.
enum ATMIPCContract {
    /// Must match `contract.IPCProtocolVersion` in the Go module. The two are
    /// upgraded together; there is no negotiation, because there is exactly one
    /// CLI on the machine and it is the one this app shipped alongside.
    static let supportedProtocolVersion = 1
    static let supportedEnvelopeVersion = 1

    static func validate(envelopeVersion: Int, protocolVersion: Int) throws {
        // The business protocol is checked first on purpose. It is the useful
        // upgrade direction for a user, and it must win over any payload shape
        // change introduced by that newer protocol.
        guard protocolVersion == supportedProtocolVersion else {
            throw ATMIPCProtocolMismatch(
                cliVersion: protocolVersion,
                appVersion: supportedProtocolVersion
            )
        }
        guard envelopeVersion == supportedEnvelopeVersion else {
            throw ATMIPCEnvelopeVersionMismatch(
                cliVersion: envelopeVersion,
                appVersion: supportedEnvelopeVersion
            )
        }
    }
}

/// Shared upgrade instructions for a CLI/app version skew.
///
/// Extracted so the dashboard schema check and the IPC protocol check cannot end
/// up quoting different install commands: whichever one a user happens to hit
/// first is the one they will follow.
enum ATMVersionSkewAdvice {
    case appTooOld
    case cliTooOld

    static func direction(cliVersion: Int, appVersion: Int) -> ATMVersionSkewAdvice {
        cliVersion > appVersion ? .appTooOld : .cliTooOld
    }

    /// The app cannot update itself — it has no privilege to replace its own
    /// bundle — so both branches name the thing the user has to do, and neither
    /// leaves them guessing which of the two halves is behind.
    var text: String {
        switch self {
        case .appTooOld:
            return "下载新版 ATM.app 覆盖安装后重启 App；从源码构建则运行 "
                + "app/macos/Scripts/build-app.sh。CLI 与 App 必须配套升级。"
        case .cliTooOld:
            return "运行 curl -fsSL "
                + "https://raw.githubusercontent.com/zane-byte-dev/atm/main/install.sh | sh"
                + " 更新 CLI，然后点刷新。"
        }
    }
}

/// Raised when the CLI answers `_ipc` with a protocol version this build does not
/// know. Distinct from a decoding failure on purpose: nothing about the payload
/// is wrong, the two halves are just different ages, and the fix is an upgrade
/// rather than a retry.
struct ATMIPCProtocolMismatch: LocalizedError, Equatable {
    let cliVersion: Int
    let appVersion: Int

    var errorDescription: String? {
        switch ATMVersionSkewAdvice.direction(cliVersion: cliVersion, appVersion: appVersion) {
        case .appTooOld:
            return "ATM App 需要更新：CLI 使用 _ipc 协议 v\(cliVersion)，本 App 只支持 v\(appVersion)。"
        case .cliTooOld:
            return "atm CLI 需要更新：本 App 需要 _ipc 协议 v\(appVersion)，CLI 只提供 v\(cliVersion)。"
        }
    }

    var recoverySuggestion: String? {
        ATMVersionSkewAdvice.direction(cliVersion: cliVersion, appVersion: appVersion).text
    }

    /// One line for a surface that shows a single string.
    var summary: String {
        [errorDescription, recoverySuggestion].compactMap { $0 }.joined(separator: " ")
    }
}

/// The transport wrapper has its own version so adding request metadata does not
/// pretend the AI Day or settings schemas changed. In practice the App and CLI
/// ship together, so the recovery is the same paired upgrade as a protocol skew.
struct ATMIPCEnvelopeVersionMismatch: LocalizedError, Equatable {
    let cliVersion: Int
    let appVersion: Int

    var errorDescription: String? {
        "ATM App 与 atm CLI 的 _ipc envelope 版本不一致（CLI v\(cliVersion)，App v\(appVersion)）。"
    }

    var recoverySuggestion: String? {
        ATMVersionSkewAdvice.direction(cliVersion: cliVersion, appVersion: appVersion).text
    }
}

/// A valid response for a different method is never accepted hopefully. This is
/// most likely a CLI/App packaging error or an adapter bug, not bad user data.
struct ATMIPCVerbMismatch: LocalizedError, Equatable {
    let expected: String
    let actual: String
    let requestID: String

    var errorDescription: String? {
        "atm CLI 返回了错误的 _ipc verb：需要 \(expected)，实际为 \(actual)（请求 \(requestID)）。"
    }
}

/// JSON carried in an error's optional `details` field. It deliberately remains
/// untyped: stable control flow uses `code`; details are diagnostic context and
/// different application services are free to attach different fields.
indirect enum ATMJSONValue: Codable, Equatable, Sendable {
    case object([String: ATMJSONValue])
    case array([ATMJSONValue])
    case string(String)
    case number(Double)
    case bool(Bool)
    case null

    init(from decoder: Decoder) throws {
        let value = try decoder.singleValueContainer()
        if value.decodeNil() {
            self = .null
        } else if let decoded = try? value.decode(Bool.self) {
            self = .bool(decoded)
        } else if let decoded = try? value.decode(Double.self) {
            self = .number(decoded)
        } else if let decoded = try? value.decode(String.self) {
            self = .string(decoded)
        } else if let decoded = try? value.decode([String: ATMJSONValue].self) {
            self = .object(decoded)
        } else if let decoded = try? value.decode([ATMJSONValue].self) {
            self = .array(decoded)
        } else {
            throw DecodingError.dataCorruptedError(
                in: value,
                debugDescription: "unsupported JSON value in IPC error details"
            )
        }
    }

    func encode(to encoder: Encoder) throws {
        var value = encoder.singleValueContainer()
        switch self {
        case .object(let object): try value.encode(object)
        case .array(let array): try value.encode(array)
        case .string(let string): try value.encode(string)
        case .number(let number): try value.encode(number)
        case .bool(let bool): try value.encode(bool)
        case .null: try value.encodeNil()
        }
    }
}

private struct ATMIPCErrorPayload: Decodable {
    let code: String
    let message: String
    let details: ATMJSONValue?
    let retryable: Bool
}

/// A service failure decoded from stdout, including its request ID even when the
/// child process exits non-zero. The stable `code` is available to future UI
/// branches; the current screens present the human message and diagnostic ID.
struct ATMIPCRemoteError: LocalizedError, Equatable {
    let code: String
    let message: String
    let details: ATMJSONValue?
    let retryable: Bool
    let requestID: String
    let verb: String

    var errorDescription: String? {
        "\(message) [\(code), \(requestID)]"
    }
}

/// The wrapper every `_ipc` answer arrives in. Decoding it checks the version
/// before the payload, so a mismatch is reported as a mismatch instead of
/// surfacing as a missing field somewhere inside `data`.
struct ATMIPCEnvelope<Value: Decodable>: Decodable {
    let envelopeVersion: Int
    let protocolVersion: Int
    let requestID: String
    let verb: String
    let data: Value

    enum CodingKeys: String, CodingKey {
        case envelopeVersion = "envelope_version"
        case protocolVersion = "protocol_version"
        case requestID = "request_id"
        case verb
        case data
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        protocolVersion = try values.decode(Int.self, forKey: .protocolVersion)
        envelopeVersion = try values.decode(Int.self, forKey: .envelopeVersion)
        try ATMIPCContract.validate(
            envelopeVersion: envelopeVersion,
            protocolVersion: protocolVersion
        )
        requestID = try values.decode(String.self, forKey: .requestID)
        verb = try values.decode(String.self, forKey: .verb)
        data = try values.decode(Value.self, forKey: .data)
    }
}

/// The identity/error half is decoded before a success payload. That ordering is
/// what lets an application error survive Cobra's non-zero exit status, and what
/// prevents a schema error inside `data` from hiding a protocol mismatch.
private struct ATMIPCEnvelopeProbe: Decodable {
    let envelopeVersion: Int
    let protocolVersion: Int
    let requestID: String
    let verb: String
    let error: ATMIPCErrorPayload?

    enum CodingKeys: String, CodingKey {
        case envelopeVersion = "envelope_version"
        case protocolVersion = "protocol_version"
        case requestID = "request_id"
        case verb
        case error
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        protocolVersion = try values.decode(Int.self, forKey: .protocolVersion)
        envelopeVersion = try values.decode(Int.self, forKey: .envelopeVersion)
        try ATMIPCContract.validate(
            envelopeVersion: envelopeVersion,
            protocolVersion: protocolVersion
        )
        requestID = try values.decode(String.self, forKey: .requestID)
        verb = try values.decode(String.self, forKey: .verb)
        error = try values.decodeIfPresent(ATMIPCErrorPayload.self, forKey: .error)
    }
}

/// Phantom request for methods whose Go handler intentionally never reads stdin.
struct ATMIPCNoRequest: Encodable {}

enum ATMIPCResponseKeyDecoding: Equatable {
    case useDefault
    case convertFromSnakeCase
}

/// One typed method in the App/CLI protocol. Timeout belongs to the method rather
/// than the argv policy: after moving `day dashboard` to `_ipc day.snapshot`, the
/// first argv token no longer says that rebuilding the dashboard may take 60s.
struct ATMIPCMethod<Request: Encodable, Response: Decodable> {
    let verb: String
    let timeout: TimeInterval
    let responseKeyDecoding: ATMIPCResponseKeyDecoding

    init(
        _ verb: String,
        timeout: TimeInterval = 15,
        responseKeyDecoding: ATMIPCResponseKeyDecoding = .convertFromSnakeCase
    ) {
        self.verb = verb
        self.timeout = timeout
        self.responseKeyDecoding = responseKeyDecoding
    }

    var arguments: [String] {
        // Array(arrayLiteral:) avoids presenting a dynamic `_ipc` prefix as a
        // literal call site to the Go-side contract scanner. The typed method
        // declarations below are the countable source of protocol verbs.
        Array(arrayLiteral: "_ipc", verb)
    }
}

/// The only App entry point for typed `_ipc` calls.
struct ATMIPCClient: Sendable {
    let runner: ATMCommandRunner

    init(runner: ATMCommandRunner) {
        self.runner = runner
    }

    init() throws {
        runner = try ATMCommandRunner()
    }

    func call<Response>(
        _ method: ATMIPCMethod<ATMIPCNoRequest, Response>
    ) async throws -> Response {
        let payload = try await validatedPayload(method, standardInput: nil)
        return try decode(payload, for: method)
    }

    func call<Request, Response>(
        _ method: ATMIPCMethod<Request, Response>,
        request: Request
    ) async throws -> Response {
        let encoder = JSONEncoder()
        let payload = try await validatedPayload(
            method,
            standardInput: encoder.encode(request)
        )
        return try decode(payload, for: method)
    }

    /// Returns the verb's `data` object as user-facing JSON rather than the IPC
    /// envelope. Used by AI Day export so unknown fields survive an older App.
    func callRawPayload(
        _ method: ATMIPCMethod<ATMIPCNoRequest, ATMJSONValue>
    ) async throws -> Data {
        let payload = try await validatedPayload(method, standardInput: nil)
        let object = try JSONSerialization.jsonObject(with: payload, options: [.fragmentsAllowed])
        var encoded = try JSONSerialization.data(
            withJSONObject: object,
            options: [.prettyPrinted, .withoutEscapingSlashes, .fragmentsAllowed]
        )
        encoded.append(0x0a)
        return encoded
    }

    private func validatedPayload<Request, Response>(
        _ method: ATMIPCMethod<Request, Response>,
        standardInput: Data?
    ) async throws -> Data {
        let result = try await runner.runRaw(
            method.arguments,
            standardInput: standardInput,
            timeout: method.timeout
        )
        let decoder = JSONDecoder()

        let probe: ATMIPCEnvelopeProbe
        do {
            probe = try decoder.decode(ATMIPCEnvelopeProbe.self, from: result.standardOutput)
        } catch let mismatch as ATMIPCProtocolMismatch {
            throw mismatch
        } catch let mismatch as ATMIPCEnvelopeVersionMismatch {
            throw mismatch
        } catch {
            // A legacy/non-IPC failure has no usable stdout envelope. Preserve
            // the established command error (and stderr) in that fallback case.
            if result.terminationStatus != 0 {
                throw result.commandError(arguments: method.arguments)
            }
            throw error
        }

        guard probe.verb == method.verb else {
            throw ATMIPCVerbMismatch(
                expected: method.verb,
                actual: probe.verb,
                requestID: probe.requestID
            )
        }
        if let error = probe.error {
            throw ATMIPCRemoteError(
                code: error.code,
                message: error.message,
                details: error.details,
                retryable: error.retryable,
                requestID: probe.requestID,
                verb: probe.verb
            )
        }
        guard result.terminationStatus == 0 else {
            throw result.commandError(arguments: method.arguments)
        }

        let object = try JSONSerialization.jsonObject(with: result.standardOutput)
        guard let envelope = object as? [String: Any],
              let payload = envelope["data"],
              !(payload is NSNull) else {
            throw DecodingError.dataCorrupted(
                DecodingError.Context(
                    codingPath: [],
                    debugDescription: "successful IPC response has no data payload"
                )
            )
        }
        return try JSONSerialization.data(withJSONObject: payload, options: [.fragmentsAllowed])
    }

    private func decode<Request, Response>(
        _ payload: Data,
        for method: ATMIPCMethod<Request, Response>
    ) throws -> Response {
        let decoder = JSONDecoder()
        if method.responseKeyDecoding == .convertFromSnakeCase {
            decoder.keyDecodingStrategy = .convertFromSnakeCase
        }
        return try decoder.decode(Response.self, from: payload)
    }
}

/// Payload of `_ipc config.settings`: every effective setting the settings screen
/// shows, in one answer.
///
/// This replaced eight separate `atm config get <key>` reads. The point was never
/// the eight process spawns — they cost about 19ms each — but the eight argv
/// arrays, each of which was its own chance to drift from the CLI silently.
struct ATMSettingsSnapshot: Decodable {
    let ownerName: String
    let grokLiveQuota: Bool
    let collectionEnabled: Bool
    let collectionIntervalMinutes: Int
    let collectionLookbackMinutes: Int
    let collectionMessageRetentionDays: Int
    let textModelBaseURL: String
    let textModelName: String
    let textModelSource: String
    let todoRefinePrompt: String
    let todoRefineOnAdd: Bool
    /// Whether a key is on disk, never the key itself: the CLI does not serialise
    /// it, so this is the only thing the app can know.
    let textModelAPIKeyConfigured: Bool

    enum CodingKeys: String, CodingKey {
        case ownerName = "owner_name"
        case grokLiveQuota = "grok_live_quota"
        case collectionEnabled = "collection_enabled"
        case collectionIntervalMinutes = "collection_interval_minutes"
        case collectionLookbackMinutes = "collection_lookback_minutes"
        case collectionMessageRetentionDays = "collection_message_retention_days"
        case textModelBaseURL = "text_model_base_url"
        case textModelName = "text_model_name"
        case textModelSource = "text_model_source"
        case todoRefinePrompt = "todo_refine_prompt"
        case todoRefineOnAdd = "todo_refine_on_add"
        case textModelAPIKeyConfigured = "text_model_api_key_configured"
    }
}

/// Every `_ipc` call the app makes. Kept in one place so the protocol's surface is
/// countable, and so app/macos/atm-cli-contract.txt has something to be checked
/// against.
enum ATMIPCCommand {
    static let settings = ATMIPCMethod<ATMIPCNoRequest, ATMSettingsSnapshot>(
        "config.settings",
        responseKeyDecoding: .useDefault
    )
    static let saveSettings = ATMIPCMethod<ATMSettingsSave, ATMSettingsSnapshot>(
        "config.save",
        responseKeyDecoding: .useDefault
    )
    static let saveCredential = ATMIPCMethod<ATMCredentialSave, ATMCredentialStatus>(
        "config.credential.save"
    )
    static let deleteCredential = ATMIPCMethod<ATMIPCNoRequest, ATMCredentialStatus>(
        "config.credential.delete"
    )
    static let checkTextModel = ATMIPCMethod<ATMTextModelCheck, ATMTextModelCheckResult>(
        "config.text_model.check",
        timeout: 45,
        responseKeyDecoding: .useDefault
    )
    /// `.useDefault` because ATMDoctorReport and ATMQuotaSnapshot already spell
    /// their snake_case keys out in CodingKeys. Converting on top of that would
    /// look for `indexedSessions` and find nothing.
    static let doctor = ATMIPCMethod<ATMIPCNoRequest, ATMDoctorReport>(
        "doctor.check",
        responseKeyDecoding: .useDefault
    )
    static let quota = ATMIPCMethod<ATMQuotaRequest, ATMQuotaSnapshot>(
        "quota.snapshot",
        responseKeyDecoding: .useDefault
    )
}

/// Narrows `_ipc quota.snapshot`. An empty agent asks for every source ATM can
/// read, which is what the dashboard wants. Whether to make the opt-in billing
/// call is the CLI's configuration to read, not a parameter this side sends.
struct ATMQuotaRequest: Encodable {
    let agent: String?
}

/// A write-only credential request. Responses contain only `configured`; the
/// key itself never comes back across the protocol boundary.
struct ATMCredentialSave: Encodable {
    let apiKey: String

    enum CodingKeys: String, CodingKey {
        case apiKey = "api_key"
    }
}

struct ATMCredentialStatus: Decodable, Equatable {
    let configured: Bool
}

/// Unsaved model-form values for a connection check. A nil key asks the CLI to
/// use the saved credential; a draft key is consumed for this call only.
struct ATMTextModelCheck: Encodable {
    let apiKey: String?
    let baseURL: String
    let model: String

    enum CodingKeys: String, CodingKey {
        case apiKey = "api_key"
        case baseURL = "base_url"
        case model
    }
}

struct ATMTextModelCheckResult: Decodable, Equatable {
    let ok: Bool
    let latencyMS: Int

    enum CodingKeys: String, CodingKey {
        case ok
        case latencyMS = "latency_ms"
    }
}

/// Parameters for `_ipc config.save`, sent as JSON on stdin.
///
/// Every field is optional so "not sent" and "sent as empty" stay different
/// questions: an empty todoRefinePrompt restores the CLI's default, while an
/// absent one means this screen did not touch it. The CLI rejects a key it does
/// not know rather than ignoring it, so a typo here fails loudly.
///
/// The API key is deliberately absent. It goes through the typed credential IPC
/// method, which writes credentials.json at 0600 and keeps the secret out of
/// config, backups, diagnostics, argv and logs.
struct ATMSettingsSave: Encodable {
    var ownerName: String?
    var grokLiveQuota: Bool?
    var collectionEnabled: Bool?
    var collectionIntervalMinutes: Int?
    var collectionLookbackMinutes: Int?
    var collectionMessageRetentionDays: Int?
    var textModelBaseURL: String?
    var textModelName: String?
    var textModelSource: String?
    var todoRefinePrompt: String?
    var todoRefineOnAdd: Bool?

    enum CodingKeys: String, CodingKey {
        case ownerName = "owner_name"
        case grokLiveQuota = "grok_live_quota"
        case collectionEnabled = "collection_enabled"
        case collectionIntervalMinutes = "collection_interval_minutes"
        case collectionLookbackMinutes = "collection_lookback_minutes"
        case collectionMessageRetentionDays = "collection_message_retention_days"
        case textModelBaseURL = "text_model_base_url"
        case textModelName = "text_model_name"
        case textModelSource = "text_model_source"
        case todoRefinePrompt = "todo_refine_prompt"
        case todoRefineOnAdd = "todo_refine_on_add"
    }

    func encoded() throws -> Data {
        let encoder = JSONEncoder()
        // Absent stays absent: the CLI treats a present null as a value it cannot
        // parse, and the whole point of the optionals is to say nothing about a
        // setting this form did not show.
        encoder.outputFormatting = []
        return try encoder.encode(self)
    }
}
