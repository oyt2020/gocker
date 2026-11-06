package cli

import (
	"context"
	"flag"
	"fmt"
)

func (d *Dispatcher) handleRm(ctx context.Context, args []string) error {
	rmCmd := flag.NewFlagSet("rm", flag.ContinueOnError)

	force := rmCmd.Bool("f", false, "실행 중인 컨테이너 강제 삭제 (SIGKILL 전송 후 삭제)")
	rmCmd.BoolVar(force, "force", false, "실행 중인 컨테이너 강제 삭제")

	rmCmd.Usage = func() {
		fmt.Println("Usage: gocker rm [OPTIONS] CONTAINER [CONTAINER...]")
		fmt.Println("\n하나 이상의 컨테이너와 번들 리소스를 삭제합니다.")
		fmt.Println("\nOptions:")
		rmCmd.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # 중지된 컨테이너 삭제")
		fmt.Println("  gocker rm my-box")
		fmt.Println("\n  # 실행 중인 컨테이너 강제 삭제")
		fmt.Println("  gocker rm -f my-box")
		fmt.Println("\n  # 여러 컨테이너 일괄 삭제")
		fmt.Println("  gocker rm box1 box2 box3")
	}

	if err := rmCmd.Parse(args); err != nil {
		return err
	}

	targets := rmCmd.Args()
	if len(targets) < 1 {
		return fmt.Errorf("삭제할 컨테이너를 지정해야 합니다. \n도움말 'gocker rm -h' 확인")
	}

	for _, target := range targets {
		id, err := d.resolveContainerID(ctx, target)
		if err != nil {
			return err
		}

		if err := d.rt.Delete(ctx, id, *force); err != nil {
			return fmt.Errorf("컨테이너 %q 삭제 실패: %w", target, err)
		}
		fmt.Println(target)
	}

	return nil
}
