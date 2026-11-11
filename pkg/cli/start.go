package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/oyt2020/gocker/pkg/runtime"
	"github.com/oyt2020/gocker/pkg/util"
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
		id, err := d.resolveContainerID(ctx, target)
		if err != nil {
			return err
		}

		if err := d.rt.Start(ctx, id); err != nil {
			return fmt.Errorf("컨테이너 %q 시작 실패: %w", target, err)
		}
		fmt.Println(target)
	}

	return nil
}

// resolveContainerID 컨테이너 이름 또는 짧은 ID를 전체 ID로 변환
func (d *Dispatcher) resolveContainerID(ctx context.Context, target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("container id or name cannot be empty")
	}

	// 1. 정규 12자리 ID 형식인 경우 단건 조회 우선 시도
	if util.IsValidID(target) {
		if meta, err := d.rt.Get(ctx, target); err == nil {
			return meta.ID, nil
		}
	}

	// 2. 전체 목록에서 이름 또는 ID 접두어 검색
	containers, err := d.rt.List(ctx, true)
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	var matches []*runtime.ContainerMetadata
	for _, c := range containers {
		if c.ID == target {
			return c.ID, nil
		}
		if (len(target) >= 4 && strings.HasPrefix(c.ID, target)) || (c.Name != "" && c.Name == target) {
			matches = append(matches, c)
		}
	}

	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf(
			"컨테이너 %q에 일치하는 대상이 %d개입니다. "+
				"'gocker ps -a'로 ID를 확인한 뒤 전체 컨테이너 ID로 다시 시도하세요",
			target, len(matches),
		)
	}

	return "", fmt.Errorf("container %q not found", target)
}
