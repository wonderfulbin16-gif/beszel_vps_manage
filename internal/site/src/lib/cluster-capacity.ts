import type { SystemCapacity, SystemRecord } from "@/types"

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
	let included = 0
	for (const system of systems) {
		if (system.cluster_excluded) continue
		included++
		for (const metric of capacityMetrics) {
			const value = validCapacity(system.capacity_override?.[metric]) ?? validCapacity(system.capacity?.[metric])
			if (value === undefined) continue
			metrics[metric].total += value
			metrics[metric].known++
		}
	}
	return { included, metrics }
}
