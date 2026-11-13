// Package image 이미지 참조를 컨테이너 rootfs 소스로 해석한다.
// Phase 2에서는 로컬 rootfs 아카이브(tar / tar.gz)만 지원한다.
package image

import (
	"os"
	"strings"
)

// DefaultRootFS 기본 이미지(alpine) rootfs 아카이브 경로 (작업 디렉터리 기준)
const DefaultRootFS = "assets/alpine-minirootfs.tar.gz"

// defaultCommand 명령어를 지정하지 않았을 때 실행할 기본 명령어
var defaultCommand = []string{"/bin/sh"}

// DefaultCommand 기본 명령어의 복사본 반환
func DefaultCommand() []string {
	return append([]string(nil), defaultCommand...)
}

// Resolve : 이미지 참조를 rootfs 아카이브 경로로 해석한다. 이미지로 인식되지 않으면 ok=false.
//   - "alpine", "alpine:latest" → 기본 alpine 아카이브
//   - .tar / .tar.gz / .tgz 확장자 → 해당 경로
//   - 존재하는 파일 경로 → 해당 경로
func Resolve(ref string) (rootfs string, ok bool) {
	switch {
	case ref == "":
		return "", false
	case ref == "alpine" || ref == "alpine:latest":
		return DefaultRootFS, true
	case strings.HasSuffix(ref, ".tar") || strings.HasSuffix(ref, ".tar.gz") || strings.HasSuffix(ref, ".tgz"):
		return ref, true
	}
	if fi, err := os.Stat(ref); err == nil && !fi.IsDir() {
		return ref, true
	}
	return "", false
}

// ResolveArgs : `[IMAGE] [COMMAND] [ARG...]` 형태의 인자를 rootfs 경로와 실행 명령어로 분리한다.
// rootfsOverride(--rootfs)가 주어지면 모든 인자를 명령어로 취급한다.
// 첫 인자가 이미지로 해석되지 않으면 기본 이미지를 사용하고 모든 인자를 명령어로 취급한다.
func ResolveArgs(rootfsOverride string, args []string) (rootfs string, command []string) {
	switch {
	case rootfsOverride != "":
		rootfs, command = rootfsOverride, args
	case len(args) == 0:
		rootfs = DefaultRootFS
	default:
		if resolved, ok := Resolve(args[0]); ok {
			rootfs, command = resolved, args[1:]
		} else {
			rootfs, command = DefaultRootFS, args
		}
	}

	if len(command) == 0 {
		command = DefaultCommand()
	}
	return rootfs, command
}
