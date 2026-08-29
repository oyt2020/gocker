## 환경
우분투 24.04

## 버전

Go : 1.26.5

## 외부 패키지
1. github.com/containerd/containerd/v2/client
2. github.com/containerd/containerd/v2/pkg/namespaces
3. github.com/containerd/platforms

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

