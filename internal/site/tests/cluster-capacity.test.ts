import { expect, test } from "bun:test"
import { summarizeCapacity } from "../src/lib/cluster-capacity"
import type { SystemRecord } from "../src/types"

test("combines detected capacities and per-field overrides for included systems", () => {
	const systems = [
		{ capacity: { cpu: 1.25, memory: 4.4 }, capacity_override: { memory: 4, disk: 40 } },
		{ capacity: { cpu: 2, disk: 24 }, capacity_override: { cpu: 1.5 } },
		{ capacity: { memory: 2.3 }, capacity_override: {} },
		{ capacity: { cpu: 100, memory: 100, disk: 100 }, cluster_excluded: true },
	] as SystemRecord[]
	expect(summarizeCapacity(systems)).toEqual({
		included: 3,
		availableMemory: { total: 0, known: 0 },
		metrics: {
			cpu: { total: 2.75, known: 2 },
			memory: { total: 6.3, known: 2 },
			disk: { total: 64, known: 2 },
		},
	})
})

test("counts unavailable values as unknown and falls back from invalid overrides", () => {
	const systems = [
		{},
		{ capacity: { cpu: 2, disk: 0 }, capacity_override: { cpu: -1, memory: Number.NaN } },
	] as SystemRecord[]
	expect(summarizeCapacity(systems)).toEqual({
		included: 2,
		availableMemory: { total: 0, known: 0 },
		metrics: {
			cpu: { total: 2, known: 1 },
			memory: { total: 0, known: 0 },
			disk: { total: 0, known: 0 },
		},
	})
})

test("sums available memory using actual bytes used and each included system's configured limit", () => {
	const gib = 2 ** 30
	const systems = [
		{ status: "up", capacity: { memory: 8 }, capacity_override: { memory: 6 }, info: { mub: 3 * gib } },
		{ status: "up", capacity: { memory: 4 }, info: { mub: 0 } },
		{ status: "up", capacity: { memory: 2 }, capacity_override: { memory: 1 }, info: { mub: 1.5 * gib } },
		{ status: "down", capacity: { memory: 8 }, info: { mub: gib } },
		{ status: "up", capacity: { memory: 8 }, info: {} },
		{ status: "up", capacity: { memory: 8 }, info: { mub: -1 } },
		{ status: "up", capacity: { memory: 8 }, info: { mub: 0 }, cluster_excluded: true },
	] as SystemRecord[]
	expect(summarizeCapacity(systems).availableMemory).toEqual({ total: 7, known: 3 })
})
