import { i18n } from "@lingui/core"
import { memo } from "react"
import sandboxInstaller from "@/lib/install-sandbox-agent.sh?raw"
import { copyToClipboard, getHubURL } from "@/lib/utils"
import { DropdownMenuContent, DropdownMenuItem } from "./ui/dropdown-menu"

// const isbeta = beszel.hub_version.includes("beta")
// const imagetag = isbeta ? ":edge" : ""

/**
 * Get the URL of the script to install the agent.
 * @param path - The path to the script (e.g. "/brew").
 * @returns The URL for the script.
 */
const getScriptUrl = (path: string = "") => {
	return `https://get.beszel.dev${path}`
	// no beta for now
	// const url = new URL("https://get.beszel.dev")
	// url.pathname = path
	// if (isBeta) {
	// 	url.searchParams.set("beta", "1")
	// }
	// return url.toString()
}

export function copyDockerCompose(port = "45876", publicKey: string, token: string) {
	copyToClipboard(`services:
  beszel-agent:
    image: henrygd/beszel-agent
    container_name: beszel-agent
    restart: unless-stopped
    network_mode: host
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./beszel_agent_data:/var/lib/beszel-agent
      # monitor other disks / partitions by mounting a folder in /extra-filesystems
      # - /mnt/disk/.beszel:/extra-filesystems/sda1:ro
    environment:
      LISTEN: ${port}
      KEY: '${publicKey}'
      TOKEN: ${token}
      HUB_URL: ${getHubURL()}`)
}

export function copyDockerRun(port = "45876", publicKey: string, token: string) {
	copyToClipboard(
		`docker run -d --name beszel-agent --network host --restart unless-stopped -v /var/run/docker.sock:/var/run/docker.sock:ro -v beszel_agent_data:/var/lib/beszel-agent -e KEY="${publicKey}" -e LISTEN=${port} -e TOKEN="${token}" -e HUB_URL="${getHubURL()}" henrygd/beszel-agent`
	)
}

export function copyLinuxCommand(port = "45876", publicKey: string, token: string, brew = false) {
	let cmd = `curl -sL ${getScriptUrl(
		brew ? "/brew" : ""
	)} -o /tmp/install-agent.sh && chmod +x /tmp/install-agent.sh && /tmp/install-agent.sh -p ${port} -k "${publicKey}" -t "${token}" -url "${getHubURL()}"`
	// brew script does not support --china-mirrors
	if (!brew && (i18n.locale + navigator.language).includes("zh-CN")) {
		cmd += ` --china-mirrors`
	}
	copyToClipboard(cmd)
}

export function copyWindowsCommand(port = "45876", publicKey: string, token: string) {
	copyToClipboard(
		`& iwr -useb ${getScriptUrl()} -OutFile "$env:TEMP\\install-agent.ps1"; & Powershell -ExecutionPolicy Bypass -File "$env:TEMP\\install-agent.ps1" -Key "${publicKey}" -Port ${port} -Token "${token}" -Url "${getHubURL()}"`
	)
}

export const sandboxHost = "/dev/null"
export const sandboxStartCommand = '"$HOME/.local/share/beszel-sandbox-agent/start"'

export function copySandboxCommand(publicKey: string, token: string) {
	const quote = (value: string) => "'" + value.replace(/[\r\n]+/g, " ").replaceAll("'", "'\"'\"'") + "'"
	const hubVersion = globalThis.BESZEL?.HUB_VERSION
	const version = /^\d+\.\d+\.\d+$/.test(hubVersion ?? "") ? hubVersion : "0.21.2"
	return copyToClipboard(`sh <<'BESZEL_SANDBOX_INSTALL'
agent_version=${quote(version)}
hub_url=${quote(getHubURL())}
hub_key=${quote(publicKey)}
agent_token=${quote(token)}
${sandboxInstaller.trimEnd()}
BESZEL_SANDBOX_INSTALL`)
}

export interface DropdownItem {
	text: string
	onClick?: () => void
	url?: string
	icons?: React.ComponentType<React.SVGProps<SVGSVGElement>>[]
}

export const InstallDropdown = memo(({ items }: { items: DropdownItem[] }) => {
	return (
		<DropdownMenuContent align="end">
			{items.map((item, index) => {
				const className = "cursor-pointer flex items-center gap-1.5"
				return item.url ? (
					<DropdownMenuItem key={index} asChild>
						<a href={item.url} className={className} target="_blank" rel="noopener noreferrer">
							{item.text}{" "}
							{item.icons?.map((Icon, iconIndex) => (
								<Icon key={iconIndex} className="size-4" />
							))}
						</a>
					</DropdownMenuItem>
				) : (
					<DropdownMenuItem key={index} onClick={item.onClick} className={className}>
						{item.text}{" "}
						{item.icons?.map((Icon, iconIndex) => (
							<Icon key={iconIndex} className="size-4" />
						))}
					</DropdownMenuItem>
				)
			})}
		</DropdownMenuContent>
	)
})
