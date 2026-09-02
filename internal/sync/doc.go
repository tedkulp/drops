// Package sync coordinates the two-machine Git transport over core, mirror,
// gitx, and lockfile.
//
// The store is one SQLite database at path P. Beside it sit the transport's own
// local files, all owned here and none of them replicated:
//
//   - mirror.jsonl — the deterministic JSONL snapshot (mirror.Marshal), the only
//     file that crosses machines.
//   - replica.json  — the sidecar holding this installation's active replica key
//     and last export head.
//   - .sync.lock    — the advisory flock serialising a sync across processes.
//   - .git/         — the mirror's history, pushed to and pulled from a shared
//     remote.
//
// Sync never authors or rewrites replicated records. Core owns the merge rules
// (revision order, tombstone wins, identity collision, content conflict); sync
// owns everything that happens between one store and another: locking,
// staleness, deterministic export bytes, the Git commit/push/fetch, the
// predecessor-chain and fork proof, the sidecar lifecycle, and rekey.
//
// The active replica key is machine-local state. It lives in the sidecar, never
// in SQLite, and is excluded from the mirror. A store may be read and may
// receive an import without one; the sidecar is minted the first time a local
// write must be exported, reused on every ordinary reopen, and a present but
// malformed sidecar is a hard error rather than something silently replaced.
// sync import never changes the destination's active key.
//
// Fork proof is layered. Core refuses, atomically, an opaque-id arriving under a
// different creation replica (identity collision) and one exact
// (generation, replica_key) revision carrying different canonical state (content
// conflict). Sync adds the chain-level proof: every export names its predecessor
// heads, so a fetched snapshot that does not descend linearly from the head this
// store last accepted from that replica is a fork and is refused before anything
// is committed. Identical exports collapse to one content hash.
package sync
