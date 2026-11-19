FROM curlimages/curl:8.18.0
RUN test -n "$https_proxy" && curl --fail --max-time 15 -sS -o /dev/null -w 'Docker build HTTPS %{http_code}\n' https://www.gstatic.com/generate_204
