#!/usr/bin/env bash
# gocker 전체 명령어 / 플래그 테스트 (root 필요). 결과는 $OUT/results.tsv 에 한 줄씩 기록한다.
# 사용법: go build -o gocker . && sudo bash scripts/fulltest.sh
# 필요한 것: runc, containerd(ctr), jq, script(1), assets/alpine-minirootfs.tar.gz. 다른 gocker 컨테이너가 없는 호스트에서 실행할 것 (기존 컨테이너를 모두 지운다).
# 실패해도 멈추지 않고 끝까지 진행한다.
set -u
set -a   # 이후 대입하는 변수는 모두 export: 테스트 명령 문자열(서브셸)에서 참조할 수 있도록

cd "$(dirname "$0")/.." || exit 1   # 저장소 루트 (기본 이미지 경로 assets/ 가 작업 디렉터리 기준이다)
OUT="${OUT:-/tmp/gocker-fulltest}"   # 결과와 테스트별 출력이 저장되는 곳 (실행할 때마다 비운다)
rm -rf "$OUT"; mkdir -p "$OUT"
RES="$OUT/results.tsv"; : > "$RES"
H="$OUT/helpers.sh"

cat > "$H" <<'EOF'
G=./gocker
ROOT=/var/lib/gocker
st()    { jq -r '.state' "$ROOT/state/$1.json"; }                      # 저장된 상태
ec()    { jq -r '.exitCode // "none"' "$ROOT/state/$1.json"; }         # 저장된 종료 코드
cmdof() { jq -r '.command | join(" ")' "$ROOT/state/$1.json"; }
imgof() { jq -r '.image' "$ROOT/state/$1.json"; }
obs()   { $G ps -a | awk -v id="$1" '$1==id{print $3}'; }              # ps가 보여주는 상태
cpid()  { $G ps -a | awk -v id="$1" '$1==id{print $4}'; }
nameof(){ $G ps -a | awk -v id="$1" '$1==id{print $2}'; }
idof()  { $G ps -a | awk -v n="$1" '$2==n{print $1}'; }                # 이름 → ID
count() { $G ps -a -q | wc -l; }
clog()  { cat "$ROOT/containers/$1/container.log"; }
gone()  {
  [ ! -e "$ROOT/state/$1.json" ] || { echo "record remains"; return 1; }
  [ ! -e "$ROOT/state/$1.lock" ] || { echo "lock file remains"; return 1; }
  [ ! -e "$ROOT/containers/$1" ] || { echo "bundle remains"; return 1; }
  ! runc list -q 2>/dev/null | grep -qx "$1" || { echo "runc container remains"; return 1; }
}
eq()    { [ "$1" = "$2" ] || { echo "got '$1', want '$2'"; return 1; }; }
nrec()  { ls "$ROOT/state" 2>/dev/null | grep -c ".json$"; }           # 레코드 수
nbun()  { ls "$ROOT/containers" 2>/dev/null | wc -l; }                   # 번들 수
nrunc() { runc list -q 2>/dev/null | wc -l; }                            # runc 컨테이너 수
snap()  { echo "ps=$(count) bundles=$(nbun) records=$(nrec) runc=$(nrunc)"; }  # 리소스 스냅샷
EOF
. "$H"

ms() { echo $(( $(date +%s%N) / 1000000 )); }
esc() { head -c 500 | tr '\t\r' '  ' | sed ':a;N;$!ba;s/\n/ | /g'; }

# rec ID SECTION DESC CMD EXPECT RC DUR VERDICT REASON OUTFILE
rec() {
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$1" "$2" "$3" "$(printf '%s' "$4" | tr '\t\n' '  ')" "$5" "$6" "$7" "$8" "$9" "$(esc < "${10}")" >> "$RES"
  printf '%-7s %-5s %s (rc=%s, %sms)%s\n' "$8" "$1" "$3" "$6" "$7" "${9:+  <- $9}"
}

# t ID SECTION DESC EXP_RC PATTERN CMD
#   EXP_RC  : 숫자 | nz(0이 아님) | any
#   PATTERN : 출력에 있어야 하는 ERE ('' 이면 검사 안 함, '!'로 시작하면 있으면 안 되는 패턴)
#   EXP_RC=any 이고 PATTERN='' 이면 판정 없이 관찰(INFO)로 기록한다.
t() {
  local id="$1" sec="$2" desc="$3" exp="$4" pat="$5" cmd="$6"
  local tmo="${TMO:-60}"
  OUTF="$OUT/$id.out"
  local t0; t0=$(ms)
  timeout -k 5 "$tmo" bash -c ". $H; $cmd" >"$OUTF" 2>&1 </dev/null
  RC=$?
  DUR=$(( $(ms) - t0 ))
  local verdict=PASS reason=""
  case "$exp" in
    any) ;;
    nz)  [ "$RC" -ne 0 ] || { verdict=FAIL; reason="종료 코드 0 (실패해야 함)"; } ;;
    *)   [ "$RC" -eq "$exp" ] || { verdict=FAIL; reason="종료 코드 $RC (기대 $exp)"; } ;;
  esac
  if [ -n "$pat" ]; then
    if [ "${pat:0:1}" = "!" ]; then
      if grep -qE -- "${pat:1}" "$OUTF"; then verdict=FAIL; reason="${reason:+$reason; }출력에 없어야 할 '${pat:1}' 있음"; fi
    else
      if ! grep -qE -- "$pat" "$OUTF"; then verdict=FAIL; reason="${reason:+$reason; }출력에 '$pat' 없음"; fi
    fi
  fi
  if [ "$DUR" -ge $(( tmo * 1000 )) ]; then verdict=FAIL; reason="${reason:+$reason; }시간 초과(${tmo}s)"; fi
  if [ "$exp" = any ] && [ -z "$pat" ]; then verdict=INFO; fi
  rec "$id" "$sec" "$desc" "$cmd" "$exp${pat:+ / $pat}" "$RC" "$DUR" "$verdict" "$reason" "$OUTF"
}

# durchk ID SECTION DESC LO HI : 직전 t의 소요 시간(ms) 검사
durchk() {
  local v=PASS r=""
  if [ "$DUR" -lt "$4" ] || [ "$DUR" -gt "$5" ]; then v=FAIL; r="소요 ${DUR}ms (기대 $4~$5ms)"; fi
  echo "duration=${DUR}ms" > "$OUT/$1.out"
  rec "$1" "$2" "$3" "(직전 명령의 소요 시간)" "$4~$5ms" "-" "$DUR" "$v" "$r" "$OUT/$1.out"
}

# blocked ID SECTION DESC CMD REASON : 선행 단계 실패로 실행하지 못한 테스트
blocked() {
  echo "not executed" > "$OUT/$1.out"
  rec "$1" "$2" "$3" "$4" "-" "-" "0" "BLOCKED" "$5" "$OUT/$1.out"
}

last() { tail -n 1 "$OUTF" | tr -d '\r'; }

clean() {
  $G ps -a -q 2>/dev/null | xargs -r $G rm -f >/dev/null 2>&1
  runc list -q 2>/dev/null | xargs -r -n1 runc delete -f >/dev/null 2>&1
}

restore_containerd() { systemctl start containerd >/dev/null 2>&1; }
trap 'restore_containerd' EXIT

clean
IMG=assets/alpine-minirootfs.tar.gz

############################################################ 전역
S=전역
t G01 $S "인자 없이 실행하면 사용법 출력"            0  'Usage: gocker'     '$G'
t G02 $S "help"                                       0  'Usage: gocker'     '$G help'
t G03 $S "-h"                                         0  'Usage: gocker'     '$G -h'
t G04 $S "--help"                                     0  'Usage: gocker'     '$G --help'
t G05 $S "알 수 없는 명령어는 실패"                  nz '알 수 없는 명령어' '$G frobnicate'
t G06 $S "create -h (도움말은 성공으로 끝나야 함)"   0  'Usage: gocker create' '$G create -h'
t G07 $S "start -h"                                   0  'Usage: gocker start'  '$G start -h'
t G08 $S "run -h"                                     0  'Usage: gocker run'    '$G run -h'
t G09 $S "stop -h"                                    0  'Usage: gocker stop'   '$G stop -h'
t G10 $S "ps -h"                                      0  'Usage: gocker ps'     '$G ps -h'
t G11 $S "rm -h"                                      0  'Usage: gocker rm'     '$G rm -h'
t G12 $S "정의되지 않은 플래그는 실패"               nz 'flag provided but not defined' '$G ps --bogus'

############################################################ create
S=create
t C01 $S "create alpine → 12자리 ID 출력"            0 '^[0-9a-f]{12}$' '$G create alpine'
C1=$(last)
t C02 $S "생성 직후 상태는 CREATED, 기본 명령은 /bin/sh" 0 '' 'eq "$(obs $C1)" CREATED && eq "$(st $C1)" CREATED && eq "$(cmdof $C1)" "/bin/sh"'
t C03 $S "인자 없는 create → 기본 이미지와 기본 명령" 0 '^[0-9a-f]{12}$' '$G create'
C3=$(last)
t C04 $S "인자 없는 create의 이미지/명령 기록"        0 '' 'eq "$(imgof $C3)" "$IMG" && eq "$(cmdof $C3)" "/bin/sh"'
t C05 $S "create alpine:latest sleep 300 → 명령 기록" 0 '^[0-9a-f]{12}$' '$G create alpine:latest sleep 300'
C5=$(last)
t C06 $S "지정한 명령이 기록됨"                       0 '' 'eq "$(cmdof $C5)" "sleep 300"'
t C07 $S "--name 지정"                                0 '^[0-9a-f]{12}$' '$G create --name c-web alpine sleep 300'
C7=$(last)
t C08 $S "ps -a에 이름 표시"                          0 '' 'eq "$(nameof $C7)" c-web'
SNAP=$(snap)
t C09 $S "같은 --name 으로 create 하면 거부"          nz 'already in use' '$G create --name c-web alpine sleep 300'
t C10 $S "거부된 create는 흔적을 남기지 않음"         0 '' 'eq "$(snap)" "$SNAP"'
t C11 $S "--hostname 지정"                            0 '^[0-9a-f]{12}$' '$G create --hostname h-create alpine hostname'
C11=$(last)
t C12 $S "--hostname 이 컨테이너에 적용됨 (start 후 로그 확인)" 0 '^h-create$' '$G start $C11 >/dev/null && sleep 1 && clog $C11'
t C13 $S "--rootfs 로 아카이브 지정 (인자는 모두 명령)" 0 '^[0-9a-f]{12}$' '$G create --rootfs $IMG sleep 300'
C13=$(last)
t C14 $S "--rootfs 사용 시 이미지/명령 기록"          0 '' 'eq "$(imgof $C13)" "$IMG" && eq "$(cmdof $C13)" "sleep 300"'
t C15 $S "아카이브 경로를 이미지 자리에 직접 지정"    0 '^[0-9a-f]{12}$' '$G create $IMG echo hi'
SNAP=$(snap)
t C16 $S "존재하지 않는 --rootfs 는 실패"             nz '' '$G create --rootfs /nonexistent/rootfs.tar.gz sh'
t C17 $S "실패한 create는 롤백되어 흔적이 없음"       0 '' 'eq "$(snap)" "$SNAP"'
t C18 $S "존재하지 않는 실행 파일은 실패 (runc create 단계)" nz '' '$G create alpine /no/such/binary'
t C19 $S "runc create 실패 후 롤백되어 흔적이 없음"   0 '' 'eq "$(snap)" "$SNAP"'
t C20 $S "create -t (거부하지 않음)"                  0 '^[0-9a-f]{12}$' '$G create -t alpine sh'
C20=$(last)
t C21 $S "create -t 컨테이너를 start 하면 RUNNING"    0 '' '$G start $C20 && sleep 1 && eq "$(obs $C20)" RUNNING'
t C22 $S "[관찰] 알 수 없는 이미지 이름(nginx:latest)" any '' '$G create nginx:latest; echo "rc=$?"'
t C23 $S "정의되지 않은 플래그는 실패"                nz 'flag provided but not defined' '$G create --bogus alpine'
clean

############################################################ start
S=start
SA=$($G create alpine sleep 300); SB=$($G create --name s-two alpine sleep 300); SC=$($G create alpine sleep 300)
SD=$($G create alpine sleep 300); SE=$($G create alpine sleep 300)
t S01 $S "start <ID>"                                 0 '' '$G start $SA'
t S02 $S "start 후 RUNNING, PID 기록, 프로세스 존재"  0 '' 'eq "$(obs $SA)" RUNNING && eq "$(st $SA)" RUNNING && p=$(cpid $SA) && [ "$p" -gt 0 ] && kill -0 "$p"'
t S03 $S "start <이름>"                               0 '' '$G start s-two && eq "$(obs $SB)" RUNNING'
t S04 $S "start <ID 접두어 6자>"                      0 '' '$G start ${SC:0:6} && eq "$(obs $SC)" RUNNING'
t S05 $S "3자 접두어는 인정하지 않음"                 nz 'not found' '$G start ${SD:0:3}'
t S06 $S "여러 컨테이너를 한 번에 start"              0 '' '$G start $SD $SE && eq "$(obs $SD)" RUNNING && eq "$(obs $SE)" RUNNING'
t S07 $S "이미 실행 중이면 실패"                      nz 'already running' '$G start $SA'
$G stop -t 1 $SA >/dev/null 2>&1
t S08 $S "STOPPED 컨테이너는 다시 start 불가"         nz 'cannot be started' '$G start $SA'
t S09 $S "존재하지 않는 컨테이너는 실패"              nz 'not found' '$G start no-such-container'
t S10 $S "인자 없으면 실패"                           nz '' '$G start'
SF=$($G create alpine sleep 300); SG=$($G create alpine sleep 300)
t S11 $S "여러 개 중 하나가 없으면 그 지점에서 중단"  nz 'not found' '$G start $SF no-such-container $SG'
t S12 $S "중단 전 대상은 시작됨, 이후 대상은 그대로"  0 '' 'eq "$(obs $SF)" RUNNING && eq "$(obs $SG)" CREATED'
SH=$($G create alpine true)
t S13 $S "곧바로 끝나는 명령: start 성공 후 STOPPED로 관측" 0 '' '$G start $SH && sleep 1 && eq "$(obs $SH)" STOPPED'
SI=$($G create alpine echo log-check)
t S14 $S "create/start 컨테이너의 출력은 container.log 에 기록" 0 '^log-check$' '$G start $SI >/dev/null && sleep 1 && clog $SI'
clean

############################################################ run
S=run
t R01 $S "포그라운드 run: 출력 전달"                  0 '^hello-run$' '$G run --name r-keep alpine echo hello-run'
t R02 $S "종료 후 컨테이너가 STOPPED로 남고 종료 코드 0 기록" 0 '' 'id=$(idof r-keep) && eq "$(st $id)" STOPPED && eq "$(ec $id)" 0'
SNAP=$(snap)
t R03 $S "--rm: 출력 전달"                            0 '^hi-rm$' '$G run --rm alpine echo hi-rm'
t R04 $S "--rm: 종료 후 흔적 없음"                    0 '' 'eq "$(snap)" "$SNAP"'
t R05 $S "종료 코드 전파 (exit 7)"                    7 '' "\$G run --rm alpine sh -c 'exit 7'"
t R06 $S "종료 코드 전파 + 기록 (exit 3, --rm 없음)"  3 '' "\$G run --name r-exit alpine sh -c 'exit 3'"
t R07 $S "종료 코드 3이 레코드에 기록됨"              0 '' 'eq "$(ec $(idof r-exit))" 3'
t R08 $S "-d: ID 출력 후 즉시 반환"                   0 '^[0-9a-f]{12}$' '$G run -d alpine sleep 300'
R8=$(last)
t R09 $S "-d: 백그라운드에서 RUNNING"                 0 '' 'eq "$(obs $R8)" RUNNING'
t R10 $S "-d --name"                                  0 '^[0-9a-f]{12}$' '$G run -d --name r-bg alpine sleep 300'
R10=$(last)
t R11 $S "-d --name: 이름으로 조회됨"                 0 '' 'eq "$(idof r-bg)" "$R10" && eq "$(obs $R10)" RUNNING'
SNAP=$(snap)
t R12 $S "-d --rm 은 거부"                            nz '함께 사용할 수 없습니다' '$G run -d --rm alpine sleep 300'
t R13 $S "거부된 run은 컨테이너를 만들지 않음"        0 '' 'eq "$(snap)" "$SNAP"'
t R14 $S "같은 --name 으로 run 하면 거부"             nz 'already in use' '$G run -d --name r-bg alpine sleep 300'
t R15 $S "이름 중복으로 거부된 run은 흔적 없음"       0 '' 'eq "$(snap)" "$SNAP"'
t R16 $S "-t: 가상 터미널로 실행"                     0 'tty-ok' 'script -qec "$G run -t --rm alpine echo tty-ok" /dev/null'
t R17 $S "-t: 컨테이너 stdout이 TTY"                  0 'IS_TTY' "script -qec \"\$G run -t --rm alpine sh -c '[ -t 1 ] && echo IS_TTY || echo NO_TTY'\" /dev/null"
t R18 $S "-t 없음: 컨테이너 stdout이 TTY가 아님"      0 'NO_TTY' "\$G run --rm alpine sh -c '[ -t 1 ] && echo IS_TTY || echo NO_TTY'"
t R19 $S "-t: 종료 코드 전파 (exit 9)"                9 '' "script -qec \"\$G run -t --rm alpine sh -c 'exit 9'\" /dev/null"
t R20 $S "-it: 표준 입력으로 셸 조작 (exit 4)"        4 'from-2-stdin' "printf 'echo from-\$((1+1))-stdin\\nexit 4\\n' | script -qec \"\$G run -it --rm alpine sh\" /dev/null"
t R21 $S "-i -t 를 따로 지정"                         4 'from-2-stdin' "printf 'echo from-\$((1+1))-stdin\\nexit 4\\n' | script -qec \"\$G run -i -t --rm alpine sh\" /dev/null"
t R22 $S "-i (TTY 없이 표준 입력 전달)"               0 '^piped-in$' 'echo piped-in | $G run -i --rm alpine cat'
t R23 $S "--hostname 지정"                            0 '^h-run$' '$G run --rm --hostname h-run alpine hostname'
t R24 $S "기본 호스트명은 gocker"                     0 '^gocker$' '$G run --rm alpine hostname'
t R25 $S "--rootfs 지정 (인자는 모두 명령)"           0 '^via-rootfs$' '$G run --rm --rootfs $IMG echo via-rootfs'
t R26 $S "인자 없는 run (기본 이미지, /bin/sh, 입력 없음 → 즉시 종료)" 0 '' '$G run --rm'
SNAP=$(snap)
t R27 $S "존재하지 않는 실행 파일은 실패"             nz '' '$G run --rm alpine /no/such/binary'
t R28 $S "실패한 run (--rm 없음)도 롤백되어 흔적 없음" 0 '' '$G run --name r-bad alpine /no/such/binary; [ $? -ne 0 ] && eq "$(snap)" "$SNAP" && eq "$(idof r-bad)" ""'
t R29 $S "이미지 뒤의 플래그는 컨테이너 명령의 인자"  0 'bin' '$G run --rm alpine ls -la /'
t R30 $S "PID 네임스페이스: 컨테이너 명령이 PID 1"    0 '^pid=1$' "\$G run --rm alpine sh -c 'echo pid=\$\$'"
t R31 $S "독립 rootfs (Alpine)"                       0 'Alpine' '$G run --rm alpine cat /etc/os-release'
t R32 $S "stderr 전달"                                0 '^to-err$' "\$G run --rm alpine sh -c 'echo to-err >&2'"
t R33 $S "기본 환경 변수 PATH"                        0 '/usr/local/sbin' "\$G run --rm alpine sh -c 'echo \$PATH'"
t R34 $S "-d: 출력은 container.log 에 기록"           0 '^to-log$' "id=\$(\$G run -d alpine sh -c 'echo to-log; sleep 300') && sleep 1 && clog \$id"
t R35 $S "-d -t: 백그라운드 TTY 컨테이너가 RUNNING"  0 '^[0-9a-f]{12}$' 'id=$($G run -d -t alpine sh) && echo "$id" && sleep 1 && eq "$(obs $id)" RUNNING'
t R36 $S "[관찰] -ti (묶은 단축 플래그)"              any '' '$G run -ti --rm alpine echo x; echo "rc=$?"'
# 포그라운드 run 중 gocker가 SIGINT/SIGTERM을 받으면 컨테이너에 SIGTERM → 2초 대기 → SIGKILL, gocker는 컨테이너의 종료 코드로 끝난다.
# timeout --preserve-status: timeout 자신의 124가 아니라 gocker의 종료 코드를 그대로 돌려받는다.
# sleep은 PID 1이라 SIGTERM을 무시하므로 SIGKILL로 끝난다 → 137
TMO=20 t R37 $S "포그라운드 run 중 SIGINT (--rm 없음) → 137로 종료" 137 '' 'timeout --preserve-status -s INT 2 $G run --name r-int alpine sleep 300'
durchk R41 $S "SIGINT 후 유예 시간(2초) 뒤 종료" 3500 8000
t R42 $S "SIGINT로 끝난 컨테이너: STOPPED, 종료 코드 137 기록, 프로세스 없음" 0 '' 'id=$(idof r-int) && eq "$(st $id)" STOPPED && eq "$(ec $id)" 137 && eq "$(cpid $id)" - && eq "$(runc state $id | jq -r .status)" stopped'
SNAP=$(snap)
TMO=20 t R38 $S "포그라운드 run --rm 중 SIGINT → 137로 종료, 컨테이너 정리됨" 137 '' 'timeout --preserve-status -s INT 2 $G run --rm --name r-int2 alpine sleep 300; rc=$?; sleep 1; eq "$(idof r-int2)" "" && eq "$(snap)" "$SNAP" && exit $rc'
# SIGTERM을 처리하는 컨테이너: 핸들러가 실행되고 그 종료 코드(43)로 끝나야 한다 (SIGKILL로 지우지 않는다)
TRAPCMD='trap "echo TRAP-TERM; exit 43" TERM INT; while :; do sleep 0.2; done'
SNAP=$(snap)
TMO=20 t R43 $S "run --rm 중 SIGTERM → 컨테이너 핸들러 실행, 43으로 종료" 43 'TRAP-TERM' 'timeout --preserve-status -s TERM 2 $G run --rm alpine sh -c "$TRAPCMD"'
t R44 $S "핸들러로 끝난 --rm 컨테이너도 정리됨"       0 '' 'eq "$(snap)" "$SNAP"'
TMO=20 t R45 $S "run -t 중 gocker 에 SIGTERM → 컨테이너에 SIGTERM 전달, 43으로 종료" 43 'TRAP-TERM' 'script -qec "timeout --preserve-status -s TERM 2 $G run -t --rm alpine sh -c \"\$TRAPCMD\"" /dev/null'
# 포그라운드 컨테이너를 밖에서 stop → 포그라운드 gocker는 128+SIGKILL=137로 종료
$G run --name r-fg alpine sleep 300 >"$OUT/R39.bg" 2>&1 </dev/null &
FG_JOB=$!
for _ in $(seq 1 50); do [ "$(obs "$(idof r-fg)")" = RUNNING ] && break; sleep 0.1; done
t R39 $S "포그라운드로 실행 중인 컨테이너를 다른 터미널에서 stop" 0 '' '$G stop -t 1 r-fg'
wait $FG_JOB; FG_RC=$?
t R40 $S "stop 당한 포그라운드 run 은 137 로 종료하고 종료 코드 기록" 0 '' 'eq "$FG_RC" 137 && eq "$(ec $(idof r-fg))" 137'
clean

############################################################ stop
S=stop
TA=$($G run -d alpine sleep 300); TB=$($G run -d alpine sleep 300)
t T01 $S "stop -t 1 (SIGTERM 무시 → SIGKILL)"         0 '' '$G stop -t 1 $TA'
durchk T02 $S "-t 1: 약 1초 뒤 SIGKILL 로 종료" 900 4000
t T03 $S "stop 후 STOPPED 기록, runc 도 stopped, 프로세스 없음" 0 '' 'eq "$(st $TA)" STOPPED && eq "$(obs $TA)" STOPPED && eq "$(runc state $TA | jq -r .status)" stopped && eq "$(cpid $TA)" -'
t T04 $S "stop -t=2 (등호 형식)"                      0 '' '$G stop -t=2 $TB'
durchk T05 $S "-t=2: 약 2초 뒤 종료" 1900 5000
TC=$($G run -d alpine sh -c 'trap "exit 0" TERM; while :; do sleep 0.2; done')
t T06 $S "SIGTERM 을 처리하는 컨테이너는 곧바로 종료" 0 '' '$G stop -t 10 $TC && eq "$(st $TC)" STOPPED'
durchk T07 $S "정상 종료는 타임아웃을 기다리지 않음" 0 3000
TD=$($G run -d alpine sleep 300)
TMO=40 t T08 $S "stop (기본 타임아웃 10초)"            0 '' '$G stop $TD && eq "$(st $TD)" STOPPED'
durchk T09 $S "기본 타임아웃: 약 10초 뒤 SIGKILL" 9900 14000
TE=$($G run -d alpine sleep 300); TF=$($G run -d --name t-named alpine sleep 300); TG=$($G run -d alpine sleep 300)
t T10 $S "여러 컨테이너를 한 번에 stop"               0 '' '$G stop -t 1 $TE $TG && eq "$(st $TE)" STOPPED && eq "$(st $TG)" STOPPED'
t T11 $S "stop <이름>"                                0 '' '$G stop -t 1 t-named && eq "$(st $TF)" STOPPED'
TH=$($G run -d alpine sleep 300)
t T12 $S "stop <ID 접두어 6자>"                       0 '' '$G stop -t 1 ${TH:0:6} && eq "$(st $TH)" STOPPED'
t T13 $S "이미 STOPPED 인 컨테이너 stop 은 성공 (멱등)" 0 '' '$G stop -t 1 $TH'
TI=$($G create alpine sleep 300)
t T14 $S "CREATED (시작 전) 컨테이너 stop"            0 '' '$G stop -t 1 $TI && eq "$(st $TI)" STOPPED'
TJ=$($G run -d alpine true); sleep 1
t T15 $S "스스로 종료한 컨테이너 stop → STOPPED 기록" 0 '' 'eq "$(st $TJ)" RUNNING && $G stop -t 5 $TJ && eq "$(st $TJ)" STOPPED'
durchk T16 $S "스스로 종료한 컨테이너는 대기 없이 반환" 0 1500
t T17 $S "존재하지 않는 컨테이너는 실패"              nz 'not found' '$G stop no-such-container'
t T18 $S "인자 없으면 실패"                           nz '' '$G stop'
TK=$($G run -d alpine sleep 300)
t T19 $S "-t 0 은 거부"                               nz '1초 이상' '$G stop -t 0 $TK'
t T20 $S "-t -1 은 거부"                              nz '1초 이상' '$G stop -t -1 $TK'
t T21 $S "-t abc 는 거부"                             nz 'invalid value' '$G stop -t abc $TK'
t T22 $S "거부된 stop 은 컨테이너를 건드리지 않음"    0 '' 'eq "$(obs $TK)" RUNNING'
TL=$($G run -d alpine sleep 300)
t T23 $S "여러 개 중 하나가 없으면 그 지점에서 중단"  nz 'not found' '$G stop -t 1 $TK no-such-container $TL'
t T24 $S "중단 전 대상은 중지됨, 이후 대상은 그대로"  0 '' 'eq "$(st $TK)" STOPPED && eq "$(obs $TL)" RUNNING'
TMO=30 t T25 $S "stop 대기 중 SIGINT → 곧바로 실패로 반환" 124 'context canceled|interrupt' 'timeout -s INT 1 $G stop -t 30 $TL'
durchk T26 $S "SIGINT 후 30초를 기다리지 않음" 900 8000
t T27 $S "취소된 stop 은 STOPPED 로 기록하지 않음"    0 '' 'eq "$(st $TL)" RUNNING && eq "$(obs $TL)" RUNNING'
t T28 $S "[관찰] detach 컨테이너의 종료 코드 기록"    any '' 'echo "exitCode=$(ec $TA)"'
clean

############################################################ ps
S=ps
t P01 $S "컨테이너가 없을 때 ps -a 는 헤더만 출력"    0 '' 'out=$($G ps -a); echo "$out"; eq "$(echo "$out" | wc -l)" 1'
t P02 $S "컨테이너가 없을 때 ps -q 는 빈 출력"        0 '' 'out=$($G ps -q); eq "$out" ""'
PC=$($G create --name p-created alpine sleep 300)
PR=$($G run -d --name p-running alpine sleep 300)
PS_=$($G run -d --name p-stopped alpine sleep 300); $G stop -t 1 $PS_ >/dev/null 2>&1
t P03 $S "ps: 실행 중인 컨테이너만"                   0 '' 'out=$($G ps); echo "$out"; echo "$out" | grep -q p-running && ! echo "$out" | grep -qE "p-created|p-stopped"'
t P04 $S "ps -a: 모든 상태"                           0 '' 'out=$($G ps -a); echo "$out"; eq "$(obs $PC)" CREATED && eq "$(obs $PR)" RUNNING && eq "$(obs $PS_)" STOPPED'
t P05 $S "ps --all"                                   0 '' 'eq "$($G ps --all | wc -l)" 4'
t P06 $S "헤더 열 구성"                               0 '^CONTAINER ID +NAME +STATUS +PID +CREATED +COMMAND' '$G ps -a'
t P07 $S "PID 열: 실행 중이면 숫자, 아니면 -"         0 '' '[ "$(cpid $PR)" -gt 0 ] && eq "$(cpid $PC)" - && eq "$(cpid $PS_)" -'
t P08 $S "ps -q: 실행 중인 ID만"                      0 '' 'eq "$($G ps -q)" "$PR"'
t P09 $S "ps -a -q: 모든 ID"                          0 '' 'out=$($G ps -a -q | sort | tr "\n" " "); want=$(printf "%s\n" $PC $PR $PS_ | sort | tr "\n" " "); eq "$out" "$want"'
t P10 $S "ps -q -a (순서 무관)"                       0 '' 'eq "$($G ps -q -a | wc -l)" 3'
t P11 $S "ps --all -q"                                0 '' 'eq "$($G ps --all -q | wc -l)" 3'
t P12 $S "추가 인자는 거부"                           nz '추가 인자' '$G ps extra'
t P13 $S "CREATED 열 표시 (N seconds ago)"            0 'seconds ago|minutes ago' '$G ps -a'
PL=$($G create alpine sh -c 'echo this-is-a-very-long-command-line-for-truncation')
t P14 $S "긴 명령은 30자로 잘려 ... 로 표시"          0 '\.\.\.$' '$G ps -a | awk -v id=$PL "\$1==id"'
t P15 $S "[관찰] ps -aq (묶은 단축 플래그)"           any '' '$G ps -aq; echo "rc=$?"'
clean

############################################################ rm
S=rm
MA=$($G run -d alpine sleep 300); $G stop -t 1 $MA >/dev/null 2>&1
t M01 $S "STOPPED 컨테이너 rm"                        0 '' '$G rm $MA'
t M02 $S "rm 후 레코드/잠금/번들/runc 컨테이너 없음"  0 '' 'gone $MA'
MB=$($G create alpine sleep 300)
t M03 $S "CREATED 컨테이너 rm"                        0 '' '$G rm $MB && gone $MB'
MC=$($G run -d alpine sleep 300); MC_PID=$(cpid $MC)
t M04 $S "RUNNING 컨테이너는 -f 없이 rm 불가"         nz 'running' '$G rm $MC'
t M05 $S "거부된 rm 은 컨테이너를 건드리지 않음"      0 '' 'eq "$(obs $MC)" RUNNING && kill -0 $MC_PID'
t M06 $S "rm -f: 실행 중인 컨테이너 강제 삭제"        0 '' '$G rm -f $MC'
t M07 $S "rm -f 후 흔적 없음, 프로세스도 종료됨"      0 '' 'gone $MC && ! kill -0 $MC_PID 2>/dev/null'
MD=$($G run -d alpine sleep 300); MD_PID=$(cpid $MD)
t M08 $S "rm --force"                                 0 '' '$G rm --force $MD && gone $MD && ! kill -0 $MD_PID 2>/dev/null'
ME=$($G create alpine sleep 300); MF=$($G create alpine sleep 300); MG=$($G create alpine sleep 300)
t M09 $S "여러 컨테이너를 한 번에 rm"                 0 '' '$G rm $ME $MF $MG && gone $ME && gone $MF && gone $MG'
MH=$($G create --name m-named alpine sleep 300)
t M10 $S "rm <이름>"                                  0 '' '$G rm m-named && gone $MH'
t M11 $S "삭제한 이름은 다시 사용할 수 있음"          0 '^[0-9a-f]{12}$' '$G create --name m-named alpine sleep 300'
MI=$(last)
t M12 $S "rm <ID 접두어 6자>"                         0 '' '$G rm ${MI:0:6} && gone $MI'
t M13 $S "존재하지 않는 컨테이너는 실패"              nz 'not found' '$G rm no-such-container'
t M14 $S "인자 없으면 실패"                           nz '' '$G rm'
MJ=$($G create alpine sleep 300); MK=$($G create alpine sleep 300)
t M15 $S "여러 개 중 하나가 없으면 그 지점에서 중단"  nz 'not found' '$G rm $MJ no-such-container $MK'
t M16 $S "중단 전 대상은 삭제됨, 이후 대상은 그대로"  0 '' 'gone $MJ && eq "$(obs $MK)" CREATED'
ML=$($G run -d alpine sleep 300); $G stop -t 1 $ML >/dev/null 2>&1
t M17 $S "rm -f 로 STOPPED 컨테이너 삭제"             0 '' '$G rm -f $ML && gone $ML'
# 유령 RUNNING: 저장은 RUNNING 인데 runc 가 컨테이너를 모르는 상태 (재부팅으로 /run/runc 가 비워진 경우와 같음)
MM=$($G run -d alpine sleep 300); runc delete -f $MM >/dev/null 2>&1
t M18 $S "유령 RUNNING: ps -a 에 STOPPED 로 표시, ps 에는 없음" 0 '' 'eq "$(st $MM)" RUNNING && eq "$(obs $MM)" STOPPED && ! $G ps -q | grep -q $MM'
t M19 $S "유령 RUNNING: -f 없이 rm 가능"              0 '' '$G rm $MM && gone $MM'
$G rm $MK >/dev/null 2>&1
t M20 $S "모두 삭제한 뒤 저장소/번들/runc 에 남은 것 없음" 0 '' 'eq "$(ls $ROOT/state | wc -l)" 0 && eq "$(ls $ROOT/containers | wc -l)" 0 && eq "$(runc list -q | wc -l)" 0'
clean

############################################################ containerd 를 쓰는 명령 (성공/실패만 기록)
S=containerd
t D01 $S "version"                                    0 'containerd version' '$G version'
TMO=180 t D02 $S "pull docker.io/library/alpine:latest" 0 '' '$G pull docker.io/library/alpine:latest'
TMO=180 t D03 $S "pull alpine:latest (짧은 이름)"      0 '' '$G pull alpine:latest'
TMO=180 t D04 $S "pull --platform=linux/amd64"         0 '' '$G pull --platform=linux/amd64 docker.io/library/busybox:latest'
TMO=180 t D05 $S "pull --snapshotter=overlayfs"        0 '' '$G pull --snapshotter=overlayfs docker.io/library/busybox:latest'
TMO=180 t D06 $S "pull --ns gocker-test"               0 '' '$G pull --ns gocker-test docker.io/library/alpine:latest'
t D07 $S "pull: 이미지 미지정은 실패"                 nz '' '$G pull'
t D08 $S "pull: 이미지 두 개는 실패"                  nz '' '$G pull docker.io/library/alpine:latest docker.io/library/busybox:latest'
TMO=120 t D09 $S "pull: 존재하지 않는 이미지는 실패"   nz '' '$G pull docker.io/library/gocker-no-such-image-xyz:latest'
t D10 $S "pull -h"                                    0 'Usage: gocker pull' '$G pull -h'
t D11 $S "images"                                     0 'alpine' '$G images'
t D12 $S "images --name busybox (필터)"               0 '' 'out=$($G images --name busybox); echo "$out"; echo "$out" | grep -q busybox && ! echo "$out" | grep -q alpine'
t D13 $S "images --platform=linux/amd64"              0 'alpine' '$G images --platform=linux/amd64'
t D14 $S "images --ns gocker-test"                    0 'alpine' '$G images --ns gocker-test'
t D15 $S "images: 추가 인자는 실패"                   nz '' '$G images extra'
t D16 $S "images -h"                                  0 'Usage: gocker images' '$G images -h'

# exec 대상: gocker run 은 runc 컨테이너를 만들기 때문에, containerd 컨테이너는 ctr 로 직접 만든다
TMO=120 t D20 $S "(준비) ctr 로 containerd 컨테이너 gk-exec 실행" 0 '' 'ctr -n default run -d docker.io/library/alpine:latest gk-exec sleep 3000 && ctr -n default task ls'
EXEC_READY=$RC
if [ "$EXEC_READY" -eq 0 ]; then
  t D21 $S "exec: 명령 실행"                           0 '^exec-ok$' '$G exec gk-exec echo exec-ok'
  t D22 $S "exec: 종료 코드 전파 (exit 5)"             5 '' "\$G exec gk-exec sh -c 'exit 5'"
  t D23 $S "exec -t"                                   0 'IS_TTY' "script -qec \"\$G exec -t gk-exec sh -c '[ -t 1 ] && echo IS_TTY || echo NO_TTY'\" /dev/null"
  t D24 $S "exec -i"                                   0 '^piped$' 'echo piped | $G exec -i gk-exec cat'
  t D25 $S "exec -it"                                  6 'in-5-exec' "printf 'echo in-\$((2+3))-exec\\nexit 6\\n' | script -qec \"\$G exec -it gk-exec sh\" /dev/null"
  t D26 $S "exec -d (백그라운드 실행)"                 0 '' '$G exec -d gk-exec touch /tmp/exec-d-marker && sleep 1 && $G exec gk-exec ls /tmp/exec-d-marker'
  t D27 $S "exec: 존재하지 않는 컨테이너는 실패"       nz '' '$G exec no-such-container echo x'
else
  for n in 21 22 23 24 25 26 27; do blocked "D$n" $S "exec 테스트 $n" "gocker exec gk-exec ..." "D20 (containerd 컨테이너 준비) 실패로 실행하지 못함"; done
fi
TMO=120 t D28 $S "(준비) ctr 로 gocker-test 네임스페이스에 gk-exec2 실행" 0 '' 'ctr -n gocker-test run -d docker.io/library/alpine:latest gk-exec2 sleep 3000'
if [ "$RC" -eq 0 ]; then
  t D29 $S "exec --ns gocker-test"                     0 '^ns-ok$' '$G exec --ns gocker-test gk-exec2 echo ns-ok'
else
  blocked D29 $S "exec --ns gocker-test" "gocker exec --ns gocker-test gk-exec2 echo ns-ok" "D28 (containerd 컨테이너 준비) 실패로 실행하지 못함"
fi
t D30 $S "exec: 인자 없으면 실패"                     nz '' '$G exec'
t D31 $S "exec: 명령 없이 컨테이너만 주면 실패"       nz '' '$G exec gk-exec'
t D32 $S "exec -h"                                    0 'Usage: gocker exec' '$G exec -h'
XR=$($G run -d --name x-runc alpine sleep 300)
t D33 $S "[관찰] gocker run 으로 만든 컨테이너에 exec" any '' '$G exec x-runc echo hi; echo "rc=$?"'
clean
ctr -n default task kill -s SIGKILL gk-exec >/dev/null 2>&1; ctr -n gocker-test task kill -s SIGKILL gk-exec2 >/dev/null 2>&1
sleep 1
ctr -n default container rm gk-exec >/dev/null 2>&1; ctr -n gocker-test container rm gk-exec2 >/dev/null 2>&1
ctr -n default task rm -f gk-exec >/dev/null 2>&1; ctr -n default container rm gk-exec >/dev/null 2>&1
ctr -n gocker-test task rm -f gk-exec2 >/dev/null 2>&1; ctr -n gocker-test container rm gk-exec2 >/dev/null 2>&1

############################################################ containerd 없이 lifecycle 동작 (Phase 2 완료 조건)
S=독립성
systemctl stop containerd >/dev/null 2>&1
t X01 $S "(준비) containerd 중지됨"                   0 '' '! systemctl is-active --quiet containerd'
t X02 $S "containerd 없이 run --rm"                   0 '^no-ctd$' '$G run --rm alpine echo no-ctd'
t X03 $S "containerd 없이 create → start → ps → stop → rm" 0 '' 'id=$($G create alpine sleep 300) && $G start $id && $G ps -q | grep -q $id && $G stop -t 1 $id && $G rm $id && gone $id'
t X04 $S "containerd 없이 version (gocker 버전은 출력)" 0 'gocker version' '$G version'
t X05 $S "containerd 없이 pull 은 실패"               nz '' '$G pull docker.io/library/alpine:latest'
restore_containerd
clean

echo
echo "=== summary"
awk -F'\t' '{c[$8]++} END{for (k in c) printf "%s=%d ", k, c[k]; print ""}' "$RES"
echo "results: $RES"
