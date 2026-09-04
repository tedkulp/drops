//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package cli

import "golang.org/x/sys/unix"

// getTermios is the ioctl(2) request that reads a terminal's attributes. See
// the linux file beside this one: same request, different spelling.
const getTermios = unix.TIOCGETA
