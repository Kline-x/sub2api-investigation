#!/usr/bin/env bash
#
# Sub2API 一键部署脚本（定制分支 Kline-x/sub2api-investigation）
#
# 用法：
#   仓库内：  bash deploy/quick-start.sh
#   远程：    curl -sSL https://raw.githubusercontent.com/Kline-x/sub2api-investigation/main/deploy/quick-start.sh | bash
#
# 做的事：生成随机密钥 → 写 .env → 拉镜像起栈 → 等待就绪 → 打印访问地址和管理员账号。
# 已有 .env 时不覆盖，直接复用（可重复执行，等价于升级到最新镜像）。
#
# 可用环境变量覆盖：
#   SUB2API_PORT   宿主机端口（默认 8080）
#   SUB2API_DIR    部署目录（默认当前目录下的 sub2api-deploy，仓库内运行时为 deploy/）
#   SUB2API_IMAGE  镜像（默认 ghcr.io/kline-x/sub2api:latest）
#   COMPOSE_PROJECT_NAME  compose 项目名（默认 sub2api）

set -euo pipefail

REPO_RAW="https://raw.githubusercontent.com/Kline-x/sub2api-investigation/main/deploy"
PORT="${SUB2API_PORT:-8080}"
PROJECT="${COMPOSE_PROJECT_NAME:-sub2api}"
IMAGE="${SUB2API_IMAGE:-ghcr.io/kline-x/sub2api:latest}"

RED=$'\033[0;31m'; GREEN=$'\033[0;32m'; YELLOW=$'\033[1;33m'; BLUE=$'\033[0;34m'; NC=$'\033[0m'
info()  { echo "${BLUE}[INFO]${NC} $*"; }
ok()    { echo "${GREEN}[ OK ]${NC} $*"; }
warn()  { echo "${YELLOW}[WARN]${NC} $*"; }
die()   { echo "${RED}[FAIL]${NC} $*" >&2; exit 1; }

# --- 1. 前置检查 -------------------------------------------------------------
command -v docker >/dev/null 2>&1 || die "未找到 docker，请先安装 Docker。"
docker info >/dev/null 2>&1 || die "docker 守护进程没跑起来，先启动 Docker。"
if docker compose version >/dev/null 2>&1; then
    DC="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
    DC="docker-compose"
else
    die "未找到 docker compose（v2 插件或 docker-compose 均可）。"
fi
ok "docker 就绪（$DC）"

# --- 2. 确定部署目录 ---------------------------------------------------------
SCRIPT_DIR=""
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
if [[ -n "${SUB2API_DIR:-}" ]]; then
    DIR="$SUB2API_DIR"
elif [[ -n "$SCRIPT_DIR" && -f "$SCRIPT_DIR/docker-compose.yml" ]]; then
    DIR="$SCRIPT_DIR"            # 在仓库里跑，直接用仓库的 compose
else
    DIR="$PWD/sub2api-deploy"    # curl | bash 跑，建独立目录
fi
mkdir -p "$DIR"
cd "$DIR"
info "部署目录：$DIR"

# --- 3. 准备 compose 文件 ----------------------------------------------------
if [[ ! -f docker-compose.yml ]]; then
    info "下载 docker-compose.yml ..."
    curl -fsSL "$REPO_RAW/docker-compose.yml" -o docker-compose.yml \
        || die "下载 docker-compose.yml 失败，检查网络或代理。"
fi
grep -q "sub2api" docker-compose.yml || die "docker-compose.yml 内容异常。"
if grep -qE '^\s+image:\s*weishaw/sub2api' docker-compose.yml; then
    die "docker-compose.yml 指向上游镜像 weishaw/sub2api，不是本仓库的定制版本。请重新获取。"
fi
ok "compose 文件就绪"

# compose 里 container_name 写死为 sub2api，若已存在同名容器且不属于本项目，
# docker 只会抛一句含糊的 "name is already in use"，这里提前说清楚。
EXIST_PROJ="$(docker inspect sub2api --format '{{index .Config.Labels "com.docker.compose.project"}}' 2>/dev/null || true)"
if [[ -n "$EXIST_PROJ" && "$EXIST_PROJ" != "$PROJECT" ]]; then
    die "已存在名为 sub2api 的容器（属于项目 '$EXIST_PROJ'）。先停掉它：docker compose -p $EXIST_PROJ down，或用 COMPOSE_PROJECT_NAME=$EXIST_PROJ 重跑本脚本。"
fi

# --- 4. 生成 .env ------------------------------------------------------------
gen_hex() {  # 生成 $1 字节的十六进制随机串
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex "$1"
    else
        head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'
    fi
}

if [[ -f .env ]]; then
    warn ".env 已存在，沿用现有配置（不会覆盖密钥）"
else
    info "生成随机密钥并写入 .env ..."
    POSTGRES_PASSWORD="$(gen_hex 24)"
    JWT_SECRET="$(gen_hex 32)"          # 64 个十六进制字符 = 64 字节，满足 >=32 字节要求
    TOTP_ENCRYPTION_KEY="$(gen_hex 32)"
    ADMIN_PASSWORD="$(gen_hex 12)"
    cat > .env <<EOF
# 由 quick-start.sh 自动生成，请妥善保管
# 注意：POSTGRES_PASSWORD 同时用于应用连库和 postgres 容器初始化，改一处两边都会变；
# 若在已有数据卷上修改，会导致连库失败（卷里的密码是首次初始化时定下的）。
SUB2API_IMAGE=$IMAGE
BIND_HOST=0.0.0.0
SERVER_PORT=$PORT
POSTGRES_USER=sub2api
POSTGRES_DB=sub2api
POSTGRES_PASSWORD=$POSTGRES_PASSWORD
# 留空表示 redis 不设密码（仅容器网络内可达）。显式写出来是为了消掉 compose 的未定义变量警告。
REDIS_PASSWORD=
JWT_SECRET=$JWT_SECRET
TOTP_ENCRYPTION_KEY=$TOTP_ENCRYPTION_KEY
ADMIN_EMAIL=admin@sub2api.local
ADMIN_PASSWORD=$ADMIN_PASSWORD
EOF
    chmod 600 .env
    ok ".env 已生成（权限 600）"
fi

# 回读，保证后面打印的和实际生效的一致
ADMIN_EMAIL="$(grep -E '^ADMIN_EMAIL=' .env | cut -d= -f2-)"
ADMIN_PASSWORD="$(grep -E '^ADMIN_PASSWORD=' .env | cut -d= -f2-)"
PORT="$(grep -E '^SERVER_PORT=' .env | cut -d= -f2- || echo "$PORT")"

# --- 5. 启动 ----------------------------------------------------------------
info "拉取镜像并启动（首次拉镜像可能要几分钟）..."
$DC -p "$PROJECT" pull --quiet 2>/dev/null || $DC -p "$PROJECT" pull
$DC -p "$PROJECT" up -d

# --- 6. 等待就绪 -------------------------------------------------------------
# 冷启动包含建库、跑迁移、初始化管理员，实测约 30-60 秒，这里最多等 180 秒。
info "等待服务就绪（冷启动约 30-60 秒）..."
DEADLINE=$((SECONDS + 180))
READY=0
while (( SECONDS < DEADLINE )); do
    if curl -fsS --max-time 3 "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
        READY=1; break
    fi
    # 容器反复重启说明是配置错误，早点报出来，别干等到超时
    RC="$(docker inspect sub2api --format '{{.RestartCount}}' 2>/dev/null || echo 0)"
    if [[ "${RC:-0}" -ge 3 ]]; then
        echo
        echo "${RED}容器反复重启（$RC 次），最后的错误日志：${NC}"
        docker logs sub2api 2>&1 | grep -iE "error|fatal|failed|panic" | tail -10
        die "启动失败。常见原因：端口 $PORT 被占用；或复用了旧数据卷但换了 POSTGRES_PASSWORD。"
    fi
    printf '.'
    sleep 3
done
echo

[[ "$READY" == "1" ]] || {
    docker logs sub2api 2>&1 | tail -20
    die "180 秒内未就绪，日志见上。"
}

# --- 7. 输出 ----------------------------------------------------------------
VERSION="$(curl -fsS --max-time 3 "http://127.0.0.1:${PORT}/health" 2>/dev/null || echo '')"
ok "部署成功"
echo
echo "  访问地址：  http://127.0.0.1:${PORT}"
echo "  管理员邮箱：${ADMIN_EMAIL}"
if [[ -n "$ADMIN_PASSWORD" ]]; then
    echo "  管理员密码：${ADMIN_PASSWORD}"
else
    echo "  管理员密码：未设置，见 docker logs sub2api | grep -i password"
fi
echo "  镜像：      ${IMAGE}"
[[ -n "$VERSION" ]] && echo "  健康检查：  ${VERSION}"
echo
echo "  配置文件：  ${DIR}/.env（含密钥，注意备份与保密）"
echo "  查看日志：  docker logs -f sub2api"
echo "  停止：      $DC -p $PROJECT down"
echo "  升级：      重新执行本脚本（会拉最新镜像，保留数据和密钥）"
echo
