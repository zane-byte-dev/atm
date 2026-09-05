import XCTest
@testable import VoxCaret

/// The transcript pipeline is the only part of dictation that can be checked without
/// a microphone, and it is also where the visible defects live: a stray space before a
/// comma, a name that came out phonetically, a model's own metadata leaking into the
/// text someone is about to send.
final class VoxCaretTextCleanupTests: XCTestCase {
    // MARK: - Model tags

    /// SenseVoice writes its language, emotion and event guesses into the text as
    /// ordinary tokens. They are not words anyone said, so none of them may survive to
    /// the cursor.
    func testModelTagsAreStripped() {
        XCTAssertEqual(
            VoxCaretTextCleanup.stripModelTags("<|zh|><|NEUTRAL|><|Speech|><|woitn|>今天天气不错"),
            "今天天气不错"
        )
        XCTAssertEqual(VoxCaretTextCleanup.stripModelTags("<||>裸标记"), "裸标记")
        XCTAssertEqual(VoxCaretTextCleanup.stripModelTags("没有标记"), "没有标记")
    }

    /// A literal `<|` in dictated text has no closing marker, so the pattern must not
    /// run away and eat the rest of the sentence.
    func testUnterminatedTagMarkerIsLeftAlone() {
        XCTAssertEqual(
            VoxCaretTextCleanup.stripModelTags("比较 a <| b 的大小"),
            "比较 a <| b 的大小"
        )
    }

    // MARK: - Whitespace

    func testWhitespaceIsCollapsedButLineBreaksSurvive() {
        XCTAssertEqual(
            VoxCaretTextCleanup.normalizeWhitespace("  第一段   有   空格  \n   第二段  "),
            "第一段 有 空格\n第二段"
        )
    }

    func testCarriageReturnsAreNormalized() {
        XCTAssertEqual(VoxCaretTextCleanup.normalizeWhitespace("一\r\n二"), "一\n二")
    }

    // MARK: - Punctuation

    /// The single most recognisable "this was dictated" artifact in mixed-language
    /// Chinese: recognizers leave the space that separated the languages sitting in
    /// front of the punctuation.
    func testSpaceBeforePunctuationIsRemoved() {
        XCTAssertEqual(VoxCaretTextCleanup.tightenPunctuation("好的 ，我看看"), "好的，我看看")
        XCTAssertEqual(VoxCaretTextCleanup.tightenPunctuation("等等   。"), "等等。")
        XCTAssertEqual(VoxCaretTextCleanup.tightenPunctuation("Okay , sure ."), "Okay, sure.")
    }

    /// Spaces after punctuation are how English reads; only the leading side is wrong.
    func testSpaceAfterPunctuationIsKept() {
        XCTAssertEqual(VoxCaretTextCleanup.tightenPunctuation("Okay, sure"), "Okay, sure")
    }

    // MARK: - Replacement dictionary

    func testAllThreeSeparatorsParse() {
        let rules = VoxCaretTextCleanup.parseReplacements(
            """
            阿童木 => ATM
            派 → Pi
            寇德 = Codex
            """
        )

        XCTAssertEqual(rules, [
            VoxCaretReplacement(source: "阿童木", target: "ATM"),
            VoxCaretReplacement(source: "派", target: "Pi"),
            VoxCaretReplacement(source: "寇德", target: "Codex"),
        ])
    }

    /// `=>` has to win over `=`, or every arrow rule would split into three parts and
    /// be silently discarded.
    func testLongestSeparatorWins() {
        XCTAssertEqual(
            VoxCaretTextCleanup.parseReplacements("阿童木 => ATM"),
            [VoxCaretReplacement(source: "阿童木", target: "ATM")]
        )
    }

    /// An incomplete `=>` rule must be rejected outright, not retried as `=`.
    ///
    /// Trying each separator until one splits cleanly reads "缺少替换词 =>" as a rule
    /// mapping 缺少替换词 to ">" — a rewrite that would then fire on text nobody asked
    /// about, from a line that looks obviously unfinished.
    func testIncompleteArrowRuleIsNotRetriedAsEquals() {
        XCTAssertTrue(VoxCaretTextCleanup.parseReplacements("缺少替换词 =>").isEmpty)
        XCTAssertTrue(VoxCaretTextCleanup.parseReplacements("=> 缺少原词").isEmpty)
        XCTAssertTrue(VoxCaretTextCleanup.parseReplacements("一 => 二 => 三").isEmpty)
    }

    func testCommentsBlankLinesAndMalformedRulesAreSkipped() {
        let rules = VoxCaretTextCleanup.parseReplacements(
            """
            # 这是注释
            阿童木 => ATM

               # 缩进的注释
            没有分隔符
            => 缺少原词
            缺少替换词 =>
            一 => 二 => 三
            """
        )

        XCTAssertEqual(rules, [VoxCaretReplacement(source: "阿童木", target: "ATM")])
    }

    /// One bad line must not take the rest of the list with it — otherwise a typo
    /// silently disables every rule someone has built up.
    func testOneMalformedRuleDoesNotDiscardTheRest() {
        let rules = VoxCaretTextCleanup.parseReplacements(
            """
            好的 => OK
            这行是坏的
            派 => Pi
            """
        )

        XCTAssertEqual(rules.count, 2)
        XCTAssertEqual(rules.map(\.target), ["OK", "Pi"])
    }

    func testReplacementsAreApplied() {
        XCTAssertEqual(
            VoxCaretTextCleanup.applyReplacements(
                "帮我看下阿童木的任务",
                [VoxCaretReplacement(source: "阿童木", target: "ATM")]
            ),
            "帮我看下ATM的任务"
        )
    }

    // MARK: - Trailing period

    /// Exactly one, so an ellipsis someone actually dictated keeps its shape.
    func testOnlyOneTrailingPeriodIsRemoved() {
        XCTAssertEqual(VoxCaretTextCleanup.removeTrailingPeriod("改完了。"), "改完了")
        XCTAssertEqual(VoxCaretTextCleanup.removeTrailingPeriod("Done."), "Done")
        XCTAssertEqual(VoxCaretTextCleanup.removeTrailingPeriod("等一下。。。"), "等一下。。")
        XCTAssertEqual(VoxCaretTextCleanup.removeTrailingPeriod("还没完？"), "还没完？")
        XCTAssertEqual(VoxCaretTextCleanup.removeTrailingPeriod(""), "")
    }

    // MARK: - Whole pipeline

    /// The order the steps run in is load-bearing: stripping a tag can leave a double
    /// space, and a replacement can introduce one in front of punctuation. Running
    /// whitespace and punctuation cleanup before those two would leave both behind.
    func testPipelineOrderCleansUpAfterEarlierSteps() {
        let result = VoxCaretTextCleanup.process(
            "<|zh|><|NEUTRAL|>  帮我看下   阿童木 ，谢谢。 ",
            replacements: [VoxCaretReplacement(source: "阿童木", target: "ATM")],
            removeTrailingPeriod: true
        )

        XCTAssertEqual(result, "帮我看下 ATM，谢谢")
    }

    func testPipelineLeavesTrailingPeriodWhenNotAskedTo() {
        XCTAssertEqual(
            VoxCaretTextCleanup.process("改完了。", replacements: [], removeTrailingPeriod: false),
            "改完了。"
        )
    }

    /// Silence, a mis-press, or a transcript that was nothing but model tags all have
    /// to come out empty rather than as a space — the coordinator treats empty as "say
    /// it again" and would otherwise paste whitespace.
    func testTagOnlyAndBlankTranscriptsCollapseToEmpty() {
        XCTAssertEqual(
            VoxCaretTextCleanup.process("<|zh|><|NEUTRAL|><|Speech|>", replacements: [], removeTrailingPeriod: true),
            ""
        )
        XCTAssertEqual(
            VoxCaretTextCleanup.process("   \n  ", replacements: [], removeTrailingPeriod: true),
            ""
        )
    }
}
