//go:build !dropscutover

package store

// realStoreAllowed is false for every ordinary build. The replacement binary is
// developed against a scratch store while the old build keeps sole ownership of
// ~/.drops/drops.db, and a half-built binary must not be able to open the store
// that carries its own construction plan.
//
// The cutover ticket flips this by building with -tags dropscutover; nothing at
// runtime can turn it on.
const realStoreAllowed = false
