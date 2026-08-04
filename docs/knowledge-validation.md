# ATM central knowledge validation

## Automated verification

```bash
GOCACHE=/private/tmp/atm-go-cache go test ./...
```

Covered behavior:

- explicit add/import writes readable canonical Markdown under `~/.atm/knowledge/<collection>`;
- repeated import of the same source updates a stable document ID instead of duplicating it;
- directory import preserves readable source-relative directories and filenames while keeping the stable ID in frontmatter;
- collection manifests produce a compact routing catalog for LLMs before retrieval;
- collection, domain, tag, and project metadata filter one central knowledge search;
- search returns stable document IDs, snippets, and line ranges without a workspace dependency;
- shared memory uses append-only `~/.atm/memory/events.jsonl` with scope-aware supersede/forget;
- unrelated recent memories are excluded from lexical recall;
- artifacts use unique IDs and atomic writes under `~/.atm/artifacts`;
- Knowledge and Memory are exposed through one deterministic CLI surface;
- schema v9 removes legacy session FTS objects and session search treats code and paths literally.

## Data ownership

- ATM-owned Knowledge, Memory, Artifact, Todo task-run evidence, and observation data remain under `~/.atm`.
- External documents enter Knowledge only through explicit `knowledge add/import` operations.
- Runtime search does not inspect project directories or another product's private data layout.
- Knowledge is one logical second brain; domain, tag, and project are metadata views.

## Recovery properties

- Markdown documents, memory events, and artifacts are canonical file-backed records.
- Observation SQLite can be rebuilt from Agent transcripts.
- A final incomplete memory JSONL record is ignored as an interrupted append; malformed records before the tail fail with a path and line number.
- Knowledge and artifact writes use a temporary sibling file, `fsync`, close, and rename.
