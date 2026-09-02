package calc

// Less reports whether a is strictly less than b.
func Less(a, b int) bool {
	return a < b
}

// Clamp holds n within [lo, hi].
func Clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
