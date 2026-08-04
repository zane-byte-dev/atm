# Collection model runners

Automatic collection classifies chat with an agent CLI running headless under a
JSON output schema. ATM drives `codex` and `grok` out of the box; any other CLI
can be taught to ATM from `~/.atm/config.json`, without a code change.

## Choosing the model

`collection_model_command` is an ordered chain, tried left to right:

```json
{
  "collection_model_command": "grok,codex,rule"
}
```

or from the CLI:

```
atm config set collection_model_command "grok,codex"
```

The next candidate runs when the previous one is rate limited, exits non-zero,
times out, is not installed, or answers in a shape ATM cannot read. A candidate
that answers with malformed JSON for the schema is a hard failure instead — the
prompt or the schema is wrong, and spending another model's quota on the same
mistake helps nobody.

A candidate may carry its own flags (`"grok --effort low,codex"`); they are
placed ahead of the profile's flags, where both CLIs expect global options.
Candidates are separated by commas, so a candidate whose own flags contain a
comma has to be declared as a runner (below) instead.

`rule` is the local high-confidence keyword classifier. Alone it means "never
call a model"; at the end of a chain it is the last resort when every model is
unavailable, and the resulting item records that it was a degraded decision.
Digests are prose and have no rule implementation, so `rule` is ignored there.

## Built-in profiles

| Command | How ATM calls it | Where the answer is read from |
| --- | --- | --- |
| `codex` | `exec --ephemeral --ignore-user-config --ignore-rules --skip-git-repo-check --sandbox read-only --output-schema <file> -`, prompt on stdin | stdout, fences trimmed |
| `grok` | `--prompt-file <file> --verbatim --json-schema <schema> --sandbox read-only --no-memory --no-subagents --disable-web-search` | `structuredOutput` in the run envelope, falling back to `text` |

Both run in a throwaway working directory named `atm-collection-model-*` and get
no writable filesystem. The isolation is not identical: codex is fully
contained, while grok has no equivalent of `--ignore-user-config` or
`--ignore-rules` and still reads `~/.grok/config.toml`. What can be denied
(memory, subagents, web search, writes) is denied, and the scratch directory
holds no rules file to pick up.

`--verbatim` is not optional for grok. Without it a chat line starting with `/`
or containing `@path` is expanded as a command or a file reference, and these
messages are untrusted input by definition.

Grok records a session per working directory, including these scratch runs. ATM
skips any session whose directory carries the `atm-collection-model-` marker, so
classification never appears in `atm session list` or `atm stats` — which also
means the classifier's own token spend is not counted there.

## Custom runners

Register other CLIs under `collection_model_runners`. The key is the name used
in `collection_model_command`; `command` defaults to the key, so a key can also
be an alias for one binary with particular flags:

```json
{
  "collection_model_command": "grok-fast,codex",
  "collection_model_runners": {
    "grok-fast": {
      "command": "~/.grok/bin/grok",
      "args": [
        "-m", "grok-4-fast",
        "--prompt-file", "{{prompt_path}}",
        "--verbatim",
        "--json-schema", "{{schema_json}}",
        "--sandbox", "read-only",
        "--no-memory", "--no-subagents", "--disable-web-search"
      ],
      "output_field": "structuredOutput",
      "timeout_seconds": 120
    }
  }
}
```

`args` is a template. ATM substitutes, per run:

| Placeholder | Value |
| --- | --- |
| `{{schema_path}}` | Path to the JSON schema, written into the scratch directory |
| `{{schema_json}}` | The schema itself, as one argument |
| `{{prompt_path}}` | Path to the prompt, written into the scratch directory |
| `{{workdir}}` | The scratch directory, which is also the process working directory |

A template that names no prompt placeholder receives the prompt on stdin. Schema
and prompt files are written only when the template asks for them.

`output_field` is for CLIs that wrap the answer in a run envelope: ATM reads
that field, or `text` if it is absent, and accepts either a JSON object or a
JSON string containing one. Omit it when the CLI prints the object directly.

`command` may start with `~/`. A key that matches a built-in profile overrides
it, which is the supported way to change how ATM calls codex or grok.

A custom runner must carry its own sandbox flags. ATM cannot know how a
third-party CLI denies network access, filesystem writes, or user rules, and it
will not pretend the model is contained when it is not — this prompt carries
private chat.

## Checking it

```
atm config get collection_model_command
atm doctor --json | jq '.issues[] | select(.code == "collection_model_unavailable")'
atm collect run --source <source-id>
atm collect status
```

`atm doctor` warns only when collection is enabled and no candidate in the chain
is installed; a chain that still has one working CLI is healthy by design.
