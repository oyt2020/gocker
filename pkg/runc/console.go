package runc

import (
	"context"
	"os"
)

// Console : `--console-socket`으로 전달되는 PTY master fd의 수신 절차를 감춘 핸들.
//
// 수신 시작 시점(runc create 이전), SCM_RIGHTS 처리, 타임아웃, 늦게 도착한 fd 회수는 모두 구현체의 책임이다.
// 호출자는 Path를 runc에 넘기고 WaitMaster로 master를 받기만 하면 된다.
// master를 받은 뒤 언제 닫을지(=컨테이너 터미널의 수명)는 호출자가 결정한다.
type Console interface {
	// Path : `--console-socket`에 넘길 유닉스 소켓 경로
	Path() string
	// WaitMaster : master fd를 기다린다. 성공하면 소유권이 호출자에게 넘어간다(호출자가 Close).
	// ctx 취소 / 타임아웃 시에는 콘솔을 닫고 에러를 반환한다.
	WaitMaster(ctx context.Context) (*os.File, error)
	// Close : 소켓을 정리한다. 여러 번 호출해도 안전하며, 아직 넘겨주지 않은 master가 있으면 함께 닫는다.
	// 이미 WaitMaster로 넘겨준 master에는 영향을 주지 않는다.
	Close() error
}
