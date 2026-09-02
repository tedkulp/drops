// Package memory owns every inference drops performs over memory text and
// supersession chains: deriving a title from a body, folding a legacy kind into
// searchable prose, normalising optional provenance, and validating supersession
// edges. It is pure — no I/O, no clock, no randomness, no database — so the
// whole of it is testable from string literals, and the cutover, import, and
// service layers consume it rather than each growing their own copy.
//
// Memory depends on model only for the shared value types its validation
// surface carries (model.ID, model.ProjectKey). It holds no SQL, no filesystem,
// and no merge policy.
package memory
