/** biome-ignore-all lint/suspicious/noAssignInExpressions: it's fine :) */
import type { PreinitializedMapStore } from "nanostores"
import { pb } from "@/lib/api"
import {
	$allSystemsById,
	$allSystemsByName,
	$downSystems,
	$pausedSystems,
	$systemsLoaded,
	$upSystems,
} from "@/lib/stores"
import type { SystemRecord } from "@/types"
import { SystemStatus } from "./enums"

const COLLECTION = pb.collection<SystemRecord>("systems")
const FIELDS_DEFAULT = "id,name,host,port,ip,ip_region,info,status,capacity,capacity_override,cluster_excluded"

let initialized = false
// biome-ignore lint/suspicious/noConfusingVoidType: typescript rocks
let unsub: (() => void) | undefined | void
let subscriptionGeneration = 0
let refreshRequest: { controller: AbortController; touched: Set<string>; promise: Promise<void> } | undefined

/** Initialize the systems manager and set up listeners */
export function init() {
	if (initialized) {
		return
	}
	initialized = true

	// sync system stores on change
	$allSystemsById.listen((newSystems, oldSystems, changedKey) => {
		if (changedKey === undefined) {
			const up: Record<string, SystemRecord> = {}
			const down: Record<string, SystemRecord> = {}
			const paused: Record<string, SystemRecord> = {}
			for (const system of Object.values(newSystems)) {
				if (system.status === SystemStatus.Up) up[system.id] = system
				if (system.status === SystemStatus.Down) down[system.id] = system
				if (system.status === SystemStatus.Paused) paused[system.id] = system
			}
			$upSystems.set(up)
			$downSystems.set(down)
			$pausedSystems.set(paused)
			return
		}
		const oldSystem = oldSystems[changedKey]
		const newSystem = newSystems[changedKey]

		// if system is undefined (deleted), remove it from the stores
		if (oldSystem && !newSystem?.id) {
			removeFromStore(oldSystem, $upSystems)
			removeFromStore(oldSystem, $downSystems)
			removeFromStore(oldSystem, $pausedSystems)
			removeFromStore(oldSystem, $allSystemsById)
		}

		if (!newSystem) {
			return
		}

		const newStatus = newSystem.status
		if (newStatus === SystemStatus.Up) {
			$upSystems.setKey(newSystem.id, newSystem)
			removeFromStore(newSystem, $downSystems)
			removeFromStore(newSystem, $pausedSystems)
		} else if (newStatus === SystemStatus.Down) {
			$downSystems.setKey(newSystem.id, newSystem)
			removeFromStore(newSystem, $upSystems)
			removeFromStore(newSystem, $pausedSystems)
		} else if (newStatus === SystemStatus.Paused) {
			$pausedSystems.setKey(newSystem.id, newSystem)
			removeFromStore(newSystem, $upSystems)
			removeFromStore(newSystem, $downSystems)
		} else if (newStatus === SystemStatus.Pending) {
			removeFromStore(newSystem, $upSystems)
			removeFromStore(newSystem, $downSystems)
			removeFromStore(newSystem, $pausedSystems)
		}
	})
}

/** Makes sure the system has valid info object and throws if not */
function validateSystemInfo(system: SystemRecord) {
	if (!system.info || !("cpu" in system.info)) {
		throw new Error(`${system.name} has no CPU info`)
	}
}

/** Add system to both name and ID stores */
export function add(system: SystemRecord) {
	try {
		validateSystemInfo(system)
		refreshRequest?.touched.add(system.id)
		$allSystemsByName.setKey(system.name, system)
		$allSystemsById.setKey(system.id, system)
	} catch (error) {
		console.error(error)
	}
}

/** Update system in stores */
export function update(system: SystemRecord) {
	try {
		validateSystemInfo(system)
		// if name changed, make sure old name is removed from the name store
		const oldName = $allSystemsById.get()[system.id]?.name
		if (oldName && oldName !== system.name) {
			$allSystemsByName.setKey(oldName, undefined as unknown as SystemRecord)
		}
		add(system)
	} catch (error) {
		console.error(error)
	}
}

/** Remove system from stores */
export function remove(system: SystemRecord) {
	refreshRequest?.touched.add(system.id)
	removeFromStore(system, $allSystemsByName)
	removeFromStore(system, $allSystemsById)
	removeFromStore(system, $upSystems)
	removeFromStore(system, $downSystems)
	removeFromStore(system, $pausedSystems)
}

/** Remove system from specific store */
function removeFromStore(system: SystemRecord, store: PreinitializedMapStore<Record<string, SystemRecord>>) {
	const key = store === $allSystemsByName ? system.name : system.id
	store.setKey(key, undefined as unknown as SystemRecord)
}

/** Action functions for subscription */
const actionFns: Record<string, (system: SystemRecord) => void> = {
	create: add,
	update: update,
	delete: remove,
}

/** Subscribe to real-time system updates from the collection */
export async function subscribe() {
	const generation = ++subscriptionGeneration
	try {
		const cleanup = await COLLECTION.subscribe(
			"*",
			({ action, record }) => {
				if (generation === subscriptionGeneration) actionFns[action]?.(record)
			},
			{ fields: FIELDS_DEFAULT }
		)
		if (generation === subscriptionGeneration) {
			unsub = cleanup
		} else {
			await cleanup()
		}
	} catch (error) {
		console.error("Failed to subscribe to systems collection:", error)
	}
}

/** Refresh all systems with latest data from the hub */
export function refresh(): Promise<void> {
	if (refreshRequest) return refreshRequest.promise

	const controller = new AbortController()
	const touched = new Set<string>()
	const promise = COLLECTION.getFullList({
		sort: "+name",
		fields: FIELDS_DEFAULT,
		signal: controller.signal,
		requestKey: null,
	})
		.then((records) => {
			if (controller.signal.aborted) return

			const byId: Record<string, SystemRecord> = {}
			for (const record of records) {
				try {
					validateSystemInfo(record)
					byId[record.id] = record
				} catch (error) {
					console.error(error)
				}
			}

			// Keep updates and deletions that arrived while the list was loading.
			const current = $allSystemsById.get()
			for (const id of touched) {
				if (current[id]) byId[id] = current[id]
				else delete byId[id]
			}

			$allSystemsByName.set(Object.fromEntries(Object.values(byId).map((system) => [system.name, system])))
			$allSystemsById.set(byId)
		})
		.finally(() => {
			if (refreshRequest?.controller === controller) {
				refreshRequest = undefined
				$systemsLoaded.set(true)
			}
		})
	refreshRequest = { controller, touched, promise }
	return promise
}

/** Unsubscribe from real-time system updates */
export function unsubscribe() {
	refreshRequest?.controller.abort()
	refreshRequest = undefined
	$systemsLoaded.set(false)
	subscriptionGeneration++
	const cleanup = unsub
	unsub = undefined
	cleanup?.()
}
