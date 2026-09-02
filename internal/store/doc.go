// Package store owns SQLite: the connection, the v7 schema, statement
// construction, typed reads and writes, and the three external-content FTS5
// indexes. Nothing above this package writes SQL.
//
// It owns no domain transaction boundary. Reads hang off Store, writes only off
// Tx, and a caller composes several writes atomically with WithTx — so the rules
// layer above can make one transaction out of an ID reservation and the entity
// that claims it, or out of a whole import, without any of that sinking to here.
//
// Every write is a compare-and-swap on the revision the caller observed, and
// this is the only layer that can tell a missing record from one another writer
// moved on, so it mints ErrNotFound and ErrConflict itself.
//
// Every write also bumps a monotonic counter of replicated writes. That is a
// fact about the store rather than a sync concept leaking downward: sync happens
// to be its only reader. Local records — workspace bindings, replica heads,
// succession, the export mark — deliberately do not bump it.
//
// There is no Store interface. There is one SQLite adapter and there will only
// ever be one, so an interface here would be a seam with nothing on the other
// side of it; every test runs against a real temporary database.
package store
