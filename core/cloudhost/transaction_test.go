package cloudhost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func deploymentSession(t *testing.T, server *testServer) *Session {
	t.Helper()
	s, err := Connect(context.Background(), Target{Host: "127.0.0.1", Port: server.port(), User: server.user, Password: server.password, HostKeyFingerprint: Fingerprint(server.key.PublicKey())})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.privilege = privilegeRoot
	return s
}

func TestMoveFailureRestoresConfigAndKeepsJournalOnUndoFailure(t *testing.T) {
	for _, undoFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "undo succeeds", true: "undo fails"}[undoFails], func(t *testing.T) {
			server := newTestServer(t, "root", "secret")
			var journal state
			restored := false
			server.respond = func(cmd, input string) (string, int) {
				if strings.Contains(cmd, statePath+".xdial-tmp") && input != "" {
					journal = state{}
					if err := json.Unmarshal([]byte(input), &journal); err != nil {
						t.Error(err)
					}
				}
				if strings.Contains(cmd, " check -c ") {
					if !journal.InProgress || !journal.created("config_moved") || journal.Baseline == nil {
						t.Error("config changed without durable rollback intent")
					}
					return "", 1
				}
				if strings.Contains(cmd, "cp -p "+configPath+".previous ") {
					restored = true
					if undoFails {
						return "", 1
					}
				}
				if strings.Contains(cmd, "rm -rf "+configDir) || strings.Contains(cmd, "rm -f "+unitPath) {
					t.Error("existing resources must survive")
				}
				return "", 0
			}
			s := deploymentSession(t, server)
			f := ubuntuFacts()
			f["singbox_path"] = "/usr/bin/sing-box"
			config, _ := serverConfigJSON(31000, "AAAAAAAAAAAAAAAAAAAAAA==")
			f["xdial_config"] = encodeTestConfig(config)
			f["xdial_service"] = "active"
			s.facts = f
			r := evaluate(f, privilegeRoot)
			s.report = &r
			_, err := s.Deploy(context.Background(), 32000, nil)
			if err == nil || !restored {
				t.Fatalf("err %v restored %v", err, restored)
			}
			if undoFails && (!journal.InProgress || AsError(err).Code != "undo_incomplete") {
				t.Fatal("failed compensation erased journal")
			}
			if !undoFails && journal.InProgress {
				t.Fatal("successful rollback left dirty state")
			}
		})
	}
}

func TestFailedJournalPreventsMutation(t *testing.T) {
	server := newTestServer(t, "root", "secret")
	changed := false
	server.respond = func(cmd, input string) (string, int) {
		if strings.Contains(cmd, statePath+".xdial-tmp") && input != "" {
			return "", 1
		}
		if strings.Contains(cmd, "apt-get install") || strings.Contains(cmd, aptKeyPath+".xdial-tmp") {
			changed = true
		}
		return "", 0
	}
	s := deploymentSession(t, server)
	s.facts = ubuntuFacts()
	r := evaluate(s.facts, privilegeRoot)
	s.report = &r
	if _, err := s.Deploy(context.Background(), 32000, nil); err == nil || changed {
		t.Fatalf("err=%v changed=%v", err, changed)
	}
}

func TestRollbackStopsBeforeRemovingRecoveryRecord(t *testing.T) {
	removed := false
	s := &Session{}
	if s.rollback(context.Background(), []undo{{step: "directory", run: func(context.Context) error { removed = true; return nil }}, {step: "package", run: func(context.Context) error { return errors.New("apt locked") }}}, func(Event) {}) || removed {
		t.Fatal("rollback must retain journal after failure")
	}
}

func TestCancellationUnblocksChannelOpening(t *testing.T) {
	server := newTestServer(t, "root", "secret")
	server.holdChannels = true
	s := deploymentSession(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.run(ctx, "true", nil); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("channel open ignored cancellation")
	}
}

func encodeTestConfig(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

func TestSudoCachedAuthenticationCannotPrefixFileWithPassword(t *testing.T) {
	server := newTestServer(t, "deploy", "password")
	s := deploymentSession(t, server)
	s.privilege = privilegeSudoPassword
	if _, err := s.runPrivileged(context.Background(), "cat", []byte("file contents")); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	command := server.commands[len(server.commands)-1]
	input := server.stdin[len(server.stdin)-1]
	server.mu.Unlock()
	dir := t.TempDir()
	// Simulate sudo accepting cached credentials without reading its stdin.
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte("#!/bin/sh\nif [ \"$1\" = -k ]; then exit 0; fi\nshift; exec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "file contents" {
		t.Fatalf("output %q err %v", output, err)
	}
}

func TestRecoveryReinspectsExistingProxyBeforeDeploy(t *testing.T) {
	server := newTestServer(t, "root", "secret")
	baseline := state{Version: 1, CreatedAt: 10}
	interrupted := state{Version: 1, InProgress: true, Created: []string{"config_moved"}, Baseline: &baseline, WasActive: true}
	f := ubuntuFacts()
	f["singbox_path"] = "/usr/bin/sing-box"
	raw, _ := json.Marshal(interrupted)
	f["xdial_state"] = encodeTestConfig(raw)
	inspected := false
	server.respond = func(cmd, input string) (string, int) {
		if strings.Contains(cmd, "id -u") && !strings.Contains(cmd, "emit os_id") {
			return "0\n", 0
		}
		if strings.Contains(cmd, "emit os_id") {
			inspected = true
			restored := ubuntuFacts()
			restored["singbox_path"] = "/usr/bin/sing-box"
			config, _ := serverConfigJSON(31000, "AAAAAAAAAAAAAAAAAAAAAA==")
			restored["xdial_config"] = encodeTestConfig(config)
			restored["xdial_service"] = "active"
			old, _ := json.Marshal(baseline)
			restored["xdial_state"] = encodeTestConfig(old)
			var output strings.Builder
			for k, v := range restored {
				output.WriteString(k + "=" + v + "\n")
			}
			return output.String(), 0
		}
		if strings.Contains(cmd, unitPath+".xdial-tmp") {
			t.Error("recovery treated original service as fresh")
		}
		if strings.Contains(cmd, " check -c ") {
			return "", 1
		}
		return "", 0
	}
	s := deploymentSession(t, server)
	s.facts = f
	r := evaluate(f, privilegeRoot)
	s.report = &r
	if _, err := s.Deploy(context.Background(), 32000, nil); err == nil || !inspected {
		t.Fatalf("err=%v inspected=%v", err, inspected)
	}
}

func TestUFWPartialFailureNeverRemovesPreexistingRule(t *testing.T) {
	server := newTestServer(t, "root", "secret")
	udpAdded := false
	deletedUDP := false
	server.respond = func(cmd, input string) (string, int) {
		if strings.Contains(cmd, "ufw show added") {
			out := "ufw allow 32000/tcp\n"
			if udpAdded {
				out += "ufw allow 32000/udp comment xdial-proxy\n"
			}
			return out, 0
		}
		if strings.Contains(cmd, "ufw allow 32000/tcp") {
			t.Error("preexisting TCP rule changed")
		}
		if strings.Contains(cmd, "ufw allow 32000/udp") {
			udpAdded = true
			return "", 1
		}
		if strings.Contains(cmd, "ufw delete allow 32000/tcp") {
			t.Error("preexisting TCP rule deleted")
		}
		if strings.Contains(cmd, "ufw delete allow 32000/udp") {
			deletedUDP = true
		}
		return "", 0
	}
	s := deploymentSession(t, server)
	f := ubuntuFacts()
	f["singbox_path"] = "/usr/bin/sing-box"
	f["ufw"] = "Status: active"
	s.facts = f
	r := evaluate(f, privilegeRoot)
	s.report = &r
	if _, err := s.Deploy(context.Background(), 32000, nil); err == nil || !deletedUDP {
		t.Fatalf("err=%v partial UDP rollback=%v", err, deletedUDP)
	}
}

func TestFailedCommitNeverReportsSuccess(t *testing.T) {
	server := newTestServer(t, "root", "secret")
	failed := false
	removed := false
	server.respond = func(cmd, input string) (string, int) {
		if strings.Contains(cmd, statePath+".xdial-tmp") && input != "" {
			var st state
			if err := json.Unmarshal([]byte(input), &st); err != nil {
				t.Error(err)
			}
			if !st.InProgress {
				failed = true
				return "", 1
			}
		}
		if strings.Contains(cmd, "rm -rf "+configDir) {
			removed = true
		}
		return "", 0
	}
	s := deploymentSession(t, server)
	s.facts = ubuntuFacts()
	r := evaluate(s.facts, privilegeRoot)
	s.report = &r
	if _, err := s.Deploy(context.Background(), 32000, nil); err == nil || !failed || !removed {
		t.Fatalf("err=%v commit failed=%v rolled back=%v", err, failed, removed)
	}
}
