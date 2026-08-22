## 환경
우분투 24.04

## 버전

Go : 1.26.5

## 외부 패키지


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

