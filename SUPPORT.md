# Support

Use GitHub issues for reproducible bugs and focused feature requests. Include the ATM version, operating system, and a minimal safe reproduction.

Run `atm diagnose --bundle` and attach the file it writes. It collects the versions, schema versions, doctor findings, data source presence, last sync error and the tail of the CLI and App logs — which is most of what a first reply would ask for — and nothing else: no session text, no todo, memory or knowledge content, no credentials, not even command arguments, and paths under your home directory rewritten to `~`. It reads local state only and uploads nothing. Read it before attaching; it is a plain JSON file.

If the App became unresponsive rather than failing outright, the bundle's `logs.app` section is the place to look: it records whether the previous run exited cleanly, and points at `~/Library/Logs/DiagnosticReports` for the macOS crash report itself.

ATM indexes local agent conversations and can contain private source code, prompts, messages, credentials, and personal data. Never upload `~/.atm`, an ATM database, transcript, configuration, or unredacted export. Replace identifiers and paths with synthetic values before posting.

Security vulnerabilities belong in a private GitHub security advisory as described in [SECURITY.md](SECURITY.md), not a public issue.
