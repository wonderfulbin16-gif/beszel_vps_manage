const systemColumnId = "system"

export function normalizeSystemColumnOrder(saved: unknown, defaultOrder: readonly string[]): string[] {
	const defaults = new Set(defaultOrder)
	const order = defaults.delete(systemColumnId) ? [systemColumnId] : []
	const seen = new Set(order)
	let insertionIndex = order.length

	if (Array.isArray(saved)) {
		for (const id of saved) {
			if (typeof id === "string" && defaults.has(id) && !seen.has(id)) {
				order.push(id)
				seen.add(id)
			}
		}
	}

	// Place new columns after their default predecessor while preserving the relative order of saved columns.
	for (const id of defaults) {
		if (!seen.has(id)) {
			order.splice(insertionIndex, 0, id)
		}
		insertionIndex = order.indexOf(id) + 1
	}
	return order
}

export function moveSystemColumn(
	order: readonly string[],
	id: string,
	direction: -1 | 1,
	visible: readonly string[] = order
): string[] {
	const nextOrder = [...order]
	const visibleOrder = order.filter((columnId) => visible.includes(columnId))
	const index = visibleOrder.indexOf(id)
	const neighbor = visibleOrder[index + direction]
	if (index < 0 || !neighbor || id === systemColumnId || neighbor === systemColumnId) return nextOrder

	const source = order.indexOf(id)
	const destination = order.indexOf(neighbor)
	nextOrder[source] = neighbor
	nextOrder[destination] = id
	return nextOrder
}

export function placeSystemColumn(
	order: readonly string[],
	source: string,
	target: string,
	side: "before" | "after",
	visible: readonly string[] = order
): string[] {
	if (source === target) return [...order]

	const movable = new Set(visible)
	movable.delete(systemColumnId)
	const visibleOrder = order.filter((id) => movable.has(id))
	const sourceIndex = visibleOrder.indexOf(source)
	if (sourceIndex < 0 || !visibleOrder.includes(target)) return [...order]

	visibleOrder.splice(sourceIndex, 1)
	const targetIndex = visibleOrder.indexOf(target)
	visibleOrder.splice(targetIndex + (side === "after" ? 1 : 0), 0, source)

	let nextVisibleIndex = 0
	return order.map((id) => (movable.has(id) ? visibleOrder[nextVisibleIndex++] : id))
}
