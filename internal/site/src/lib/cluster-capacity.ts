import type { SystemCapacity, SystemRecord } from "@/types"
import { SystemStatus } from "./enums"

export const capacityMetrics = ["cpu", "memory", "disk"] as const
export type CapacityMetric = keyof SystemCapacity

export function validCapacity(value: unknown): number | undefined {
	return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined
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
			const value = validCapacity(system.capacity_override?.[metric]) ?? validCapacity(system.capacity?.[metric])
			if (value === undefined) continue
			metrics[metric].total += value
			metrics[metric].known++
			if (metric === "memory" && system.status === SystemStatus.Up) {
				const usedBytes = system.info?.mub
				if (typeof usedBytes === "number" && Number.isFinite(usedBytes) && usedBytes >= 0) {
					availableMemory.total += Math.max(0, value - usedBytes / 2 ** 30)
					availableMemory.known++
				}
			}
		}
	}
	return { included, metrics, availableMemory }
}
