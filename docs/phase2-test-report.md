# Gocker Phase 2 전체 명령어 테스트 결과 보고서

- 테스트 일자: 2026-11-19
- 대상: `phase2_clean` 브랜치, 커밋 `e8bf747`
- 방식: EC2에서 root 권한으로 gocker의 모든 명령어와 플래그를 실제 runc / containerd 위에서 실행
- 테스트 스크립트: [scripts/fulltest.sh](../scripts/fulltest.sh) (192개 테스트)
- 코드 수정: 없음 (테스트와 원인 파악만 수행)
- 관련 문서: [아키텍처](architecture.md), [상태 모델과 라이프사이클](lifecycle.md), [설계 결정 기록](decisions.md), [운영 정책](policies.md)

## 1. 요약

| 구분 | 테스트 | 성공 | 실패 | 관찰 |
|---|---:|---:|---:|---:|
| runc 기반 명령 (create / start / run / stop / ps / rm, 전역, 독립성) | 162 | 150 | 8 | 4 |
| containerd 기반 명령 (version / pull / images / exec) | 30 | 24 | 5 | 1 |
| **합계** | **192** | **174** | **13** | **5** |

"관찰"은 성공/실패를 판정하지 않고 현재 동작만 기록한 항목이다 (지원하지 않는 입력 형태, 받아들인 한계 등).

**runc 기반 명령의 실패 8건은 원인 2가지로 묶인다.** 둘 다 이미 알려진 항목이고, 이번 테스트에서 새로 드러난 실패는 없다.

| # | 증상 | 해당 테스트 | 영향 |
|---|---|---|---|
| F1 | 모든 서브커맨드의 `-h`가 도움말을 출력한 뒤 종료 코드 1로 끝남 | G06~G11 (6건) | 기능에는 영향 없음 |
| F2 | `-t`로 만든 컨테이너가 생성 직후 죽음: `create -t` 후 `start` 실패, `run -d -t` 실패 | C21, R35 (2건) | 후속 명령(`start`) 진행 불가. [설계 결정 기록](decisions.md) 16번에서 받아들인 한계 |

핵심 라이프사이클은 모두 정상이다. 생성, 시작, 실행, 종료 코드 전파, SIGTERM → SIGKILL 전환, 포그라운드 실행 중 시그널 처리, 삭제 후 리소스 정리, 실패 시 롤백, 이름 중복 방지, 유령 RUNNING 처리, containerd 없이 동작하는 것까지 확인했다.

테스트 도중 다른 테스트를 막은 실패는 없었다 (실행하지 못한 테스트 0건). 테스트가 끝난 뒤 gocker 레코드, 번들, runc 컨테이너는 하나도 남지 않았다.

## 2. 이전 테스트 대비 변경

이전 테스트는 2026-11-16, 커밋 `bff5d62`에서 187개를 실행했다 (성공 167 / 실패 14 / 관찰 6). 그 뒤 커밋 4개가 들어갔다.

| 커밋 | 내용 |
|---|---|
| `cac3555` | `run -i`가 가상 터미널 없이도 표준 입력을 컨테이너에 전달하도록 수정 |
| `8453929` | Run 관련 메서드와 타입을 `run.go`로 분리 |
| `b139bbb` | 포그라운드 `run` 취소 시 컨테이너를 SIGTERM 후 SIGKILL로 끝내고 종료 코드를 반환하도록 수정 |
| `e8bf747` | 포그라운드 `run` 시그널 E2E 케이스 추가 |

### 결과가 바뀐 테스트

| 테스트 | 이전 (`bff5d62`) | 이번 (`e8bf747`) |
|---|---|---|
| R22 `run -i` (TTY 없음) | 실패. 표준 입력이 전달되지 않음 (이전 보고서의 F2) | 성공 |
| R37 포그라운드 `run` 중 SIGINT (`--rm` 없음) | 관찰. gocker가 `context canceled`로 끝나고 컨테이너는 RUNNING으로 남음 | 성공. 137로 종료, 컨테이너는 STOPPED |
| R38 포그라운드 `run --rm` 중 SIGINT | 성공 (정리만 확인) | 성공. 종료 코드 137까지 확인 |

### 스크립트 변경

| 테스트 | 변경 |
|---|---|
| R37 | 관찰에서 검사로 바꿨다. gocker의 종료 코드가 137인지 확인한다 |
| R38 | 정리뿐 아니라 종료 코드(137)도 확인한다 |
| R41, R42 | 새로 추가. SIGINT 뒤 유예 시간만큼 기다리는지, 레코드에 STOPPED와 137이 남는지 확인한다 |
| R43, R44 | 새로 추가. `--rm` 실행 중 SIGTERM을 받으면 컨테이너의 핸들러가 실행되고 그 종료 코드로 끝나는지, 그 뒤 정리되는지 확인한다 |
| R45 | 새로 추가. `run -t`에서 gocker에 직접 온 SIGTERM이 컨테이너에 전달되는지 확인한다 |
| C21, R35 | 관찰에서 검사로 바꿨다. 이전 보고서에서 이미 실패(F3)로 분류했던 항목이라 판정을 스크립트와 맞췄다 |

이전 보고서의 F3은 이번 보고서에서 F2가 되었다. 내용은 같다.

## 3. 테스트 환경

| 항목 | 값 |
|---|---|
| 호스트 | AWS EC2, Ubuntu 24.04.5 LTS, x86_64, 2 vCPU / 3.7 GiB |
| 커널 / cgroup | 7.0.0-1014-aws / cgroup v2 |
| Go | 1.26.6 |
| runc | 1.3.4-0ubuntu1~24.04.1 |
| containerd | 2.2.1 |
| rootfs | alpine-minirootfs 3.20.3 (저장소 CI와 같은 URL) |

## 4. 사전 테스트

| 테스트 | 결과 |
|---|---|
| `go vet ./...` | 성공 |
| `go test ./...` | 성공 (88개 통과, 실패 0) |
| `go test -race ./...` | 성공 (경쟁 상태 검출 없음) |
| [scripts/e2e.sh](../scripts/e2e.sh) | 성공 (41개 통과) |

## 5. 명령어별 결과

| 명령어 | 테스트 | 성공 | 실패 | 관찰 | 판정 |
|---|---:|---:|---:|---:|---|
| 전역 (사용법, `help`, 알 수 없는 명령) | 6 | 6 | 0 | 0 | 성공 |
| `create` | 24 | 21 | 2 | 1 | 일부 실패 (F1, F2) |
| `start` | 15 | 14 | 1 | 0 | 일부 실패 (F1) |
| `run` | 46 | 43 | 2 | 1 | 일부 실패 (F1, F2) |
| `stop` | 29 | 27 | 1 | 1 | 일부 실패 (F1) |
| `ps` | 16 | 14 | 1 | 1 | 일부 실패 (F1) |
| `rm` | 21 | 20 | 1 | 0 | 일부 실패 (F1) |
| containerd 없이 lifecycle 동작 | 5 | 5 | 0 | 0 | 성공 |
| `version` (containerd) | 1 | 1 | 0 | 0 | 성공 |
| `pull` (containerd) | 9 | 7 | 2 | 0 | 일부 실패 |
| `images` (containerd) | 6 | 5 | 1 | 0 | 일부 실패 |
| `exec` (containerd) | 14 | 11 | 2 | 1 | 일부 실패 |

`-h` 실패(F1)를 빼면 `start`, `stop`, `ps`, `rm`은 모든 플래그와 인자 형태에서 성공했다.

### 플래그별 결과 (runc 기반 명령)

| 명령어 | 플래그 / 인자 | 결과 |
|---|---|---|
| `create` | 이미지 이름, 인자 없음, 아카이브 경로 직접 지정 | 성공 |
| `create` | `--name` (중복 거부 포함) | 성공 |
| `create` | `--hostname` | 성공 |
| `create` | `--rootfs` | 성공 |
| `create` | `-t` | **실패 (F2)**: 생성은 되지만 이후 `start` 불가 |
| `create` | `-h` | **실패 (F1)** |
| `start` | ID, 이름, ID 접두어, 여러 개 | 성공 |
| `start` | `-h` | **실패 (F1)** |
| `run` | 포그라운드, 종료 코드 전파 | 성공 |
| `run` | `--rm` | 성공 |
| `run` | `-d` | 성공 |
| `run` | `-d --rm` (거부) | 성공 |
| `run` | `-t` | 성공 |
| `run` | `-it`, `-i -t` | 성공 |
| `run` | `-i` (TTY 없음) | 성공 |
| `run` | `-d -t` | **실패 (F2)** |
| `run` | `--name` (중복 거부 포함) | 성공 |
| `run` | `--hostname` | 성공 |
| `run` | `--rootfs` | 성공 |
| `run` | 실행 중 SIGINT / SIGTERM (`--rm` 유무, `-t` 포함) | 성공 |
| `run` | `-h` | **실패 (F1)** |
| `stop` | `-t N`, `-t=N`, 기본 타임아웃, 잘못된 값 거부 | 성공 |
| `stop` | ID, 이름, ID 접두어, 여러 개 | 성공 |
| `stop` | `-h` | **실패 (F1)** |
| `ps` | 플래그 없음, `-a`, `--all`, `-q`, 조합 | 성공 |
| `ps` | `-h` | **실패 (F1)** |
| `rm` | 플래그 없음, `-f`, `--force` | 성공 |
| `rm` | ID, 이름, ID 접두어, 여러 개 | 성공 |
| `rm` | `-h` | **실패 (F1)** |

## 6. 실패 항목 상세 (runc 기반 명령)

코드 위치는 커밋 `e8bf747` 기준이다.

### F1. 서브커맨드의 `-h`가 종료 코드 1로 끝난다

- **해당 테스트**: G06~G11 (`create`, `start`, `run`, `stop`, `ps`, `rm`)
- **증상**: 도움말은 정상 출력되지만, 마지막에 `gocker: flag: help requested`가 붙고 종료 코드가 1이다. `gocker -h`, `gocker help`는 정상(0)이다.
- **원인**: 각 핸들러가 `flag.FlagSet.Parse`의 에러를 그대로 반환한다. Go의 `flag` 패키지는 `-h`를 만나면 Usage를 출력한 뒤 `flag.ErrHelp`를 반환하는데, 이것이 일반 에러처럼 `main`까지 올라가 에러 메시지 출력과 `os.Exit(1)`로 이어진다.
- **코드 위치**
  - `Parse` 에러를 그대로 반환하는 곳: [create.go:34](../pkg/cli/create.go), [start.go:24](../pkg/cli/start.go), [run.go:42](../pkg/cli/run.go), [stop.go:32](../pkg/cli/stop.go), [ps.go:35](../pkg/cli/ps.go), [rm.go:29](../pkg/cli/rm.go)
  - 에러를 출력하고 1로 종료하는 곳: [main.go:34-37](../main.go)
- **후속 영향**: 없음. 도움말 내용은 정상이고 다른 명령에 영향을 주지 않는다. 다만 스크립트에서 `-h`의 종료 코드를 확인하면 실패로 처리된다.
- **참고**: containerd 기반 명령(`pull`, `images`, `exec`)의 `-h`도 같은 증상이다 (D10, D16, D32). 같은 패턴이 [pull.go:38](../pkg/cli/pull.go), [images.go:37](../pkg/cli/images.go), [exec.go:33](../pkg/cli/exec.go)에 있다.

### F2. `-t`로 만든 컨테이너가 생성 직후 죽는다

- **해당 테스트**: C21 (`create -t` 후 `start`), R35 (`run -d -t`)
- **증상**
  - `gocker create -t alpine sh`: 성공(0)하고 ID를 출력한다 (C20). 그러나 직후 `gocker start <ID>`는 `cannot be started from STOPPED state`로 실패한다 (종료 코드 1).
  - `gocker run -d -t alpine sh`: `start container: container "..." cannot be started from STOPPED state`로 실패한다 (종료 코드 1). ID가 출력되지 않고 컨테이너는 정리되어 남지 않는다.
- **원인**: gocker에는 데몬이 없어서 `Create`가 `runc create` 직후 PTY master를 닫는다. master가 닫히면 그 터미널을 쓰던 컨테이너 init 프로세스가 hangup으로 죽는다. 2026-11-16에 진단한 결과는 다음과 같다 (관련 코드는 그 뒤 바뀌지 않았다).

  | | `create -t` 직후 | `create` (TTY 없음) 직후 |
  |---|---|---|
  | runc 상태 | `stopped` | `created` |
  | init 프로세스 | 없음 | 살아 있음 (`runc:[2:INIT]`) |
  | 저장된 상태 | `CREATED` | `CREATED` |

  이후 `start`는 runc 관측 상태(STOPPED)를 보고 전이를 거부한다. `run -d -t`는 내부적으로 같은 `Create`를 거치므로 같은 이유로 실패하고, 시작 실패 후 정리 로직이 컨테이너를 삭제한다.
- **코드 위치**
  - master를 닫는 곳 (주석에 "이후 start는 실패한다(의도된 동작)"이라고 적혀 있음): [runtime.go:131-134](../pkg/runtime/runtime.go)
  - `run -d`가 이 `Create`를 사용하는 곳: [run.go:102](../pkg/runtime/run.go)
- **후속 영향 (진행하지 못하는 명령)**
  - `create -t`로 만든 컨테이너는 **`start`를 할 수 없다.** 할 수 있는 것은 `ps -a`(STOPPED로 표시)와 `rm`뿐이다.
  - `run -d -t`는 컨테이너 자체가 만들어지지 않으므로 그 뒤의 `ps`, `stop`, `rm`을 진행할 대상이 없다.
  - 포그라운드 `run -t`, `run -it`는 gocker가 master를 계속 쥐고 있어 정상 동작한다 (R16~R21, R45).
- **설계상 위치**: 데몬이 없는 구조에서 두 조합을 거부하지 않고 실패로 두기로 했다 ([설계 결정 기록](decisions.md) 16번).

## 7. 관찰 항목 (판정 없음)

| 테스트 | 내용 | 현재 동작 |
|---|---|---|
| C22 | `create nginx:latest` (알 수 없는 이미지) | 실패(1)하지만 원인 메시지가 부정확하다. 이미지로 인식하지 못한 첫 인자를 명령으로 취급해 `exec: "nginx:latest": executable file not found`가 나온다 ([image.go:25-37](../pkg/image/image.go)). |
| R36 | `run -ti` | `flag provided but not defined: -ti`. `-it`만 별도 플래그로 정의돼 있다. |
| P15 | `ps -aq` | `flag provided but not defined: -aq`. Go `flag` 패키지는 단축 플래그 묶음을 지원하지 않는다. `-a -q`는 동작한다. |
| T28 | detach 컨테이너의 종료 코드 | 기록되지 않는다 (`exitCode` 없음). 데몬이 없는 구조에서 받아들인 한계다. |
| D33 | `gocker run`으로 만든 컨테이너에 `exec` | `container "x-runc" in namespace "default": not found`. `exec`는 아직 containerd 컨테이너만 대상으로 한다. |

테스트 중 확인한 그 밖의 사항:

- containerd가 꺼져 있으면 `gocker version`은 성공하지만 연결 타임아웃 때문에 10초가 걸린다 (X04, 10015ms). `pull`도 같은 시간 뒤에 실패한다 (X05).

## 8. 포그라운드 실행 중 시그널 처리

이번 테스트에서 새로 검사한 부분이다. gocker가 SIGINT나 SIGTERM을 받으면 컨테이너에 SIGTERM을 보내고, 2초 안에 끝나지 않으면 SIGKILL을 보낸 뒤 컨테이너의 종료 코드로 끝나야 한다 ([운영 정책](policies.md) 1.4).

| 테스트 | 상황 | gocker 종료 코드 | 소요 시간 | 확인한 것 |
|---|---|---:|---:|---|
| R37, R41, R42 | `run`, SIGTERM을 무시하는 컨테이너(`sleep`), gocker에 SIGINT | 137 | 4027ms | 2초 유예 뒤 SIGKILL. 레코드는 STOPPED, `exitCode` 137, 프로세스 없음, runc 상태 `stopped` |
| R38 | `run --rm`, 같은 컨테이너, gocker에 SIGINT | 137 | 5253ms (확인용 대기 1초 포함) | 종료 후 컨테이너가 정리되어 남은 것 없음 |
| R43, R44 | `run --rm`, SIGTERM을 처리하는 컨테이너, gocker에 SIGTERM | 43 | 2282ms | 컨테이너 핸들러가 실행됨(`TRAP-TERM` 출력). 그 종료 코드로 끝나고, 그 뒤 정리됨 |
| R45 | `run -t --rm`, SIGTERM을 처리하는 컨테이너, gocker에 SIGTERM | 43 | 2311ms | 가상 터미널 모드에서도 컨테이너가 SIGTERM을 받음 |
| R39, R40 | 포그라운드 `run`을 다른 터미널에서 `stop -t 1` | 137 | | 포그라운드 gocker가 137로 끝나고 레코드에 기록됨 |

소요 시간은 gocker를 띄운 시점부터 잰 것이고, 테스트는 띄운 뒤 2초가 지나서 시그널을 보낸다. 따라서 R43과 R45는 시그널을 받고 0.3초 안에 끝났다. 컨테이너의 반복문이 0.2초마다 핸들러를 확인하기 때문이다.

이전 동작과의 비교는 2장에 있다.

## 9. containerd 기반 명령 (성공/실패만 기록)

요청에 따라 실패 원인은 분석하지 않았다.

| 테스트 | 명령 | 결과 |
|---|---|---|
| D01 | `gocker version` | 성공 |
| D02 | `gocker pull docker.io/library/alpine:latest` | 성공 |
| D03 | `gocker pull alpine:latest` (짧은 이름) | **실패** (`failed to resolve reference "alpine:latest"`) |
| D04 | `gocker pull --platform=linux/amd64 ...` | 성공 |
| D05 | `gocker pull --snapshotter=overlayfs ...` | 성공 |
| D06 | `gocker pull --ns gocker-test ...` | 성공 |
| D07 | `gocker pull` (이미지 미지정 → 거부) | 성공 |
| D08 | `gocker pull A B` (이미지 두 개 → 거부) | 성공 |
| D09 | 존재하지 않는 이미지 → 실패해야 함 | 성공 |
| D10 | `gocker pull -h` | **실패** (종료 코드 1, F1과 같은 증상) |
| D11 | `gocker images` | 성공 |
| D12 | `gocker images --name busybox` | 성공 |
| D13 | `gocker images --platform=linux/amd64` | 성공 |
| D14 | `gocker images --ns gocker-test` | 성공 |
| D15 | `gocker images extra` (추가 인자 → 거부) | 성공 |
| D16 | `gocker images -h` | **실패** (종료 코드 1, F1과 같은 증상) |
| D21 | `gocker exec <컨테이너> echo` | 성공 |
| D22 | `gocker exec` 종료 코드 전파 | 성공 |
| D23 | `gocker exec -t` | 성공 |
| D24 | `echo piped \| gocker exec -i <컨테이너> cat` | **실패** (출력은 나오지만 끝나지 않아 60초 타임아웃) |
| D25 | `gocker exec -it` | 성공 |
| D26 | `gocker exec -d` | 성공 |
| D27 | 존재하지 않는 컨테이너 → 실패해야 함 | 성공 |
| D29 | `gocker exec --ns gocker-test` | 성공 |
| D30 | `gocker exec` (인자 없음 → 거부) | 성공 |
| D31 | `gocker exec <컨테이너>` (명령 없음 → 거부) | 성공 |
| D32 | `gocker exec -h` | **실패** (종료 코드 1, F1과 같은 증상) |

`exec` 테스트는 gocker로는 containerd 컨테이너를 만들 수 없어서, `ctr run`으로 만든 컨테이너를 대상으로 했다 (D20, D28 준비 단계 성공). containerd 기반 명령의 결과는 이전 테스트와 같다.

## 10. Phase 2 완료 조건 대비

| 로드맵 완료 조건 | 결과 | 근거 테스트 |
|---|---|---|
| containerd 없이 create / start / ps / stop / rm 동작 | 충족 | X02, X03 (containerd 서비스를 중지한 상태에서 실행) |
| Container State가 올바르게 전환된다 | 충족 | C02, S02, S07, S08, T03, T14, R42 |
| Process 종료가 정상적으로 감지된다 | 충족 | S13, R02, R05~R07, T15, R40, R42 |
| SIGTERM 후 Timeout 발생 시 SIGKILL | 충족 | T01~T09 (SIGTERM 처리 시 즉시 종료, 무시 시 타임아웃 뒤 SIGKILL), R37, R41 |
| 삭제 후 runc Resource가 남지 않는다 | 충족 | M02, M07, M20, R44 |
| 중간 실패 시 생성한 Resource가 Rollback된다 | 충족 | C17, C19, R28 |

## 11. 다시 실행하는 방법

```bash
go build -o gocker .
```

```bash
sudo bash scripts/fulltest.sh
```

- 필요한 것: runc, containerd(`ctr`), `jq`, `script(1)`, `assets/alpine-minirootfs.tar.gz`, 인터넷 연결(`pull` 테스트).
- **호스트의 gocker 컨테이너를 모두 지우고, 독립성 테스트 동안 containerd 서비스를 멈춘다.** 테스트 전용 호스트에서 실행해야 한다.
- 결과는 `/tmp/gocker-fulltest/results.tsv`에 테스트마다 한 줄씩 기록된다 (`OUT` 환경 변수로 위치를 바꿀 수 있다). 테스트별 출력은 같은 디렉터리의 `<ID>.out`에 있다.
- 이번 실행에서 테스트 명령의 소요 시간 합계는 약 141초였다 (준비 단계 제외).

## 부록. 전체 테스트 목록

표기: `gocker`는 테스트한 바이너리, `$C1` 같은 변수는 앞 단계에서 만든 컨테이너의 ID다. `eq`, `st`, `obs`, `gone` 등은 [fulltest.sh](../scripts/fulltest.sh) 앞부분에 정의된 보조 함수다 (`st`: 저장된 상태, `ec`: 저장된 종료 코드, `obs`: `ps`가 보여주는 상태, `gone`: 레코드/잠금/번들/runc 컨테이너가 모두 없는지, `snap`: 리소스 개수 스냅샷).

### 전역

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| G01 | 인자 없이 실행하면 사용법 출력 | `gocker` | 성공 |  |
| G02 | help | `gocker help` | 성공 |  |
| G03 | -h | `gocker -h` | 성공 |  |
| G04 | --help | `gocker --help` | 성공 |  |
| G05 | 알 수 없는 명령어는 실패 | `gocker frobnicate` | 성공 |  |
| G12 | 정의되지 않은 플래그는 실패 | `gocker ps --bogus` | 성공 |  |

### create

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| G06 | create -h (도움말은 성공으로 끝나야 함) | `gocker create -h` | **실패** | F1 |
| C01 | create alpine → 12자리 ID 출력 | `gocker create alpine` | 성공 |  |
| C02 | 생성 직후 상태는 CREATED, 기본 명령은 /bin/sh | `eq "$(obs $C1)" CREATED && eq "$(st $C1)" CREATED && eq "$(cmdof $C1)" "/bin/sh"` | 성공 |  |
| C03 | 인자 없는 create → 기본 이미지와 기본 명령 | `gocker create` | 성공 |  |
| C04 | 인자 없는 create의 이미지/명령 기록 | `eq "$(imgof $C3)" "$IMG" && eq "$(cmdof $C3)" "/bin/sh"` | 성공 |  |
| C05 | create alpine:latest sleep 300 → 명령 기록 | `gocker create alpine:latest sleep 300` | 성공 |  |
| C06 | 지정한 명령이 기록됨 | `eq "$(cmdof $C5)" "sleep 300"` | 성공 |  |
| C07 | --name 지정 | `gocker create --name c-web alpine sleep 300` | 성공 |  |
| C08 | ps -a에 이름 표시 | `eq "$(nameof $C7)" c-web` | 성공 |  |
| C09 | 같은 --name 으로 create 하면 거부 | `gocker create --name c-web alpine sleep 300` | 성공 |  |
| C10 | 거부된 create는 흔적을 남기지 않음 | `eq "$(snap)" "$SNAP"` | 성공 |  |
| C11 | --hostname 지정 | `gocker create --hostname h-create alpine hostname` | 성공 |  |
| C12 | --hostname 이 컨테이너에 적용됨 (start 후 로그 확인) | `gocker start $C11 >/dev/null && sleep 1 && clog $C11` | 성공 |  |
| C13 | --rootfs 로 아카이브 지정 (인자는 모두 명령) | `gocker create --rootfs $IMG sleep 300` | 성공 |  |
| C14 | --rootfs 사용 시 이미지/명령 기록 | `eq "$(imgof $C13)" "$IMG" && eq "$(cmdof $C13)" "sleep 300"` | 성공 |  |
| C15 | 아카이브 경로를 이미지 자리에 직접 지정 | `gocker create $IMG echo hi` | 성공 |  |
| C16 | 존재하지 않는 --rootfs 는 실패 | `gocker create --rootfs /nonexistent/rootfs.tar.gz sh` | 성공 |  |
| C17 | 실패한 create는 롤백되어 흔적이 없음 | `eq "$(snap)" "$SNAP"` | 성공 |  |
| C18 | 존재하지 않는 실행 파일은 실패 (runc create 단계) | `gocker create alpine /no/such/binary` | 성공 |  |
| C19 | runc create 실패 후 롤백되어 흔적이 없음 | `eq "$(snap)" "$SNAP"` | 성공 |  |
| C20 | create -t (거부하지 않음) | `gocker create -t alpine sh` | 성공 |  |
| C21 | create -t 컨테이너를 start 하면 RUNNING | `gocker start $C20 && sleep 1 && eq "$(obs $C20)" RUNNING` | **실패** | F2 |
| C22 | [관찰] 알 수 없는 이미지 이름(nginx:latest) | `gocker create nginx:latest; echo "rc=$?"` | 관찰 | 6장 참고 |
| C23 | 정의되지 않은 플래그는 실패 | `gocker create --bogus alpine` | 성공 |  |

### start

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| G07 | start -h | `gocker start -h` | **실패** | F1 |
| S01 | start <ID> | `gocker start $SA` | 성공 |  |
| S02 | start 후 RUNNING, PID 기록, 프로세스 존재 | `eq "$(obs $SA)" RUNNING && eq "$(st $SA)" RUNNING && p=$(cpid $SA) && [ "$p" -gt 0 ] && kill -0 "$p"` | 성공 |  |
| S03 | start <이름> | `gocker start s-two && eq "$(obs $SB)" RUNNING` | 성공 |  |
| S04 | start <ID 접두어 6자> | `gocker start ${SC:0:6} && eq "$(obs $SC)" RUNNING` | 성공 |  |
| S05 | 3자 접두어는 인정하지 않음 | `gocker start ${SD:0:3}` | 성공 |  |
| S06 | 여러 컨테이너를 한 번에 start | `gocker start $SD $SE && eq "$(obs $SD)" RUNNING && eq "$(obs $SE)" RUNNING` | 성공 |  |
| S07 | 이미 실행 중이면 실패 | `gocker start $SA` | 성공 |  |
| S08 | STOPPED 컨테이너는 다시 start 불가 | `gocker start $SA` | 성공 |  |
| S09 | 존재하지 않는 컨테이너는 실패 | `gocker start no-such-container` | 성공 |  |
| S10 | 인자 없으면 실패 | `gocker start` | 성공 |  |
| S11 | 여러 개 중 하나가 없으면 그 지점에서 중단 | `gocker start $SF no-such-container $SG` | 성공 |  |
| S12 | 중단 전 대상은 시작됨, 이후 대상은 그대로 | `eq "$(obs $SF)" RUNNING && eq "$(obs $SG)" CREATED` | 성공 |  |
| S13 | 곧바로 끝나는 명령: start 성공 후 STOPPED로 관측 | `gocker start $SH && sleep 1 && eq "$(obs $SH)" STOPPED` | 성공 |  |
| S14 | create/start 컨테이너의 출력은 container.log 에 기록 | `gocker start $SI >/dev/null && sleep 1 && clog $SI` | 성공 |  |

### run

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| G08 | run -h | `gocker run -h` | **실패** | F1 |
| R01 | 포그라운드 run: 출력 전달 | `gocker run --name r-keep alpine echo hello-run` | 성공 |  |
| R02 | 종료 후 컨테이너가 STOPPED로 남고 종료 코드 0 기록 | `id=$(idof r-keep) && eq "$(st $id)" STOPPED && eq "$(ec $id)" 0` | 성공 |  |
| R03 | --rm: 출력 전달 | `gocker run --rm alpine echo hi-rm` | 성공 |  |
| R04 | --rm: 종료 후 흔적 없음 | `eq "$(snap)" "$SNAP"` | 성공 |  |
| R05 | 종료 코드 전파 (exit 7) | `gocker run --rm alpine sh -c 'exit 7'` | 성공 |  |
| R06 | 종료 코드 전파 + 기록 (exit 3, --rm 없음) | `gocker run --name r-exit alpine sh -c 'exit 3'` | 성공 |  |
| R07 | 종료 코드 3이 레코드에 기록됨 | `eq "$(ec $(idof r-exit))" 3` | 성공 |  |
| R08 | -d: ID 출력 후 즉시 반환 | `gocker run -d alpine sleep 300` | 성공 |  |
| R09 | -d: 백그라운드에서 RUNNING | `eq "$(obs $R8)" RUNNING` | 성공 |  |
| R10 | -d --name | `gocker run -d --name r-bg alpine sleep 300` | 성공 |  |
| R11 | -d --name: 이름으로 조회됨 | `eq "$(idof r-bg)" "$R10" && eq "$(obs $R10)" RUNNING` | 성공 |  |
| R12 | -d --rm 은 거부 | `gocker run -d --rm alpine sleep 300` | 성공 |  |
| R13 | 거부된 run은 컨테이너를 만들지 않음 | `eq "$(snap)" "$SNAP"` | 성공 |  |
| R14 | 같은 --name 으로 run 하면 거부 | `gocker run -d --name r-bg alpine sleep 300` | 성공 |  |
| R15 | 이름 중복으로 거부된 run은 흔적 없음 | `eq "$(snap)" "$SNAP"` | 성공 |  |
| R16 | -t: 가상 터미널로 실행 | `script -qec "gocker run -t --rm alpine echo tty-ok" /dev/null` | 성공 |  |
| R17 | -t: 컨테이너 stdout이 TTY | `script -qec "gocker run -t --rm alpine sh -c '[ -t 1 ] && echo IS_TTY \|\| echo NO_TTY'" /dev/null` | 성공 |  |
| R18 | -t 없음: 컨테이너 stdout이 TTY가 아님 | `gocker run --rm alpine sh -c '[ -t 1 ] && echo IS_TTY \|\| echo NO_TTY'` | 성공 |  |
| R19 | -t: 종료 코드 전파 (exit 9) | `script -qec "gocker run -t --rm alpine sh -c 'exit 9'" /dev/null` | 성공 |  |
| R20 | -it: 표준 입력으로 셸 조작 (exit 4) | `printf 'echo from-$((1+1))-stdin\nexit 4\n' \| script -qec "gocker run -it --rm alpine sh" /dev/null` | 성공 |  |
| R21 | -i -t 를 따로 지정 | `printf 'echo from-$((1+1))-stdin\nexit 4\n' \| script -qec "gocker run -i -t --rm alpine sh" /dev/null` | 성공 |  |
| R22 | -i (TTY 없이 표준 입력 전달) | `echo piped-in \| gocker run -i --rm alpine cat` | 성공 |  |
| R23 | --hostname 지정 | `gocker run --rm --hostname h-run alpine hostname` | 성공 |  |
| R24 | 기본 호스트명은 gocker | `gocker run --rm alpine hostname` | 성공 |  |
| R25 | --rootfs 지정 (인자는 모두 명령) | `gocker run --rm --rootfs $IMG echo via-rootfs` | 성공 |  |
| R26 | 인자 없는 run (기본 이미지, /bin/sh, 입력 없음 → 즉시 종료) | `gocker run --rm` | 성공 |  |
| R27 | 존재하지 않는 실행 파일은 실패 | `gocker run --rm alpine /no/such/binary` | 성공 |  |
| R28 | 실패한 run (--rm 없음)도 롤백되어 흔적 없음 | `gocker run --name r-bad alpine /no/such/binary; [ $? -ne 0 ] && eq "$(snap)" "$SNAP" && eq "$(idof r-bad)" ""` | 성공 |  |
| R29 | 이미지 뒤의 플래그는 컨테이너 명령의 인자 | `gocker run --rm alpine ls -la /` | 성공 |  |
| R30 | PID 네임스페이스: 컨테이너 명령이 PID 1 | `gocker run --rm alpine sh -c 'echo pid=$$'` | 성공 |  |
| R31 | 독립 rootfs (Alpine) | `gocker run --rm alpine cat /etc/os-release` | 성공 |  |
| R32 | stderr 전달 | `gocker run --rm alpine sh -c 'echo to-err >&2'` | 성공 |  |
| R33 | 기본 환경 변수 PATH | `gocker run --rm alpine sh -c 'echo $PATH'` | 성공 |  |
| R34 | -d: 출력은 container.log 에 기록 | `id=$(gocker run -d alpine sh -c 'echo to-log; sleep 300') && sleep 1 && clog $id` | 성공 |  |
| R35 | -d -t: 백그라운드 TTY 컨테이너가 RUNNING | `id=$(gocker run -d -t alpine sh) && echo "$id" && sleep 1 && eq "$(obs $id)" RUNNING` | **실패** | F2 |
| R36 | [관찰] -ti (묶은 단축 플래그) | `gocker run -ti --rm alpine echo x; echo "rc=$?"` | 관찰 | 6장 참고 |
| R37 | 포그라운드 run 중 SIGINT (--rm 없음) → 137로 종료 | `timeout --preserve-status -s INT 2 gocker run --name r-int alpine sleep 300` | 성공 |  |
| R41 | SIGINT 후 유예 시간(2초) 뒤 종료 | `(직전 명령의 소요 시간)` | 성공 | 4027ms |
| R42 | SIGINT로 끝난 컨테이너: STOPPED, 종료 코드 137 기록, 프로세스 없음 | `id=$(idof r-int) && eq "$(st $id)" STOPPED && eq "$(ec $id)" 137 && eq "$(cpid $id)" - && eq "$(runc state $id \| jq -r .status)" stopped` | 성공 |  |
| R38 | 포그라운드 run --rm 중 SIGINT → 137로 종료, 컨테이너 정리됨 | `timeout --preserve-status -s INT 2 gocker run --rm --name r-int2 alpine sleep 300; rc=$?; sleep 1; eq "$(idof r-int2)" "" && eq "$(snap)" "$SNAP" && exit $rc` | 성공 |  |
| R43 | run --rm 중 SIGTERM → 컨테이너 핸들러 실행, 43으로 종료 | `timeout --preserve-status -s TERM 2 gocker run --rm alpine sh -c "$TRAPCMD"` | 성공 |  |
| R44 | 핸들러로 끝난 --rm 컨테이너도 정리됨 | `eq "$(snap)" "$SNAP"` | 성공 |  |
| R45 | run -t 중 gocker 에 SIGTERM → 컨테이너에 SIGTERM 전달, 43으로 종료 | `script -qec "timeout --preserve-status -s TERM 2 gocker run -t --rm alpine sh -c \"\$TRAPCMD\"" /dev/null` | 성공 |  |
| R39 | 포그라운드로 실행 중인 컨테이너를 다른 터미널에서 stop | `gocker stop -t 1 r-fg` | 성공 |  |
| R40 | stop 당한 포그라운드 run 은 137 로 종료하고 종료 코드 기록 | `eq "$FG_RC" 137 && eq "$(ec $(idof r-fg))" 137` | 성공 |  |

### stop

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| G09 | stop -h | `gocker stop -h` | **실패** | F1 |
| T01 | stop -t 1 (SIGTERM 무시 → SIGKILL) | `gocker stop -t 1 $TA` | 성공 |  |
| T02 | -t 1: 약 1초 뒤 SIGKILL 로 종료 | `(직전 명령의 소요 시간)` | 성공 | 1043ms |
| T03 | stop 후 STOPPED 기록, runc 도 stopped, 프로세스 없음 | `eq "$(st $TA)" STOPPED && eq "$(obs $TA)" STOPPED && eq "$(runc state $TA \| jq -r .status)" stopped && eq "$(cpid $TA)" -` | 성공 |  |
| T04 | stop -t=2 (등호 형식) | `gocker stop -t=2 $TB` | 성공 |  |
| T05 | -t=2: 약 2초 뒤 종료 | `(직전 명령의 소요 시간)` | 성공 | 2044ms |
| T06 | SIGTERM 을 처리하는 컨테이너는 곧바로 종료 | `gocker stop -t 10 $TC && eq "$(st $TC)" STOPPED` | 성공 |  |
| T07 | 정상 종료는 타임아웃을 기다리지 않음 | `(직전 명령의 소요 시간)` | 성공 | 239ms |
| T08 | stop (기본 타임아웃 10초) | `gocker stop $TD && eq "$(st $TD)" STOPPED` | 성공 |  |
| T09 | 기본 타임아웃: 약 10초 뒤 SIGKILL | `(직전 명령의 소요 시간)` | 성공 | 10047ms |
| T10 | 여러 컨테이너를 한 번에 stop | `gocker stop -t 1 $TE $TG && eq "$(st $TE)" STOPPED && eq "$(st $TG)" STOPPED` | 성공 |  |
| T11 | stop <이름> | `gocker stop -t 1 t-named && eq "$(st $TF)" STOPPED` | 성공 |  |
| T12 | stop <ID 접두어 6자> | `gocker stop -t 1 ${TH:0:6} && eq "$(st $TH)" STOPPED` | 성공 |  |
| T13 | 이미 STOPPED 인 컨테이너 stop 은 성공 (멱등) | `gocker stop -t 1 $TH` | 성공 |  |
| T14 | CREATED (시작 전) 컨테이너 stop | `gocker stop -t 1 $TI && eq "$(st $TI)" STOPPED` | 성공 |  |
| T15 | 스스로 종료한 컨테이너 stop → STOPPED 기록 | `eq "$(st $TJ)" RUNNING && gocker stop -t 5 $TJ && eq "$(st $TJ)" STOPPED` | 성공 |  |
| T16 | 스스로 종료한 컨테이너는 대기 없이 반환 | `(직전 명령의 소요 시간)` | 성공 | 31ms |
| T17 | 존재하지 않는 컨테이너는 실패 | `gocker stop no-such-container` | 성공 |  |
| T18 | 인자 없으면 실패 | `gocker stop` | 성공 |  |
| T19 | -t 0 은 거부 | `gocker stop -t 0 $TK` | 성공 |  |
| T20 | -t -1 은 거부 | `gocker stop -t -1 $TK` | 성공 |  |
| T21 | -t abc 는 거부 | `gocker stop -t abc $TK` | 성공 |  |
| T22 | 거부된 stop 은 컨테이너를 건드리지 않음 | `eq "$(obs $TK)" RUNNING` | 성공 |  |
| T23 | 여러 개 중 하나가 없으면 그 지점에서 중단 | `gocker stop -t 1 $TK no-such-container $TL` | 성공 |  |
| T24 | 중단 전 대상은 중지됨, 이후 대상은 그대로 | `eq "$(st $TK)" STOPPED && eq "$(obs $TL)" RUNNING` | 성공 |  |
| T25 | stop 대기 중 SIGINT → 곧바로 실패로 반환 | `timeout -s INT 1 gocker stop -t 30 $TL` | 성공 |  |
| T26 | SIGINT 후 30초를 기다리지 않음 | `(직전 명령의 소요 시간)` | 성공 | 1009ms |
| T27 | 취소된 stop 은 STOPPED 로 기록하지 않음 | `eq "$(st $TL)" RUNNING && eq "$(obs $TL)" RUNNING` | 성공 |  |
| T28 | [관찰] detach 컨테이너의 종료 코드 기록 | `echo "exitCode=$(ec $TA)"` | 관찰 | 6장 참고 |

### ps

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| G10 | ps -h | `gocker ps -h` | **실패** | F1 |
| P01 | 컨테이너가 없을 때 ps -a 는 헤더만 출력 | `out=$(gocker ps -a); echo "$out"; eq "$(echo "$out" \| wc -l)" 1` | 성공 |  |
| P02 | 컨테이너가 없을 때 ps -q 는 빈 출력 | `out=$(gocker ps -q); eq "$out" ""` | 성공 |  |
| P03 | ps: 실행 중인 컨테이너만 | `out=$(gocker ps); echo "$out"; echo "$out" \| grep -q p-running && ! echo "$out" \| grep -qE "p-created\|p-stopped"` | 성공 |  |
| P04 | ps -a: 모든 상태 | `out=$(gocker ps -a); echo "$out"; eq "$(obs $PC)" CREATED && eq "$(obs $PR)" RUNNING && eq "$(obs $PS_)" STOPPED` | 성공 |  |
| P05 | ps --all | `eq "$(gocker ps --all \| wc -l)" 4` | 성공 |  |
| P06 | 헤더 열 구성 | `gocker ps -a` | 성공 |  |
| P07 | PID 열: 실행 중이면 숫자, 아니면 - | `[ "$(cpid $PR)" -gt 0 ] && eq "$(cpid $PC)" - && eq "$(cpid $PS_)" -` | 성공 |  |
| P08 | ps -q: 실행 중인 ID만 | `eq "$(gocker ps -q)" "$PR"` | 성공 |  |
| P09 | ps -a -q: 모든 ID | `out=$(gocker ps -a -q \| sort \| tr "\n" " "); want=$(printf "%s\n" $PC $PR $PS_ \| sort \| tr "\n" " "); eq "$out" "$want"` | 성공 |  |
| P10 | ps -q -a (순서 무관) | `eq "$(gocker ps -q -a \| wc -l)" 3` | 성공 |  |
| P11 | ps --all -q | `eq "$(gocker ps --all -q \| wc -l)" 3` | 성공 |  |
| P12 | 추가 인자는 거부 | `gocker ps extra` | 성공 |  |
| P13 | CREATED 열 표시 (N seconds ago) | `gocker ps -a` | 성공 |  |
| P14 | 긴 명령은 30자로 잘려 ... 로 표시 | `gocker ps -a \| awk -v id=$PL "\$1==id"` | 성공 |  |
| P15 | [관찰] ps -aq (묶은 단축 플래그) | `gocker ps -aq; echo "rc=$?"` | 관찰 | 6장 참고 |

### rm

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| G11 | rm -h | `gocker rm -h` | **실패** | F1 |
| M01 | STOPPED 컨테이너 rm | `gocker rm $MA` | 성공 |  |
| M02 | rm 후 레코드/잠금/번들/runc 컨테이너 없음 | `gone $MA` | 성공 |  |
| M03 | CREATED 컨테이너 rm | `gocker rm $MB && gone $MB` | 성공 |  |
| M04 | RUNNING 컨테이너는 -f 없이 rm 불가 | `gocker rm $MC` | 성공 |  |
| M05 | 거부된 rm 은 컨테이너를 건드리지 않음 | `eq "$(obs $MC)" RUNNING && kill -0 $MC_PID` | 성공 |  |
| M06 | rm -f: 실행 중인 컨테이너 강제 삭제 | `gocker rm -f $MC` | 성공 |  |
| M07 | rm -f 후 흔적 없음, 프로세스도 종료됨 | `gone $MC && ! kill -0 $MC_PID 2>/dev/null` | 성공 |  |
| M08 | rm --force | `gocker rm --force $MD && gone $MD && ! kill -0 $MD_PID 2>/dev/null` | 성공 |  |
| M09 | 여러 컨테이너를 한 번에 rm | `gocker rm $ME $MF $MG && gone $ME && gone $MF && gone $MG` | 성공 |  |
| M10 | rm <이름> | `gocker rm m-named && gone $MH` | 성공 |  |
| M11 | 삭제한 이름은 다시 사용할 수 있음 | `gocker create --name m-named alpine sleep 300` | 성공 |  |
| M12 | rm <ID 접두어 6자> | `gocker rm ${MI:0:6} && gone $MI` | 성공 |  |
| M13 | 존재하지 않는 컨테이너는 실패 | `gocker rm no-such-container` | 성공 |  |
| M14 | 인자 없으면 실패 | `gocker rm` | 성공 |  |
| M15 | 여러 개 중 하나가 없으면 그 지점에서 중단 | `gocker rm $MJ no-such-container $MK` | 성공 |  |
| M16 | 중단 전 대상은 삭제됨, 이후 대상은 그대로 | `gone $MJ && eq "$(obs $MK)" CREATED` | 성공 |  |
| M17 | rm -f 로 STOPPED 컨테이너 삭제 | `gocker rm -f $ML && gone $ML` | 성공 |  |
| M18 | 유령 RUNNING: ps -a 에 STOPPED 로 표시, ps 에는 없음 | `eq "$(st $MM)" RUNNING && eq "$(obs $MM)" STOPPED && ! gocker ps -q \| grep -q $MM` | 성공 |  |
| M19 | 유령 RUNNING: -f 없이 rm 가능 | `gocker rm $MM && gone $MM` | 성공 |  |
| M20 | 모두 삭제한 뒤 저장소/번들/runc 에 남은 것 없음 | `eq "$(ls $ROOT/state \| wc -l)" 0 && eq "$(ls $ROOT/containers \| wc -l)" 0 && eq "$(runc list -q \| wc -l)" 0` | 성공 |  |

### containerd 없이 lifecycle 동작

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| X01 | (준비) containerd 중지됨 | `! systemctl is-active --quiet containerd` | 성공 |  |
| X02 | containerd 없이 run --rm | `gocker run --rm alpine echo no-ctd` | 성공 |  |
| X03 | containerd 없이 create → start → ps → stop → rm | `id=$(gocker create alpine sleep 300) && gocker start $id && gocker ps -q \| grep -q $id && gocker stop -t 1 $id && gocker rm $id && gone $id` | 성공 |  |
| X04 | containerd 없이 version (gocker 버전은 출력) | `gocker version` | 성공 |  |
| X05 | containerd 없이 pull 은 실패 | `gocker pull docker.io/library/alpine:latest` | 성공 |  |

### version (containerd)

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| D01 | version | `gocker version` | 성공 |  |

### pull (containerd)

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| D02 | pull docker.io/library/alpine:latest | `gocker pull docker.io/library/alpine:latest` | 성공 |  |
| D03 | pull alpine:latest (짧은 이름) | `gocker pull alpine:latest` | **실패** | 종료 코드 1 (기대 0) |
| D04 | pull --platform=linux/amd64 | `gocker pull --platform=linux/amd64 docker.io/library/busybox:latest` | 성공 |  |
| D05 | pull --snapshotter=overlayfs | `gocker pull --snapshotter=overlayfs docker.io/library/busybox:latest` | 성공 |  |
| D06 | pull --ns gocker-test | `gocker pull --ns gocker-test docker.io/library/alpine:latest` | 성공 |  |
| D07 | pull: 이미지 미지정은 실패 | `gocker pull` | 성공 |  |
| D08 | pull: 이미지 두 개는 실패 | `gocker pull docker.io/library/alpine:latest docker.io/library/busybox:latest` | 성공 |  |
| D09 | pull: 존재하지 않는 이미지는 실패 | `gocker pull docker.io/library/gocker-no-such-image-xyz:latest` | 성공 |  |
| D10 | pull -h | `gocker pull -h` | **실패** | 종료 코드 1 (기대 0) |

### images (containerd)

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| D11 | images | `gocker images` | 성공 |  |
| D12 | images --name busybox (필터) | `out=$(gocker images --name busybox); echo "$out"; echo "$out" \| grep -q busybox && ! echo "$out" \| grep -q alpine` | 성공 |  |
| D13 | images --platform=linux/amd64 | `gocker images --platform=linux/amd64` | 성공 |  |
| D14 | images --ns gocker-test | `gocker images --ns gocker-test` | 성공 |  |
| D15 | images: 추가 인자는 실패 | `gocker images extra` | 성공 |  |
| D16 | images -h | `gocker images -h` | **실패** | 종료 코드 1 (기대 0) |

### exec (containerd)

| ID | 항목 | 명령 | 결과 | 비고 |
|---|---|---|---|---|
| D20 | (준비) ctr 로 containerd 컨테이너 gk-exec 실행 | `ctr -n default run -d docker.io/library/alpine:latest gk-exec sleep 3000 && ctr -n default task ls` | 성공 |  |
| D21 | exec: 명령 실행 | `gocker exec gk-exec echo exec-ok` | 성공 |  |
| D22 | exec: 종료 코드 전파 (exit 5) | `gocker exec gk-exec sh -c 'exit 5'` | 성공 |  |
| D23 | exec -t | `script -qec "gocker exec -t gk-exec sh -c '[ -t 1 ] && echo IS_TTY \|\| echo NO_TTY'" /dev/null` | 성공 |  |
| D24 | exec -i | `echo piped \| gocker exec -i gk-exec cat` | **실패** | 종료 코드 124 (기대 0); 시간 초과(60s) |
| D25 | exec -it | `printf 'echo in-$((2+3))-exec\nexit 6\n' \| script -qec "gocker exec -it gk-exec sh" /dev/null` | 성공 |  |
| D26 | exec -d (백그라운드 실행) | `gocker exec -d gk-exec touch /tmp/exec-d-marker && sleep 1 && gocker exec gk-exec ls /tmp/exec-d-marker` | 성공 |  |
| D27 | exec: 존재하지 않는 컨테이너는 실패 | `gocker exec no-such-container echo x` | 성공 |  |
| D28 | (준비) ctr 로 gocker-test 네임스페이스에 gk-exec2 실행 | `ctr -n gocker-test run -d docker.io/library/alpine:latest gk-exec2 sleep 3000` | 성공 |  |
| D29 | exec --ns gocker-test | `gocker exec --ns gocker-test gk-exec2 echo ns-ok` | 성공 |  |
| D30 | exec: 인자 없으면 실패 | `gocker exec` | 성공 |  |
| D31 | exec: 명령 없이 컨테이너만 주면 실패 | `gocker exec gk-exec` | 성공 |  |
| D32 | exec -h | `gocker exec -h` | **실패** | 종료 코드 1 (기대 0) |
| D33 | [관찰] gocker run 으로 만든 컨테이너에 exec | `gocker exec x-runc echo hi; echo "rc=$?"` | 관찰 | 6장 참고 |

