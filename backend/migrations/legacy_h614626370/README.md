# Archived fork migrations

These are byte-for-byte historical SQL files from h614626370-del/sub2api.
They are outside the `//go:embed *.sql` migration glob and MUST NOT be moved back
to the active directory or replayed on an upstream installation.

The versioned manifests list every migration and its trimmed SHA-256 digest for
the source tags. Shared files are supplied by the unchanged upstream migration
set. Fork-only files are kept here to reproduce historical schemas in isolated
regression tests without fetching Git history, and to preserve an audit trail.

The active runner retains historical database ledger entries unchanged. It only
checks files in its active migration set, so the unmodified upstream runner can
take over after the bridge migration. Do not manually delete migration records
or replace their checksums.
