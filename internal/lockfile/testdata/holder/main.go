// Command holder takes the lock named by argv[1], prints "held", and waits for
// stdin to close. It exists so contention tests run a real second process:
// flock is per-open-file-description, so two goroutines in one process do not
// contend the way two processes do, and a same-process test would prove the
// wrong thing.
//
// An optional argv[2] is a millisecond delay applied after stdin closes and
// before the lock is released. Without it, closing stdin and the kernel
// dropping the flock race with no guaranteed order — a caller's retry loop
// might see the lock already free on its first attempt, which proves nothing
// about the loop. The delay makes "still held after the close signal" a
// guarantee instead of a race.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/tedkulp/drops/internal/lockfile"
)

func main() {
	l, err := lockfile.Acquire(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "acquire:", err)
		os.Exit(1)
	}
	defer l.Release()
	fmt.Println("held")
	io.Copy(io.Discard, os.Stdin) // block until the parent closes stdin
	if len(os.Args) > 2 {
		ms, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad delay:", err)
			os.Exit(1)
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}
