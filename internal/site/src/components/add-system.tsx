import { Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath } from "@nanostores/router"
import { ChevronDownIcon, ExternalLinkIcon } from "lucide-react"
import { memo, useEffect, useRef, useState } from "react"
import { SystemIPRegion } from "@/components/system-ip-region"
import { Button } from "@/components/ui/button"
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { toast } from "@/components/ui/use-toast"
import { isReadOnlyUser, pb } from "@/lib/api"
import { SystemStatus } from "@/lib/enums"
import { $publicKey } from "@/lib/stores"
import { cn, copyToClipboard, generateToken, tokenMap, useBrowserStorage } from "@/lib/utils"
import type { SystemRecord } from "@/types"
import {
	copyDockerCompose,
	copyDockerRun,
	copyLinuxCommand,
	copySandboxCommand,
	copyWindowsCommand,
	type DropdownItem,
	InstallDropdown,
	sandboxHost,
	sandboxStartCommand,
} from "./install-dropdowns"
import { $router, basePath, Link, navigate } from "./router"
import { DropdownMenu, DropdownMenuTrigger } from "./ui/dropdown-menu"
import { AppleIcon, DockerIcon, FreeBsdIcon, TuxIcon, WindowsIcon } from "./ui/icons"
import { InputCopy } from "./ui/input-copy"

// To avoid a refactor of the dialog, we will just keep this function as a "skeleton" for the actual dialog
export function AddSystemDialog({ open, setOpen }: { open: boolean; setOpen: (open: boolean) => void }) {
	const opened = useRef(false)
	if (open) {
		opened.current = true
	}

	if (isReadOnlyUser()) {
		return null
	}

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			{opened.current && <SystemDialog setOpen={setOpen} />}
		</Dialog>
	)
}

/**
 * Token to be used for the next system.
 * Prevents token changing if user copies config, then closes dialog and opens again.
 */
let nextSystemToken: string | null = null

/**
 * SystemDialog component for adding or editing a system.
 * @param {Object} props - The component props.
 * @param {function} props.setOpen - Function to set the open state of the dialog.
 * @param {SystemRecord} [props.system] - Optional system record for editing an existing system.
 */
export const SystemDialog = ({ setOpen, system }: { setOpen: (open: boolean) => void; system?: SystemRecord }) => {
	const { t } = useLingui()
	const publicKey = useStore($publicKey)
	const port = useRef<HTMLInputElement>(null)
	const initialHost = system?.host === sandboxHost ? "" : (system?.host ?? "")
	const [hostValue, setHostValue] = useState(initialHost)
	const isUnixSocket = hostValue.startsWith("/")
	const [savedTab, setSavedTab] = useBrowserStorage("as-tab", "docker")
	const [editTab, setEditTab] = useState<string | null>(null)
	const defaultEditTab = system?.host === sandboxHost ? "sandbox" : savedTab === "binary" ? "binary" : "docker"
	const tab = system ? (editTab ?? defaultEditTab) : savedTab
	const isSandbox = tab === "sandbox"
	const [token, setToken] = useState(system?.token ?? "")
	const [isSaving, setIsSaving] = useState(false)

	useEffect(() => {
		;(async () => {
			// if no system, generate a new token
			if (!system) {
				nextSystemToken ||= generateToken()
				return setToken(nextSystemToken)
			}
			// if system exists,get the token from the fingerprint record
			if (tokenMap.has(system.id)) {
				return setToken(tokenMap.get(system.id)!)
			}
			const { token } = await pb.collection("fingerprints").getFirstListItem(`system = "${system.id}"`, {
				fields: "token",
			})
			tokenMap.set(system.id, token)
			setToken(token)
		})()
	}, [system?.id, nextSystemToken])

	async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
		e.preventDefault()
		if (isSaving || (isSandbox && (!token || !publicKey))) {
			return
		}
		const formData = new FormData(e.currentTarget)
		const data = {
			name: String(formData.get("name")),
			host: isSandbox ? sandboxHost : hostValue,
			port: isSandbox ? "" : String(formData.get("port") ?? ""),
		}
		const metadata = {
			ip: String(formData.get("ip") ?? "").trim(),
		}
		setIsSaving(true)
		try {
			if (system) {
				// Don't send users on update, or other users of a shared system would be removed
				const settingsChanged =
					data.name !== system.name ||
					data.host !== system.host ||
					((!isUnixSocket || isSandbox) && data.port !== system.port)
				await pb
					.collection("systems")
					.update(system.id, settingsChanged ? { ...data, ...metadata, status: SystemStatus.Pending } : metadata)
			} else if (isSandbox) {
				const id = generateToken().replaceAll("-", "").slice(0, 15)
				const batch = pb.createBatch()
				batch.collection("systems").create({ id, ...data, ...metadata, users: pb.authStore.record!.id })
				batch.collection("fingerprints").create({ system: id, token })
				await batch.send()
				nextSystemToken = null
			} else {
				const createdSystem = await pb.collection("systems").create({
					...data,
					...metadata,
					users: pb.authStore.record!.id,
				})
				await pb.collection("fingerprints").create({
					system: createdSystem.id,
					token,
				})
				// Reset the current token after successful system
				// creation so next system gets a new token
				nextSystemToken = null
			}
			setOpen(false)
			if (isSandbox && !system) {
				void copySandboxCommand(publicKey, token)
			}
			navigate(basePath)
		} catch (e) {
			console.error(e)
			toast({ variant: "destructive", description: t`Could not save the system. Try again.` })
		} finally {
			setIsSaving(false)
		}
	}

	return (
		<DialogContent
			className="w-[90%] sm:w-auto sm:ns-dialog max-w-full max-h-[calc(100dvh-2rem)] overflow-y-auto rounded-lg"
			onCloseAutoFocus={() => {
				setHostValue(initialHost)
				setEditTab(null)
			}}
		>
			<Tabs value={tab} onValueChange={system ? setEditTab : setSavedTab}>
				<DialogHeader>
					<DialogTitle className="mb-1 pb-1 max-w-100 truncate pr-8">
						{system ? <Trans>Edit System</Trans> : <Trans>Add System</Trans>}
					</DialogTitle>
					<TabsList className="grid w-full grid-cols-3">
						<TabsTrigger value="docker">Docker</TabsTrigger>
						<TabsTrigger value="binary">
							<Trans>Binary</Trans>
						</TabsTrigger>
						<TabsTrigger value="sandbox" className="px-1">
							<Trans>Altare / Sandbox</Trans>
						</TabsTrigger>
					</TabsList>
				</DialogHeader>
				{/* Keep descriptions in the same grid cell so tab changes do not resize the dialog. */}
				<DialogDescription asChild className="mb-3 leading-relaxed">
					<div className="grid">
						{/* Docker (set tab index to prevent auto focusing content in edit system dialog) */}
						<TabsContent
							value="docker"
							forceMount
							tabIndex={-1}
							className="col-start-1 row-start-1 data-[state=inactive]:invisible"
						>
							<p className="w-0 min-w-full">
								<Trans>
									Copy the
									<code className="bg-muted px-1 rounded-sm leading-3">docker-compose.yml</code> content for the agent
									below, or register agents automatically with a{" "}
									<Link
										onClick={() => setOpen(false)}
										href={getPagePath($router, "settings", { name: "tokens" })}
										className="link"
									>
										universal token
									</Link>
									.
								</Trans>
							</p>
						</TabsContent>
						{/* Binary */}
						<TabsContent
							value="binary"
							forceMount
							tabIndex={-1}
							className="col-start-1 row-start-1 data-[state=inactive]:invisible"
						>
							<p className="w-0 min-w-full">
								<Trans>
									Copy the installation command for the agent below, or register agents automatically with a{" "}
									<Link
										onClick={() => setOpen(false)}
										href={getPagePath($router, "settings", { name: "tokens" })}
										className="link"
									>
										universal token
									</Link>
									.
								</Trans>
							</p>
						</TabsContent>
						<TabsContent
							value="sandbox"
							forceMount
							tabIndex={-1}
							className="col-start-1 row-start-1 data-[state=inactive]:invisible"
						>
							<p className="w-0 min-w-full">
								<Trans>
									For Altare and other Linux sandboxes without systemd. Save the system, then paste the installation
									command into its terminal. The agent connects to the hub over WebSocket.
								</Trans>
							</p>
						</TabsContent>
					</div>
				</DialogDescription>
				<form onSubmit={handleSubmit}>
					<div className="grid xs:grid-cols-[auto_1fr] gap-y-3 gap-x-4 items-center mt-1 mb-4">
						<Label htmlFor="name" className="xs:text-end">
							<Trans>Name</Trans>
						</Label>
						<Input id="name" name="name" defaultValue={system?.name} required />
						<Label htmlFor="host" className={cn("xs:text-end", isSandbox && "hidden")}>
							<Trans>Host / IP</Trans>
						</Label>
						<Input
							id="host"
							name="host"
							value={hostValue}
							required={!isSandbox}
							className={cn(isSandbox && "hidden")}
							onChange={(e) => {
								setHostValue(e.target.value)
							}}
						/>
						<Label htmlFor="port" className={cn("xs:text-end", (isUnixSocket || isSandbox) && "hidden")}>
							<Trans>Port</Trans>
						</Label>
						<Input
							ref={port}
							name="port"
							id="port"
							defaultValue={system?.port || "45876"}
							required={!isUnixSocket && !isSandbox}
							className={cn((isUnixSocket || isSandbox) && "hidden")}
						/>
						<Label htmlFor="ip" className="xs:text-end">
							<Trans>IP</Trans>
						</Label>
						<Input
							id="ip"
							name="ip"
							defaultValue={system?.ip}
							maxLength={64}
							autoCapitalize="none"
							spellCheck={false}
						/>
						<Label htmlFor="ip-region" className="xs:text-end">
							<Trans>IP Region</Trans>
						</Label>
						<SystemIPRegion region={system?.ip_region} />
						<Label htmlFor="pkey" className="xs:text-end whitespace-pre">
							<Trans comment="Use 'Key' if your language requires many more characters">Public Key</Trans>
						</Label>
						<InputCopy value={publicKey} id="pkey" name="pkey" />
						<Label htmlFor="tkn" className="xs:text-end whitespace-pre">
							<Trans>Token</Trans>
						</Label>
						<InputCopy value={token} id="tkn" name="tkn" />
					</div>
					<DialogFooter className="flex justify-end gap-x-2 gap-y-3 flex-col mt-5">
						<div className={cn("grid", isSandbox && "hidden")}>
							{/* Docker */}
							<div className={cn("col-start-1 row-start-1", tab !== "docker" && "invisible")}>
								<CopyButton
									text={t({ message: "Copy docker compose", context: "Button to copy docker compose file content" })}
									onClick={async () =>
										copyDockerCompose(isUnixSocket ? hostValue : port.current?.value, publicKey, token)
									}
									icon={<DockerIcon className="size-4 -me-0.5" />}
									dropdownItems={[
										{
											text: t({ message: "Copy docker run", context: "Button to copy docker run command" }),
											onClick: async () =>
												copyDockerRun(isUnixSocket ? hostValue : port.current?.value, publicKey, token),
											icons: [DockerIcon],
										},
									]}
								/>
							</div>
							{/* Binary */}
							<div className={cn("col-start-1 row-start-1", tab !== "binary" && "invisible")}>
								<CopyButton
									text={t`Copy Linux command`}
									icon={<TuxIcon className="size-4" />}
									onClick={async () =>
										copyLinuxCommand(isUnixSocket ? hostValue : port.current?.value, publicKey, token)
									}
									dropdownItems={[
										{
											text: t({ message: "Homebrew command", context: "Button to copy install command" }),
											onClick: async () =>
												copyLinuxCommand(isUnixSocket ? hostValue : port.current?.value, publicKey, token, true),
											icons: [AppleIcon, TuxIcon],
										},
										{
											text: t({ message: "Windows command", context: "Button to copy install command" }),
											onClick: async () =>
												copyWindowsCommand(isUnixSocket ? hostValue : port.current?.value, publicKey, token),
											icons: [WindowsIcon],
										},
										{
											text: t({ message: "FreeBSD command", context: "Button to copy install command" }),
											onClick: async () =>
												copyLinuxCommand(isUnixSocket ? hostValue : port.current?.value, publicKey, token),
											icons: [FreeBsdIcon],
										},
										{
											text: t`Manual setup instructions`,
											url: "https://beszel.dev/guide/agent-installation#binary",
											icons: [ExternalLinkIcon],
										},
									]}
								/>
							</div>
						</div>
						{isSandbox && (
							<div className="grid gap-2">
								{system && (
									<Button
										type="button"
										variant="outline"
										onClick={() => copySandboxCommand(publicKey, token)}
										disabled={!publicKey || !token}
									>
										<Trans>Copy Sandbox Install Command</Trans>
									</Button>
								)}
								<Button type="button" variant="outline" onClick={() => copyToClipboard(sandboxStartCommand)}>
									<Trans>Copy Restart Recovery Command</Trans>
								</Button>
								<p className="text-sm text-muted-foreground">
									<Trans>For recovery after reboot, run this command every minute in Altare Schedules.</Trans>
								</p>
							</div>
						)}
						{/* Save */}
						<Button disabled={isSaving || (isSandbox && (!token || !publicKey))}>
							{system ? (
								<Trans>Save System</Trans>
							) : isSandbox ? (
								<Trans>Add System and Copy Command</Trans>
							) : (
								<Trans>Add System</Trans>
							)}
						</Button>
					</DialogFooter>
				</form>
			</Tabs>
		</DialogContent>
	)
}

interface CopyButtonProps {
	text: string
	onClick: () => void
	dropdownItems: DropdownItem[]
	icon?: React.ReactElement<any>
}

const CopyButton = memo((props: CopyButtonProps) => {
	return (
		<div className="flex gap-0 rounded-lg">
			<Button
				type="button"
				variant="outline"
				onClick={props.onClick}
				className="rounded-e-none dark:border-e-0 grow flex items-center gap-2"
			>
				{props.text} {props.icon}
			</Button>
			<div className="w-px h-full bg-muted"></div>
			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<Button variant="outline" className={"px-2 rounded-s-none border-s-0"}>
						<ChevronDownIcon />
					</Button>
				</DropdownMenuTrigger>
				<InstallDropdown items={props.dropdownItems} />
			</DropdownMenu>
		</div>
	)
})
