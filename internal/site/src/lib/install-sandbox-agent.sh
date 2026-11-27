set -eu
umask 077

if [ "$(uname -s)" != Linux ]; then
  echo "Beszel Sandbox Agent requires Linux." >&2
  exit 1
fi
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "Beszel Sandbox Agent supports Linux x86_64 and ARM64." >&2; exit 1 ;;
esac
for tool in curl tar sha256sum flock nohup; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Install $tool and run this command again." >&2
    exit 1
  fi
done

agent_dir="$HOME/.local/share/beszel-sandbox-agent"
mkdir -p "$agent_dir/data"
chmod 700 "$agent_dir"
exec 8>"$agent_dir/install.lock"
flock 8

if [ -f "$agent_dir/token" ] && {
  [ "$(cat "$agent_dir/token")" != "$agent_token" ] ||
  [ "$(cat "$agent_dir/hub-url")" != "$hub_url" ] ||
  [ "$(cat "$agent_dir/hub-key")" != "$hub_key" ]
}; then
  echo "This sandbox already has a Beszel Agent for a different configuration. Use its existing system record." >&2
  exit 1
fi

if [ ! -x "$agent_dir/beszel-agent" ]; then
  tmp_dir=$(mktemp -d "$agent_dir/install.XXXXXX")
  trap 'rm -rf "$tmp_dir"' EXIT
  asset="beszel-agent_linux_${arch}.tar.gz"
  release="https://github.com/henrygd/beszel/releases/download/v$agent_version"
  (
    cd "$tmp_dir"
    curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 "$release/beszel_${agent_version}_checksums.txt" -o checksums.txt
    awk -v asset="$asset" '$2 == asset' checksums.txt > agent.sha256
    curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 "$release/$asset" -o "$asset"
    sha256sum -c agent.sha256
    tar -xzf "$asset" beszel-agent
    chmod 700 beszel-agent
    mv beszel-agent "$agent_dir/beszel-agent"
  )
fi

printf '%s\n' "$hub_url" > "$agent_dir/hub-url"
printf '%s\n' "$hub_key" > "$agent_dir/hub-key"
printf '%s\n' "$agent_token" > "$agent_dir/token"
cat > "$agent_dir/start" <<'BESZEL_SANDBOX_START'
#!/bin/sh
set -eu
umask 077
cd "$(dirname "$0")"
unset TOKEN BESZEL_AGENT_TOKEN
export BESZEL_AGENT_HUB_URL="$(cat hub-url)"
export BESZEL_AGENT_KEY="$(cat hub-key)"
export BESZEL_AGENT_TOKEN_FILE="$PWD/token"
export BESZEL_AGENT_DATA_DIR="$PWD/data"
export BESZEL_AGENT_DISABLE_SSH=true
nohup flock -n "$PWD/agent.lock" "$PWD/beszel-agent" </dev/null >> "$PWD/agent.log" 2>&1 &
BESZEL_SANDBOX_START
chmod 700 "$agent_dir/start"
"$agent_dir/start" 8>&-
printf 'Beszel Agent started in the background. Check the Hub or run:\n  tail -n 20 "%s/agent.log"\n' "$agent_dir"
