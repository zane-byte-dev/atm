import Foundation

/// Everything that happens to a transcript between the recognizer and the cursor.
///
/// Deliberately free of AppKit, UserDefaults and audio: what makes dictation feel
/// wrong is almost never the recognizer, it is a stray space before a comma or a
/// name that comes out phonetically. Those are string problems, and keeping them
/// here means they can be tested without a microphone.
enum ATMVoiceTextCleanup {
    /// Strips SenseVoice's inline metadata tags.
    ///
    /// The model emits its language, emotion and event guesses in the text itself —
    /// `<|zh|><|NEUTRAL|><|Speech|>你好` — because they are ordinary tokens to it.
    /// They are not words anyone said, so they never reach the cursor.
    static func stripModelTags(_ value: String) -> String {
        value.replacingOccurrences(
            of: "<\\|[^|>]*\\|>",
            with: "",
            options: .regularExpression
        )
    }

    /// Collapses runs of spaces and tabs, drops spaces hugging a line break, and
    /// trims the ends. Line breaks themselves survive: a pause long enough for the
    /// recognizer to break a line is usually a real break.
    static func normalizeWhitespace(_ value: String) -> String {
        value
            .replacingOccurrences(of: "\r\n", with: "\n")
            .replacingOccurrences(of: "[ \\t]+", with: " ", options: .regularExpression)
            .replacingOccurrences(of: " *\n *", with: "\n", options: .regularExpression)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Removes the space recognizers leave in front of punctuation when a sentence
    /// mixes languages — "好的 ，我看看" — which is the single most visible way
    /// dictated Chinese reads as machine output.
    static func tightenPunctuation(_ value: String) -> String {
        value.replacingOccurrences(
            of: " +([，。！？；：、,.!?;:])",
            with: "$1",
            options: .regularExpression
        )
    }

    /// Parses the replacement dictionary a person types into settings.
    ///
    /// One rule per line, `原词 => 替换词`. `→` and a bare `=` are accepted too
    /// because all three get typed; `#` starts a comment. A malformed line is
    /// skipped rather than rejecting the whole list — one typo should not silently
    /// disable every other rule.
    static func parseReplacements(_ raw: String) -> [ATMVoiceReplacement] {
        raw.split(whereSeparator: \.isNewline).compactMap { line in
            let value = line.trimmingCharacters(in: .whitespaces)
            guard !value.isEmpty, !value.hasPrefix("#") else { return nil }
            // The separator is chosen first and then committed to. Trying each in turn
            // and taking whichever happens to split cleanly reads the incomplete rule
            // "缺少替换词 =>" as `=` with a target of ">" — a rule that would silently
            // rewrite text nobody asked about. Longest first so "=>" is not seen as "=".
            guard let separator = ["=>", "→", "="].first(where: { value.contains($0) }) else {
                return nil
            }
            let parts = value.components(separatedBy: separator)
            guard parts.count == 2 else { return nil }
            let source = parts[0].trimmingCharacters(in: .whitespaces)
            let target = parts[1].trimmingCharacters(in: .whitespaces)
            guard !source.isEmpty, !target.isEmpty else { return nil }
            return ATMVoiceReplacement(source: source, target: target)
        }
    }

    static func applyReplacements(_ value: String, _ replacements: [ATMVoiceReplacement]) -> String {
        replacements.reduce(value) { text, replacement in
            text.replacingOccurrences(of: replacement.source, with: replacement.target)
        }
    }

    /// Drops one trailing full stop.
    ///
    /// Dictation usually lands mid-thought — into a chat box, a commit message, a
    /// prompt — where the recognizer's sentence-final 。 is one keystroke to delete
    /// every single time. Only ever one, so "等一下。。。" keeps its shape.
    static func removeTrailingPeriod(_ value: String) -> String {
        guard let last = value.last, last == "。" || last == "." else { return value }
        return String(value.dropLast())
    }

    /// The whole pipeline, in the order the steps depend on each other: tags out
    /// before whitespace (removing a tag can leave a double space), replacements
    /// before punctuation tightening (a replacement can introduce a space), and the
    /// trailing stop last, once nothing else will append to the end.
    static func process(
        _ transcript: String,
        replacements: [ATMVoiceReplacement],
        removeTrailingPeriod shouldRemoveTrailingPeriod: Bool
    ) -> String {
        var text = stripModelTags(transcript)
        text = normalizeWhitespace(text)
        text = applyReplacements(text, replacements)
        text = tightenPunctuation(text)
        if shouldRemoveTrailingPeriod {
            text = removeTrailingPeriod(text)
        }
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Reads the pipeline's inputs from the stored preferences.
    static func process(_ transcript: String) -> String {
        process(
            transcript,
            replacements: ATMVoiceInputPreferences.replacements,
            removeTrailingPeriod: ATMVoiceInputPreferences.removeTrailingPeriod
        )
    }
}
