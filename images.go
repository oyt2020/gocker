package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/containerd/containerd/v2/pkg/namespaces"
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

		if len(shortDigest) > 12 {
			shortDigest = shortDigest[:12]
		}

		size, err := img.Size(ctx)

		if err != nil {
			log.Fatalf("사이즈 실패, %v", err)
		}

		fmt.Fprintf(tw, "%s\t%s\t%d\n",
			repo,
			shortDigest,
			size)

	}
	tw.Flush()

}
