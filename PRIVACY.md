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

Indexed agent sessions can outlive their upstream transcript files. Use
`atm session forget` after the source transcript has been removed and the index
has been synchronized. Connector messages are retained for 90 days by default;
set `collection_message_retention_days` to a shorter period when appropriate.

Back up, share, and delete the ATM data directory with the same care as the
underlying transcripts and messaging data.

## Network and external-process activity

Most commands operate on local files. Network or external-process activity is
limited to features the user invokes or enables, including:

- `atm config update-pricing`, which downloads public model pricing from
  OpenRouter;
- opt-in live Grok quota lookup, which uses the locally stored Grok credential;
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
