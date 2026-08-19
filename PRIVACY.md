# Privacy and Data Handling

ATM is local-first. It does not provide a hosted account service and does not
send analytics or telemetry to the ATM maintainers.

## Data ATM reads

Depending on the features enabled, ATM can read:

- local transcripts and metadata produced by supported AI coding agents;
- model, token, cost, quota, tool-call, working-directory, and session data;
- Todo, binding, memory, knowledge, comment, and artifact content created in
  ATM;
- messages from explicitly configured connector sources;

These sources can contain source code, prompts, model responses, personal data,
company-confidential information, credentials embedded in transcripts, and
other sensitive material.

## Local storage

ATM stores structured data in `~/.atm/atm.db` by default. Configuration is kept
in `~/.atm/config.json`; knowledge and related local files are kept under the
ATM data directory. The data directory can be changed in configuration.

The one secret ATM stores itself — the credential for the built-in DeepSeek text
service — is kept out of ordinary configuration. It lives alone in
`~/.atm/credentials.json` as mode `0600` inside a `0700` data directory, and ATM
refuses to read it when the permissions are wider. Config output, `atm backup`
archives and `atm diagnose` bundles all leave the file out, so a routine backup
or a support bundle cannot carry a live key. Manage it with `atm config
credential status | set | delete` — `set` reads stdin, so the key never reaches a
command line or shell history — or in ATM.app under 设置 → 模型.
`DEEPSEEK_API_KEY` overrides the file for one command without writing to disk.

Indexed agent sessions can outlive their upstream transcript files. Use
`atm session forget` after the source transcript has been removed and the index
has been synchronized. Connector messages are retained for 90 days by default;
set `collection_message_retention_days` to a shorter period when appropriate.

When the outbound action gate is installed, `approvals` holds the target and the
body of each message an agent tried to send, whether or not you approved it. That
is the point of the table — you cannot decide about a message you cannot read, and
you cannot audit a send you have no record of — but it means outgoing message text
now lives in the database, and rides into `atm backup` archives with everything
else that is ATM's own record. Preview values are scanned for credentials first,
so a webhook URL is stored with its access token masked. `atm diagnose` bundles
never include message bodies.

Back up, share, and delete the ATM data directory with the same care as the
underlying transcripts and messaging data.

## Network and external-process activity

Most commands operate on local files. Network or external-process activity is
limited to features the user invokes or enables, including:

- `atm config update-pricing`, which downloads public model pricing from
  OpenRouter;
- opt-in live Grok quota lookup, which uses the locally stored Grok credential;
- `atm todo refine`, which sends one task's fields, a Markdown-card excerpt and
  any request you typed for that pass to the configured DeepSeek endpoint — only
  when asked for, unless you opt into `todo_refine_on_add` to also run it after a
  desktop capture — and `atm config test-text-model`, which reaches
  the same endpoint with a fixed prompt and no task content. Neither sends
  anything before a credential is set;
- explicitly configured message connectors and model commands;
- release installation and update commands that download from GitHub.

ATM does not copy those credentials into its main database. External tools and
services remain governed by their own privacy policies and access controls.

## Exports and issue reports

`atm session export` can contain raw conversation text. Treat exports as
sensitive. Do not attach an ATM database, export, transcript, configuration, or
screenshot to a public issue without reviewing and redacting it first.

## Multi-user systems

ATM is intended for a single local OS user. It does not provide tenant
isolation. On shared machines, use OS account separation and filesystem
permissions to protect the ATM data directory and upstream agent data.
