package cli

import (
	"context"
	"flag"
	"fmt"
)

func (d *Dispatcher) handleStart(ctx context.Context, args []string) error {
	startCmd := flag.NewFlagSet("start", flag.ContinueOnError)

	startCmd.Usage = func() {
		fmt.Println("Usage: gocker start [OPTIONS] CONTAINER [CONTAINER...]")
		fmt.Println("\n하나 이상의 중지된 컨테이너를 다시 시작합니다.")
		fmt.Println("\nOptions:")
		startCmd.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # 단일 컨테이너 시작")
		fmt.Println("  gocker start my-nginx")
		fmt.Println("\n  # 여러 컨테이너 시작")
		fmt.Println("  gocker start my-nginx c8a19d3f10be")
	}

	if err := startCmd.Parse(args); err != nil {
		return err
	}

	targets := startCmd.Args()
	if len(targets) < 1 {
		return fmt.Errorf("시작할 컨테이너 이름 또는 ID를 지정해야 합니다. \n도움말 'gocker start -h' 확인")
	}

	for _, target := range targets {
		if err := d.rt.Start(ctx, target); err != nil {
			return fmt.Errorf("컨테이너 %q 시작 실패: %w", target, err)
		}
		fmt.Println(target)
	}

	return nil
}
