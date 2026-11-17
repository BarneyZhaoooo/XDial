//go:build !windows

package libbox

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func tryLockStateFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errTailscaleStateInUse
	}
	return err
}

func unlockStateFile(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }
