import { describe, expect, mock, test } from "bun:test"
import type { ReactNode } from "react"
import { renderToStaticMarkup } from "react-dom/server"

mock.module("@lingui/core/macro", () => ({ t: () => "", plural: () => "" }))
mock.module("@lingui/react/macro", () => ({
	Trans: ({ children }: { children?: ReactNode }) => children,
	Plural: () => null,
	useLingui: () => ({}),
}))

const { SystemIPRegion } = await import("../src/components/system-ip-region")

describe("automatically detected IP region", () => {
	test("presents a saved region without including it in the form payload", () => {
		const markup = renderToStaticMarkup(<SystemIPRegion region="日本 · 東京" />)
		const input = markup.match(/<input\b[^>]*>/)?.[0]

		expect(input).toContain('value="日本 · 東京"')
		expect(input).toContain('readOnly=""')
		expect(input).not.toContain("name=")
		expect(markup).toContain('href="https://db-ip.com"')
		expect(markup).toContain('rel="noreferrer"')
	})

	test("omits the data attribution when no region is displayed", () => {
		const markup = renderToStaticMarkup(<SystemIPRegion />)

		expect(markup).not.toContain("https://db-ip.com")
	})
})
