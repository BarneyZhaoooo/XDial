//go:build windows

package engine

import (
	"golang.org/x/sys/windows"
	"testing"
	"unsafe"
)

func TestOwnedWindowsJobContainsChildAndRetiresDescendants(t *testing.T) {
	path := fixturePath(t, "spawn")
	cmd, closer, ready := ownedStart(t, path)
	grandchild := readOwnedReady(t, path+".grand")
	job := closer.(*windowsProcessLease)
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(ready.PID))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	var member int32
	result, _, callErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob").Call(uintptr(process), uintptr(job.job), uintptr(unsafe.Pointer(&member)))
	if result == 0 || member == 0 {
		t.Fatal("child not in exact owner Job", callErr)
	}
	echoOwned(t, grandchild.Address)
	job.Close()
	waitOwnedCommand(t, cmd)
	waitOwnedClosed(t, ready.Address)
	waitOwnedClosed(t, grandchild.Address)
}
