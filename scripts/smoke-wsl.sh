#!/bin/sh
set -eu
. "$HOME/.config/local-proxy-gateway/env.sh"
test -n "${https_proxy:-}"
printf 'WSL proxy endpoint: %s\n' "$https_proxy"
curl --noproxy '' --proxy "$https_proxy" --max-time 15 -s -o /dev/null -w 'WSL HTTPS status: %{http_code}\n' https://www.gstatic.com/generate_204
