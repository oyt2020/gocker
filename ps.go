package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

func handlePsCommand(ctx context.Context, r *Runtime, args []string) error {
	psCmd := flag.NewFlagSet("ps", flag.ContinueOnError)

	ns := psCmd.String("ns", "default", "네임스페이스 지정")
	all := psCmd.Bool("all", false, "모든 컨테이너 프로세스 조회")
	quiet := psCmd.Bool("q", false, "출력 간소화(이름만 출력)")

	psCmd.Usage = func() {
		fmt.Println("Usage: ps [-all]")
		fmt.Println("\n컨테이너 프로세스 목록을 출력합니다.")
		fmt.Println("\nOptions")
		psCmd.PrintDefaults()

		fmt.Println("\nExamples")
		fmt.Println(" # Running 상태인 프로세스")
		fmt.Println(" gocker ps")

		fmt.Println("\n # 모든 상태(Running, Stopped, Exited 등) 조회")
		fmt.Println(" gocker ps -all")

		fmt.Println("\n # 특정 네임스페이스에서만 조회")
		fmt.Println(" gocker ps --ns=default")

		fmt.Println("\n # 출력 간소화")
		fmt.Println(" gocker ps -q")
	}

	if err := psCmd.Parse(args); err != nil {
		return err
	}

	remainArgs := psCmd.Args()

	if len(remainArgs) != 0 {
		return fmt.Errorf("gocker ps 명령은 추가 인자를 받지 않습니다. \n 도움말 'gocker ps -h' 확인")
	}

	opts := PsOptions{
		Namespace: *ns,
		All:       *all,
	}

	psResult, err := r.Ps(ctx, opts)

	if err != nil {
		return err
	}

	if *quiet {
		for _, c := range psResult {
			fmt.Println(c.ContainerID)
		}
	} else {
		var tw *tabwriter.Writer

		tw = tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "CONTAINER ID\tIMAGE\tPID\tSTATUS"); err != nil {
			return err
		}

		for _, c := range psResult {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",

				c.ContainerID,
				c.ImageName,
				c.Pid,
				c.Status,
			); err != nil {
				return err
			}
		}

		if err := tw.Flush(); err != nil {
			return err
		}
	}
	return nil
}
