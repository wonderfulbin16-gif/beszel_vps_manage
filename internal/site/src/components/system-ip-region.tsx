import { Trans } from "@lingui/react/macro"
import { Input } from "@/components/ui/input"

export function IPGeolocationCredit() {
	return (
		<a
			href="https://db-ip.com"
			target="_blank"
			rel="noreferrer"
			className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
		>
			<Trans>IP Geolocation by DB-IP</Trans>
		</a>
	)
}

export function SystemIPRegion({ region }: { region?: string }) {
	return (
		<div className="grid min-w-0 gap-1">
			<Input id="ip-region" value={region ?? ""} readOnly aria-describedby="ip-region-help" />
			<p id="ip-region-help" className="text-xs text-muted-foreground">
				<Trans>Automatically detected from the IP after saving.</Trans>
			</p>
			{region && <IPGeolocationCredit />}
		</div>
	)
}
