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
	"sync"
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

var errCollectorNotConfigured = errors.New("collector is not configured")

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

	workerContext, stopWorkers := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Go(func() {
		retryWorker(workerContext, config, "Beszel Agent", runAgent)
	})
	workers.Go(func() {
		retryWorker(workerContext, config, "Opportunity Collector", runCollector)
	})

	var hubResult error
	select {
	case hubResult = <-hubDone:
		stopWorkers()
	case shutdownSignal := <-signals:
		stopWorkers()
		hubResult = stopProcess(hub, hubDone, shutdownSignal, config.shutdownTimeout)
	}
	workers.Wait()

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

func retryWorker(ctx context.Context, config supervisorConfig, name string, run func(context.Context, supervisorConfig) error) {
	var lastFailure string
	for ctx.Err() == nil {
		err := run(ctx, config)
		if ctx.Err() != nil {
			return
		}
		failure := "process exited"
		if err != nil {
			failure = err.Error()
		}
		if !errors.Is(err, errCollectorNotConfigured) && failure != lastFailure {
			log.Printf("%s stopped (%s), retrying in %s", name, failure, config.retryDelay)
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
	return waitForWorker(ctx, agent, config.shutdownTimeout)
}

func runCollector(ctx context.Context, config supervisorConfig) error {
	root := filepath.Join(config.dataPath, "opportunity-collector")
	access := filepath.Join(root, "access.json")
	program := filepath.Join(root, "collector.py")
	for _, path := range []string{access, program} {
		file, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return errCollectorNotConfigured
		}
		if err != nil {
			return err
		}
		if !file.Mode().IsRegular() {
			return errors.New("collector configuration is not a regular file")
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	output, err := os.OpenFile(filepath.Join(root, "collector.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer output.Close()

	collector := exec.Command("python3", "-Bu", program, "--platform", "linux",
		"--access", access, "--state-dir", filepath.Join(root, "state"))
	collector.Dir = root
	collector.Stdout, collector.Stderr = output, output
	collector.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=/tmp"}
	if err := collector.Start(); err != nil {
		return err
	}
	return waitForWorker(ctx, collector, config.shutdownTimeout)
}

func waitForWorker(ctx context.Context, command *exec.Cmd, shutdownTimeout time.Duration) error {
	done := waitForProcess(command)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return stopProcess(command, done, syscall.SIGTERM, shutdownTimeout)
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
