// Package mirror defines the deterministic JSONL transport representation for
// drops replicated records.
//
// A mirror is one snapshot: a header line naming the predecessor snapshot
// heads, followed by one line per typed record. Every line is a single JSON
// object carrying a "kind" discriminator; record lines wrap the model record
// under "record". Marshal emits records in canonical (kind, key) order and
// sorts predecessor heads, so two semantically equal snapshots are
// byte-identical. Parse validates every line and rejects malformed input.
//
// Mirror owns the wire bytes and the canonical encoding of composite record
// keys. It holds no git, filesystem, SQLite, or merge policy.
package mirror
