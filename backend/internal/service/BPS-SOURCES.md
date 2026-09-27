# BPS Protocol Provenance

The BPS implementation was ported from MACOS-DO/sub4api at `ca30f2141`.
The following provenance was recorded by that source project. This port retains
the independent BPS platform, account scheduling, Redis state, billing and HTTP
entry points; it does not import credential collection or plugin binaries.

- `ranxi2001/sub2api`, commit `055a1cd1470b3b866d04aad8d1514bd34cac73ab`,
  `backend/internal/service/basispoints/`: tool-envelope decoding, raw custom-tool
  markers, structured output, tool catalog and plan arguments. Source license:
  LGPL-3.0, matching this repository.
- `Nonary/ghcp_proxy`, commit `ad23ce2db3b5212c0355762d981c3877322fb160`,
  `excel_upstream.py` and `responses_replay_ids.py`: tool tunneling, call replay,
  encrypted reasoning, plan-success receipts and result IDs. Source license:
  Unlicense.
- `1812095643/sub2api-excel2api-plugin`, commit
  `ad2039077e5a625c846ed405d3473f7bc74b601a`: comparison of tool/args envelopes,
  turn continuity and result replay; no plugin binaries or static tool catalog
  were imported.

The referenced Go package's NOTICE also attributes the original BPS protocol
to `hloolx/codex2api` (whose README declares MIT), at commits
`9d02d3f5e5d69632ebb9590082a833c0a0916356`,
`c125e560eefb5fd15c995943eb1e111795b0635f`,
`20ff3e860d9a149e2df731e37ba1d9b56ae053fc`,
`d39f7e3697aab342e303bf4be0142e39b6a58515` and
`4dea83ec53b7668419edd2a9a9dd40fb55fdaacd`; it also references
`JaxsonWang/cpa-plugin-oai-basispoints` at
`05b2d97efa1bd117da6bd4d362d6e88f8e483680`.
