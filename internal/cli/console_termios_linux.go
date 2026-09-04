//go:build linux

package cli

import "golang.org/x/sys/unix"

// getTermios is the ioctl(2) request that reads a terminal's attributes. Linux
// spells it TCGETS; the BSDs, macOS included, spell the same request TIOCGETA.
// That constant is the entire platform difference in this package, so it is the
// only thing the build tags split — isTerminal itself stays one implementation.
const getTermios = unix.TCGETS
