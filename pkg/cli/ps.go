package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

func (d *Dispatcher) handlePs(ctx context.Context, args []string) error {
	psCmd := flag.NewFlagSet("ps", flag.ContinueOnError)

	all := psCmd.Bool("all", false, "모든 상태의 컨테이너 조회 (기본값: 실행 중인 컨테이너만)")
	psCmd.BoolVar(all, "a", false, "모든 상태의 컨테이너 조회 (단축형)")
	quiet := psCmd.Bool("q", false, "컨테이너 ID만 간소하게 출력")

	psCmd.Usage = func() {
		fmt.Println("Usage: gocker ps [OPTIONS]")
		fmt.Println("\n컨테이너 목록을 출력합니다.")
		fmt.Println("\nOptions:")
		psCmd.PrintDefaults()

		fmt.Println("\nExamples:")
		fmt.Println("  # 실행 중인 컨테이너 조회")
		fmt.Println("  gocker ps")
		fmt.Println("\n  # 모든 상태(Created, Running, Stopped 등) 조회")
		fmt.Println("  gocker ps -a")
		fmt.Println("\n  # 컨테이너 ID만 출력")
		fmt.Println("  gocker ps -q")
	}

	if err := psCmd.Parse(args); err != nil {
		return err
	}

	remainArgs := psCmd.Args()
	if len(remainArgs) != 0 {
		return fmt.Errorf("gocker ps 명령은 추가 인자를 받지 않습니다. \n도움말 'gocker ps -h' 확인")
	}

	containers, err := d.rt.List(ctx, *all)
	if err != nil {
		return fmt.Errorf("컨테이너 목록 조회 실패: %w", err)
	}

	if *quiet {
		for _, c := range containers {
			shortID := c.ID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			fmt.Println(shortID)
		}
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "CONTAINER ID\tNAME\tSTATUS\tPID\tCREATED\tCOMMAND"); err != nil {
		return err
	}

	for _, c := range containers {
		shortID := c.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}

		name := c.Name
		if name == "" {
			name = "-"
		}

		pidStr := "-"
		if c.Pid > 0 {
			pidStr = fmt.Sprintf("%d", c.Pid)
		}

		createdStr := formatSince(time.Since(c.CreatedAt))
		cmdStr := strings.Join(c.Command, " ")
		if len(cmdStr) > 30 {
			cmdStr = cmdStr[:27] + "..."
		}
		if cmdStr == "" {
			cmdStr = "-"
		}

		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID,
			name,
			c.State,
			pidStr,
			createdStr,
			cmdStr,
		); err != nil {
			return err
		}
	}

	return tw.Flush()
}

func formatSince(d time.Duration) string {
	sec := int(d.Seconds())
	if sec < 60 {
		return fmt.Sprintf("%d seconds ago", sec)
	}
	min := sec / 60
	if min < 60 {
		return fmt.Sprintf("%d minutes ago", min)
	}
	hours := min / 60
	if hours < 24 {
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := hours / 24
	return fmt.Sprintf("%d days ago", days)
}
