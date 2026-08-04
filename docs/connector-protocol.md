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
      "timeout_seconds": 45
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
      "content": "Please prepare the release"
    }
  ],
  "cursor": 1785772860
}
```

`cursor` is the newest point safe to checkpoint. If omitted, ATM uses the
largest `created_at` returned. An empty successful response is
`{"messages":[],"cursor":<unchanged-or-safe-time>}`.

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

## CLI example

```sh
atm collect source search deploy --connector slack --kind channel
atm collect source add --connector slack --kind channel --id C123 --name deploys
atm collect run --source cs_...
atm collect history cs_... --limit 50
atm collect search failure --source cs_...
```
