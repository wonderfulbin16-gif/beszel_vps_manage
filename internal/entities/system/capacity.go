package system

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Capacity holds provisioned resources in vCPU and GiB. Zero means unknown.
type Capacity struct {
	CPU    float64 `json:"cpu,omitempty"`
	Memory float64 `json:"memory,omitempty"`
	Disk   float64 `json:"disk,omitempty"`
}

// ReportedCapacity derives configuration from agent samples. Container limits
// must not inherit the host's logical CPU count or the shared filesystem size.
func ReportedCapacity(previous Capacity, host, ip string, info Info, stats Stats, details *Details) Capacity {
	const bytesPerGiB = 1 << 30
	host = strings.ToLower(strings.TrimSpace(host))
	var supervisedDisk float64
	if host == "/run/beszel/agent.sock" {
		// The Northflank supervisor explicitly monitors the Hub's mounted data volume.
		supervisedDisk = firstCapacity(stats.DiskTotal, previous.Disk)
	}
	if info.CapacityScoped {
		return Capacity{
			CPU:    positiveCapacity(info.CPUCapacity),
			Memory: float64(info.MemoryCapacity) / bytesPerGiB,
			Disk:   supervisedDisk,
		}
	}

	sandboxEndpoint := host == "/dev/null" || strings.HasSuffix(host, ".tryaltare.ai") ||
		strings.HasSuffix(host, ".altare.global")
	sandboxDisk := info.SandboxDiskEnabled || stats.SandboxDiskUsed != nil
	// Before the agent reported explicit capacity, these deployments already collected
	// memory via cgroups, while CPU inventory and disk capacity exposed the node.
	if (sandboxEndpoint && sandboxDisk) || host == "/run/beszel/agent.sock" {
		return Capacity{Memory: recordedMemoryCapacity(stats, details, previous.Memory), Disk: supervisedDisk}
	}
	if sandboxEndpoint || sandboxDisk || strings.HasPrefix(host, "ibssh_") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(ip)), "ibssh_") {
		return Capacity{}
	}

	threads := info.Threads
	if details != nil && details.Threads > 0 {
		threads = details.Threads
	}
	return Capacity{
		CPU:    firstCapacity(info.CPUCapacity, float64(threads), previous.CPU),
		Memory: firstCapacity(float64(info.MemoryCapacity)/bytesPerGiB, recordedMemoryCapacity(stats, details, previous.Memory)),
		Disk:   firstCapacity(stats.DiskTotal, previous.Disk),
	}
}

func recordedMemoryCapacity(stats Stats, details *Details, previous float64) float64 {
	const bytesPerGiB = 1 << 30
	var declared uint64
	if details != nil {
		declared = details.MemoryTotal
	}
	return firstCapacity(float64(stats.MemBytes[0])/bytesPerGiB, stats.Mem, float64(declared)/bytesPerGiB, previous)
}

func firstCapacity(values ...float64) float64 {
	for _, value := range values {
		if value = positiveCapacity(value); value > 0 {
			return value
		}
	}
	return 0
}

func positiveCapacity(value float64) float64 {
	if value > 0 && !math.IsInf(value, 0) {
		return value
	}
	return 0
}

// ValidateCapacityOverride accepts a partial object or null to clear all
// corrections. Every supplied value must be a positive finite JSON number.
func ValidateCapacityOverride(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return fmt.Errorf("enter a capacity object or null: %w", err)
	}
	for key, rawValue := range fields {
		switch key {
		case "cpu", "memory", "disk":
			var value float64
			if err := json.Unmarshal(rawValue, &value); err != nil || positiveCapacity(value) == 0 {
				return fmt.Errorf("%s must be a positive finite number", key)
			}
		default:
			return fmt.Errorf("unknown capacity field %q", key)
		}
	}
	return nil
}
