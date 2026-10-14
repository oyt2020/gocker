## 환경
우분투 24.04

## 버전

Go : 1.26.3

## 외부 패키지
1. github.com/containerd/containerd/v2
2. github.com/containerd/platforms
3. github.com/opencontainers/image-spec
4. golang.org/x/term

# 사전 준비

### containerd

설치
```
sudo apt install -y containerd
```

확인
```bash
sudo ctr images pull docker.io/library/alpine:latest

sudo ctr run --rm \
  docker.io/library/alpine:latest \
  hello-test \
  echo "hello containerd"
```

---

## gocker pull

containerd를 사용하여 컨테이너 이미지를 로컬로 다운로드합니다.

이미지를 pull하면 이미지 실행에 필요한 레이어가 함께 다운로드되며, 지정된 snapshotter를 사용하여 이미지의 파일 시스템을 unpack합니다.

### Usage

```bash
gocker pull [OPTIONS] <image>
```

### Options

| 옵션 | 설명 |
| --- | --- |
| `--platform` | 다운로드할 이미지의 대상 플랫폼을 지정합니다. 예: `linux/amd64`, `linux/arm64` |
| `--snapshotter` | 이미지 레이어를 unpack할 때 사용할 snapshotter를 지정합니다. 예: `overlayfs`, `native` |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `pull` 명령어의 도움말을 출력합니다. |

`--platform`을 지정하지 않으면 현재 시스템의 기본 플랫폼을 사용합니다.

`snapshotter`를 별도로 지정하지 않으면 containerd에 설정된 기본 snapshotter를 사용합니다.

### Examples

#### 이미지 다운로드

```bash
gocker pull docker.io/library/busybox:latest
```

#### 특정 플랫폼의 이미지 다운로드

```bash
gocker pull --platform=linux/amd64 docker.io/library/busybox:latest
```

ARM64 이미지를 다운로드하려면 다음과 같이 사용할 수 있습니다.

```bash
gocker pull --platform=linux/arm64 docker.io/library/busybox:latest
```

#### Snapshotter 지정

```bash
gocker pull --snapshotter=overlayfs docker.io/library/busybox:latest
```

#### 플랫폼과 Snapshotter 함께 지정

```bash
gocker pull \
  --platform=linux/amd64 \
  --snapshotter=overlayfs \
  docker.io/library/busybox:latest
```

### 동작 방식

`gocker pull`은 내부적으로 containerd의 Pull API를 사용하여 이미지를 다운로드합니다.

이미지를 다운로드할 때 다음 작업을 수행합니다.

1. 이미지와 관련된 메타데이터를 다운로드합니다.
2. 지정된 플랫폼에 맞는 이미지 manifest와 레이어를 가져옵니다.
3. 이미지 레이어를 content store에 저장합니다.
4. 지정된 snapshotter를 사용하여 이미지 파일 시스템을 unpack합니다.

따라서 pull이 완료된 이미지는 이후 컨테이너 생성 및 실행에 사용할 수 있습니다.

### Help

자세한 사용 방법은 다음 명령어로 확인할 수 있습니다.

```bash
gocker pull -h
```

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
| --- | --- |
| `--name` | 이미지 이름, 태그, 레지스트리의 부분 일치 필터입니다. 예: `nginx`, `docker` |
| `--platform` | 조회할 이미지의 대상 플랫폼을 지정합니다. 예: `linux/amd64`, `linux/arm64` |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `images` 명령어의 도움말을 출력합니다. |

`--platform`을 지정하면 해당 플랫폼의 이미지 크기 및 호스트 플랫폼과의 일치 여부를 매칭하여 표시합니다.

### Examples

#### 로컬 이미지 전체 목록 조회

```bash
gocker images
```

#### 특정 이름을 포함하는 이미지 필터링

```bash
gocker images --name=nginx
```

#### 특정 플랫폼 이미지 목록 조회

```bash
gocker images --platform=linux/amd64
```

#### 특정 네임스페이스의 이미지 목록 조회

```bash
gocker images --ns=default
```

### 동작 방식

`gocker images`는 내부적으로 containerd의 ListImages API를 사용하여 로컬에 저장된 이미지 목록을 가져옵니다.

이미지 목록을 조회할 때 다음 작업을 수행합니다.

1. 지정된 네임스페이스(`--ns`, 기본값: `default`)에 등록된 모든 이미지를 조회합니다.
2. `--name` 옵션이 지정된 경우, 대소문자를 구분하지 않고 이미지 전체 이름(`Repository`)에 해당 키워드가 포함된 이미지만 필터링합니다.
3. `--platform` 옵션이 지정된 경우 대상 플랫폼에 맞는 크기(`SIZE`)와 호스트 플랫폼과의 일치 여부(`PLATFORM STATUS`)를 계산합니다.
4. 플랫폼을 별도로 지정하지 않은 경우 호스트 기본 플랫폼과의 일치 여부를 확인하며, 불일치 시 이미지 하위 매니페스트(Children)를 탐색하여 다운로드된 실제 플랫폼 정보를 찾아 표시합니다.
5. 조회된 결과를 `REPOSITORY`, `IMAGE ID`(12자리 다이제스트), `SIZE`, `PLATFORM STATUS` 컬럼 형태로 정렬하여 출력합니다.

### Help

자세한 사용 방법은 다음 명령어로 확인할 수 있습니다.

```bash
gocker images -h
```

---

## gocker run

containerd를 사용하여 컨테이너를 생성하고 시작합니다.

이미지로부터 루트 파일 시스템 스냅샷을 생성하고 OCI 사양(Spec)에 맞추어 프로세스를 실행합니다. 백그라운드 실행(`-d`), 프로세스 종료 시 자동 삭제(`--rm`), 가상 터미널(`-t`) 할당을 지원합니다.

### Usage

```bash
gocker run [OPTIONS] IMAGE [COMMAND] [ARG...]
```

> [!NOTE]
> 동시에 하나의 컨테이너만 생성 및 시작할 수 있습니다.

### Options

| 옵션 | 설명 |
| --- | --- |
| `--name` | 생성할 컨테이너의 이름을 지정합니다. (미지정 시 12자리 무작위 16진수 문자열 자동 생성) |
| `-d` | 백그라운드(Detach) 모드로 컨테이너를 실행합니다. |
| `--rm` | 컨테이너 프로세스가 종료되면 컨테이너 메타데이터 및 스냅샷을 자동으로 삭제합니다. |
| `-t` | 가상 터미널(TTY)을 할당합니다. |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `run` 명령어의 도움말을 출력합니다. |

### Examples

#### 백그라운드로 컨테이너 실행

```bash
gocker run -d --name my-nginx docker.io/library/nginx:latest
```

#### 포그라운드에서 일회성 명령어 실행 후 자동 삭제

```bash
gocker run --rm docker.io/library/busybox:latest echo Hello Gocker
```

#### 가상 터미널 할당하여 실행

```bash
gocker run -t docker.io/library/nginx:latest sh
```

### 동작 방식

`gocker run`은 containerd의 Container 및 Task API를 사용하여 컨테이너 생성부터 실행 및 라이프사이클 관리를 수행합니다.

컨테이너를 실행할 때 다음 작업을 수행합니다.

1. 로컬 이미지 저장소에서 지정된 `IMAGE`를 조회합니다.
2. OCI 이미지 설정을 기반으로 프로세스 Spec을 구성하고, 인자로 전달된 명령어(`[COMMAND] [ARG...]`)가 있을 경우 실행 프로세스 인자로 덮어씌웁니다.
3. 컨테이너 ID를 설정하고, 이미지 레이어를 기반으로 새로운 스냅샷(`WithNewSnapshot`)과 컨테이너 메타데이터를 생성합니다.
4. 컨테이너로부터 새로운 Task를 생성하고 I/O 스트림을 연결합니다.
   - **백그라운드(`-d`)**: `cio.NullIO`를 연결하여 Task를 시작(`Start`)한 뒤 생성된 컨테이너 ID를 출력합니다.
   - **포그라운드**: 표준 입출력(`os.Stdin`, `os.Stdout`, `os.Stderr`)을 연결하고 Task의 종료를 대기(`Wait`)한 뒤 종료 코드(Exit Code)를 출력합니다. Task 시작 전 실패가 발생하면 생성된 컨테이너 및 스냅샷을 롤백(삭제)합니다.
5. `--rm` 플래그가 지정된 경우, 컨테이너 종료 후 컨테이너 메타데이터와 파일 시스템 스냅샷을 자동으로 정리합니다.

### Help

자세한 사용 방법은 다음 명령어로 확인할 수 있습니다.

```bash
gocker run -h
```

---

## gocker ps

현재 containerd에 존재하는 컨테이너 프로세스 목록 및 상태를 출력합니다.

기본적으로 실행 중인 컨테이너만 조회하며, 중지된 컨테이너를 포함한 전체 조회 및 컨테이너 ID만 간소하게 출력하는 기능을 제공합니다.

### Usage

```bash
gocker ps [OPTIONS]
```

### Options

| 옵션 | 설명 |
| --- | --- |
| `-all` | 중지/종료된 컨테이너를 포함한 모든 프로세스(Running, Stopped, Exited 등)를 조회합니다. |
| `-q` | 컨테이너 ID만 간소화하여 한 줄씩 출력합니다. |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `ps` 명령어의 도움말을 출력합니다. |

### Examples

#### 실행 중인 컨테이너 목록 조회

```bash
gocker ps
```

#### 모든 상태의 컨테이너 목록 조회

```bash
gocker ps -all
```

#### 특정 네임스페이스의 컨테이너 목록 조회

```bash
gocker ps --ns=default
```

#### 출력 간소화 (컨테이너 ID만 출력)

```bash
gocker ps -q
```

### 동작 방식

`gocker ps`는 지정된 네임스페이스의 컨테이너 메타데이터와 Task 상태를 조회하여 동작합니다.

1. 지정된 네임스페이스(`--ns`, 기본값: `default`) 내의 전체 컨테이너 목록을 조회합니다.
2. 각 컨테이너와 연결된 Task의 상태(`task.Status`) 및 Host PID를 확인합니다.
   - 연결된 Task가 없거나 종료된 경우 상태를 `Exited` (PID: -1)로 처리합니다.
   - Task가 존재하는 경우 `Running`, `Stopped`, `Paused` 등의 상태와 실행 중인 PID를 가져옵니다.
3. `-all` 옵션이 지정되지 않은 경우 `Running` 또는 `Paused` 상태의 실행 중인 컨테이너만 필터링합니다.
4. `-q` 옵션이 활성화되어 있으면 컨테이너 ID만 출력하고, 그렇지 않으면 `CONTAINER ID`, `IMAGE`, `PID`, `STATUS` 컬럼 형태로 정렬하여 출력합니다.

### Help

자세한 사용 방법은 다음 명령어로 확인할 수 있습니다.

```bash
gocker ps -h
```

---

## gocker stop

하나 이상의 실행 중인 컨테이너를 중지합니다.

컨테이너에 정상 종료 신호(SIGTERM)를 전달하고, 지정된 타임아웃 내에 종료되지 않으면 강제 종료 신호(SIGKILL)를 전송합니다.

### Usage

```bash
gocker stop [OPTIONS] CONTAINER [CONTAINER...]
```

### Options

| 옵션 | 설명 |
| --- | --- |
| `-t` | 컨테이너 종료 대기 타임아웃 시간(단위: 초)을 지정합니다. (기본값: `10`) |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `stop` 명령어의 도움말을 출력합니다. |

`-t` 대기 시간은 1초 이상으로 지정해야 합니다.

### Examples

#### 단일 컨테이너 중지

```bash
gocker stop my_nginx
```

#### 여러 컨테이너 동시 중지

```bash
gocker stop my_nginx my_busybox
```

#### 종료 대기 타임아웃 지정 후 중지

```bash
gocker stop -t=5 my_nginx
```

#### 특정 네임스페이스의 컨테이너 중지

```bash
gocker stop --ns=default my_nginx
```

### 동작 방식

`gocker stop`은 컨테이너 Task에 단계적으로 종료 신호를 전달하여 안전한 종료를 보장합니다.

1. 지정된 컨테이너 이름(ID)으로 컨테이너 및 Task를 로드하고 현재 상태가 `Running`인지 확인합니다.
2. 프로세스 종료 알림을 감지할 `Wait` 채널을 설정합니다.
3. 프로세스에 `SIGTERM` 신호를 전송하여 정상 종료를 유도합니다.
4. 지정된 타임아웃(`-t`, 기본값: 10초) 동안 대기하며, 기한 내에 종료되지 않을 경우 `SIGKILL` 신호를 전송하여 강제 종료합니다.
5. Task 종료가 완료되면 남아있는 Task 리소스를 삭제(`Delete`)하여 정리하고, 중지된 컨테이너 ID를 출력합니다.

### Help

자세한 사용 방법은 다음 명령어로 확인할 수 있습니다.

```bash
gocker stop -h
```

---

## gocker start

하나 이상의 중지된 컨테이너를 다시 시작합니다.

기본적으로 백그라운드에서 실행되며, `-attach` 옵션을 지정하여 포그라운드 화면과 표준 입출력을 연결할 수 있습니다.

### Usage

```bash
gocker start [OPTIONS] CONTAINER [CONTAINER...]
```

> [!NOTE]
> 포그라운드 실행(`-attach`) 시에는 단 하나의 컨테이너만 지정할 수 있습니다.

### Options

| 옵션 | 설명 |
| --- | --- |
| `-attach` | 포그라운드로 실행하며 컨테이너의 표준 입출력 화면을 연결합니다. |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `start` 명령어의 도움말을 출력합니다. |

### Examples

#### 백그라운드로 컨테이너 다시 시작

```bash
gocker start my-nginx
```

#### 여러 컨테이너 동시 시작

```bash
gocker start my-nginx c8a19d3f10be
```

#### 포그라운드로 실행 화면 연결

```bash
gocker start -attach my-nginx
```

### 동작 방식

`gocker start`는 기존에 생성된 컨테이너 메타데이터와 파일 시스템 스냅샷을 재사용하여 새로운 Task를 생성 및 실행합니다.

1. 대상 컨테이너를 로드하고 기존 Task 상태를 확인합니다. 이미 실행 중(`Running`)이거나 비정상 상태인 경우 오류를 반환하며, 종료된(`Stopped`) 잔여 Task가 있으면 정리(`Delete`)합니다.
2. 컨테이너로부터 새로운 Task(`container.NewTask`)를 생성합니다.
3. 실행 모드에 따라 I/O 및 라이프사이클을 처리합니다.
   - **백그라운드 (기본)**: `cio.NullIO`를 연결하여 Task를 시작(`Start`)한 후 컨테이너 ID를 출력합니다.
   - **포그라운드 (`-attach`)**: 컨테이너 Spec의 터미널 설정 여부를 반영하여 표준 입출력(`WithStdio`, `WithTerminal`)을 연결합니다. Task 시작 후 프로세스가 종료될 때까지 대기(`Wait`)하며, 종료 시 Task를 삭제 정리하고 프로세스 종료 코드를 출력합니다.

### Help

자세한 사용 방법은 다음 명령어로 확인할 수 있습니다.

```bash
gocker start -h
```

---

## gocker exec

실행 중인 컨테이너 내부에서 새로운 프로세스(명령어)를 실행합니다.

`-it` 옵션을 통해 대화형 터미널 쉘에 접속하거나, 백그라운드(`-d`)로 컨테이너 내부 명령을 실행할 수 있습니다.

### Usage

```bash
gocker exec [OPTIONS] CONTAINER COMMAND [ARG...]
```

### Options

| 옵션 | 설명 |
| --- | --- |
| `-it` | 대화형 가상 터미널을 할당합니다. (`-i`와 `-t` 동시 적용) |
| `-i` | 표준 입력(stdin)을 유지합니다. |
| `-t` | 가상 터미널(TTY)을 할당합니다. |
| `-d` | 명령어를 백그라운드(Detach) 모드로 실행합니다. |
| `--ns` | 네임스페이스를 지정합니다. (기본값: `default`) |
| `-h` | `exec` 명령어의 도움말을 출력합니다. |

### Examples

#### 실행 중인 컨테이너의 쉘에 대화형으로 접속

```bash
gocker exec -it my-nginx sh
```

#### 컨테이너 내부 파일 목록 확인 (단일 명령어 실행)

```bash
gocker exec my-nginx ls -la /etc/nginx
```

#### 백그라운드에서 명령어 실행

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

### Help

자세한 사용 방법은 다음 명령어로 확인할 수 있습니다.

```bash
gocker exec -h
```

---

## gocker version

containerd 데몬의 버전 및 리비전 정보를 출력합니다.

### Usage

```bash
gocker version
```

### Examples

```bash
gocker version
```

### 동작 방식

`gocker version`은 containerd 클라이언트의 `Version(ctx)` API를 호출하여 현재 통신 중인 containerd 데몬의 버전(`Version`) 및 Git 리비전(`Revision`) 정보를 가져와 출력합니다.


