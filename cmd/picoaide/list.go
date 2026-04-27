package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/config"
	dockerpkg "github.com/picoaide/picoaide/internal/docker"
)

func List(cfg *config.GlobalConfig) error {
	containers, err := auth.GetAllContainers()
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		fmt.Println("暂无用户。请先运行 init 命令初始化。")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "用户名\t状态\t镜像\tIP")
	fmt.Fprintln(w, "──────\t────\t────\t──")

	ctx := context.Background()
	for _, c := range containers {
		status := c.Status
		if c.ContainerID != "" {
			status = dockerpkg.ContainerStatus(ctx, c.ContainerID)
			if status == "running" {
				status = "Up"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Username, status, c.Image, c.IP)
	}
	w.Flush()
	return nil
}
