import { redirectPage } from "@nanostores/router"
import { $router } from "@/components/router"
import { pb } from "@/lib/api"
import { $authenticated } from "@/lib/stores"

/** All interactive and automatic sign-ins start at the systems dashboard. */
export function completeLogin() {
	if (pb.authStore.isValid) {
		redirectPage($router, "home")
		$authenticated.set(true)
	}
}
