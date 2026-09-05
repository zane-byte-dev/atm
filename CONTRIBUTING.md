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

The CLI and Web workspace require Go 1.25 or newer; building embedded Web assets
also requires Node.js 24 and npm. The optional ATM Menu and VoxCaret apps use
Swift Package Manager and require macOS 13.4 or newer. `app/macos` is retained
as historical source only and is not part of the supported build or test matrix.

Run the focused tests while developing:

```bash
go test ./...
go vet ./...
npm run check --prefix app/web
npm run test --prefix app/web
swift test --package-path app/menubar
swift test --package-path app/voice
```

Before requesting review, run the complete release contract:

```bash
./scripts/release-check.sh
```

The script tests the Go packages, vets and builds the CLI, cross-compiles the
release targets, verifies installer naming, and tests the current Web and
optional native products when run in their supported environments.

## Pull requests

Explain what changed, why it changed, how it was tested, and any behavior that
was not verified. Add tests for bug fixes and new behavior. Update user-facing
documentation when commands, configuration, storage, or privacy behavior
changes.

By submitting a contribution, you confirm that you have the right to submit it
and agree that it is licensed under the repository's MIT license.

Security vulnerabilities and sensitive data exposures should be reported using
the process in [SECURITY.md](SECURITY.md), not through a public issue.
