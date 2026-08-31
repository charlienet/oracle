#!/usr/bin/env bash
# =============================================================================
# run-oracle-tests.sh — Oracle 集成测试容器编排脚本
#
# 用途：
#   一键自动化「启动容器 → 等待就绪 → 修复监听 → 跑集成测试 → 停止删除」
#   全流程，支持 11g 与 12c 两个镜像，默认按「先 11g 后 12c」串行执行。
#
# 用法：
#   scripts/run-oracle-tests.sh [11g|12c|all]     （默认 all）
#
# 可选环境变量（覆盖默认行为）：
#   ORACLE_SERVICE_11G  11g 服务名（默认 JEM11GR2；镜像服务名待实跑确认，
#                       大概率 = SID(JEM11GR2) 或 JEM，不一致时请用此变量覆盖）
#   ORACLE_SID_11G / ORACLE_SID_12C   实例 SID（默认 JEM11GR2 / jem）
#   ORACLE_SERVICE_12C  12c 服务名（默认 jempdb）
#   ORACLE_DSN_USER / ORACLE_DSN_PASS 测试账号（默认 testuser / testpass）
#   SKIP_11G=1 / SKIP_12C=1           跳过对应版本
#   INSTANCE_WAIT                     实例就绪轮询上限秒数（默认 1200 = 20 分钟）
#
# 依赖：
#   - 宿主机 docker（可运行 Oracle 容器）
#   - 宿主机 1521 端口空闲（两版本串行复用，勿与其它进程冲突）
#   - 宿主机 go（运行集成测试 ./tests/）
#   - 可选 nc（无则回退 bash /dev/tcp 探测端口）
#
# 镜像来源（本机已有，仓库与标签固定）：
#   11g: registry.cn-shanghai.aliyuncs.com/techerwang/oracle:ora11g11204
#        实例 SID=JEM11GR2，单实例无 PDB，
#        ORACLE_HOME=/u01/app/oracle/product/11.2.0.4/dbhome_1
#   12c: registry.cn-shanghai.aliyuncs.com/techerwang/oracle:ora12c_12201
#        实例 SID=jem，PDB=JEMPDB（服务名 jempdb），
#        ORACLE_HOME=/u01/app/oracle/product/12.2.0.1/dbhome_1
#
# 已知关键坑（本脚本已固化处理，勿回退）：
#   1. 镜像 Cmd=[init]，数据库不会自动拉起：容器起来后必须手动
#      startup（12c 还需 ALTER PLUGGABLE DATABASE ALL OPEN）；
#      就绪判据 = PMON 进程存在 + 1521 端口可连 + 实例 open_mode=READ WRITE。
#   2. listener.ora 写死构建时主机名，容器主机名随机导致 TNS-12545：
#      修复方案 = 监听地址改为 HOST=0.0.0.0 后 lsnrctl stop/start。
#   3. 禁止静态 SID_LIST_LISTENER 注册（会导致 ORA-01034/ORA-27101）：
#      使用动态注册 —— ALTER SYSTEM REGISTER + 轮询 lsnrctl services 出现 READY。
#   4. 两容器共用宿主 1521 端口：串行执行，每版本结束立即 docker stop + rm。
#   5. 镜像不预置测试账号（实测 go-ora 连接报 ORA-01017 invalid username/password）：
#      脚本在监听修复之后、跑测试之前自动创建 TESTUSER 测试账号（幂等：
#      已存在则跳过创建，仅做连通性验证），创建/验证失败则终止该版本。
#   6. 11g startup 报 ORA-00845（MEMORY_TARGET not supported on this system）：
#      docker 默认 /dev/shm=64MB 不满足实例 memory_target 需求（SGA 需在共享
#      内存分配），11g 容器必须以 --shm-size=4g 启动；12c 镜像内存参数配置不同
#      不受影响，保持默认 shm 大小（已实测 PASS，勿改）。
#
# 逻辑说明（与规格的偏差，采用更稳妥方案）：
#   规格中「就绪轮询检查 PMON（12c 先 startup）」与「就绪后执行 startup」存在
#   先后矛盾——镜像不自动拉起数据库，不 startup 则 PMON 永不出现、必然超时。
#   故采用：先等「容器内可执行 docker exec」（systemd 空壳就绪），随后立即
#   startup + 打开 PDB，再轮询 PMON/端口/OPEN 判定实例就绪。
# =============================================================================

set -uo pipefail

# ---------- 常量 ----------
IMAGE_11G="registry.cn-shanghai.aliyuncs.com/techerwang/oracle:ora11g11204"
IMAGE_12C="registry.cn-shanghai.aliyuncs.com/techerwang/oracle:ora12c_12201"
CONTAINER_NAME="oracle-test"
HOST_PORT=1521
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTAINER_READY_TIMEOUT=600    # 容器 / systemd 空壳就绪上限（秒）
INSTANCE_WAIT=${INSTANCE_WAIT:-1200}  # 实例 OPEN 轮询上限（秒），默认 20 分钟
POLL_INTERVAL=10               # 就绪轮询间隔（秒）
REGISTER_WAIT=90               # 动态注册 READY 轮询上限（秒）

# ---------- 颜色（非 tty 自动禁用） ----------
if [ -t 1 ]; then
  C_GREEN=$'\033[0;32m'; C_RED=$'\033[0;31m'; C_YELLOW=$'\033[0;33m'; C_RESET=$'\033[0m'
else
  C_GREEN=''; C_RED=''; C_YELLOW=''; C_RESET=''
fi

# ---------- 基础工具 ----------
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

usage() {
  cat <<'EOF'
用法: scripts/run-oracle-tests.sh [11g|12c|all]   (默认 all = 先 11g 后 12c 串行)
可选环境变量:
  ORACLE_SERVICE_11G   11g 服务名（默认 JEM11GR2，实跑不一致时覆盖，如 JEM）
  ORACLE_DSN_USER/PASS 测试账号（默认 testuser/testpass）
  SKIP_11G=1/SKIP_12C=1 跳过对应版本
  INSTANCE_WAIT        实例就绪轮询上限秒数（默认 1200）
EOF
}

# 清理容器（幂等；EXIT trap 兜底，保证任何失败路径都删除容器）
cleanup_container() {
  log "清理容器 ${CONTAINER_NAME}"
  docker stop "${CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup_container EXIT

# 端口探测：优先 nc，回退 bash /dev/tcp
port_open() {
  local port=$1 host=${2:-127.0.0.1}
  if command -v nc >/dev/null 2>&1; then
    nc -z -w 3 "${host}" "${port}" >/dev/null 2>&1
  else
    (exec 3<>"/dev/tcp/${host}/${port}") >/dev/null 2>&1
  fi
}

# 通用轮询：wait_until "描述" 超时秒 间隔秒 检查函数名 [参数...]
wait_until() {
  local desc=$1 timeout=$2 interval=$3 fn=$4
  shift 4
  local waited=0
  while (( waited < timeout )); do
    if "${fn}" "$@"; then
      log "OK - ${desc}"
      return 0
    fi
    sleep "${interval}"
    waited=$((waited + interval))
  done
  log "超时 - ${desc}"
  return 1
}

# 阶段失败：打印原因、清理容器，返回 1（由调用方决定是否 return）
phase_fail() {
  local version=$1 msg=$2
  log "[${version}] 失败: ${msg}"
  cleanup_container
  return 1
}

# ---------- 阶段 A：等待容器就绪（systemd 空壳可执行 docker exec） ----------
wait_container_ready() {
  local waited=0
  while (( waited < CONTAINER_READY_TIMEOUT )); do
    if docker ps --filter "name=^/${CONTAINER_NAME}$" --format '{{.Names}}' | grep -q "${CONTAINER_NAME}"; then
      if docker exec "${CONTAINER_NAME}" sh -c 'true' 2>/dev/null; then
        return 0
      fi
    else
      # 容器已退出/死亡则立即失败并打印日志
      if docker inspect -f '{{.State.Status}}' "${CONTAINER_NAME}" 2>/dev/null | grep -qE 'exited|dead'; then
        log "容器异常退出，日志尾部："
        docker logs "${CONTAINER_NAME}" 2>&1 | tail -n 50
        return 1
      fi
    fi
    sleep "${POLL_INTERVAL}"
    waited=$((waited + POLL_INTERVAL))
  done
  log "容器未在 ${CONTAINER_READY_TIMEOUT}s 内就绪"
  return 1
}

# ---------- 阶段 B：手动启动实例（镜像不自动拉起）+ 12c 打开 PDB ----------
# 返回 0/1：startup 显式失败（出现 ORA- 且实例未打开）时打印 sqlplus 完整输出并返回 1，
# 由 run_version 走 phase_fail 终止本版本，避免进入就绪轮询空耗 20 分钟。
start_instance() {
  local version=$1 sid=$2 out
  # 11g 防御日志：确认容器内 /dev/shm 满足 memory_target 需求（ORA-00845 排查用）。
  # --shm-size 必须在 docker run 时指定（启动后无法修改），故此处仅做核查记录，
  # 取值采用务实方案固定 --shm-size=4g（见 run_version 版本分支）。
  if [ "${version}" = "11g" ]; then
    log "[${version}] 容器 /dev/shm 与内存参考信息（ORA-00845 排查）："
    docker exec "${CONTAINER_NAME}" sh -c 'df -h /dev/shm; echo "---"; grep -E "MemTotal|MemAvailable" /proc/meminfo' 2>/dev/null || true
    # 尽力读取 spfile 中的内存参数（spfile 为二进制，grep -a 按文本匹配；缺失时静默）
    docker exec "${CONTAINER_NAME}" su - oracle -c 'grep -a -oE "memory_[a-z]+=[0-9]+" $ORACLE_HOME/dbs/spfile*.ora 2>/dev/null || true' 2>/dev/null || true
  fi

  log "[${version}] 执行实例 startup（SID=${sid}）..."
  out=$(docker exec "${CONTAINER_NAME}" su - oracle -c "export ORACLE_SID=${sid}; echo 'startup;' | sqlplus -s / as sysdba" 2>&1)
  echo "$out" | grep -iE 'Database opened|already started|ORA-[0-9]+' | head -n 5 || true
  # 显式失败：出现 ORA- 错误且实例未成功打开（"Database opened" 视为成功；
  # 早期 ORA-32004 类参数警告因最终打开成功不受影响）
  if echo "${out}" | grep -qE 'ORA-[0-9]+' && ! echo "${out}" | grep -qi 'Database opened'; then
    log "[${version}] startup 失败（ORA 错误），sqlplus 完整输出："
    echo "${out}"
    return 1
  fi
  if [ "${version}" = "12c" ]; then
    log "[${version}] 打开 PDB：ALTER PLUGGABLE DATABASE ALL OPEN"
    out=$(docker exec "${CONTAINER_NAME}" su - oracle -c "export ORACLE_SID=${sid}; echo 'ALTER PLUGGABLE DATABASE ALL OPEN;' | sqlplus -s / as sysdba" 2>&1)
    echo "$out" | tail -n 3
  fi
  log "[${version}] startup 命令已下发，进入实例就绪轮询"
  return 0
}

# ---------- 阶段 C：实例就绪判据 ----------
# 实例 open_mode = READ WRITE（隐式验证 sqlplus / as sysdba 可用）
is_instance_open() {
  local sid=$1 out
  out=$(docker exec "${CONTAINER_NAME}" su - oracle -c "export ORACLE_SID=${sid}; echo 'select open_mode from v\$database;' | sqlplus -s / as sysdba" 2>/dev/null)
  [[ "${out}" == *"READ WRITE"* ]]
}

# 12c 专用：至少一个 PDB open_mode = READ WRITE
is_pdb_open() {
  local sid=$1 out
  out=$(docker exec "${CONTAINER_NAME}" su - oracle -c "export ORACLE_SID=${sid}; echo 'select open_mode from v\$pdbs;' | sqlplus -s / as sysdba" 2>/dev/null)
  [[ "${out}" == *"READ WRITE"* ]]
}

# 轮询：容器存活 + PMON 存在 + 1521 可连 + 实例 OPEN（12c 另需 PDB OPEN）
wait_instance_ready() {
  local version=$1 sid=$2 has_pdb=$3
  local waited=0 pmon=0 portok=0 openok=0 pdbsok=0
  while (( waited < INSTANCE_WAIT )); do
    pmon=0; portok=0; openok=0; pdbsok=0
    # ① 容器存活
    if ! docker ps --filter "name=^/${CONTAINER_NAME}$" --format '{{.Names}}' | grep -q "${CONTAINER_NAME}"; then
      log "容器意外退出，日志尾部："
      docker logs "${CONTAINER_NAME}" 2>&1 | tail -n 30
      return 1
    fi
    # ② PMON 进程存在（ora_pmon_<SID>）
    if docker exec "${CONTAINER_NAME}" sh -c 'grep -l ora_pmon_ /proc/*/cmdline >/dev/null 2>&1'; then
      pmon=1
    fi
    # ③ 端口 1521 可连
    if port_open "${HOST_PORT}"; then
      portok=1
    fi
    # ④ 实例 OPEN
    if is_instance_open "${sid}"; then
      openok=1
    fi
    # ⑤ 12c 额外要求 PDB OPEN
    if [ "${has_pdb}" = "1" ]; then
      is_pdb_open "${sid}" && pdbsok=1
    else
      pdbsok=1
    fi

    if (( pmon == 1 && portok == 1 && openok == 1 && pdbsok == 1 )); then
      return 0
    fi
    if (( waited % 60 == 0 )); then
      log "  等待实例就绪 ${waited}s ... (PMON=${pmon} 端口=${portok} OPEN=${openok} PDB=${pdbsok})"
    fi
    sleep "${POLL_INTERVAL}"
    waited=$((waited + POLL_INTERVAL))
  done
  log "实例就绪超时（${INSTANCE_WAIT}s）"
  return 1
}

# ---------- 阶段 D：监听修复 + 动态注册 ----------
listener_ready() {
  docker exec "${CONTAINER_NAME}" su - oracle -c 'lsnrctl services' 2>/dev/null | grep -q 'READY'
}

fix_listener() {
  local version=$1 oracle_home=$2 sid=$3
  local tns_admin="" lsnr_dir lsnr_ora out
  # 确定 listener.ora 实际目录：镜像若设置 TNS_ADMIN 则优先使用
  tns_admin=$(docker exec "${CONTAINER_NAME}" su - oracle -c 'echo ${TNS_ADMIN:-}' 2>/dev/null)
  if [ -n "${tns_admin}" ]; then
    lsnr_dir="${tns_admin}"
  else
    lsnr_dir="${oracle_home}/network/admin"
  fi
  lsnr_ora="${lsnr_dir}/listener.ora"

  log "[${version}] 写入 listener.ora（HOST=0.0.0.0，去写死主机名，不配静态 SID_LIST）: ${lsnr_ora}"
  # heredoc 用引号定界符防宿主展开；docker exec -i 透传 stdin
  if ! docker exec -i "${CONTAINER_NAME}" sh -c "cat > '${lsnr_ora}'" <<'LSNR_EOF'
LISTENER =
  (DESCRIPTION_LIST =
    (DESCRIPTION =
      (ADDRESS = (PROTOCOL = TCP)(HOST = 0.0.0.0)(PORT = 1521))
      (ADDRESS = (PROTOCOL = IPC)(KEY = EXTPROC1521))
    )
  )
LSNR_EOF
  then
    log "[${version}] 写入 listener.ora 失败"
    return 1
  fi

  log "[${version}] lsnrctl stop（未启动时失败可容忍）"
  docker exec "${CONTAINER_NAME}" su - oracle -c 'lsnrctl stop' >/dev/null 2>&1 || true
  sleep 2

  log "[${version}] lsnrctl start"
  out=$(docker exec "${CONTAINER_NAME}" su - oracle -c 'lsnrctl start' 2>&1)
  if ! echo "${out}" | grep -q 'completed successfully'; then
    log "首次 lsnrctl start 异常，5 秒后重试"
    sleep 5
    out=$(docker exec "${CONTAINER_NAME}" su - oracle -c 'lsnrctl start' 2>&1)
    if ! echo "${out}" | grep -q 'completed successfully'; then
      echo "${out}" | tail -n 25
      return 1
    fi
  fi

  log "[${version}] 动态注册：ALTER SYSTEM REGISTER"
  docker exec "${CONTAINER_NAME}" su - oracle -c "export ORACLE_SID=${sid}; echo 'ALTER SYSTEM REGISTER;' | sqlplus -s / as sysdba" 2>&1 | tail -n 3

  log "[${version}] 轮询 lsnrctl services 出现 READY（最多 ${REGISTER_WAIT}s）"
  if ! wait_until "监听服务注册 READY" "${REGISTER_WAIT}" 3 listener_ready; then
    log "lsnrctl services 输出（供排查服务名/注册状态）："
    docker exec "${CONTAINER_NAME}" su - oracle -c 'lsnrctl services' 2>&1 | tail -n 30
    return 1
  fi
  log "[${version}] 监听就绪，服务已动态注册"
  return 0
}

# ---------- 阶段 E：测试账号自动创建（幂等） ----------
# 镜像不预置测试账号（实测 go-ora 连接报 ORA-01017），跑测试前必须自动创建；
# 幂等：账号已存在则跳过创建，仅做连通性验证（脚本可重复运行）。
# 所有 SQL 写入容器内 /tmp/*.sql 后以 sqlplus -s / as sysdba @file 执行，
# 规避多层引号转义问题（12c 容器内已验证此方式可靠）。
# 参数：version sid oracle_home pdb service dsn_user dsn_pass
#   pdb 为空 = 11g（无 PDB，不切换 CONTAINER）；非空 = 12c PDB 名（如 JEMPDB）。
#   oracle_home 目前仅用于日志展示；sqlplus 经 su - oracle 登录 shell 的 PATH 调用，
#   与现有 start_instance/fix_listener 保持一致。
ensure_test_user() {
  local version=$1 sid=$2 oracle_home=$3 pdb=$4 service=$5 dsn_user=$6 dsn_pass=$7
  local check_sql create_sql out count

  # 1. 以 sysdba 判断账号是否存在（12c 先 ALTER SESSION SET CONTAINER，11g 直接查）
  check_sql="/tmp/check_test_user.sql"
  if ! docker exec -i "${CONTAINER_NAME}" sh -c "cat > '${check_sql}'" <<EOF
${pdb:+ALTER SESSION SET CONTAINER=${pdb};}
SELECT COUNT(*) FROM dba_users WHERE username='${dsn_user^^}';
EOF
  then
    log "[${version}] 写入存在性检查 SQL 失败"
    return 1
  fi
  out=$(docker exec "${CONTAINER_NAME}" su - oracle -c "export ORACLE_SID=${sid}; sqlplus -s / as sysdba @${check_sql}" 2>/dev/null)
  count=$(echo "${out}" | grep -oE '[0-9]+' | tail -n 1)
  count=${count:-0}
  log "[${version}] 测试账号 ${dsn_user} 存在性检查（ORACLE_HOME=${oracle_home}）: count=${count}"

  if [ "${count}" -gt 0 ]; then
    log "[${version}] 测试账号 ${dsn_user} 已存在，跳过创建（SKIP）"
  else
    # 2. 创建账号 + 授权（12c 带 CONTAINER 切换，11g 不带）
    create_sql="/tmp/create_test_user.sql"
    if ! docker exec -i "${CONTAINER_NAME}" sh -c "cat > '${create_sql}'" <<EOF
${pdb:+ALTER SESSION SET CONTAINER=${pdb};}
CREATE USER ${dsn_user} IDENTIFIED BY ${dsn_pass} DEFAULT TABLESPACE USERS QUOTA UNLIMITED ON USERS;
GRANT CONNECT, RESOURCE, CREATE SESSION, CREATE TABLE, CREATE SEQUENCE, CREATE TRIGGER, CREATE PROCEDURE, CREATE VIEW TO ${dsn_user};
EOF
    then
      log "[${version}] 写入创建 SQL 失败"
      return 1
    fi
    log "[${version}] 创建测试账号 ${dsn_user}（PDB=${pdb:-无，单实例}）"
    out=$(docker exec "${CONTAINER_NAME}" su - oracle -c "export ORACLE_SID=${sid}; sqlplus -s / as sysdba @${create_sql}" 2>&1)
    if ! echo "${out}" | grep -qi 'User created'; then
      log "[${version}] 创建测试账号失败，sqlplus 输出："
      echo "${out}" | tail -n 25
      return 1
    fi
  fi

  # 3. 最终验证：以测试账号 SELECT 'CONN_OK' FROM DUAL（走监听，模拟真实测试连接）
  log "[${version}] 验证测试账号连通性: ${dsn_user}@//localhost:${HOST_PORT}/${service}"
  out=$(docker exec "${CONTAINER_NAME}" su - oracle -c "echo \"SELECT 'CONN_OK' FROM DUAL;\" | sqlplus -s ${dsn_user}/${dsn_pass}@//localhost:${HOST_PORT}/${service}" 2>&1)
  if ! echo "${out}" | grep -q 'CONN_OK'; then
    log "[${version}] 测试账号连通性验证失败，sqlplus 输出："
    echo "${out}" | tail -n 25
    return 1
  fi
  log "[${version}] 测试账号 ${dsn_user} 就绪（可连接）"
  return 0
}

# ---------- 单版本全流程 ----------
run_version() {
  local version=$1
  local image sid oracle_home service has_pdb pdb shm_size dsn_user dsn_pass dsn test_rc

  case "${version}" in
    11g)
      image="${IMAGE_11G}"
      sid=${ORACLE_SID_11G:-JEM11GR2}
      oracle_home=/u01/app/oracle/product/11.2.0.4/dbhome_1
      service=${ORACLE_SERVICE_11G:-JEM11GR2}   # 服务名待实跑确认，用 ORACLE_SERVICE_11G 覆盖
      has_pdb=0
      pdb=""
      # ORA-00845：docker 默认 /dev/shm=64MB 不满足 memory_target，11g 需放大。
      # spfile 的 memory_target 在容器启动前无法可靠读取，且 --shm-size 必须
      # docker run 时指定（启动后不可改），故采用务实方案固定 4g（宿主机
      # 可用内存约 23GiB，充足）；startup 前会打印容器内 /dev/shm 实际大小核查。
      shm_size="--shm-size=4g"
      ;;
    12c)
      image="${IMAGE_12C}"
      sid=${ORACLE_SID_12C:-jem}
      oracle_home=/u01/app/oracle/product/12.2.0.1/dbhome_1
      service=${ORACLE_SERVICE_12C:-jempdb}
      has_pdb=1
      pdb="JEMPDB"
      shm_size=""   # 12c 镜像内存配置不受 /dev/shm 影响（已实测 PASS），保持 docker 默认
      ;;
    *)
      log "未知版本: ${version}"
      return 2
      ;;
  esac

  dsn_user=${ORACLE_DSN_USER:-testuser}
  dsn_pass=${ORACLE_DSN_PASS:-testpass}
  dsn="oracle://${dsn_user}:${dsn_pass}@localhost:${HOST_PORT}/${service}?SSL=false"

  log "======================== [${version}] 开始 ========================"

  # 1. 端口与残留容器检查
  if docker ps -a --filter "name=^/${CONTAINER_NAME}$" --format '{{.Names}}' | grep -q "${CONTAINER_NAME}"; then
    log "发现残留容器 ${CONTAINER_NAME}，脚本接管 stop+rm"
    cleanup_container
  fi
  if port_open "${HOST_PORT}"; then
    phase_fail "${version}" "宿主机 ${HOST_PORT} 端口已被其他进程占用（非本脚本容器），请先释放"
    return 1
  fi

  # 2. 启动容器（11g 追加 --shm-size=4g 规避 ORA-00845；12c 保持默认参数）
  if ! docker image inspect "${image}" >/dev/null 2>&1; then
    phase_fail "${version}" "镜像不存在: ${image}"
    return 1
  fi
  local -a run_args=()
  [ -n "${shm_size}" ] && run_args+=("${shm_size}")
  log "[${version}] docker run -d --name ${CONTAINER_NAME} ${shm_size} -p ${HOST_PORT}:1521 ${image}"
  if ! docker run -d --name "${CONTAINER_NAME}" "${run_args[@]}" -p "${HOST_PORT}:1521" "${image}" >/dev/null 2>&1; then
    phase_fail "${version}" "docker run 失败（请检查 1521 端口占用或 docker 状态）"
    return 1
  fi

  # 3. 等待容器就绪（可执行 docker exec）
  if ! wait_container_ready; then
    phase_fail "${version}" "容器未能在 ${CONTAINER_READY_TIMEOUT}s 内就绪"
    return 1
  fi
  log "[${version}] 容器就绪"

  # 4. 手动启动实例（镜像 Cmd=[init] 不会自动拉起）+ 12c 打开 PDB
  #    startup 显式失败（如 ORA-00845）时 start_instance 已打印完整输出，直接终止，
  #    避免进入就绪轮询空耗 INSTANCE_WAIT。
  if ! start_instance "${version}" "${sid}"; then
    phase_fail "${version}" "实例 startup 失败（详见上方 sqlplus 完整输出）"
    return 1
  fi

  # 5. 等待实例就绪（PMON + 端口 + OPEN + PDB）
  if ! wait_instance_ready "${version}" "${sid}" "${has_pdb}"; then
    phase_fail "${version}" "实例未就绪（启动超时）"
    return 1
  fi
  log "[${version}] 实例就绪：PMON 存在、${HOST_PORT} 可连、实例 OPEN"

  # 6. 监听修复 + 动态注册
  if ! fix_listener "${version}" "${oracle_home}" "${sid}"; then
    phase_fail "${version}" "监听修复/动态注册失败"
    return 1
  fi

  # 6.5 自动创建测试账号（镜像不预置，幂等；创建后验证连通性，失败即终止本版本）
  if ! ensure_test_user "${version}" "${sid}" "${oracle_home}" "${pdb}" "${service}" "${dsn_user}" "${dsn_pass}"; then
    phase_fail "${version}" "测试账号创建/验证失败"
    return 1
  fi

  # 7. 运行集成测试
  log "[${version}] 运行集成测试：ORACLE_DSN=${dsn}"
  ( cd "${PROJECT_ROOT}" && ORACLE_DSN="${dsn}" go test ./tests/ -count=1 )
  test_rc=$?

  # 8. 清理（无论成败）
  cleanup_container

  if [ "${test_rc}" -eq 0 ]; then
    log "[${version}] ${C_GREEN}PASS${C_RESET}"
    return 0
  else
    log "[${version}] ${C_RED}FAIL${C_RESET}（go test 退出码 ${test_rc}）"
    return 1
  fi
}

# ---------- 主流程 ----------
main() {
  local target=${1:-all}
  local -a versions=() results=()
  local v r failed=0

  case "${target}" in
    all)     versions=(11g 12c) ;;
    11g|12c) versions=("${target}") ;;
    *)       usage; exit 2 ;;
  esac

  if ! command -v docker >/dev/null 2>&1; then log "缺少依赖: docker"; exit 1; fi
  if ! command -v go >/dev/null 2>&1; then log "缺少依赖: go"; exit 1; fi

  for v in "${versions[@]}"; do
    if [ "${v}" = "11g" ] && [ "${SKIP_11G:-0}" = "1" ]; then
      log "[11g] 跳过（SKIP_11G=1）"; results+=("11g SKIP"); continue
    fi
    if [ "${v}" = "12c" ] && [ "${SKIP_12C:-0}" = "1" ]; then
      log "[12c] 跳过（SKIP_12C=1）"; results+=("12c SKIP"); continue
    fi
    if run_version "${v}"; then
      results+=("${v} PASS")
    else
      results+=("${v} FAIL")
    fi
  done

  log "==================== 汇总 ===================="
  for r in "${results[@]}"; do
    case "${r}" in
      *PASS) log "${C_GREEN}${r}${C_RESET}" ;;
      *FAIL) log "${C_RED}${r}${C_RESET}"; failed=1 ;;
      *)     log "${C_YELLOW}${r}${C_RESET}" ;;
    esac
  done
  if [ "${failed}" -eq 1 ]; then
    log "存在失败版本，整体退出码 1"
    return 1
  fi
  log "全部版本通过"
  return 0
}

main "$@"
