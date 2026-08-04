# Contributing to ATM

Thank you for helping improve ATM.

## Before opening a change

- Search existing issues and pull requests for related work.
- Keep changes focused and describe the user-visible behavior and compatibility
  impact.
- Only contribute code and assets that you have the right to publish under this
  repository's MIT license. Do not submit employer-confidential code, internal
  service details, proprietary samples, or third-party material without a
  compatible license.
- Use synthetic fixtures. Never commit real transcripts, chat identifiers,
  names, email addresses, credentials, company domains, or personal filesystem
  paths.

## Development setup

The CLI requires Go 1.25 or newer. The macOS application uses Swift Package
Manager and requires macOS 13 or newer with a Swift 5.9-compatible toolchain.

Run the focused tests while developing:

```bash
go test ./...
go vet ./...
swift test --package-path app/macos
```

Before requesting review, run the complete release contract:

```bash
./scripts/release-check.sh
```

The script tests the Go packages, vets and builds the CLI, cross-compiles the
release targets, verifies installer naming, and tests the macOS package when
run on macOS.

## Pull requests

Explain what changed, why it changed, how it was tested, and any behavior that
was not verified. Add tests for bug fixes and new behavior. Update user-facing
documentation when commands, configuration, storage, or privacy behavior
changes.

By submitting a contribution, you confirm that you have the right to submit it
and agree that it is licensed under the repository's MIT license.

Security vulnerabilities and sensitive data exposures should be reported using
the process in [SECURITY.md](SECURITY.md), not through a public issue.

