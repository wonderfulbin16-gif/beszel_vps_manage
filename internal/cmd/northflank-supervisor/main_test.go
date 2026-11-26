//go:build linux || darwin

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestAgentFailureRetriesWhileHubKeepsRunning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HUB_REQUIRED_ENV", "present")
	t.Setenv("TOKEN", "must-not-leak")
	t.Setenv("HUB_URL", "must-not-leak")
	hub := writeExecutable(t, dir, "hub", `test "$HUB_REQUIRED_ENV" = present || exit 5
test "$1" = serve && test "$2" = --http=0.0.0.0:8090 || exit 6
echo "$$" > "`+dir+`/hub.pid"
trap 'echo "$$" > "`+dir+`/hub.stopped"; exit 0' TERM
while :; do sleep 0.02; done
`)
	agent := writeExecutable(t, dir, "agent", `test -z "$TOKEN" && test -z "$HUB_URL" || exit 2
test "$USE_CGROUP" = true && test "$SKIP_SYSTEMD" = true || exit 3
test -n "$LISTEN" && test -d "$(dirname "$LISTEN")" && test "$FILESYSTEM" = "`+dir+`" || exit 4
echo "$KEY" >> "`+dir+`/agent.matches"
exit 7
`)
	keyFile, wantKey := writeFixtureKey(t, dir)
	signals := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		done <- supervise(supervisorConfig{
			hubPath: hub, agentPath: agent, dataPath: dir, keyPath: keyFile,
			socketPath: filepath.Join(dir, "run", "agent.sock"),
			args:       []string{"serve", "--http=0.0.0.0:8090"},
			retryDelay: 20 * time.Millisecond, shutdownTimeout: time.Second,
		}, signals)
	}()
	t.Cleanup(func() {
		select {
		case signals <- syscall.SIGTERM:
		default:
		}
	})
	matches := awaitFile(t, filepath.Join(dir, "agent.matches"), func(s string) bool {
		return strings.Count(s, wantKey+"\n") >= 2
	})
	for _, key := range strings.Split(strings.TrimSpace(matches), "\n") {
		if key != wantKey {
			t.Fatal("Agent received a public key other than the Hub's")
		}
	}
	originalPID := awaitFile(t, filepath.Join(dir, "hub.pid"), func(s string) bool { return s != "" })
	select {
	case code := <-done:
		t.Fatalf("Agent failure stopped Hub with code %d", code)
	default:
	}
	signals <- syscall.SIGTERM
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("supervisor exit after Hub shutdown = %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop")
	}
	if stoppedPID := awaitFile(t, filepath.Join(dir, "hub.stopped"), func(s string) bool { return s != "" }); stoppedPID != originalPID {
		t.Fatalf("Hub PID changed after Agent failure: %s -> %s", originalPID, stoppedPID)
	}
}

func TestHubStatusIsReturnedAndAgentIsStopped(t *testing.T) {
	dir := t.TempDir()
	hub := writeExecutable(t, dir, "hub", `while ! test -f "`+dir+`/agent.ready"; do sleep 0.02; done
exit 42
`)
	agent := writeExecutable(t, dir, "agent", `trap 'echo stopped > "`+dir+`/agent.stopped"; exit 0' TERM
echo ready > "`+dir+`/agent.ready"
while :; do sleep 0.02; done
`)
	keyFile, _ := writeFixtureKey(t, dir)
	signals := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		done <- supervise(supervisorConfig{
			hubPath: hub, agentPath: agent, dataPath: dir, keyPath: keyFile,
			socketPath: filepath.Join(dir, "agent.sock"), retryDelay: 10 * time.Millisecond,
			shutdownTimeout: 500 * time.Millisecond,
		}, signals)
	}()
	select {
	case code := <-done:
		if code != 42 {
			t.Fatalf("supervisor exit when Hub exits = %d, want 42", code)
		}
	case <-time.After(2 * time.Second):
		signals <- syscall.SIGTERM
		t.Fatal("supervisor did not exit with Hub")
	}
	awaitFile(t, filepath.Join(dir, "agent.stopped"), func(s string) bool { return s == "stopped\n" })
}

func writeExecutable(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFixtureKey(t *testing.T, dir string) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(privateKeyPEM), 0600); err != nil {
		t.Fatal(err)
	}
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return path, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublicKey)))
}

func awaitFile(t *testing.T, path string, ready func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		value, _ := os.ReadFile(path)
		if ready(string(value)) {
			return string(value)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", filepath.Base(path))
	return ""
}
