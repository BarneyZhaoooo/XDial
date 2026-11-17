//go:build !windows

package libbox

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestRuleSetReadRejectsFIFOAndOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rule")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readValidatedNERuleSet(path, "source", 1024); err == nil {
		t.Fatal("FIFO admitted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"rules":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readValidatedNERuleSet(path, "source", 8); err == nil {
		t.Fatal("oversize admitted")
	}
	if _, _, _, err := readValidatedNERuleSet(path, "source", 1024); err != nil {
		t.Fatal(err)
	}
}
