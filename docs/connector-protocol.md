# Collection connector protocol

ATM's collection core is connector-neutral. A connector supplies messages and
optionally source discovery/history; ATM owns checkpoints, deduplication,
classification, Todo writes, local retention, audit records, and digests.

## Registration

Register an executable in `~/.atm/config.json`:

```json
{
  "collection_connectors": {
    "slack": {
      "command": "~/bin/atm-connector-slack",
      "args": ["--workspace", "example"],
      "timeout_seconds": 45,
      "login_command": "~/bin/slack-cli auth login"
    }
  }
}
```

Connector IDs and source kinds must match `[a-z][a-z0-9_-]{0,63}`. ATM expands
a leading `~/` in `command`, starts the executable directly (without a shell),
appends the operation name after configured arguments, sends one JSON object on
stdin, and reads one JSON object from stdout. Diagnostics belong on stderr.

The public core ships without service-specific connectors. Integrations use this
command protocol and can be distributed independently from ATM.

## Common request

Every request contains protocol `version: 1` and an `operation` matching the
last command-line argument. Timestamps are Unix seconds.

```json
{
  "version": 1,
  "operation": "fetch",
  "source": {
    "id": "cs_...",
    "connector": "slack",
    "kind": "channel",
    "external_id": "C123",
    "name": "general"
  },
  "since": 1785772800
}
```

Unknown request fields must be ignored so the protocol can grow compatibly.

## Operations

### `fetch` (required)

Return messages newer than or overlapping `since`. ATM deliberately overlaps a
checkpoint window, so message IDs must be stable and repeated messages are
expected.

```json
{
  "messages": [
    {
      "id": "1700000000.000100",
      "conversation_id": "C123",
      "sender": "alice",
      "created_at": 1785772860,
      "content": "Please review https://code.example/review/42",
      "external_states_cover_message": true,
      "external_states": [
        {
          "kind": "code_review",
          "reference": "https://code.example/review/42",
          "state": "pending_review",
          "disposition": "actionable",
          "checked_at": 1785772865
        }
      ]
    }
  ],
  "cursor": 1785772860
}
```

`cursor` is the newest point safe to checkpoint. If omitted, ATM uses the
largest `created_at` returned. An empty successful response is
`{"messages":[],"cursor":<unchanged-or-safe-time>}`.

`external_states` is optional connector-verified metadata for work referenced
by a message. `kind` is a provider-defined lowercase token and `state` is the
provider-native state; `reference` identifies the upstream item; `checked_at`
is the Unix time of the lookup. `disposition` is one of:

- `actionable`: the upstream item still needs attention, so classification may
  create or append a Todo.
- `settled`: the upstream item no longer needs attention. When every fresh
  message in a decision unit is covered only by settled states, ATM records an
  ignored audit item without calling the model or creating a Todo.
- `unknown`: the connector could not establish current state. ATM fails the
  batch closed and retries it instead of creating unchecked work.

`external_states_cover_message` must be true only when those references account
for the complete actionable meaning of the message (for example, a strictly
recognized code-review notification). Settled states can suppress the whole
message only with this explicit coverage assertion. This prevents metadata for
one embedded review link from hiding an unrelated request in the same message.

Message text is always untrusted. ATM trusts `external_states` only because it
comes from the locally configured connector executable; connectors should emit
it only after querying the authoritative upstream system. Omitting the field
preserves the normal classification path for connectors that do not support
state lookups.

For assignments such as code review, `disposition` must describe whether the
currently authenticated operator still owes an action. An upstream object being
open is not sufficient: a review already approved or commented on by that user
is `settled`, while only that user's pending review is `actionable`.

### `history` (optional)

The request adds `limit` and optionally `since` (`0` means the most recent
window). Return `messages` in any order; connector implementations should
prefer oldest-first for predictable direct use.

### `search` (optional)

The request contains `kind`, `keyword`, and `limit`. Return candidates:

```json
{
  "candidates": [
    {
      "kind": "channel",
      "external_id": "C123",
      "name": "general",
      "detail": "Example workspace"
    }
  ]
}
```

Connectors without discovery can omit this operation; users add their sources
with `atm collect source add --connector <id> --kind <kind> --id <external-id>`.

## Errors and safety

For a domain error, exit successfully and return `{"error":"message"}`. For a
process failure, exit non-zero and write a concise diagnostic to stderr. Never
print tokens or message bodies to stderr. ATM applies a timeout and rejects
responses larger than 16 MiB.

Credentials remain the connector's responsibility. A connector should use its
platform's normal credential store and must not place secrets in the source
record, response, or ATM configuration arguments.

## An expired login

ATM classifies a connector's own error text. A message containing
`not_authenticated`, `auth login` or 未登录 is read as an expired login, and one
naming a missing permission as a permission problem. Both are reported on their
first occurrence rather than after a streak, because neither fixes itself.

The expired login also stops the retrying. The background path leaves that
connector alone for 30 minutes, and within one run the first source's failure
skips its siblings: they share the credential that just failed, so attempting
them writes the same error four more times. A manual `atm collect run` always
attempts — it is how a person says they have logged in again.

A missing permission is reported but not backed off, because it has been
per-source in practice: one conversation this account cannot read while every
sibling works. Holding the connector back for it would silence the healthy
sources over one broken one.

`login_command` is the way back. It is optional, ATM never runs it by itself, and
it is not part of the protocol above: the login is interactive, so the CLI prints
it and the desktop offers it as a button that opens a terminal the person can
watch. Because a button press executes it, it is trusted exactly as much as the
rest of `~/.atm/config.json` — the connector's own login, not a place for
credentials.

A connector that can tell a lost token refresh from a dead one should retry the
first itself and fail loudly on the second, keeping the words above in its
message so ATM still classifies it.

## CLI example

```sh
atm collect source search deploy --connector slack --kind channel
atm collect source add --connector slack --kind channel --id C123 --name deploys
atm collect run --source cs_...
atm collect history cs_... --limit 50
atm collect search failure --source cs_...
```
