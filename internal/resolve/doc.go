// Package resolve determines project scope from explicit facts.
//
// Resolve is a pure function of its Request: the working directory, the
// invocation flags, the Git facts gitx gathered, and the store's Projects,
// Workspace bindings and repository locators. It never reads the environment,
// touches the filesystem, shells out, or opens SQLite, and it never writes —
// a write's Registration is something it describes for the caller to persist.
package resolve
