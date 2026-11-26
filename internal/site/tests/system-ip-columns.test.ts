import { describe, expect, mock, test } from "bun:test"
import { createTable, getCoreRowModel, getSortedRowModel, type TableState } from "@tanstack/react-table"
import type { SystemRecord } from "../src/types"

mock.module("@lingui/core/macro", () => ({
	t: (strings: TemplateStringsArray | { message: string }) => ("message" in strings ? strings.message : strings[0]),
	plural: () => "",
}))

mock.module("@lingui/react/macro", () => ({ Plural: () => null, Trans: () => null, useLingui: () => ({}) }))
mock.module("../src/components/alerts/alert-button", () => ({ default: () => null }))
mock.module("../src/components/system-actions-button", () => ({ ActionsButton: () => null }))

const { SystemsTableColumns } = await import("../src/components/systems-table/systems-table-columns")

function createSystemsTable(state: Partial<TableState>) {
	const data = [
		{ name: "First", host: "agent.example", ip: "2001:db8::1", ip_region: "Tokyo", info: {} },
		{ name: "Second", host: "/run/beszel/agent.sock", ip: "192.0.2.10", ip_region: "Amsterdam", info: {} },
		{ name: "Third", host: "unset.example", info: {} },
	] as SystemRecord[]

	return createTable<SystemRecord>({
		data,
		columns: SystemsTableColumns("table"),
		state,
		getCoreRowModel: getCoreRowModel(),
		getSortedRowModel: getSortedRowModel(),
		onStateChange: () => {},
		renderFallbackValue: null,
	})
}

describe("system IP metadata columns", () => {
	test("inserts the optional fields after system with existing visibility preferences", () => {
		const table = createSystemsTable({ columnVisibility: { disk: false } })
		const columns = table.getVisibleLeafColumns().map((column) => column.id)
		expect(columns.slice(0, 4)).toEqual(["system", "ip", "ip_region", "cpu"])
		expect(columns).not.toContain("disk")
		expect(table.getColumn("ip")?.getCanHide()).toBe(true)
		expect(table.getColumn("ip_region")?.getCanHide()).toBe(true)
	})

	test("uses recorded IPs instead of connection hosts and sorts region text", () => {
		const table = createSystemsTable({ sorting: [{ id: "ip_region", desc: false }] })
		const rows = table.getSortedRowModel().rows
		expect(rows.map((row) => row.getValue("ip"))).toEqual(["192.0.2.10", "2001:db8::1", undefined])
	})
})
