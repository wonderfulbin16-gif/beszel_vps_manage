package agent

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	sandboxDiskScanInterval = 5 * time.Minute
	sandboxDiskScanTimeout  = 45 * time.Second
	sandboxDiskSampleMaxAge = 30 * time.Minute
)

type sandboxDiskSample struct {
	collectedAt time.Time
	usedGiB     float64
}

type sandboxDiskSampler struct {
	latest atomic.Pointer[sandboxDiskSample]
}

// run scans the sandbox in the background. Successive monitoring requests
// reuse the most recent completed scan, even when a subsequent scan fails.
func (s *sandboxDiskSampler) run(ctx context.Context) {
	ticker := time.NewTicker(sandboxDiskScanInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		usedGiB, err := scanSandboxDiskUsage(ctx)
		switch {
		case err == nil:
			s.latest.Store(&sandboxDiskSample{collectedAt: time.Now(), usedGiB: usedGiB})
		case ctx.Err() == nil:
			slog.Warn("Failed to measure sandbox disk usage", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *sandboxDiskSampler) usage(now time.Time) *float64 {
	sample := s.latest.Load()
	if sample == nil || now.Sub(sample.collectedAt) > sandboxDiskSampleMaxAge {
		return nil
	}
	return &sample.usedGiB
}

func scanSandboxDiskUsage(ctx context.Context) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, sandboxDiskScanTimeout)
	defer cancel()

	// Only traverse the root filesystem. GNU du reports allocated bytes here,
	// including files visible from image layers in a merged filesystem.
	path, err := exec.LookPath("du")
	if err != nil {
		return 0, err
	}
	args := []string{"-x", "-s", "-B1", "--", "/"}
	if nice, err := exec.LookPath("nice"); err == nil {
		args = append([]string{"-n", "15", path}, args...)
		path = nice
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"LC_ALL=C"}
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, err
	}
	return parseSandboxDiskUsage(string(output))
}

func parseSandboxDiskUsage(output string) (float64, error) {
	fields := strings.Fields(output)
	if len(fields) != 2 || fields[1] != "/" {
		return 0, errors.New("expected one root directory total from du")
	}
	used, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, errors.New("invalid allocated byte count from du")
	}
	return float64(used) / (1 << 30), nil
}
