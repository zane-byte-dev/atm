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
      "timeout_seconds": 10,
      "visible_metrics": ["amount"]
    }
  }
}
```

Provider and card identifiers match `[a-z][a-z0-9_-]{0,63}`. ATM expands a
leading `~/` in the command path, starts providers without a shell, runs all
configured providers concurrently, and limits each provider to 1 MiB of stdout
and 64 KiB of stderr. The default timeout is 10 seconds.

`visible_metrics` is optional. When present, ATM keeps only those metric IDs in
CLI and App output and drops cards with no selected metrics. Omit it (or use an
empty list) to show every metric. For example, an Idealab provider that returns
`count` and `amount` can use `"visible_metrics": ["amount"]` to show only the
amount row.

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
      "url": "https://example.com/account",
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

`url` is optional and names the page this reading came from — the account page
where the numbers can be seen in full or the quota topped up. It must be an
absolute `http` or `https` URL of at most 2048 bytes; any other scheme is a
provider error, because the App hands this address to the system browser. The
App makes the whole card and its quick-panel rows clickable, and `atm quota`
prints it as `Page:`.

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

## Missing readings

Returning `"cards": []` means "nothing to report right now" — a daily quota
before the day's first observation, a bridge that is not running. That is not an
error and produces no warning, including when `visible_metrics` is set.

ATM remembers the last cards each provider returned in
`~/.atm/quota_provider_cards.json`. While a provider reports nothing or fails,
those cards stay on screen as placeholders instead of disappearing: same agent,
provider, title, period, `observed_at`, and `url` — a missing reading is exactly
when "where do I refresh this" is most worth a click — with empty `metrics`, no
`source`, and

```json
{"unavailable": true, "unavailable_reason": "empty"}
```

added by ATM — `"error"` when the provider could not be run. Providers cannot set
either field; ATM strips both from a response. The CLI prints `no data (…)` where
the numbers would be and labels the timestamp `Last observed`. The App renders
暂无数据 / 读取失败 on the card and keeps it out of the menu bar and its tooltip,
which report readings only.

A provider's first successful run is what puts its card on screen: before that
there is nothing to hold in place. Placeholders stop after seven days without a
reading, and removing a provider from `config.json` drops its cards at once.
