package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
)

func handlePsCommand(r *Runtime, args []string) {
	psCmd := flag.NewFlagSet("ps", flag.ExitOnError)

	ns := psCmd.String("ns", "default", "네임스페이스 지정")
	all := psCmd.Bool("all", false, "모든 컨테이너 프로세스 조회")
	quite := psCmd.Bool("q", false, "출력 간소화(이름만 출력)")

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

	psCmd.Parse(args)

	handlePs(r, *ns, *all, *quite)
}

func handlePs(r *Runtime, ns string, all bool, quiet bool) {
	ctx := namespaces.WithNamespace(context.Background(), ns)

	containers, err := r.client.Containers(ctx)

	if err != nil {
		log.Fatalf("첫 번째 에러 (컨테이너 조회 실패) %v", err)
	}

	var tw *tabwriter.Writer

	if !quiet {
		tw = tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		fmt.Fprintln(tw, "CONTAINER ID\tIMAGE\tPID\tSTATUS")
	}

	for _, c := range containers {

		info, err := c.Info(ctx)
		if err != nil {
			log.Fatalf("두 번째 에러 (메타데이터 조회 실패) %v", err)
		}

		task, taskErr := c.Task(ctx, nil)
		var isRunning bool
		var statusStr string
		var pid int64

		if taskErr != nil {
			isRunning = false
			statusStr = "Exited"
			pid = -1

		} else {
			status, err := task.Status(ctx)
			if err == nil {
				switch status.Status {
				case client.Running:
					isRunning = true
					statusStr = "Running"
					pid = int64(task.Pid())
				case client.Stopped:
					isRunning = false
					statusStr = "Stopped"
				case client.Paused:
					isRunning = true
					statusStr = "Paused"
					pid = int64(task.Pid())
				default:
					statusStr = string(status.Status)
				}
			} else {
				statusStr = "Unknown"
			}
		}

		if !all && !isRunning {
			continue
		}

		if quiet {
			fmt.Println(c.ID())
			continue
		}

		imageName := info.Image

		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
			c.ID(),
			imageName,
			pid,
			statusStr,
		)
	}

	if !quiet {
		tw.Flush()
	}
	}
