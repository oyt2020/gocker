package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
)

func handlePs(r *Runtime, ns string, all bool) {
	ctx := namespaces.WithNamespace(context.Background(), ns)

	containers, err := r.client.Containers(ctx)

	if err != nil {
		log.Fatalf("첫 번째 에러 (컨테이너 조회 실패) %v", err)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "CONTAINER ID\tIMAGE\tPID\tSTATUS")

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

		imageName := info.Image

		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
			c.ID(),
			imageName,
			pid,
			statusStr,
		)
	}
	tw.Flush()
}
