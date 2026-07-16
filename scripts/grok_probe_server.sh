#!/usr/bin/env bash
# 在【部署 sub2api 的服务器上】运行，判断 xAI 403 是 IP 地域封锁还是 header 指纹拦截。
#
# 用法:
#   1) 改下面 PG_CONTAINER 为你的 postgres 容器名 (docker ps 查)
#   2) bash grok_probe_server.sh <账号ID>
#
# 若某组 header 返回非 403(如 200/429/401)，说明是 header 层可绕过 ——
#   把那组值填进 sub2api 的环境变量 GROK_CLI_USER_AGENT / GROK_CLI_VERSION / GROK_EXTRA_HEADERS。
# 若全部同样 403，说明是服务器出口 IP 被 xAI 地域封 —— 需换 IP 或给账号挂能出海的干净代理。
set -u
PG_CONTAINER="${PG_CONTAINER:-sub2api-postgres-1}"   # 改成你的 postgres 容器名
PG_USER="${PG_USER:-sub2api}"
PG_DB="${PG_DB:-sub2api}"
ACC="${1:-1}"

read -r BASEURL TOKEN < <(docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -At -F' ' \
  -c "select credentials->>'base_url', credentials->>'access_token' from accounts where id=${ACC}")
[ -z "${TOKEN:-}" ] && { echo "账号 ${ACC} 无 token（或容器名/库名不对）"; exit 1; }

URL="${BASEURL%/}/responses"
echo "账号 ${ACC}  端点 ${URL}"
echo "================================================================"
PAYLOAD='{"model":"grok-4.5","input":"hi","stream":false}'

probe () {
  local name="$1"; shift
  local code
  code=$(curl -s -o /tmp/gp.txt -w "%{http_code}" --max-time 30 -X POST "$URL" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" "$@" -d "$PAYLOAD")
  printf "[%-20s] HTTP %s | %s\n" "$name" "$code" "$(head -c 160 /tmp/gp.txt | tr '\n' ' ')"
}

probe "baseline-0.2.93"   -H "User-Agent: sub2api-grok/1.0" -H "X-Grok-Client-Version: 0.2.93"
probe "auth-only"
probe "ver-0.1.51"        -H "User-Agent: sub2api-grok/1.0" -H "X-Grok-Client-Version: 0.1.51"
probe "grokcli-0.1.51"    -H "User-Agent: grok-cli/0.1.51" -H "X-Grok-Client-Version: 0.1.51"
probe "grok-build-0.1.51" -H "User-Agent: grok-build/0.1.51" -H "X-Grok-Client-Version: 0.1.51"
probe "browser-UA"        -H "User-Agent: Mozilla/5.0 (X11; Linux x86_64) Chrome/126.0 Safari/537.36"
echo "================================================================"
echo "看响应头(是否 Cloudflare 层)可加: curl -sD- -o/dev/null ... | grep -iE 'server|cf-'"
