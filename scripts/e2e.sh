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
