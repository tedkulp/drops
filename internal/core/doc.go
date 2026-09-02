// Package core owns drops rules and every domain transaction boundary.
//
// Store supplies typed SQLite reads and single-record compare-and-swap writes;
// Core composes them into permanent ID reservation, entity lifecycle,
// relationship, supersession, import, export, and integrity operations. It
// accepts only Clock, IDSource, and WarnSink collaborators. Filesystem paths are
// opaque binding values here: Git, locking, path discovery, rendering, and CLI
// policy remain above this package.
package core
