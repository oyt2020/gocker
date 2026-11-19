// Package reaper 컨테이너 init 프로세스의 종료 상태를 수거(reap)한다.
//
// 종료 상태는 커널이 부모 프로세스에게만 wait()로 넘겨준다. 그런데 `runc create`는 컨테이너 프로세스를
// 만든 뒤 곧바로 종료하므로, 컨테이너 프로세스는 고아가 되어 호스트 init(PID 1)에 입양되고 종료 코드는
// 거기서 버려진다. gocker가 child subreaper가 되면 고아가 된 자손이 init 대신 gocker에 입양되므로,
// gocker가 살아 있는 동안(포그라운드 run)에는 직접 wait하여 종료 코드를 얻을 수 있다.
package reaper

import "context"

// Reaper : 고아가 된 자손 프로세스를 입양하고 종료 상태를 수거한다
type Reaper interface {
	// Adopt : 현재 프로세스를 child subreaper로 지정한다. 이후 고아가 되는 자손은 이 프로세스에 입양된다.
	// 입양은 부모가 죽는 시점에 결정되므로, 반드시 `runc create` 호출 전에 불러야 한다. 여러 번 불러도 안전하다.
	Adopt() error
	// Wait : pid가 종료될 때까지 기다린 뒤 종료 상태를 수거하고 종료 코드를 반환한다.
	// 정상 종료면 exit status, 시그널로 죽었으면 128+시그널 번호다 (셸 / Docker 관례).
	// pid는 이 프로세스의 자식(입양된 자손 포함)이어야 한다.
	Wait(ctx context.Context, pid int) (exitCode int, err error)
}
