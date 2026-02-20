# Relay Message Schema

Relay stores messages as NDJSON records in `agents/<name>/inbox.jsonl`.

## Schema

### Required fields

- `id` (string): message identifier (ULID in Relay-generated messages)
- `from` (string): sender agent name
- `to` (string): recipient agent name
- `ts` (string): RFC3339 UTC timestamp
- `body` (string): message content

### Optional fields

- `subject` (string): short summary line
- `thread` (string): conversation/thread identifier
- `tags` (array of strings): labels for filtering/routing
- `priority` (string): message priority label
- `reply_to` (string): parent message id when replying

## Validation

`internal/core.Message.Validate()` enforces:

- All required fields are present and non-empty
- `ts` parses as RFC3339
- `body` does not exceed `core.MaxBodySize` (64 KiB)
- Optional string fields are not whitespace-only when set
- `tags` cannot contain empty entries

## Backward compatibility

Relay keeps compatibility with legacy ad-hoc message writers:

- `store.Send` auto-fills missing `id` with a new ULID
- `store.Send` auto-fills missing `ts` with current UTC RFC3339 time
- `store.Send` defaults missing `subject` to `body` (trimmed to 80 chars)
- `store.Send` defaults missing `priority` to `normal`

Existing inbox messages remain readable; schema validation is applied at send-time for newly written messages.
