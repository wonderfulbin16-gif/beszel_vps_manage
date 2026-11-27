import type { SystemCapacity, SystemRecord } from "@/types"
import { SystemStatus } from "./enums"

export const capacityMetrics = ["cpu", "memory", "disk"] as const
export type CapacityMetric = keyof SystemCapacity

export function validCapacity(value: unknown): number | undefined {
	return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined
}

export function effectiveCapacity(system: SystemRecord, metric: CapacityMetric) {
	return validCapacity(system.capacity_override?.[metric]) ?? validCapacity(system.capacity?.[metric])
}

export function availableMemoryGiB(system: SystemRecord, total: number | undefined) {
	const usedBytes = system.info?.mub
	if (
		total === undefined ||
		system.status !== SystemStatus.Up ||
		typeof usedBytes !== "number" ||
		!Number.isFinite(usedBytes) ||
		usedBytes < 0
	) {
		return undefined
	}
	return Math.max(0, total - usedBytes / 2 ** 30)
}

export function summarizeCapacity(systems: readonly SystemRecord[]) {
	const metrics = {
		cpu: { total: 0, known: 0 },
		memory: { total: 0, known: 0 },
		disk: { total: 0, known: 0 },
	}
	const availableMemory = { total: 0, known: 0 }
	let included = 0
	for (const system of systems) {
		if (system.cluster_excluded) continue
		included++
		for (const metric of capacityMetrics) {
			const value = effectiveCapacity(system, metric)
			if (value === undefined) continue
			metrics[metric].total += value
			metrics[metric].known++
			if (metric === "memory") {
				const available = availableMemoryGiB(system, value)
				if (available !== undefined) {
					availableMemory.total += available
					availableMemory.known++
				}
			}
		}
	}
	return { included, metrics, availableMemory }
}
