# Quota provider protocol

ATM can render quota cards from private or third-party executables without
linking service-specific endpoints, credentials, or page parsers into the
public core.

## Registration

Register providers in `~/.atm/config.json`:

```json
{
  "quota_providers": {
    "example": {
      "command": "~/.local/bin/atm-quota-example",
      "args": ["--profile", "work"],
      "timeout_seconds": 10
    }
  }
}
```

Provider and card identifiers match `[a-z][a-z0-9_-]{0,63}`. ATM expands a
leading `~/` in the command path, starts providers without a shell, runs all
configured providers concurrently, and limits each provider to 1 MiB of stdout
and 64 KiB of stderr. The default timeout is 10 seconds.

## Request

ATM appends `quota` to the configured arguments and writes one JSON object to
stdin:

```json
{"version":1,"operation":"quota"}
```

## Response

The provider writes one JSON object to stdout:

```json
{
  "version": 1,
  "cards": [
    {
      "id": "daily",
      "agent": "claude",
      "provider": "example",
      "title": "Team plan",
      "period": "Today",
      "observed_at": "2026-08-04T03:28:37Z",
      "source": "browser",
      "metrics": [
        {
          "id": "count",
          "label": "Daily requests",
          "used": 428,
          "limit": 4000,
          "unit": "requests"
        },
        {
          "id": "amount",
          "label": "Daily amount",
          "used": 266.58,
          "limit": 1200,
          "currency": "CNY",
          "precision": 2
        }
      ]
    }
  ]
}
```

`provider` may be omitted and defaults to the configured provider ID. ATM
validates finite non-negative `used` values, positive `limit` values, RFC 3339
timestamps, unique metric IDs, and a precision from 0 through 6. It computes
`used_percent`; providers cannot supply a conflicting percentage.

A provider may return several cards and agents. ATM merges the resulting
`provider_cards` into the normal `atm quota --json` entry for each agent, so a
built-in rate-limit window and external cards can coexist.

To report a service-level failure with exit status zero, return:

```json
{"version":1,"error":"account page has not been observed today"}
```

Provider failures are warnings and do not hide healthy built-in or other
provider cards. Credentials, network access, freshness policy, and any native
browser bridge remain the provider's responsibility.
