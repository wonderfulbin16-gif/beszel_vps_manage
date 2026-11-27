import { pb } from "@/lib/api"

export interface SystemTraffic {
	receivedBytes: number
	sentBytes: number
	coveredSeconds: number
	sampleCount: number
	firstSampleAt?: string
	lastSampleAt?: string
}

export type SystemTrafficById = Readonly<Record<string, SystemTraffic>>

export const EMPTY_SYSTEM_TRAFFIC: SystemTrafficById = {}

export async function getSystemTraffic(signal: AbortSignal): Promise<SystemTrafficById> {
	const { systems } = await pb.send<{ systems: SystemTrafficById }>("/api/beszel/network-traffic", {
		method: "GET",
		signal,
		requestKey: null,
	})
	return systems
}
