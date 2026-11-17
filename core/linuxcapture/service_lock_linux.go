//go:build linux && !android

package linuxcapture

import "io"

type serviceLock struct{ state *stateDir }

func (l *serviceLock) Close() error {
	if l.state != nil {
		l.state.close()
		l.state = nil
	}
	return nil
}

// LockServiceDirectory applies the journal's root-only, no-symlink path and
// lock checks to the parent service state directory. Capture uses a separate
// child directory, so this lock remains held even while disconnected.
func LockServiceDirectory(path string) (io.Closer, error) {
	s, e := openState(path)
	if e != nil {
		return nil, e
	}
	return &serviceLock{s}, nil
}
