#!/bin/sh
set -eu
taskdir="$HOME/.config/local-proxy-gateway"
mkdir -p "$taskdir"
case "${1:-}" in
enable)
 cat > "$taskdir/env.sh" <<'ENV'
# Managed by local-proxy-gateway. Source this file in the current shell if needed.
if [ -f "$HOME/.config/local-proxy-gateway/enabled" ]; then
  _gateway_host=$(ip -4 route show default | awk 'NR==1 {print $3}')
  if [ -n "$_gateway_host" ]; then
    export http_proxy="http://$_gateway_host:17890" https_proxy="http://$_gateway_host:17890"
    export HTTP_PROXY="$http_proxy" HTTPS_PROXY="$https_proxy"
    export ALL_PROXY="socks5h://$_gateway_host:17890" all_proxy="socks5h://$_gateway_host:17890"
    export no_proxy="localhost,127.0.0.1,::1,.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10" NO_PROXY="localhost,127.0.0.1,::1,.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10"
  fi
  unset _gateway_host
fi
ENV
 for shellfile in "$HOME/.profile" "$HOME/.bashrc"; do
  if ! grep -q 'local-proxy-gateway/env.sh' "$shellfile" 2>/dev/null; then
   cp -p "$shellfile" "$taskdir/$(basename "$shellfile").before" 2>/dev/null || true
   printf '\n# Local proxy gateway (toggle in dashboard)\n[ ! -f "$HOME/.config/local-proxy-gateway/env.sh" ] || . "$HOME/.config/local-proxy-gateway/env.sh"\n' >> "$shellfile"
  fi
 done
 : > "$taskdir/enabled"
 printf '%s\n' 'WSL shell integration enabled. Open a new shell to apply.'
 ;;
disable)
 if [ -f "$taskdir/enabled" ]; then mv "$taskdir/enabled" "$taskdir/disabled"; fi
 printf '%s\n' 'WSL integration disabled. Existing shells keep their environment until reopened.'
 ;;
*) exit 2;;
esac
