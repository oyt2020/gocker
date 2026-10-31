package runc

import "io"

// IOOption : runc 실행을 위한 I/O 스트리밍 및 터미널 옵션 구조체
type IOOption struct {
	Stdin         io.Reader // FD 0
	Stdout        io.Writer // FD 1
	Stderr        io.Writer // FD 2
	ConsoleSocket string    // Master FD를 받기 위한 유닉스 소켓 경로
}
