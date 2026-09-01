## ATM task management

ATM is the single source of truth for cross-project work state.

At the start of project work:

- Read the compact `ATM current` or `ATM candidates` line injected at session start.
- Keep the current binding if it still matches the request; otherwise run `atm session unbind --reason scope-changed`.
- Bind a matching candidate with `atm session bind <id>`. If the candidates are insufficient, run `atm todo match "<goal>" --prompt`.
- Stay unbound when no todo fits. Search before creating; use `atm now --json` only for a genuinely global attention view.

While working:

- After a meaningful milestone, run `atm todo log "<progress>"` for the bound todo.
- When Agent implementation is complete, run `atm todo submit --reason "<result and evidence>"`. This moves work to `review`; it never marks it done.
- Never run `atm todo done` as an Agent. `done` is the human acceptance decision after review.
- When work pauses on an external condition, run `atm todo edit <id> --wake "<observable condition>"` while the todo remains `in_progress`.
- Use `atm todo start <id>` to begin open work. Reopening review/done requires `--reopen-reason`; binding a review Todo requires the same flag. Use `atm todo edit <id> --status open` to return work to the backlog, and `atm todo edit` for priority, scope, or maintenance metadata.

Bound commands may omit the todo ID. `submit`, `archive`, and waiting metadata automatically unbind the session.

Control task count:

- Do not create an ATM todo for a small change that can be completed in the current turn.
- Create one only for cross-session work, a clear project mainline, an externally waiting item, or an explicit tracking request.
- Check for an existing matching todo first; never create duplicates.
- Use the concrete repository name as the ATM project, not container directories such as `work` or `mox`.

Before the final response:

- Reconcile material progress or state changes back into ATM.
- If implementation completed, submit it for review; do not silently convert Agent completion into human acceptance.
- Do not modify unrelated ATM todos.
