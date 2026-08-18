import XCTest

@testable import ATMMenuBarApp

final class GuardToolSettingsTests: XCTestCase {

    private func tool(_ json: String) throws -> ATMGuardTool {
        try JSONDecoder().decode(ATMGuardTool.self, from: Data(json.utf8))
    }

    // MARK: - Tool state

    func testAHealthyToolReadsAsEnabled() throws {
        let tool = try tool(
            #"{"tool":"dws","bin_path":"/x/dws","real_path":"/x/dws-atm-real","installed":true,"clobbered":false,"rules":1}"#
        )
        XCTAssertTrue(tool.isHealthy)
        XCTAssertEqual(tool.stateText, "已启用")
        XCTAssertNil(tool.problemAdvice)
    }

    /// The state that would otherwise be invisible: installed, reported fine, and
    /// walked around every time the tool is called by name.
    func testAShadowedToolIsNotHealthyAndSaysWhichCopyWins() throws {
        let tool = try tool(
            #"{"tool":"a1","bin_path":"/q/a1","installed":true,"clobbered":false,"shadowed_by":"/local/a1","rules":1}"#
        )
        XCTAssertTrue(tool.installed)
        XCTAssertFalse(tool.isHealthy, "an installed but bypassed gate must not read as healthy")
        XCTAssertEqual(tool.stateText, "被 PATH 绕过")
        XCTAssertTrue(tool.problemAdvice?.contains("/local/a1") == true, tool.problemAdvice ?? "")
    }

    func testAnOverwrittenShimIsReportedAsRepairable() throws {
        let tool = try tool(
            #"{"tool":"dws","bin_path":"/x/dws","installed":false,"clobbered":true,"rules":1}"#
        )
        XCTAssertEqual(tool.stateText, "被覆盖")
        XCTAssertTrue(tool.problemAdvice?.contains("重新启用") == true, tool.problemAdvice ?? "")
    }

    func testAShimWithNoRealBinaryIsADistinctProblem() throws {
        let tool = try tool(
            #"{"tool":"dws","bin_path":"/x/dws","installed":true,"clobbered":true,"rules":1}"#
        )
        XCTAssertEqual(tool.stateText, "真身丢了")
        XCTAssertFalse(tool.isHealthy)
    }

    /// The case that makes the path field necessary at all: `dws` is never on PATH.
    func testAToolWithNoKnownPathAsksForOne() throws {
        let tool = try tool(#"{"tool":"dws","bin_path":"","installed":false,"clobbered":false,"rules":1}"#)
        XCTAssertTrue(tool.needsPath)
        XCTAssertEqual(tool.stateText, "未找到路径")
        XCTAssertTrue(tool.problemAdvice?.contains("绝对路径") == true, tool.problemAdvice ?? "")
    }

    /// The state that produced "why is it 未启用, and why does 启用 do nothing?": a
    /// recorded path that no longer exists. Pressing the button submitted the dead
    /// path, which is the one thing that cannot work.
    func testARecordedPathThatIsGoneIsItsOwnState() throws {
        let tool = try tool(
            #"{"tool":"dws","bin_path":"/gone/dws","bin_exists":false,"installed":false,"clobbered":false,"rules":1}"#
        )
        XCTAssertTrue(tool.pathIsMissing)
        XCTAssertFalse(tool.needsPath, "the path is recorded; it is the file that is missing")
        XCTAssertEqual(tool.stateText, "路径不存在")
        XCTAssertTrue(tool.problemAdvice?.contains("没有文件") == true, tool.problemAdvice ?? "")
        XCTAssertTrue(tool.needsPathInput, "the card must offer a way to give a new path")
    }

    func testAnExistingPathThatIsSimplyNotGatedIsNotConfusedWithAMissingOne() throws {
        let tool = try tool(
            #"{"tool":"a1","bin_path":"/local/a1","bin_exists":true,"installed":false,"clobbered":false,"rules":1}"#
        )
        XCTAssertFalse(tool.pathIsMissing)
        XCTAssertEqual(tool.stateText, "未启用")
        XCTAssertNil(tool.problemAdvice)
        XCTAssertTrue(tool.needsPathInput)
    }

    /// bin_exists is optional so an older CLI's output still decodes; absent must
    /// not be read as "missing".
    func testAbsentBinExistsIsNotReadAsMissing() throws {
        let tool = try tool(
            #"{"tool":"a1","bin_path":"/local/a1","installed":true,"clobbered":false,"rules":1}"#
        )
        XCTAssertFalse(tool.pathIsMissing)
        XCTAssertTrue(tool.isHealthy)
    }

    /// Installing a gate in front of a tool with no rules gates nothing, so it must
    /// not look like success.
    func testAnInstalledToolWithNoRulesIsCalledOut() throws {
        let tool = try tool(
            #"{"tool":"mytool","bin_path":"/x/mytool","installed":true,"clobbered":false,"rules":0}"#
        )
        XCTAssertFalse(tool.isHealthy)
        XCTAssertEqual(tool.stateText, "无规则，全部直通")
    }

    // MARK: - Rule view

    func testABuiltinRuleCannotBeDeleted() throws {
        let rule = try JSONDecoder().decode(
            ATMGuardRule.self,
            from: Data(
                #"{"tool":"dws","id":"chat-send","label":"发送钉钉消息","path":["chat","message","send"],"target_flags":["--group","--user"],"body_flags":["--text"],"enabled":true,"builtin":true,"overridden":false}"#
                    .utf8))
        XCTAssertFalse(rule.isDeletable, "removing a built-in's override restores it, so delete is the wrong verb")
        XCTAssertEqual(rule.originText, "内置")
        XCTAssertEqual(rule.matcherText, "chat message send")
    }

    func testAPatchedBuiltinSaysSo() throws {
        let rule = try JSONDecoder().decode(
            ATMGuardRule.self,
            from: Data(
                #"{"tool":"a1","id":"mr-remind","path":["repo","mr","remind"],"enabled":false,"builtin":true,"overridden":true}"#
                    .utf8))
        XCTAssertEqual(rule.originText, "内置 · 已改")
        XCTAssertFalse(rule.enabled)
        // The matcher survives the patch, so the row can still show what it is about.
        XCTAssertEqual(rule.matcherText, "repo mr remind")
    }

    func testARuleMatchedByPatternRendersThePattern() throws {
        let rule = try JSONDecoder().decode(
            ATMGuardRule.self,
            from: Data(
                #"{"tool":"aone-kit","id":"ata-webhook-push","argv_pattern":"^ata::message-ding-talk-send-to-webhook$","enabled":true,"builtin":true,"overridden":false}"#
                    .utf8))
        XCTAssertEqual(rule.matcherText, "~ ^ata::message-ding-talk-send-to-webhook$")
    }

    func testACustomRuleIsDeletable() throws {
        let rule = try JSONDecoder().decode(
            ATMGuardRule.self,
            from: Data(
                #"{"tool":"mytool","id":"doc-write","path":["doc","write"],"enabled":true,"builtin":false,"overridden":true}"#
                    .utf8))
        XCTAssertTrue(rule.isDeletable)
        XCTAssertEqual(rule.originText, "自定义")
    }

    // MARK: - The form

    private var validDraft: ATMGuardRuleDraft {
        var draft = ATMGuardRuleDraft()
        draft.tool = "dws"
        draft.ruleID = "chat-send"
        draft.label = "发送钉钉消息"
        draft.path = "chat message send"
        draft.targetFlags = "--group, --user"
        draft.bodyFlags = "--text"
        return draft
    }

    func testTheFormBuildsTheRuleTheCLIExpects() throws {
        let payload = try validDraft.jsonPayload()
        let rule = try JSONSerialization.jsonObject(with: payload) as! [String: Any]
        XCTAssertEqual(rule["id"] as? String, "chat-send")
        XCTAssertEqual(rule["label"] as? String, "发送钉钉消息")
        XCTAssertEqual(rule["path"] as? [String], ["chat", "message", "send"])
        XCTAssertEqual((rule["target"] as? [String: Any])?["flags"] as? [String], ["--group", "--user"])
        XCTAssertEqual((rule["body"] as? [String: Any])?["flags"] as? [String], ["--text"])
    }

    /// Extractors only affect what the approval card shows. A rule without them
    /// still gates, so the form must not require them.
    func testExtractorsAreOptional() throws {
        var draft = validDraft
        draft.targetFlags = ""
        draft.bodyFlags = ""
        draft.label = ""
        XCTAssertNil(draft.validationMessage)
        let rule = try JSONSerialization.jsonObject(with: try draft.jsonPayload()) as! [String: Any]
        XCTAssertNil(rule["target"])
        XCTAssertNil(rule["body"])
        XCTAssertNil(rule["label"])
        XCTAssertEqual(rule["path"] as? [String], ["chat", "message", "send"])
    }

    /// A subcommand is required: without one the rule has no matcher, which the CLI
    /// only accepts as a patch onto a built-in of the same id. Better to say so in
    /// the form than to surface it as an error after saving.
    func testTheFormRefusesARuleWithNoSubcommand() {
        var draft = validDraft
        draft.path = "   "
        XCTAssertNotNil(draft.validationMessage)
        XCTAssertTrue(draft.validationMessage?.contains("子命令") == true, draft.validationMessage ?? "")
    }

    func testTheFormRefusesAMissingToolOrID() {
        var draft = validDraft
        draft.tool = " "
        XCTAssertNotNil(draft.validationMessage)
        draft = validDraft
        draft.ruleID = ""
        XCTAssertNotNil(draft.validationMessage)
    }

    func testFlagListsTolerateSpacingAndTrailingSeparators() {
        var draft = validDraft
        draft.targetFlags = " --group ,, --user , "
        draft.path = "  chat   message  send  "
        XCTAssertEqual(draft.targetFlagTokens, ["--group", "--user"])
        XCTAssertEqual(draft.pathTokens, ["chat", "message", "send"])
    }

    /// Switching a rule off must send only its id and the flag. Restating the
    /// matcher would let this copy drift from the built-in's real one and silently
    /// change what is gated.
    func testTogglePayloadCarriesNothingButTheIDAndTheFlag() throws {
        let payload = try ATMGuardRuleDraft.togglePayload(ruleID: "chat-send", enabled: false)
        let rule = try JSONSerialization.jsonObject(with: payload) as! [String: Any]
        XCTAssertEqual(Set(rule.keys), ["id", "enabled"])
        XCTAssertEqual(rule["id"] as? String, "chat-send")
        XCTAssertEqual(rule["enabled"] as? Bool, false)
    }

    // MARK: - Command construction

    func testInstallArgvOmitsAnEmptyPathSoTheCLICanResolveIt() {
        XCTAssertEqual(
            ATMCommandBuilder.guardInstall(tool: "a1", bin: ""),
            ["guard", "install", "a1", "--json"])
        XCTAssertEqual(
            ATMCommandBuilder.guardInstall(tool: "dws", bin: "/q/dws"),
            ["guard", "install", "dws", "--bin", "/q/dws", "--json"])
    }

    func testRuleArgvDoesNotCarryTheRuleItself() {
        // The rule travels on stdin: argv is the one place a value would show up in
        // process listings.
        let argv = ATMCommandBuilder.guardRuleSet(tool: "dws")
        XCTAssertEqual(argv, ["guard", "rule", "set", "dws", "--json"])
        XCTAssertFalse(argv.contains { $0.contains("{") })
    }

    func testRemoveAndForgetArgv() {
        XCTAssertEqual(
            ATMCommandBuilder.guardRuleRemove(tool: "mytool", ruleID: "doc-write"),
            ["guard", "rule", "remove", "mytool", "doc-write", "--json"])
        XCTAssertEqual(
            ATMCommandBuilder.guardToolForget(tool: "mytool"),
            ["guard", "forget", "mytool", "--json"])
    }
}
