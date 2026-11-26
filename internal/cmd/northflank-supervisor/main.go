//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// The defaults match the deployment image. Native integration tests can set
// fixture paths at link time without adding runtime environment overrides.
var (
	hubPath    = "/beszel"
	agentPath  = "/agent"
	dataPath   = "/beszel_data"
	socketPath = "/run/beszel/agent.sock"
)

type supervisorConfig struct {
	hubPath, agentPath, dataPath, keyPath, socketPath string
	args                                              []string
	retryDelay, shutdownTimeout                       time.Duration
}

func main() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	os.Exit(supervise(supervisorConfig{
		hubPath:         hubPath,
		agentPath:       agentPath,
		dataPath:        dataPath,
		keyPath:         filepath.Join(dataPath, "id_ed25519"),
		socketPath:      socketPath,
		args:            os.Args[1:],
		retryDelay:      10 * time.Second,
		shutdownTimeout: 8 * time.Second,
	}, signals))
}

func supervise(config supervisorConfig, signals <-chan os.Signal) int {
	hub := exec.Command(config.hubPath, config.args...)
	hub.Stdin, hub.Stdout, hub.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := hub.Start(); err != nil {
		log.Printf("Cannot start Beszel Hub: %v", err)
		return 1
	}
	hubDone := waitForProcess(hub)

	agentContext, stopAgent := context.WithCancel(context.Background())
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		retryAgent(agentContext, config)
	}()

	var hubResult error
	select {
	case hubResult = <-hubDone:
		stopAgent()
	case shutdownSignal := <-signals:
		stopAgent()
		hubResult = stopProcess(hub, hubDone, shutdownSignal, config.shutdownTimeout)
	}
	<-agentDone

	var exitError *exec.ExitError
	switch {
	case hubResult == nil:
		return 0
	case errors.As(hubResult, &exitError):
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitError.ExitCode()
	default:
		log.Printf("Cannot wait for Beszel Hub: %v", hubResult)
		return 1
	}
}

func retryAgent(ctx context.Context, config supervisorConfig) {
	var lastFailure string
	for ctx.Err() == nil {
		err := runAgent(ctx, config)
		if ctx.Err() != nil {
			return
		}
		failure := "process exited"
		if err != nil {
			failure = err.Error()
		}
		if failure != lastFailure {
			log.Printf("Beszel Agent stopped (%s), retrying in %s", failure, config.retryDelay)
			lastFailure = failure
		}
		timer := time.NewTimer(config.retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func runAgent(ctx context.Context, config supervisorConfig) error {
	privateKey, err := os.ReadFile(config.keyPath)
	if err != nil {
		return errors.New("Hub key is not ready")
	}
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return errors.New("Hub key could not be loaded")
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))

	runDir := filepath.Dir(config.socketPath)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return errors.New("socket directory could not be created")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	agent := exec.Command(config.agentPath)
	agent.Stdout, agent.Stderr = os.Stdout, os.Stderr
	// Do not pass Hub secrets or WebSocket/host-monitoring settings to the agent.
	agent.Env = []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + runDir,
		"TMPDIR=" + runDir,
		"DATA_DIR=" + runDir,
		"LISTEN=" + config.socketPath,
		"KEY=" + publicKey,
		"USE_CGROUP=true",
		"FILESYSTEM=" + config.dataPath,
		"DOCKER_HOST=",
		"FAN_SENSORS=",
		"SENSORS=",
		"SMART_DEVICES=",
		"SKIP_GPU=true",
		"SKIP_SYSTEMD=true",
		"SKIP_WIFI=true",
	}
	if err := agent.Start(); err != nil {
		return errors.New("process could not be started")
	}
	done := waitForProcess(agent)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return stopProcess(agent, done, syscall.SIGTERM, config.shutdownTimeout)
	}
}

func waitForProcess(command *exec.Cmd) <-chan error {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	return done
}

func stopProcess(command *exec.Cmd, done <-chan error, shutdownSignal os.Signal, timeout time.Duration) error {
	_ = command.Process.Signal(shutdownSignal)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-done:
		return result
	case <-timer.C:
		_ = command.Process.Kill()
		return <-done
	}
}
