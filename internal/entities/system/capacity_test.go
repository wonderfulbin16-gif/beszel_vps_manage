package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportedCapacity(t *testing.T) {
	previous := Capacity{CPU: 8, Memory: 8, Disk: 250}
	raw := Stats{MemBytes: [4]uint64{2 << 30}, DiskTotal: 1000}
	details := &Details{Threads: 16, MemoryTotal: 8 << 30}

	assert.Equal(t, Capacity{CPU: 16, Memory: 2, Disk: 1000},
		ReportedCapacity(previous, "server.example.test", "", Info{}, raw, details))
	assert.Equal(t, Capacity{CPU: 8, Memory: 2, Disk: 1000},
		ReportedCapacity(previous, "server.example.test", "", Info{}, raw, nil))
	assert.Equal(t, Capacity{Memory: 2},
		ReportedCapacity(previous, "se-sto02.tryaltare.ai", "", Info{SandboxDiskEnabled: true}, raw, details))
	diskUsed := 2.25
	historical := raw
	historical.SandboxDiskUsed = &diskUsed
	assert.Equal(t, Capacity{Memory: 2},
		ReportedCapacity(previous, "se-sto02.tryaltare.ai", "", Info{}, historical, details))
	assert.Empty(t,
		ReportedCapacity(previous, "/dev/null", "", Info{}, raw, details))
	assert.Empty(t,
		ReportedCapacity(previous, "localhost", "ibssh_example", Info{}, raw, details))
	assert.Equal(t, Capacity{CPU: 0.65, Memory: 2},
		ReportedCapacity(previous, "/dev/null", "", Info{CapacityScoped: true, CPUCapacity: 0.65, MemoryCapacity: 2 << 30}, raw, details))
	assert.Empty(t,
		ReportedCapacity(previous, "/dev/null", "", Info{CapacityScoped: true}, raw, details))
	assert.Equal(t, Capacity{CPU: 0.2, Memory: 0.5, Disk: 6},
		ReportedCapacity(previous, "/run/beszel/agent.sock", "", Info{CapacityScoped: true, CPUCapacity: 0.2, MemoryCapacity: 512 << 20}, Stats{DiskTotal: 6}, details))
}

func TestValidateCapacityOverride(t *testing.T) {
	for _, input := range []string{"null", "{}", `{"cpu":0.65,"memory":2.3,"disk":40}`} {
		require.NoError(t, ValidateCapacityOverride(input), input)
	}
	for _, input := range []string{`{"other":3}`, `{"CPU":3}`, `{"cpu":0}`, `{"cpu":-1}`, `{"disk":"40"}`,
		`{"memory":null}`, `{"memory":1e999}`, `[1]`, `"test"`, `{}` + `{}`} {
		require.Error(t, ValidateCapacityOverride(input), input)
	}
}
