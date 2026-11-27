import { type PointerEvent, useEffect, useRef, useState } from "react"

type DropTarget = { id: string; side: "before" | "after" }
type PointerDrag = {
	id: string
	pointerId: number
	startX: number
	startY: number
	x: number
	y: number
	started: boolean
	viewport: HTMLElement
}

function getDropTarget(list: HTMLElement, drag: PointerDrag): DropTarget | null {
	const bounds = list.getBoundingClientRect()
	const viewport = drag.viewport.getBoundingClientRect()
	if (
		drag.x < bounds.left ||
		drag.x > bounds.right ||
		drag.y < Math.max(bounds.top, viewport.top) ||
		drag.y > Math.min(bounds.bottom, viewport.bottom)
	) {
		return null
	}

	let last: DropTarget | null = null
	for (const row of list.querySelectorAll<HTMLElement>("[data-column-drop-id]")) {
		const id = row.dataset.columnDropId
		if (!id) continue
		const rect = row.getBoundingClientRect()
		if (rect.bottom < viewport.top || rect.top > viewport.bottom) continue
		if (drag.y < rect.bottom) {
			return { id, side: drag.y < (rect.top + rect.bottom) / 2 ? "before" : "after" }
		}
		last = { id, side: "after" }
	}
	return last
}

export function useColumnDrag(place: (source: string, target: DropTarget) => void) {
	const list = useRef<HTMLDivElement>(null)
	const pointer = useRef<PointerDrag | null>(null)
	const frame = useRef(0)
	const [source, setSource] = useState<string>()
	const [target, setTarget] = useState<DropTarget | null>(null)

	useEffect(() => () => cancelAnimationFrame(frame.current), [])

	function update() {
		const drag = pointer.current
		const listElement = list.current
		if (!drag?.started || !listElement) return

		const bounds = listElement.getBoundingClientRect()
		const viewport = drag.viewport.getBoundingClientRect()
		if (drag.x >= bounds.left && drag.x <= bounds.right && drag.y >= viewport.top && drag.y <= viewport.bottom) {
			const edge = Math.min(32, viewport.height / 4)
			const above = Math.max(0, viewport.top + edge - drag.y)
			const below = Math.max(0, drag.y - (viewport.bottom - edge))
			if (above || below) drag.viewport.scrollTop += (below - above) / 2
		}
		const drop = getDropTarget(listElement, drag)
		const next = drop?.id === drag.id ? null : drop
		setTarget((current) => (current?.id === next?.id && current?.side === next?.side ? current : next))
		frame.current = requestAnimationFrame(update)
	}

	function start(event: PointerEvent<HTMLButtonElement>, id: string) {
		if (!event.isPrimary || event.button !== 0 || pointer.current) return
		const viewport = list.current?.closest<HTMLElement>("[data-radix-menu-content]")
		if (!viewport) return
		event.preventDefault()
		event.currentTarget.setPointerCapture(event.pointerId)
		pointer.current = {
			id,
			pointerId: event.pointerId,
			startX: event.clientX,
			startY: event.clientY,
			x: event.clientX,
			y: event.clientY,
			started: false,
			viewport,
		}
	}

	function move(event: PointerEvent<HTMLDivElement>) {
		const drag = pointer.current
		if (!drag || drag.pointerId !== event.pointerId) return
		drag.x = event.clientX
		drag.y = event.clientY
		if (!drag.started && Math.hypot(drag.x - drag.startX, drag.y - drag.startY) >= 4) {
			drag.started = true
			setSource(drag.id)
			update()
		}
	}

	function cancel(event: PointerEvent<HTMLDivElement>) {
		if (pointer.current?.pointerId !== event.pointerId) return
		pointer.current = null
		cancelAnimationFrame(frame.current)
		setSource(undefined)
		setTarget(null)
	}

	function finish(event: PointerEvent<HTMLDivElement>) {
		const drag = pointer.current
		if (!drag || drag.pointerId !== event.pointerId) return
		drag.x = event.clientX
		drag.y = event.clientY
		const drop = drag.started && list.current ? getDropTarget(list.current, drag) : null
		cancel(event)
		if (drop && drop.id !== drag.id) place(drag.id, drop)
	}

	return { list, source, target, start, move, cancel, finish }
}
