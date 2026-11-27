import { Plural, Trans, useLingui } from "@lingui/react/macro"
import { CpuIcon, HardDriveIcon, LoaderCircleIcon, MemoryStickIcon, Settings2Icon } from "lucide-react"
import { type FormEvent, useId, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardDescription, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { toast } from "@/components/ui/use-toast"
import { isReadOnlyUser, pb } from "@/lib/api"
import { type CapacityMetric, capacityMetrics, summarizeCapacity, validCapacity } from "@/lib/cluster-capacity"
import { $allSystemsById } from "@/lib/stores"
import { update as updateSystem } from "@/lib/systemsManager"
import { formatNumber } from "@/lib/utils"
import type { SystemRecord } from "@/types"

const units = { cpu: "vCPU", memory: "GiB", disk: "GiB" }
const icons = { cpu: CpuIcon, memory: MemoryStickIcon, disk: HardDriveIcon }
type Labels = Record<CapacityMetric, string>

export function ClusterCapacity({ systems }: { systems: readonly SystemRecord[] }) {
	const { t } = useLingui()
	const titleId = useId()
	const selectId = useId()
	const [open, setOpen] = useState(false)
	const [selectedId, setSelectedId] = useState("")
	const [saving, setSaving] = useState(false)
	const selected = systems.find(({ id }) => id === selectedId) ?? systems[0]
	const labels = { cpu: t`CPU`, memory: t`Memory`, disk: t`Disk` }
	const summary = summarizeCapacity(systems)
	const canEdit = !isReadOnlyUser()
	return (
		<Card className="grid gap-4 p-4 sm:p-5" role="region" aria-labelledby={titleId}>
			<div className="flex flex-wrap items-start justify-between gap-3">
				<div className="grid gap-1.5">
					<CardTitle id={titleId} className="text-base">
						<Trans>Cluster capacity</Trans>
					</CardTitle>
					<CardDescription>
						<Trans>Included systems</Trans>{" "}
						<span className="tabular-nums">
							{formatNumber(summary.included)} / {formatNumber(systems.length)}
						</span>
					</CardDescription>
				</div>
				{canEdit && (
					<Dialog open={open} onOpenChange={(nextOpen) => !saving && setOpen(nextOpen)}>
						<DialogTrigger asChild>
							<Button type="button" variant="outline" size="sm" disabled={!selected}>
								<Settings2Icon aria-hidden="true" />
								<Trans>Configure and summarize</Trans>
							</Button>
						</DialogTrigger>
						<DialogContent className="max-h-[calc(100dvh_-_1.5rem)] w-[calc(100%_-_1.5rem)] overflow-y-auto rounded-lg sm:max-w-xl">
							<DialogHeader>
								<DialogTitle>
									<Trans>Configure and summarize</Trans>
								</DialogTitle>
								<DialogDescription>
									<Trans>Leave a field blank to use the detected capacity.</Trans>
								</DialogDescription>
							</DialogHeader>
							<CapacityTotals summary={summary} labels={labels} />
							{selected && (
								<>
									<div className="grid gap-2">
										<Label htmlFor={selectId}>
											<Trans>System</Trans>
										</Label>
										<Select value={selected.id} onValueChange={setSelectedId} disabled={saving}>
											<SelectTrigger id={selectId}>
												<SelectValue />
											</SelectTrigger>
											<SelectContent>
												{systems.map(({ id, name }) => (
													<SelectItem key={id} value={id}>
														{name}
													</SelectItem>
												))}
											</SelectContent>
										</Select>
									</div>
									<CapacityEditor
										key={selected.id}
										system={selected}
										labels={labels}
										saving={saving}
										setSaving={setSaving}
									/>
								</>
							)}
						</DialogContent>
					</Dialog>
				)}
			</div>
			<CapacityTotals summary={summary} labels={labels} />
		</Card>
	)
}

function CapacityTotals({ summary, labels }: { summary: ReturnType<typeof summarizeCapacity>; labels: Labels }) {
	const { t } = useLingui()
	return (
		<dl className="grid grid-cols-1 gap-2 sm:grid-cols-3">
			{capacityMetrics.map((key) => {
				const Icon = icons[key]
				const { total, known } = summary.metrics[key]
				const missing = summary.included - known
				return (
					<div
						key={key}
						className="grid min-w-0 content-start gap-1 rounded-md border border-border/60 bg-muted/30 px-3 py-2.5"
					>
						<dt className="flex items-center gap-1.5 text-sm text-muted-foreground">
							<Icon className="size-3.5" aria-hidden="true" />
							{labels[key]}
						</dt>
						<dd className="text-lg font-semibold tabular-nums wrap-anywhere">
							{known ? (
								<>
									{formatNumber(total)} <span className="text-sm font-normal text-muted-foreground">{units[key]}</span>
								</>
							) : (
								<span aria-label={t`Unknown`}>—</span>
							)}
						</dd>
						{missing > 0 && (
							<dd className="text-xs text-muted-foreground">
								<Plural value={missing} one="Missing data for # system" other="Missing data for # systems" />
							</dd>
						)}
					</div>
				)
			})}
		</dl>
	)
}

function CapacityEditor({
	system,
	labels,
	saving,
	setSaving,
}: {
	system: SystemRecord
	labels: Labels
	saving: boolean
	setSaving: (saving: boolean) => void
}) {
	const { t } = useLingui()
	const formId = useId()
	const [edits, setEdits] = useState<Partial<Record<CapacityMetric, string>>>({})
	const [excludedEdit, setExcludedEdit] = useState<boolean>()
	const [error, setError] = useState("")
	const excluded = excludedEdit ?? system.cluster_excluded ?? false
	const dirty = Object.keys(edits).length > 0 || excludedEdit !== undefined

	function inputValue(metric: CapacityMetric) {
		return edits[metric] ?? validCapacity(system.capacity_override?.[metric])?.toString() ?? ""
	}

	async function save(event: FormEvent<HTMLFormElement>) {
		event.preventDefault()
		if (saving || !dirty || isReadOnlyUser()) return
		setError("")

		const current = $allSystemsById.get()[system.id] ?? system
		const patch: Pick<SystemRecord, "capacity_override" | "cluster_excluded"> = {}
		for (const metric of capacityMetrics) {
			const raw = edits[metric]
			if (raw === undefined) continue
			const value = validCapacity(Number(raw))
			if (value === undefined && raw.trim() !== "") {
				setError(t`Enter a positive number for each manual capacity.`)
				return
			}
			const overrides = (patch.capacity_override ??= { ...current.capacity_override })
			if (value === undefined) delete overrides[metric]
			else overrides[metric] = value
		}
		if (excludedEdit !== undefined) patch.cluster_excluded = excludedEdit

		setSaving(true)
		try {
			const saved = await pb.collection<SystemRecord>("systems").update(system.id, patch)
			updateSystem(saved)
			setEdits({})
			setExcludedEdit(undefined)
			toast({ title: t`Capacity settings saved` })
		} catch (error) {
			console.error(error)
			setError(t`Unable to save capacity settings. Try again.`)
		} finally {
			setSaving(false)
		}
	}

	return (
		<form className="grid gap-4" onSubmit={save}>
			<div className="flex items-center gap-2">
				<Checkbox
					id={`${formId}-included`}
					checked={!excluded}
					onCheckedChange={(checked) => setExcludedEdit(checked !== true)}
					disabled={saving}
				/>
				<Label htmlFor={`${formId}-included`}>
					<Trans>Include in cluster totals</Trans>
				</Label>
			</div>
			<div className="grid gap-3 sm:grid-cols-3">
				{capacityMetrics.map((key) => {
					const auto = validCapacity(system.capacity?.[key])
					const inputId = `${formId}-${key}`
					return (
						<div key={key} className="grid min-w-0 gap-1.5">
							<Label htmlFor={inputId}>
								{labels[key]} <span className="text-muted-foreground">({units[key]})</span>
							</Label>
							<Input
								id={inputId}
								type="number"
								step="any"
								inputMode="decimal"
								aria-describedby={`${inputId}-detected`}
								placeholder={t`Automatic`}
								value={inputValue(key)}
								disabled={saving}
								onChange={(event) => setEdits({ ...edits, [key]: event.target.value })}
							/>
							<p id={`${inputId}-detected`} className="text-xs text-muted-foreground">
								<Trans>Detected capacity</Trans>{" "}
								{auto === undefined ? t`Unknown` : `${formatNumber(auto)} ${units[key]}`}
							</p>
						</div>
					)
				})}
			</div>
			{error && (
				<p className="text-sm text-destructive" role="alert">
					{error}
				</p>
			)}
			<DialogFooter className="gap-2">
				<Button
					type="button"
					variant="outline"
					disabled={saving || !capacityMetrics.some((key) => inputValue(key) !== "")}
					onClick={() => setEdits({ cpu: "", memory: "", disk: "" })}
				>
					<Trans>Clear manual values</Trans>
				</Button>
				<Button type="submit" disabled={!dirty || saving}>
					{saving && <LoaderCircleIcon className="animate-spin" aria-hidden="true" />}
					<Trans>Save</Trans>
				</Button>
			</DialogFooter>
		</form>
	)
}
