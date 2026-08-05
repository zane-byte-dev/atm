# Support

Use GitHub issues for reproducible bugs and focused feature requests. Include the ATM version, operating system, and a minimal safe reproduction.

Run `atm diagnose --bundle` and attach the file it writes. It collects the versions, schema versions, doctor findings, data source presence and last sync error — which is most of what a first reply would ask for — and nothing else: no session text, no todo, memory or knowledge content, no credentials, and paths under your home directory rewritten to `~`. It reads local state only and uploads nothing. Read it before attaching; it is a plain JSON file.

ATM indexes local agent conversations and can contain private source code, prompts, messages, credentials, and personal data. Never upload `~/.atm`, an ATM database, transcript, configuration, or unredacted export. Replace identifiers and paths with synthetic values before posting.

Security vulnerabilities belong in a private GitHub security advisory as described in [SECURITY.md](SECURITY.md), not a public issue.
