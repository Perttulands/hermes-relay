# Code Review

Scope: `main` branch as of 2026-02-19.

---

## Dead Code / Unused Files

- `internal/cli/exec.go` — 6-line file that exists solely to declare
  `var execCommand = exec.Command` so tests can monkey-patch it. Fine, but
  the pattern is fragile and the file is easy to miss.
- `store.ReservationHash()` is exported but never called outside the package.
  Should be unexported (`reservationHash`).

---

## Obvious Bugs

- **Unchecked `f.Close()` in `ConsumeCommand`** (`store.go:455`): the `.consumed`
  sidecar file is closed without checking the error. A failed close could leave
  a corrupt sidecar and allow a second consumer to claim the same command.
- **`json.Unmarshal` errors dropped in tests** (`cli_test.go:77`, `~:117`):
  silent unmarshal failures cause the test to assert against a zero-value struct
  rather than fail loudly, masking real regressions.

---

## Missing Error Handling

- `cli.go` status command discards errors wholesale:
  ```go
  agents, _ := c.store.ListAgents()
  reservations, _ := c.store.ListReservations()
  meta, _ := c.store.ReadMeta(name)   // inside loop
  ```
  A failing store read silently shows incomplete output with no indication
  something went wrong.
- `os.UserHomeDir()` error is silently ignored (`cli.go:51`); the fallback is an
  empty string, producing a `/.relay` default dir — probably harmless but worth
  a log line.

---

## Inconsistencies Between Docs and Code

- **README vs `go.mod`**: README lists `bd` (Beads CLI) v0.46.0 as a dependency,
  but `go.mod` only has `github.com/oklog/ulid/v2`. `bd` is an external runtime
  tool, not a Go module — the README should clarify it is a *runtime* requirement,
  not a Go dependency.
- **`wake` command**: code references "Athena gateway" and writes a trigger file;
  README makes no mention of Athena or the wake protocol, leaving the feature
  undocumented.
- **"per review §3.2" comments** in `store.go`: reference an in-repo review
  document that does not exist on `main` (it lives only on the abandoned branch
  as `REVIEW.md`).
- **Version**: `cli.go` declares `const version = "0.1.0"` but there is no
  corresponding git tag.

---

## TODO / FIXME / HACK Comments

None found in the codebase.

---

## Other Observations

- Pattern overlap detection (`patternsOverlap`) is conservative
  (prefix-matching only). Edge cases such as `**/*.go` vs `internal/**` are
  not handled, so the function may allow conflicting reservations for complex
  glob patterns.
- `ReadInbox` tolerates partial trailing lines (good for crash recovery) but
  silently skips malformed JSON lines mid-file without any warning, making
  corruption hard to diagnose.
- The stress test (`TestStressNoLoss`: 20 K messages, 20 writers) is excellent
  and should be kept in CI.
