#!/usr/bin/env bash
# Gocker 런타임 라이프사이클 E2E 테스트 (root 권한 필요)
# 실패해도 중단하지 않고 끝까지 실행한 뒤, 실패한 명령어 목록만 모아서 출력한다. (set -e 사용 안 함)
set -u

GOCKER="${GOCKER:-./gocker}"
TMP=$(mktemp -d)
FAILED=()
LAST_OUT=""
n=0

# run_case <설명> <명령...> : 성공(exit 0)을 기대한다.
# 출력은 파이프가 아닌 파일로 받는다 (run -d 컨테이너가 gocker의 stdout을 물고 있어도 멈추지 않도록).
run_case() {
  local desc="$1"; shift
  n=$((n + 1))
  local log="$TMP/$n.log"
  timeout 60 "$@" >"$log" 2>&1
  local rc=$?
  if [ $rc -eq 0 ]; then
    echo "PASS  $desc"
  else
    echo "FAIL  $desc (exit=$rc)"
    echo "::group::$desc 출력"; cat "$log"; echo "::endgroup::"
    FAILED+=("$desc  ->  $*")
  fi
  LAST_OUT=$(tail -n 1 "$log" | tr -d '\r')
}

# expect_contains <설명> <기대 문자열> <명령...> : 성공 + 출력에 기대 문자열 포함
expect_contains() {
  local desc="$1" want="$2"; shift 2
  run_case "$desc" "$@"
  if [ -z "$want" ] || ! grep -q -- "$want" "$TMP/$n.log"; then
    echo "FAIL  $desc (출력에 '$want' 없음)"
    FAILED+=("$desc  ->  출력에 '$want' 없음")
  fi
}

# expect_exit <설명> <기대 종료 코드> <명령...> : 명령이 정확히 그 종료 코드로 끝나야 한다
expect_exit() {
  local desc="$1" want="$2"; shift 2
  n=$((n + 1))
  local log="$TMP/$n.log"
  timeout 60 "$@" >"$log" 2>&1
  local rc=$?
  if [ $rc -eq "$want" ]; then
    echo "PASS  $desc"
  else
    echo "FAIL  $desc (exit=$rc, 기대=$want)"
    echo "::group::$desc 출력"; cat "$log"; echo "::endgroup::"
    FAILED+=("$desc  ->  $* (exit=$rc, 기대=$want)")
  fi
}

# --- 1. create → start → stop → rm ---
run_case "create" $GOCKER create alpine sleep 300
CID=$LAST_OUT
expect_contains "create 후 ps -a에 존재" "$CID" $GOCKER ps -a -q
run_case "start" $GOCKER start "$CID"
expect_contains "start 후 ps에 존재(running)" "$CID" $GOCKER ps -q
run_case "stop" $GOCKER stop -t 2 "$CID"
run_case "rm" $GOCKER rm "$CID"

# --- 2. run -d (백그라운드) → stop → rm ---
run_case "run -d" $GOCKER run -d alpine sleep 300
DID=$LAST_OUT
expect_contains "run -d 후 ps에 존재(running)" "$DID" $GOCKER ps -q
run_case "run -d 후 stop" $GOCKER stop -t 2 "$DID"
run_case "run -d 후 rm" $GOCKER rm "$DID"

# --- 3. run (포그라운드) --rm ---
expect_contains "run 포그라운드 --rm" "hello" $GOCKER run --rm alpine echo hello

# --- 4. run -t (포그라운드 + 가상 터미널) --rm ---
# CI에는 실제 TTY가 없으므로 script(1)로 PTY를 붙여 터미널 경로를 검증한다
expect_contains "run -t 포그라운드 --rm" "tty-ok" script -qec "$GOCKER run -t --rm alpine echo tty-ok" /dev/null

# --- 5. 포그라운드 run 종료 코드 전파 (subreaper + wait) ---
expect_exit "run 종료 코드 전파" 7 $GOCKER run --rm alpine sh -c 'exit 7'
expect_exit "run -t 종료 코드 전파" 9 script -qec "$GOCKER run -t --rm alpine sh -c 'exit 9'" /dev/null

# 시그널로 죽은 컨테이너는 128+시그널 번호. 포그라운드 run을 백그라운드 잡으로 띄우고 밖에서 stop한다.
# sleep은 컨테이너의 PID 1이라 SIGTERM을 무시하므로 타임아웃 뒤 SIGKILL(9)로 종료된다 → 137
$GOCKER run --name e2e-sig alpine sleep 300 >"$TMP/sig.log" 2>&1 &
SIG_JOB=$!
for _ in $(seq 1 50); do
  $GOCKER ps | grep -q e2e-sig && break
  sleep 0.1
done
run_case "포그라운드 컨테이너 stop" $GOCKER stop -t 1 e2e-sig
wait $SIG_JOB
SIG_RC=$?
n=$((n + 1))
if [ $SIG_RC -eq 137 ]; then
  echo "PASS  run 시그널 종료 코드 (SIGKILL=137)"
else
  echo "FAIL  run 시그널 종료 코드 (exit=$SIG_RC, 기대=137)"
  echo "::group::run 출력"; cat "$TMP/sig.log"; echo "::endgroup::"
  FAILED+=("run 시그널 종료 코드  ->  exit=$SIG_RC, 기대=137")
fi
run_case "포그라운드 컨테이너 rm" $GOCKER rm e2e-sig

# --- 6. 이미 스스로 종료한 컨테이너 stop (신호 없이 성공해야 한다) → rm ---
run_case "run -d (즉시 종료)" $GOCKER run -d alpine true
XID=$LAST_OUT
sleep 1
run_case "종료된 컨테이너 stop" $GOCKER stop -t 2 "$XID"
run_case "종료된 컨테이너 rm" $GOCKER rm "$XID"

# --- 7. stop 도중 Ctrl+C(SIGINT): 곧바로 실패로 반환하고, 살아 있는 컨테이너를 STOPPED로 기록하지 않는다 ---
run_case "run -d (SIGTERM 무시)" $GOCKER run -d alpine sleep 300
IID=$LAST_OUT
# sleep은 PID 1이라 SIGTERM을 무시하므로 stop은 30초 대기에 들어간다. 1초 뒤 SIGINT를 보내 중단시킨다.
INT_T0=$SECONDS
timeout -s INT 1 $GOCKER stop -t 30 "$IID" >"$TMP/int.log" 2>&1
INT_RC=$?
INT_ELAPSED=$((SECONDS - INT_T0))
n=$((n + 1))
if [ $INT_RC -ne 0 ] && [ $INT_ELAPSED -lt 10 ]; then
  echo "PASS  stop 도중 SIGINT → 곧바로 실패로 반환 (${INT_ELAPSED}s)"
else
  echo "FAIL  stop 도중 SIGINT (exit=$INT_RC, ${INT_ELAPSED}s)"
  echo "::group::stop 출력"; cat "$TMP/int.log"; echo "::endgroup::"
  FAILED+=("stop 도중 SIGINT  ->  exit=$INT_RC, ${INT_ELAPSED}s")
fi
expect_contains "취소된 stop 후에도 running" "$IID" $GOCKER ps -q
run_case "취소된 stop 후 rm -f" $GOCKER rm -f "$IID"

# --- 결과 ---
echo
if [ ${#FAILED[@]} -eq 0 ]; then
  echo "모든 테스트 통과 (${n}개)"
  exit 0
fi
echo "실패한 명령어 (${#FAILED[@]}개):"
printf '  - %s\n' "${FAILED[@]}"
{
  echo "### 실패한 명령어"
  printf -- '- `%s`\n' "${FAILED[@]}"
} >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
exit 1
