package main

import (
	"context"
	"flag"
	"fmt"
)

func handleStopCommand(ctx context.Context, r *Runtime, args []string) error {
	stopCmd := flag.NewFlagSet("stop", flag.ContinueOnError)

	ns := stopCmd.String("ns", "default", "네임스페이스 지정")
	timeoutSec := stopCmd.Int("t", 10, "종료 대기 타임아웃 시간 지정 (단위 초, 기본 값 10초)")

	stopCmd.Usage = func() {
		fmt.Println("Usage: gocker stop [OPTIONS] CONTAINER")
		fmt.Println("\n 하나 이상의 실행 중인 컨테이너를 중지합니다.")
		fmt.Println("\nOptions")
		stopCmd.PrintDefaults()

		fmt.Println("\nExamples")
		fmt.Println(" # 단일 컨테이너 중지")
		fmt.Println(" gocker stop my_nginx")

		fmt.Println("\n # 여러 컨테이너 중지")
		fmt.Println(" gocker stop my_nginx my_busybox")

		fmt.Println("\n # 5초 대기 후 강제 종료")
		fmt.Println(" gocker stop -t=5 my_nginx")
	}

	if err := stopCmd.Parse(args); err != nil {
		return err
	}

	targets := stopCmd.Args()
	if len(targets) < 1 {
		return fmt.Errorf("정지할 컨테이너를 지정해야 합니다. \n 도움말 'gocker stop -h' 확인")
	}

	if *timeoutSec <= 0 {
		return fmt.Errorf("대기 시간은 1초 이상으로 지정해야 합니다. 현재 지정 시간 : %d", *timeoutSec)
	}

	opts := StopOptions{
		Namespace: *ns,
		Timeout:   *timeoutSec,
	}

	result, err := r.Stop(ctx, targets, opts)

	if err != nil {
		return err
	}

	for _, i := range result.ContainerID {
		fmt.Println(i)
	}

	return nil
}
