# Gocker

Go로 만든 교육용 컨테이너 런타임이다. 지금 단계(Phase 2)에서는 OCI 번들(`config.json` + `rootfs`)을 직접 만들고 runc를 직접 제어해 컨테이너를 실행한다.

- 상주 프로세스(데몬, shim)가 없다. 명령 한 번이 프로세스 한 번이다.
- 컨테이너 라이프사이클 명령(`create`, `start`, `run`, `stop`, `ps`, `rm`)은 containerd 없이 동작한다.
- 이미지 관련 명령(`pull`, `images`)과 `exec`, `version`은 Phase 1에서 만든 containerd 기반 구현을 쓴다.

| 명령 | 설명 | 백엔드 |
|---|---|---|
| [`create`](#gocker-create) | 컨테이너를 만든다 (실행하지 않음) | runc |
| [`start`](#gocker-start) | 만들어 둔 컨테이너를 시작한다 | runc |
| [`run`](#gocker-run) | 컨테이너를 만들고 바로 실행한다 | runc |
| [`stop`](#gocker-stop) | 실행 중인 컨테이너를 중지한다 | runc |
| [`ps`](#gocker-ps) | 컨테이너 목록을 출력한다 | runc |
| [`rm`](#gocker-rm) | 컨테이너를 삭제한다 | runc |
| [`pull`](#gocker-pull) | 이미지를 내려받는다 | containerd |
| [`images`](#gocker-images) | 내려받은 이미지 목록을 출력한다 | containerd |
| [`exec`](#gocker-exec) | 실행 중인 containerd 컨테이너 안에서 명령을 실행한다 | containerd |
| [`version`](#gocker-version) | 버전 정보를 출력한다 | containerd |

## 문서

| 문서 | 내용 |
|---|---|
| [아키텍처](docs/architecture.md) | 패키지 구조, 의존 규칙, 디스크 레이아웃, 실행 흐름 |
| [상태 모델과 라이프사이클](docs/lifecycle.md) | 상태 전이, 저장된 상태와 관측한 상태, 종료 코드 |
| [설계 결정 기록](docs/decisions.md) | 선택지가 갈린 지점과 고른 이유 |
| [운영 정책](docs/policies.md) | 시그널, 실패 처리, 동시성, 에러 분류 규칙 |
| [테스트 보고서](docs/phase2-test-report.md) | 모든 명령어와 플래그에 대한 테스트 결과 (192개) |

## 환경

| 항목 | 값 |
|---|---|
| OS | Ubuntu 24.04 (Linux 전용) |
| Go | 1.26.6 |
| runc | 필수. `create`, `start`, `run`, `stop`, `ps`, `rm`이 사용한다 |
| containerd | `pull`, `images`, `exec`, `version`을 쓸 때만 필요하다 |
| 권한 | root. runc 실행과 `/var/lib/gocker` 접근에 필요하다 |

### 외부 패키지

1. github.com/containerd/containerd/v2
2. github.com/containerd/errdefs
3. github.com/containerd/platforms
4. github.com/opencontainers/image-spec
5. github.com/opencontainers/runtime-spec
6. golang.org/x/sys
7. golang.org/x/term

## 준비

### runc와 containerd 설치

```bash
sudo apt install -y runc containerd
```

### rootfs 아카이브 준비

Phase 2의 `create`와 `run`은 containerd 이미지가 아니라 **로컬 rootfs 아카이브**(`.tar`, `.tar.gz`, `.tgz`)로 컨테이너를 만든다. 기본 이미지 `alpine`은 `assets/alpine-minirootfs.tar.gz`를 가리킨다. 이 파일은 저장소에 들어 있지 않으므로 내려받아야 한다.

```bash
mkdir -p assets
```

```bash
curl -fsSL -o assets/alpine-minirootfs.tar.gz https://dl-cdn.alpinelinux.org/alpine/v3.20/releases/x86_64/alpine-minirootfs-3.20.3-x86_64.tar.gz
```

기본 경로는 작업 디렉터리 기준이다. `alpine`이라는 이름을 쓰려면 저장소 루트에서 실행해야 한다.

### 빌드

```bash
go build -o gocker .
```

## 빠른 시작

```bash
sudo ./gocker run --rm alpine echo "Hello Gocker"
```

```bash
sudo ./gocker run -it --rm alpine sh
```

```bash
sudo ./gocker run -d --name my-box alpine sleep 300
```

```bash
sudo ./gocker ps -a
```

```bash
sudo ./gocker stop my-box
```

```bash
sudo ./gocker rm my-box
```

## 공통 규칙

### 이미지 인자

`create`와 `run`의 첫 번째 인자는 다음 순서로 해석한다.

| 첫 번째 인자 | 해석 |
|---|---|
| `alpine`, `alpine:latest` | `assets/alpine-minirootfs.tar.gz` |
| `.tar`, `.tar.gz`, `.tgz`로 끝나는 경로 | 그 아카이브 |
| 존재하는 파일의 경로 | 그 파일 |
| 그 밖 | 이미지가 아니라 명령으로 본다. 이미지는 기본값(`alpine`)을 쓴다 |

- `--rootfs`를 주면 남은 인자는 모두 명령이다.
- 명령을 주지 않으면 `/bin/sh`를 실행한다.
- 플래그는 이미지 앞에 써야 한다. 이미지 뒤의 플래그는 컨테이너 명령의 인자가 된다.

### 컨테이너 지정

`start`, `stop`, `rm`은 컨테이너를 세 가지 방법으로 지정할 수 있다.

| 방법 | 예 |
|---|---|
| 전체 ID (12자리) | `3f9a1c0b7e2d` |
| 이름 | `my-box` |
| ID의 앞부분 (4자 이상) | `3f9a` |

여러 컨테이너에 일치하면 실행하지 않고 에러를 낸다. 여러 대상을 한 번에 주면 앞에서부터 처리하고, 하나가 실패하면 그 지점에서 멈춘다.

### 상태

| 상태 | 뜻 |
|---|---|
| `CREATED` | 만들어졌고 아직 시작하지 않았다 |
| `RUNNING` | 실행 중이다 |
| `STOPPED` | 끝났다. 다시 시작할 수 없고 삭제만 할 수 있다 |

### 저장 위치

| 경로 | 내용 |
|---|---|
| `/var/lib/gocker/state/` | 컨테이너 레코드(`<id>.json`)와 잠금 파일 |
| `/var/lib/gocker/containers/<id>/` | OCI 번들 (`config.json`, `rootfs/`, `container.log`, `runc.log`) |

---

## gocker create

rootfs 아카이브로 컨테이너를 만든다. 실행은 하지 않는다. 성공하면 컨테이너 ID를 출력한다.

### Usage

```bash
gocker create [OPTIONS] [ROOTFS_OR_IMAGE] [COMMAND] [ARG...]
```

### Options

| 옵션 | 설명 |
|---|---|
| `--name` | 컨테이너 이름. 다른 컨테이너와 겹칠 수 없다 |
| `--hostname` | 컨테이너 호스트명 (기본값: `gocker`) |
| `--rootfs` | rootfs 아카이브 경로. 지정하면 남은 인자는 모두 명령이다 |
| `-t` | 가상 터미널(TTY) 할당. [알려진 한계](#알려진-한계) 참고 |
| `-h` | 도움말 출력 |

### Examples

```bash
gocker create --name my-box alpine sleep 300
```

```bash
gocker create --rootfs assets/alpine-minirootfs.tar.gz sh
```

### 동작 방식

1. 컨테이너 ID(12자리 16진수)를 만든다.
2. 레코드를 `CREATED` 상태로 저장한다. 이름이 있으면 중복을 검사한다.
3. 번들 디렉터리를 만들고 rootfs 아카이브를 푼다.
4. OCI 런타임 명세(`config.json`)를 만들어 저장한다.
5. `runc create`를 실행한다.

도중에 실패하면 만든 것을 역순으로 되돌린다. 컨테이너의 출력은 번들의 `container.log`에 기록된다.

---

## gocker start

`create`로 만들어 둔 컨테이너를 시작한다. 백그라운드로 실행되며 시작한 컨테이너를 한 줄씩 출력한다.

### Usage

```bash
gocker start CONTAINER [CONTAINER...]
```

### Options

| 옵션 | 설명 |
|---|---|
| `-h` | 도움말 출력 |

### Examples

```bash
gocker start my-box
```

```bash
gocker start my-box 3f9a1c0b7e2d
```

### 동작 방식

1. 컨테이너의 현재 상태를 확인한다. `CREATED`일 때만 시작한다.
2. `runc start`를 실행한다.
3. 레코드를 `RUNNING`으로 바꾸고 PID를 기록한다.

이미 실행 중이거나 `STOPPED`인 컨테이너는 시작할 수 없다.

---

## gocker run

컨테이너를 만들고 바로 실행한다. `create`와 `start`를 한 번에 한다.

### Usage

```bash
gocker run [OPTIONS] [ROOTFS_OR_IMAGE] [COMMAND] [ARG...]
```

### Options

| 옵션 | 설명 |
|---|---|
| `--name` | 컨테이너 이름. 다른 컨테이너와 겹칠 수 없다 |
| `-d` | 백그라운드로 실행한다. 컨테이너 ID를 출력하고 바로 반환한다 |
| `--rm` | 컨테이너가 끝나면 자동으로 삭제한다. `-d`와 함께 쓸 수 없다 |
| `-t` | 가상 터미널(TTY) 할당 |
| `-i` | 표준 입력을 컨테이너에 연결한다. `-d`에서는 무시된다 |
| `-it` | `-i -t`와 같다 |
| `--hostname` | 컨테이너 호스트명 (기본값: `gocker`) |
| `--rootfs` | rootfs 아카이브 경로. 지정하면 남은 인자는 모두 명령이다 |
| `-h` | 도움말 출력 |

### Examples

```bash
gocker run --rm alpine echo "Hello Gocker"
```

```bash
gocker run -it --rm alpine sh
```

```bash
gocker run -d --name my-box alpine sleep 300
```

```bash
echo hello | gocker run -i --rm alpine cat
```

### 실행 방식별 동작

| 실행 방식 | 표준 입력 | 출력 | 종료 코드 |
|---|---|---|---|
| 포그라운드 (기본) | 연결하지 않음 | 현재 터미널 | 컨테이너의 종료 코드 |
| `-i` | gocker의 표준 입력 | 현재 터미널 | 컨테이너의 종료 코드 |
| `-t`, `-it` | 가상 터미널 | 가상 터미널 | 컨테이너의 종료 코드 |
| `-d` | 연결하지 않음 | `container.log` | 0 (컨테이너의 종료 코드는 알 수 없음) |

### 종료 코드

포그라운드 `run`은 컨테이너가 끝날 때까지 기다린 뒤 컨테이너의 종료 코드로 끝난다.

| 컨테이너 | gocker의 종료 코드 |
|---|---|
| N으로 종료 | N |
| 시그널로 종료 | 128 + 시그널 번호 (SIGKILL이면 137) |

종료 코드는 레코드에도 기록된다. `run -d`는 gocker가 컨테이너보다 먼저 끝나므로 종료 코드를 얻지 못한다.

### 실행 중 Ctrl+C

포그라운드 `run` 중에 gocker가 SIGINT나 SIGTERM을 받으면 컨테이너를 중지한다.

1. 컨테이너에 SIGTERM을 보낸다.
2. 2초 안에 끝나지 않으면 SIGKILL을 보낸다.
3. 컨테이너의 종료 코드로 끝난다.

`-t`에서 키보드로 누른 Ctrl+C는 컨테이너 안의 프로세스에 SIGINT로 전달된다.

### 동작 방식

1. `-d`와 `--rm`을 함께 주면 아무것도 만들지 않고 거부한다.
2. 포그라운드면 gocker를 child subreaper로 지정한다. 컨테이너 프로세스의 종료 상태를 직접 수거하기 위해서다.
3. `create`와 같은 순서로 컨테이너를 만든다 (레코드 → 번들 → `runc create`).
4. `runc start`를 실행한다. 실패하면 컨테이너를 삭제한다.
5. `-d`면 ID를 출력하고 반환한다.
6. 포그라운드면 컨테이너가 끝날 때까지 기다리고, 레코드에 `STOPPED`와 종료 코드를 기록한다.
7. `--rm`이면 컨테이너를 삭제한다.

---

## gocker stop

실행 중인 컨테이너를 중지한다. 중지한 컨테이너를 한 줄씩 출력한다.

### Usage

```bash
gocker stop [OPTIONS] CONTAINER [CONTAINER...]
```

### Options

| 옵션 | 설명 |
|---|---|
| `-t` | SIGTERM 뒤 종료를 기다리는 시간 (초, 기본값: `10`). 1 이상이어야 한다 |
| `-h` | 도움말 출력 |

### Examples

```bash
gocker stop my-box
```

```bash
gocker stop -t 5 my-box
```

```bash
gocker stop my-box my-other-box
```

### 동작 방식

1. 컨테이너가 이미 끝났으면 신호를 보내지 않고 레코드만 `STOPPED`로 맞춘다.
2. SIGTERM을 보낸다.
3. `-t` 시간 동안 종료를 기다린다.
4. 끝나지 않으면 SIGKILL을 보내고, 실제로 끝났는지 확인한다.
5. 종료를 확인하면 레코드를 `STOPPED`로 바꾼다.

종료를 확인하지 못하면(대기 중 Ctrl+C, runc 조회 실패 등) 에러를 반환하고 레코드를 바꾸지 않는다. 이미 `STOPPED`인 컨테이너에 다시 호출해도 성공한다.

---

## gocker ps

컨테이너 목록을 출력한다. 기본은 실행 중인 컨테이너만 보여 준다.

### Usage

```bash
gocker ps [OPTIONS]
```

### Options

| 옵션 | 설명 |
|---|---|
| `-a`, `--all` | 모든 상태의 컨테이너를 보여 준다 |
| `-q` | 컨테이너 ID만 출력한다 |
| `-h` | 도움말 출력 |

`-a`와 `-q`를 함께 쓰려면 `-a -q`로 따로 써야 한다. `-aq`처럼 묶어 쓸 수 없다.

### Examples

```bash
gocker ps
```

```bash
gocker ps -a
```

```bash
gocker ps -a -q
```

### 출력

```
CONTAINER ID  NAME    STATUS   PID    CREATED         COMMAND
3f9a1c0b7e2d  my-box  RUNNING  12345  10 seconds ago  sleep 300
```

### 동작 방식

1. 저장된 레코드를 모두 읽는다.
2. 컨테이너마다 `runc state`로 실제 상태를 확인해 반영한다.
3. `-a`가 없으면 `RUNNING`만 남긴다.

`ps`는 레코드를 고치지 않는다. 백그라운드 컨테이너가 스스로 끝났거나 호스트가 재부팅된 경우에도, 레코드와 관계없이 실제 상태(`STOPPED`)를 보여 준다.

---

## gocker rm

컨테이너를 삭제한다. 삭제한 컨테이너를 한 줄씩 출력한다.

### Usage

```bash
gocker rm [OPTIONS] CONTAINER [CONTAINER...]
```

### Options

| 옵션 | 설명 |
|---|---|
| `-f`, `--force` | 실행 중인 컨테이너를 강제로 삭제한다 |
| `-h` | 도움말 출력 |

### Examples

```bash
gocker rm my-box
```

```bash
gocker rm -f my-box
```

```bash
gocker rm box1 box2 box3
```

### 동작 방식

1. 컨테이너가 `RUNNING`이면 `-f`가 없을 때 거부한다.
2. runc 컨테이너를 삭제한다 (`-f`면 `runc delete --force`).
3. 번들 디렉터리를 삭제한다.
4. 레코드를 삭제한다.

도중에 실패하면 거기서 멈추고 레코드를 남긴다. 다시 `rm`을 실행하면 남은 것을 정리한다. 삭제한 컨테이너의 이름은 다시 쓸 수 있다.

---

## gocker pull

containerd를 사용하여 컨테이너 이미지를 로컬로 다운로드합니다.

이미지를 pull하면 이미지 실행에 필요한 레이어가 함께 다운로드되며, 지정된 snapshotter를 사용하여 이미지의 파일 시스템을 unpack합니다.

> [!NOTE]
> `pull`로 받은 이미지는 containerd에 저장됩니다. Phase 2의 `create`와 `run`은 이 이미지를 쓰지 않고 로컬 rootfs 아카이브를 씁니다.

### Usage

```bash
gocker pull [OPTIONS] <image>
```

이미지 이름은 `docker.io/library/busybox:latest`처럼 레지스트리를 포함한 전체 이름으로 써야 합니다.

### Options

| 옵션 | 설명 |
|---|---|
| `--platform` | 다운로드할 이미지의 대상 플랫폼을 지정합니다. 예: `linux/amd64`, `linux/arm64` |
| `--snapshotter` | 이미지 레이어를 unpack할 때 사용할 snapshotter를 지정합니다. 예: `overlayfs`, `native` |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `pull` 명령어의 도움말을 출력합니다. |

`--platform`을 지정하지 않으면 현재 시스템의 기본 플랫폼을 사용합니다.

`snapshotter`를 별도로 지정하지 않으면 containerd에 설정된 기본 snapshotter를 사용합니다.

### Examples

```bash
gocker pull docker.io/library/busybox:latest
```

```bash
gocker pull --platform=linux/amd64 docker.io/library/busybox:latest
```

```bash
gocker pull --snapshotter=overlayfs docker.io/library/busybox:latest
```

```bash
gocker pull --platform=linux/amd64 --snapshotter=overlayfs docker.io/library/busybox:latest
```

### 동작 방식

`gocker pull`은 내부적으로 containerd의 Pull API를 사용하여 이미지를 다운로드합니다.

1. 이미지와 관련된 메타데이터를 다운로드합니다.
2. 지정된 플랫폼에 맞는 이미지 manifest와 레이어를 가져옵니다.
3. 이미지 레이어를 content store에 저장합니다.
4. 지정된 snapshotter를 사용하여 이미지 파일 시스템을 unpack합니다.

---

## gocker images

containerd에 로컬로 저장된 컨테이너 이미지 목록을 출력합니다.

이미지 이름 필터링, 대상 플랫폼과의 일치 여부 확인, 이미지 크기 및 다이제스트(ID)를 확인할 수 있습니다.

### Usage

```bash
gocker images [OPTIONS]
```

### Options

| 옵션 | 설명 |
|---|---|
| `--name` | 이미지 이름, 태그, 레지스트리의 부분 일치 필터입니다. 예: `nginx`, `docker` |
| `--platform` | 조회할 이미지의 대상 플랫폼을 지정합니다. 예: `linux/amd64`, `linux/arm64` |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `images` 명령어의 도움말을 출력합니다. |

`--platform`을 지정하면 해당 플랫폼의 이미지 크기 및 호스트 플랫폼과의 일치 여부를 매칭하여 표시합니다.

### Examples

```bash
gocker images
```

```bash
gocker images --name=nginx
```

```bash
gocker images --platform=linux/amd64
```

```bash
gocker images --ns=default
```

### 동작 방식

`gocker images`는 내부적으로 containerd의 ListImages API를 사용하여 로컬에 저장된 이미지 목록을 가져옵니다.

1. 지정된 네임스페이스(`--ns`, 기본값: `default`)에 등록된 모든 이미지를 조회합니다.
2. `--name` 옵션이 지정된 경우, 대소문자를 구분하지 않고 이미지 전체 이름(`Repository`)에 해당 키워드가 포함된 이미지만 필터링합니다.
3. `--platform` 옵션이 지정된 경우 대상 플랫폼에 맞는 크기(`SIZE`)와 호스트 플랫폼과의 일치 여부(`PLATFORM STATUS`)를 계산합니다.
4. 플랫폼을 별도로 지정하지 않은 경우 호스트 기본 플랫폼과의 일치 여부를 확인하며, 불일치 시 이미지 하위 매니페스트(Children)를 탐색하여 다운로드된 실제 플랫폼 정보를 찾아 표시합니다.
5. 조회된 결과를 `REPOSITORY`, `IMAGE ID`(12자리 다이제스트), `SIZE`, `PLATFORM STATUS` 컬럼 형태로 정렬하여 출력합니다.

---

## gocker exec

실행 중인 컨테이너 내부에서 새로운 프로세스(명령어)를 실행합니다.

> [!NOTE]
> `exec`는 아직 containerd 컨테이너만 대상으로 합니다. `gocker run`이나 `gocker create`로 만든 컨테이너에는 쓸 수 없습니다. 이후 `runc exec`로 확장할 예정입니다.

### Usage

```bash
gocker exec [OPTIONS] CONTAINER COMMAND [ARG...]
```

### Options

| 옵션 | 설명 |
|---|---|
| `-it` | 대화형 가상 터미널을 할당합니다. (`-i`와 `-t` 동시 적용) |
| `-i` | 표준 입력(stdin)을 유지합니다. |
| `-t` | 가상 터미널(TTY)을 할당합니다. |
| `-d` | 명령어를 백그라운드(Detach) 모드로 실행합니다. |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `exec` 명령어의 도움말을 출력합니다. |

### Examples

```bash
gocker exec -it my-nginx sh
```

```bash
gocker exec my-nginx ls -la /etc/nginx
```

```bash
gocker exec -d my-nginx touch /tmp/gocker.log
```

### 동작 방식

`gocker exec`는 containerd의 Task Exec API를 사용하여 이미 실행 중인 컨테이너의 네임스페이스 안에서 새로운 프로세스를 생성합니다.

1. 대상 컨테이너와 Task를 로드하여 컨테이너가 실행 중(`Running`)인지 확인합니다.
2. 기존 컨테이너의 프로세스 Spec을 복제하고, 전달된 명령어(`COMMAND [ARG...]`) 및 TTY 설정을 반영합니다.
3. I/O 스트림 및 터미널 모드를 구성합니다.
   - **백그라운드(`-d`)**: `cio.NullIO`를 사용합니다.
   - **포그라운드/대화형**: 표준 입출력(`os.Stdin`, `os.Stdout`, `os.Stderr`)을 연결합니다. TTY 모드이며 백그라운드가 아닌 경우, 로컬 터미널을 원시 모드(`term.MakeRaw`)로 전환하여 사용자의 키 입력(Ctrl+C, 화살표 키 등)이 컨테이너 내부 프로세스로 그대로 전달되도록 합니다.
4. 임의의 Exec ID(`exec-<random>`)로 프로세스를 생성(`task.Exec`)하고 시작(`Start`)합니다.
5. 백그라운드 실행이 아닌 경우 프로세스가 종료될 때까지 대기(`Wait`)하고, 종료 코드를 반환한 후 exec 프로세스를 정리(`Delete`)합니다.

---

## gocker version

gocker의 버전과 containerd 데몬의 버전 및 리비전 정보를 출력합니다.

### Usage

```bash
gocker version
```

### 동작 방식

gocker 버전을 먼저 출력한 뒤, containerd 클라이언트의 `Version(ctx)` API로 데몬의 버전(`Version`)과 Git 리비전(`Revision`)을 가져와 출력합니다. containerd에 연결할 수 없으면 gocker 버전만 출력하고 성공으로 끝납니다.

---

## 알려진 한계

| 항목 | 현재 동작 |
|---|---|
| 서브커맨드의 `-h` | 도움말은 출력되지만 종료 코드가 1이다 |
| `create -t` 뒤의 `start`, `run -d -t` | 실패한다. 데몬이 없어 가상 터미널을 유지할 프로세스가 없다. `-t`는 포그라운드 `run`에서만 쓸 수 있다 |
| `run -d`의 종료 코드 | 기록되지 않는다 |
| 중지된 컨테이너 | 다시 시작할 수 없다 |
| 포그라운드 `run` 중 SIGINT | 컨테이너에는 SIGTERM으로 전달된다 |
| Ctrl+C 뒤 유예 시간(2초) 중에 한 번 더 Ctrl+C | gocker가 즉시 끝나고 컨테이너가 남을 수 있다 |
| 알 수 없는 이미지 이름 | 명령으로 해석되어 "실행 파일을 찾을 수 없음" 에러가 난다 |
| `-ti`, `-aq` 같은 묶은 플래그 | 지원하지 않는다 |
| rootfs 아카이브 | 신뢰할 수 있는 것만 써야 한다. 아카이브 안의 심볼릭 링크를 거쳐 대상 디렉터리 밖에 쓰는 것을 막지 못한다 |
| `exec` | containerd 컨테이너만 대상이다 |
| `pull` | `alpine:latest` 같은 짧은 이름을 받지 못한다 |

## 테스트

단위 테스트는 root 권한 없이 실행할 수 있다.

```bash
go test ./...
```

E2E 테스트는 root 권한과 runc, `assets/alpine-minirootfs.tar.gz`가 필요하다. 먼저 빌드한 뒤 실행한다.

```bash
sudo bash scripts/e2e.sh
```

모든 명령어와 플래그를 확인하는 전체 테스트는 containerd, `jq`, `script(1)`도 필요하다. **호스트의 gocker 컨테이너를 모두 지우고 테스트 도중 containerd를 잠시 멈추므로** 테스트 전용 호스트에서 실행한다. 결과는 [테스트 보고서](docs/phase2-test-report.md)에 있다.

```bash
sudo bash scripts/fulltest.sh
```
