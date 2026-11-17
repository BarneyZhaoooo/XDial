//go:build linux && !android

package linuxservice

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestConfigWatchSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"etc", "policy", "replacement"} {
		if e := os.Mkdir(filepath.Join(root, dir), 0700); e != nil {
			t.Fatal(e)
		}
	}
	file := filepath.Join(root, "policy", "nsswitch.conf")
	if e := os.WriteFile(file, []byte("hosts: files dns\n"), 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(root, "etc", "nsswitch.conf")
	if e := os.Symlink("../policy/nsswitch.conf", link); e != nil {
		t.Fatal(e)
	}
	w, e := fsnotify.NewWatcher()
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if e = watchConfigPath(w, link); e != nil {
		t.Fatal(e)
	}
	wait := func(path string) {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case event := <-w.Events:
				if event.Name == path {
					return
				}
			case e := <-w.Errors:
				t.Fatal(e)
			case <-timer.C:
				t.Fatal("configuration change not observed", path)
			}
		}
	}
	if e = os.WriteFile(file, []byte("hosts: resolve dns\n"), 0600); e != nil {
		t.Fatal(e)
	}
	wait(file)
	// Replace an intermediate link and re-arm its new target, like an
	// authselect profile switch; the old directory watch alone is insufficient.
	newFile := filepath.Join(root, "replacement", "nsswitch.conf")
	if e = os.WriteFile(newFile, []byte("hosts: files dns\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink("../replacement/nsswitch.conf", link+".new"); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(link+".new", link); e != nil {
		t.Fatal(e)
	}
	wait(link)
	if e = watchConfigPath(w, link); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(newFile, []byte("hosts: resolve dns\n"), 0600); e != nil {
		t.Fatal(e)
	}
	wait(newFile)
	if e = os.Symlink("loop", filepath.Join(root, "loop")); e != nil {
		t.Fatal(e)
	}
	if watchConfigPath(w, filepath.Join(root, "loop")) == nil {
		t.Fatal("symlink cycle admitted")
	}
}
