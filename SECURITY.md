# Security Policy

## Supported versions

Security fixes are made on the default branch and included in the next release.
Only the latest published release is supported.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting entry for this repository when it
is available. Include the affected version, impact, reproduction steps, and any
suggested mitigation.

If private reporting is not available, open a public issue that asks the
maintainers for a secure contact channel. Do not include exploit code, private
transcripts, credentials, tokens, internal URLs, personal data, or other
sensitive details in a public issue.

Please allow the maintainers a reasonable period to investigate and publish a
fix before public disclosure.

## Sensitive bug reports

ATM indexes local AI-agent transcripts and can optionally import messaging
content. Before attaching logs, exports, screenshots, configuration, or an ATM
database to an issue, replace or remove:

- prompts, model responses, thinking content, and tool arguments;
- names, email addresses, chat identifiers, repository paths, and internal URLs;
- API keys, cookies, bearer tokens, native-messaging payloads, and credentials;
- Todo, memory, knowledge, and collection content that is not intended to be
  public.

Prefer a minimal synthetic reproduction over real user data.

## Security boundaries

ATM is a local application, not a security sandbox. A user who can read the ATM
data directory can read the indexed content. Commands configured as Todo
completion hooks and external model or connector commands run with the current
user's privileges. Review those commands before enabling them.

