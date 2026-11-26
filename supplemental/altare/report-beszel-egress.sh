#!/bin/sh
set -eu

hub=${1:?provide the Beszel Hub HTTPS origin}
case "$hub" in
    https://*) ;;
    *) printf '%s\n' 'Beszel Hub requires HTTPS.' >&2; exit 2 ;;
esac

state=/opt/beszel-agent/.egress-last-ok
now=$(date +%s)
if [ "${2:-}" != --now ] && [ -r "$state" ]; then
    read -r last_ok < "$state" || last_ok=0
    case "$last_ok" in
        ''|*[!0-9]*) last_ok=0 ;;
    esac
    if [ "$last_ok" -le "$now" ] && [ "$((now - last_ok))" -lt 900 ]; then
        exit 0
    fi
fi

agent_pid=$(pgrep -xo beszel-agent) || exit 0
token=$(tr '\000' '\n' < "/proc/$agent_pid/environ" | sed -n 's/^TOKEN=//p')
case "$token" in
    ''|*[!a-zA-Z0-9_-]*) printf '%s\n' 'Beszel Agent token is unavailable.' >&2; exit 1 ;;
esac

# Curl reads the token from stdin so it never appears in process arguments.
status=$(
    printf 'X-Token: %s\n' "$token" |
        curl -q --ipv4 --noproxy '*' --fail --silent --show-error \
            --connect-timeout 5 --max-time 12 --header @- --request POST \
            --output /dev/null --write-out '%{http_code}' \
            "${hub%/}/api/beszel/agent-egress"
)
printf 'Beszel egress HTTP %s\n' "$status"
[ "$status" = 204 ] || exit 1

umask 077
temporary=$(mktemp "$state.XXXXXX")
trap 'rm -f "$temporary"' EXIT
printf '%s\n' "$now" > "$temporary"
mv -f "$temporary" "$state"
