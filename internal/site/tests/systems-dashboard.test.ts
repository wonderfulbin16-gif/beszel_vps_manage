import { afterEach, describe, expect, mock, test } from "bun:test"
import type { SystemRecord } from "../src/types"

mock.module("@lingui/core/macro", () => ({ t: () => "", plural: () => "" }))

await import("../src/lib/utils")
const { $router, navigate } = await import("../src/components/router")
const { $allSystemsById, $allSystemsByName, $authenticated, $downSystems, $upSystems } = await import(
	"../src/lib/stores"
)
const { pb } = await import("../src/lib/api")
const { completeLogin } = await import("../src/components/login/complete-login")
const systems = await import("../src/lib/systemsManager")

systems.init()
const beforeSend = pb.beforeSend

function system(id: string, name: string, status: SystemRecord["status"] = "up") {
	return { id, name, info: { cpu: 0 }, status } as SystemRecord
}

function systemsResponse(items: SystemRecord[]) {
	return new Response(JSON.stringify({ page: 1, perPage: 1000, totalItems: items.length, totalPages: 1, items }))
}

function mockFetch(fetch: () => Promise<Response>) {
	pb.beforeSend = (url, options) => ({ url, options: { ...options, fetch } })
}

afterEach(() => {
	pb.beforeSend = beforeSend
	pb.authStore.clear()
	$authenticated.set(false)
	$allSystemsById.set({})
	$allSystemsByName.set({})
	systems.unsubscribe()
})

describe("systems dashboard", () => {
	test("a successful login replaces a remembered deep link with the dashboard", () => {
		navigate("/system/saved-system")
		expect($router.get()?.route).toBe("system")
		const token = `test.${btoa(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 3600 }))}.test`
		pb.authStore.save(token)
		completeLogin()
		expect($authenticated.get()).toBe(true)
		expect($router.get()?.route).toBe("home")
	})

	test("refresh removes deleted systems, reconciles names and updates status groups", async () => {
		systems.add(system("first", "Old name"))
		systems.add(system("deleted", "Deleted machine"))
		mockFetch(async () => systemsResponse([system("first", "New name", "down")]))
		await systems.refresh()
		expect(Object.keys($allSystemsById.get())).toEqual(["first"])
		expect(Object.keys($allSystemsByName.get())).toEqual(["New name"])
		expect(Object.keys($upSystems.get())).toEqual([])
		expect(Object.keys($downSystems.get())).toEqual(["first"])
	})

	test("overlapping refreshes share a request and preserve newer changes", async () => {
		const response = Promise.withResolvers<Response>()
		const requested = Promise.withResolvers<void>()
		let requestCount = 0
		systems.add(system("first", "Existing"))
		const removed = system("deleted", "Deleted")
		systems.add(removed)
		mockFetch(() => {
			requestCount++
			requested.resolve()
			return response.promise
		})

		const refresh = systems.refresh()
		expect(systems.refresh()).toBe(refresh)
		await requested.promise
		systems.update(system("first", "Live edit"))
		systems.remove(removed)
		response.resolve(systemsResponse([system("first", "Stale snapshot"), removed]))
		await refresh
		expect(requestCount).toBe(1)
		expect(Object.keys($allSystemsByName.get())).toEqual(["Live edit"])
	})

	test("refresh reports network failures without clearing displayed systems", async () => {
		systems.add(system("first", "Existing"))
		mockFetch(async () => {
			throw new TypeError("Network unavailable")
		})
		await expect(systems.refresh()).rejects.toThrow()
		expect(Object.keys($allSystemsById.get())).toEqual(["first"])
	})
})
