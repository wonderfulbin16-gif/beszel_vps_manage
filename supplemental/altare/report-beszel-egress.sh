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
process_dir=/proc/$agent_pid
agent_setting() {
    tr '\000' '\n' < "$process_dir/environ" | awk -v key="$1" '
        index($0, "BESZEL_AGENT_" key "=") == 1 {
            print substr($0, length("BESZEL_AGENT_" key "=") + 1)
            prefixed = 1
            exit
        }
        index($0, key "=") == 1 { value = substr($0, length(key "=") + 1) }
        END { if (!prefixed && value != "") print value }
    '
}

token=$(agent_setting TOKEN)
if [ -z "$token" ]; then
    token_file=$(agent_setting TOKEN_FILE)
    case "$token_file" in
        '') ;;
        /*) token=$(sed -e 's/^[[:space:]]*//;s/[[:space:]]*$//' -e '/^#/d;/^$/d' "$token_file") ;;
        *) token=$(sed -e 's/^[[:space:]]*//;s/[[:space:]]*$//' -e '/^#/d;/^$/d' "$process_dir/cwd/$token_file") ;;
    esac
fi
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
