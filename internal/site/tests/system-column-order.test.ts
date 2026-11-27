import { expect, test } from "bun:test"
import { moveSystemColumn, normalizeSystemColumnOrder } from "../src/lib/system-column-order"

const defaults = ["system", "ip", "cpu", "memory", "trafficIngress", "trafficEgress", "actions"]

test("uses the latest default when an account has no valid saved order", () => {
	for (const saved of [undefined, null, "invalid", {}, []]) {
		expect(normalizeSystemColumnOrder(saved, defaults)).toEqual(defaults)
	}
})

test("keeps the system name first and retains a user's relative order as columns are added", () => {
	const saved = ["cpu", "ip", "cpu", "retired", "actions", {}, null, "system"]
	expect(normalizeSystemColumnOrder(saved, defaults)).toEqual([
		"system",
		"cpu",
		"memory",
		"trafficIngress",
		"trafficEgress",
		"ip",
		"actions",
	])
})

test("moves adjacent columns without mutating the saved order or moving the system name", () => {
	const order = Object.freeze(["system", "cpu", "memory", "actions"])
	expect(moveSystemColumn(order, "memory", -1)).toEqual(["system", "memory", "cpu", "actions"])
	expect(moveSystemColumn(order, "memory", 1)).toEqual(["system", "cpu", "actions", "memory"])
	expect(moveSystemColumn(order, "cpu", -1)).toEqual(order)
	expect(moveSystemColumn(order, "actions", 1)).toEqual(order)
	expect(moveSystemColumn(order, "system", 1)).toEqual(order)
	expect(moveSystemColumn(order, "removed", -1)).toEqual(order)
	expect(order).toEqual(["system", "cpu", "memory", "actions"])
})

test("moves only visible columns and keeps hidden columns in their original positions", () => {
	const order = ["system", "cpu", "ip", "memory", "actions"]
	const visible = ["system", "cpu", "memory", "actions"]
	expect(moveSystemColumn(order, "memory", -1, visible)).toEqual(["system", "memory", "ip", "cpu", "actions"])
	expect(moveSystemColumn(order, "cpu", 1, visible)).toEqual(["system", "memory", "ip", "cpu", "actions"])
	expect(moveSystemColumn(order, "ip", -1, visible)).toEqual(order)
})
