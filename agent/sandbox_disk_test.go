package agent

import (
	"testing"
	"time"
)

func TestParseSandboxDiskUsage(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   float64
		valid  bool
	}{
		{name: "allocated bytes", output: "2415919104\t/\n", want: 2.25, valid: true},
		{name: "zero bytes", output: "0\t/\n", valid: true},
		{name: "truncated", output: "2415919104\n"},
		{name: "different filesystem", output: "2415919104\t/home\n"},
		{name: "multiple totals", output: "1073741824\t/\n2147483648\t/\n"},
		{name: "invalid number", output: "2.25G\t/\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSandboxDiskUsage(tt.output)
			if (err == nil) != tt.valid || got != tt.want {
				t.Fatalf("parseSandboxDiskUsage(%q) = %v, %v, want %v, valid %v", tt.output, got, err, tt.want, tt.valid)
			}
		})
	}
}

func TestSandboxDiskSamplerUsesOnlyFreshSamples(t *testing.T) {
	var sampler sandboxDiskSampler
	now := time.Now()
	if got := sampler.usage(now); got != nil {
		t.Fatalf("usage without a sample = %v, want nil", *got)
	}

	sampler.latest.Store(&sandboxDiskSample{collectedAt: now})
	if got := sampler.usage(now.Add(5 * time.Minute)); got == nil || *got != 0 {
		t.Fatalf("cached zero usage = %v, want a measured zero", got)
	}
	if got := sampler.usage(now.Add(31 * time.Minute)); got != nil {
		t.Fatalf("expired usage = %v, want nil", *got)
	}
}
