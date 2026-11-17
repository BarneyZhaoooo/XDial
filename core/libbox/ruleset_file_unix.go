//go:build !windows

package libbox

import (
	"golang.org/x/sys/unix"
	"os"
)

// Nonblocking open prevents a replaced FIFO from blocking configuration
// preparation before the same descriptor's regular-file check.
func openRuleSetFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
