# Ansible listing accepts more than its output cap without an error

**Symptoms:** `oversized observation accepted: 4194305 <nil>`

**First seen:** 2026-09

**Affects:** local workspace/Preview implementation before the bounded-buffer fix

**Status:** fixed in local source; regression test retained

## Symptom

The preview overflow test produced 4 MiB + 1 byte, but the observation accepted
all of it and returned no error:

```text
oversized observation accepted: 4194305 <nil>
```

A direct test of `boundedBuffer.Write` would miss this failure: it occurred while
copying real child-process output through the process wrapper.

## Root cause

`boundedBuffer` anonymously embedded `bytes.Buffer`. Its promoted `ReadFrom`
method satisfied `io.ReaderFrom`, allowing the process-output copy path to bypass
the custom capped `Write` method. The apparent 4 MiB guard therefore did not
bound every write path.

## Fix

The implementation in [process.go](../internal/ansible/process.go) now stores the
buffer in a named field, provides only explicit `Bytes`/`String` accessors, and
routes writes through the capped method. It no longer inherits `ReadFrom`.
No user configuration change is needed.

## Prevention

Keep the subprocess-level case in
[`TestPreviewCancellationCapsAndKindGuard`](../internal/ansible/preview_test.go).
The oversized fixture must return an error, retain no more than the configured
cap, and leave `Parsed` false. The targeted race test and domain race suite passed
after the fix. When wrapping standard I/O types, inspect promoted interfaces as
well as the methods you explicitly override.

## Related

- [Workspace and execution preview](../backlog/workspace-execution-ux.md)
