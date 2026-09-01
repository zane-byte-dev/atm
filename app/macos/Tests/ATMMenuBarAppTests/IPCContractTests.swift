import XCTest
@testable import ATMMenuBarApp

/// The `_ipc` protocol's value is that a version skew is refused instead of
/// decoded hopefully, so these tests are mostly about the refusal. A payload that
/// decodes is the easy half; the half worth pinning is what happens when the CLI
/// on the machine is not the one this build was written against.
final class IPCContractTests: XCTestCase {
    private func envelope(protocolVersion: Int) -> Data {
        Data("""
        {"envelope_version":1,"protocol_version":\(protocolVersion),"request_id":"ipc-settings-1","verb":"config.settings","data":{
          "owner_name":"墨水","grok_live_quota":true,"collection_enabled":true,
          "collection_interval_minutes":5,"collection_lookback_minutes":60,
          "collection_message_retention_days":90,
          "text_model_base_url":"https://api.deepseek.com",
          "text_model_name":"deepseek-v4-flash","text_model_source":"deepseek",
          "todo_refine_prompt":"保守拆分","todo_refine_on_add":false,
          "text_model_api_key_configured":true}}
        """.utf8)
    }

    private func shellQuoted(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
    }

    private func runner(
        stdout: String,
        stderr: String = "",
        status: Int32 = 0
    ) throws -> (ATMCommandRunner, URL) {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-ipc-client-\(UUID().uuidString)", isDirectory: true)
        let script = directory.appendingPathComponent("atm")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let contents = """
        #!/bin/sh
        /usr/bin/printf '%s' \(shellQuoted(stdout))
        /usr/bin/printf '%s' \(shellQuoted(stderr)) >&2
        exit \(status)
        """
        try contents.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
        return (try ATMCommandRunner(environment: ["ATM_EXECUTABLE": script.path]), directory)
    }

    func testSettingsEnvelopeDecodesEveryFieldTheScreenShows() throws {
        let decoded = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMSettingsSnapshot>.self,
            from: envelope(protocolVersion: ATMIPCContract.supportedProtocolVersion)
        )
        XCTAssertEqual(decoded.verb, "config.settings")
        let settings = decoded.data
        XCTAssertEqual(settings.ownerName, "墨水")
        XCTAssertTrue(settings.grokLiveQuota)
        XCTAssertTrue(settings.collectionEnabled)
        XCTAssertEqual(settings.collectionIntervalMinutes, 5)
        XCTAssertEqual(settings.collectionLookbackMinutes, 60)
        XCTAssertEqual(settings.collectionMessageRetentionDays, 90)
        XCTAssertEqual(settings.textModelBaseURL, "https://api.deepseek.com")
        XCTAssertEqual(settings.textModelName, "deepseek-v4-flash")
        XCTAssertEqual(settings.textModelSource, "deepseek")
        XCTAssertEqual(settings.todoRefinePrompt, "保守拆分")
        XCTAssertFalse(settings.todoRefineOnAdd)
        XCTAssertTrue(settings.textModelAPIKeyConfigured)
    }

    func testANewerProtocolIsRefusedRatherThanPartiallyDecoded() {
        let newer = ATMIPCContract.supportedProtocolVersion + 1
        do {
            _ = try JSONDecoder().decode(
                ATMIPCEnvelope<ATMSettingsSnapshot>.self,
                from: envelope(protocolVersion: newer)
            )
            XCTFail("a newer protocol version should not decode")
        } catch let mismatch as ATMIPCProtocolMismatch {
            XCTAssertEqual(mismatch.cliVersion, newer)
            XCTAssertEqual(mismatch.appVersion, ATMIPCContract.supportedProtocolVersion)
            // The app cannot replace its own bundle, so the message has to send the
            // user at the App and not at the CLI.
            XCTAssertTrue(mismatch.summary.contains("ATM App 需要更新"), mismatch.summary)
        } catch {
            XCTFail("expected ATMIPCProtocolMismatch, got \(error)")
        }
    }

    func testAnOlderProtocolPointsAtTheCLIInstead() {
        let older = ATMIPCContract.supportedProtocolVersion - 1
        do {
            _ = try JSONDecoder().decode(
                ATMIPCEnvelope<ATMSettingsSnapshot>.self,
                from: envelope(protocolVersion: older)
            )
            XCTFail("an older protocol version should not decode")
        } catch let mismatch as ATMIPCProtocolMismatch {
            XCTAssertTrue(mismatch.summary.contains("atm CLI 需要更新"), mismatch.summary)
            XCTAssertTrue(mismatch.summary.contains("install.sh"), mismatch.summary)
        } catch {
            XCTFail("expected ATMIPCProtocolMismatch, got \(error)")
        }
    }

    /// The version is read before the payload on purpose. If it were not, a
    /// reshaped `data` would surface as a missing-field decoding error and the user
    /// would be told their data is broken rather than that their halves disagree.
    func testTheVersionIsCheckedBeforeThePayload() {
        let reshaped = Data("""
        {"envelope_version":1,"protocol_version":\(ATMIPCContract.supportedProtocolVersion + 1),"request_id":"ipc-newer","verb":"config.settings","data":{}}
        """.utf8)
        do {
            _ = try JSONDecoder().decode(ATMIPCEnvelope<ATMSettingsSnapshot>.self, from: reshaped)
            XCTFail("expected a refusal")
        } catch is ATMIPCProtocolMismatch {
            // Correct: reported as a skew, not as a malformed payload.
        } catch {
            XCTFail("expected ATMIPCProtocolMismatch, got \(error)")
        }
    }

    /// Both skew checks quote install instructions; a user who hits one and then
    /// the other must not be sent two different ways.
    func testDashboardAndIPCSkewGiveTheSameUpgradeInstructions() {
        let dashboard = ATMDashboardSchemaMismatch(cliVersion: 9, appVersion: 1)
        let ipc = ATMIPCProtocolMismatch(cliVersion: 9, appVersion: 1)
        XCTAssertEqual(dashboard.recoverySuggestion, ipc.recoverySuggestion)
    }

    /// Captured from the real CLI (`atm _ipc config.settings` against a clean HOME,
    /// with only the long default prompt shortened). Following the same rule as
    /// GuardWireContractTests: this is the only assertion here that both sides
    /// agree on field names — every other test checks one side against its own idea
    /// of the other.
    func testTheCLIsActualEnvelopeDecodes() throws {
        let line = Data(#"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-captured","verb":"config.settings","data":{"owner_name":"","grok_live_quota":false,"collection_enabled":false,"collection_interval_minutes":5,"collection_lookback_minutes":60,"collection_message_retention_days":90,"text_model_base_url":"https://api.deepseek.com","text_model_name":"deepseek-v4-flash","text_model_source":"deepseek","todo_refine_prompt":"保守拆分","todo_refine_on_add":false,"text_model_api_key_configured":false}}"#.utf8)
        let decoded = try JSONDecoder().decode(ATMIPCEnvelope<ATMSettingsSnapshot>.self, from: line)
        XCTAssertEqual(decoded.data.textModelName, "deepseek-v4-flash")
        XCTAssertEqual(decoded.data.collectionMessageRetentionDays, 90)
        XCTAssertFalse(decoded.data.textModelAPIKeyConfigured)
        // An empty owner_name is a real value, not a missing one: the CLI leaves it
        // empty and the display layer substitutes 我.
        XCTAssertEqual(decoded.data.ownerName, "")

        // Every field the CLI sends must be one the app reads. A field it silently
        // drops is a setting the screen cannot show.
        let raw = try JSONSerialization.jsonObject(with: line) as! [String: Any]
        let envelopeKeys: Set<String> = [
            "envelope_version", "protocol_version", "request_id", "verb", "data",
        ]
        XCTAssertTrue(Set(raw.keys).subtracting(envelopeKeys).isEmpty,
                      "unread envelope fields: \(Set(raw.keys).subtracting(envelopeKeys))")
        let payload = raw["data"] as! [String: Any]
        let payloadKeys: Set<String> = [
            "owner_name", "grok_live_quota", "collection_enabled",
            "collection_interval_minutes", "collection_lookback_minutes",
            "collection_message_retention_days", "text_model_base_url",
            "text_model_name", "text_model_source", "todo_refine_prompt",
            "todo_refine_on_add", "text_model_api_key_configured",
        ]
        XCTAssertTrue(Set(payload.keys).subtracting(payloadKeys).isEmpty,
                      "the CLI sends settings the app drops: \(Set(payload.keys).subtracting(payloadKeys))")
        // And the other direction: a field the app expects but the CLI stopped
        // sending would decode as a failure above, so only the extra-field case
        // needs its own assertion.
        XCTAssertTrue(payloadKeys.subtracting(Set(payload.keys)).isEmpty,
                      "the app expects settings the CLI no longer sends: \(payloadKeys.subtracting(Set(payload.keys)))")
    }

    /// The optionals exist so "this form did not touch that setting" is sayable.
    /// If they encoded as null the CLI would try to parse null as a value and
    /// reject the whole batch, which is a confusing way to fail.
    func testUnsetFieldsAreOmittedRatherThanSentAsNull() throws {
        let save = ATMSettingsSave(
            textModelBaseURL: "https://api.deepseek.com",
            textModelName: "deepseek-v4-flash",
            textModelSource: "deepseek",
            todoRefinePrompt: ""
        )
        let object = try JSONSerialization.jsonObject(with: try save.encoded()) as! [String: Any]
        XCTAssertEqual(Set(object.keys), [
            "text_model_base_url", "text_model_name", "text_model_source", "todo_refine_prompt",
        ])
        // An empty prompt is a real value — it restores the CLI's default — so it
        // must survive as an empty string rather than being dropped as falsy.
        XCTAssertEqual(object["todo_refine_prompt"] as? String, "")
        XCTAssertNil(object["owner_name"])
        XCTAssertNil(object["grok_live_quota"])
    }

    /// Field names have to match what the CLI accepts; it refuses unknown keys
    /// rather than ignoring them, so a typo fails the whole save.
    func testSaveKeysMatchTheSnapshotKeys() throws {
        var everything = ATMSettingsSave()
        everything.ownerName = "墨水"
        everything.grokLiveQuota = true
        everything.collectionEnabled = true
        everything.collectionIntervalMinutes = 5
        everything.collectionLookbackMinutes = 60
        everything.collectionMessageRetentionDays = 90
        everything.textModelBaseURL = "https://api.deepseek.com"
        everything.textModelName = "m"
        everything.textModelSource = "s"
        everything.todoRefinePrompt = "p"
        everything.todoRefineOnAdd = false
        let object = try JSONSerialization.jsonObject(with: try everything.encoded()) as! [String: Any]
        // Same keys as the snapshot, minus the credential flag, which is not a
        // setting and is not writable through this verb.
        XCTAssertEqual(Set(object.keys), [
            "owner_name", "grok_live_quota", "collection_enabled",
            "collection_interval_minutes", "collection_lookback_minutes",
            "collection_message_retention_days", "text_model_base_url",
            "text_model_name", "text_model_source", "todo_refine_prompt",
            "todo_refine_on_add",
        ])
        XCTAssertNil(object["text_model_api_key_configured"])
    }

    func testTheSaveVerbIsTheOneTheCLIRegisters() {
        XCTAssertEqual(ATMIPCCommand.saveSettings.verb, "config.save")
        XCTAssertEqual(ATMIPCCommand.saveSettings.arguments, ["_ipc", "config.save"])
    }

    func testTheSettingsVerbIsTheOneTheCLIRegisters() {
        // Spelled out rather than derived: this is the string that crosses the
        // process boundary, so a typo here is exactly the bug being guarded.
        XCTAssertEqual(ATMIPCCommand.settings.verb, "config.settings")
        XCTAssertEqual(ATMIPCCommand.settings.arguments, ["_ipc", "config.settings"])
    }

    func testCredentialAndConnectionCheckUseTypedIPCOnly() throws {
        XCTAssertEqual(ATMIPCCommand.saveCredential.arguments, ["_ipc", "config.credential.save"])
        XCTAssertEqual(ATMIPCCommand.deleteCredential.arguments, ["_ipc", "config.credential.delete"])
        XCTAssertEqual(ATMIPCCommand.checkTextModel.arguments, ["_ipc", "config.text_model.check"])
        XCTAssertEqual(ATMIPCCommand.checkTextModel.timeout, 45)

        let credential = try JSONSerialization.jsonObject(
            with: JSONEncoder().encode(ATMCredentialSave(apiKey: "sk-draft"))
        ) as! [String: Any]
        XCTAssertEqual(credential["api_key"] as? String, "sk-draft")
        XCTAssertEqual(Set(credential.keys), ["api_key"])

        let savedKeyCheck = try JSONSerialization.jsonObject(
            with: JSONEncoder().encode(ATMTextModelCheck(
                apiKey: nil,
                baseURL: "https://models.example/v1",
                model: "flash"
            ))
        ) as! [String: Any]
        XCTAssertNil(savedKeyCheck["api_key"])
        XCTAssertEqual(savedKeyCheck["base_url"] as? String, "https://models.example/v1")
        XCTAssertEqual(savedKeyCheck["model"] as? String, "flash")
    }

    func testConnectionCheckResponseDecodesItsWireKeys() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-check","verb":"config.text_model.check","data":{"ok":true,"latency_ms":37}}"#
        let (runner, directory) = try runner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }

        let result = try await ATMIPCClient(runner: runner).call(
            ATMIPCCommand.checkTextModel,
            request: ATMTextModelCheck(
                apiKey: "unsaved",
                baseURL: "https://models.example",
                model: "flash"
            )
        )

        XCTAssertEqual(result, ATMTextModelCheckResult(ok: true, latencyMS: 37))
    }

    private struct Greeting: Decodable {
        let greeting: String
    }

    func testTypedClientValidatesVerbAndDecodesPayload() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-ok","verb":"example.greet","data":{"greeting":"hello"}}"#
        let (runner, directory) = try runner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }
        let method = ATMIPCMethod<ATMIPCNoRequest, Greeting>("example.greet")

        let greeting = try await ATMIPCClient(runner: runner).call(method)

        XCTAssertEqual(greeting.greeting, "hello")
    }

    func testTypedClientRefusesAResponseForAnotherVerb() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-wrong","verb":"example.other","data":{"greeting":"hello"}}"#
        let (runner, directory) = try runner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }
        let method = ATMIPCMethod<ATMIPCNoRequest, Greeting>("example.greet")

        do {
            _ = try await ATMIPCClient(runner: runner).call(method)
            XCTFail("a response for another verb must not be accepted")
        } catch let mismatch as ATMIPCVerbMismatch {
            XCTAssertEqual(mismatch.expected, "example.greet")
            XCTAssertEqual(mismatch.actual, "example.other")
            XCTAssertEqual(mismatch.requestID, "ipc-wrong")
        } catch {
            XCTFail("expected ATMIPCVerbMismatch, got \(error)")
        }
    }

    func testNonzeroExitStillSurfacesStructuredIPCError() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-conflict","verb":"example.update","error":{"code":"conflict","message":"revision changed","details":{"current_revision":4},"retryable":true}}"#
        let (runner, directory) = try runner(
            stdout: response,
            stderr: "Error: revision changed",
            status: 1
        )
        defer { try? FileManager.default.removeItem(at: directory) }
        let method = ATMIPCMethod<ATMIPCNoRequest, Greeting>("example.update")

        do {
            _ = try await ATMIPCClient(runner: runner).call(method)
            XCTFail("the error envelope must be thrown")
        } catch let remote as ATMIPCRemoteError {
            XCTAssertEqual(remote.code, "conflict")
            XCTAssertEqual(remote.message, "revision changed")
            XCTAssertEqual(remote.details, .object(["current_revision": .number(4)]))
            XCTAssertTrue(remote.retryable)
            XCTAssertEqual(remote.requestID, "ipc-conflict")
            XCTAssertEqual(remote.verb, "example.update")
        } catch {
            XCTFail("expected ATMIPCRemoteError, got \(error)")
        }
    }

    func testRawRunnerKeepsStdoutFromANonzeroProcess() async throws {
        let (runner, directory) = try runner(stdout: "structured stdout", stderr: "fallback", status: 9)
        defer { try? FileManager.default.removeItem(at: directory) }

        let result = try await runner.runRaw(["_ipc", "example.fail"])

        XCTAssertEqual(result.terminationStatus, 9)
        XCTAssertEqual(String(data: result.standardOutput, encoding: .utf8), "structured stdout")
        XCTAssertEqual(String(data: result.standardError, encoding: .utf8), "fallback")
    }

    func testRawPayloadDropsEnvelopeAndPreservesUnknownExportFields() async throws {
        // 2^53+1 cannot survive a Double round-trip. Export must keep the raw
        // integer because future schema fields may use 64-bit IDs unknown to
        // this version of the App.
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-export","verb":"day.data.export","data":{"schema_version":3,"privacy":{},"atlas":{},"history":{},"feedback":[],"future_field":{"exact_id":9007199254740993}}}"#
        let (runner, directory) = try runner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }

        let data = try await ATMIPCClient(runner: runner).callRawPayload(ATMAIDayCommand.exportData)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])

        XCTAssertEqual(object["schema_version"] as? Int, 3)
        XCTAssertNotNil(object["future_field"])
        XCTAssertNil(object["protocol_version"])
        XCTAssertNil(object["verb"])
        XCTAssertNil(object["data"])
        XCTAssertNil(object["error"])
        XCTAssertTrue(
            String(decoding: data, as: UTF8.self).contains("9007199254740993"),
            "raw export rounded an unknown 64-bit integer"
        )
    }
}
