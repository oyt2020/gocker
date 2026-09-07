package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/platforms"
)

func handleImagesCommand() {}

func handleImages(r *Runtime, ns string) {

	ctx := context.Background()

	if ns == "" {
		ctx = namespaces.WithNamespace(ctx, "default")
	} else {
		ctx = namespaces.WithNamespace(ctx, ns)
	}

	//imageStore := r.client.ImageService()
	//imageList2, err := imageStore.List(ctx)
	imageList, err := r.client.ListImages(ctx)

	if err != nil {
		log.Fatalf("첫 번째 실패 : %v", err)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "REPOSITORY\tIMAGE ID\tSIZE")

	for _, img := range imageList {

		repo := img.Name()
		shortDigest := img.Target().Digest.Hex()
		//platformStr := "Unknown"
		//if p := img.Target().Platform; p != nil {
		//	platformStr = fmt.Sprintf("%s/%s", p.OS, p.Architecture)
		//}
		//platform := platforms.DefaultString()

		if len(shortDigest) > 12 {
			shortDigest = shortDigest[:12]
		}

		var sizeStr string

		size, err := img.Size(ctx)

		if err == nil {
			sizeStr = formatBytes(size)
		} else {
			cs := r.client.ContentStore()
			meta := img.Metadata()
			totalSize, sizeErr := meta.Size(ctx, cs, platforms.All)
			if sizeErr == nil && totalSize > 0 {
				sizeStr = formatBytes(totalSize)
			} else {
				sizeStr = "N/A"
			}
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\n",
			repo,
			shortDigest,
			sizeStr,
		)

	}
	tw.Flush()

}

func formatBytes(bytes int64) string {
	const uint = 1024

	if bytes < uint {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(uint), 0

	for n := bytes / uint; n >= uint; n /= uint {
		div *= uint
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
