package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/config"
	dockerpkg "github.com/picoaide/picoaide/internal/docker"
	"github.com/picoaide/picoaide/internal/user"
)

func Upgrade(cfg *config.GlobalConfig, configPath, newTag string, targetUser string) error {
	oldTag := cfg.Image.Tag
	if newTag == "" {
		newTag = oldTag
	}

	tagChanged := oldTag != newTag
	imageRef := fmt.Sprintf("%s:%s", cfg.Image.Name, newTag)

	if tagChanged {
		fmt.Printf("升级镜像: %s:%s -> %s:%s\n", cfg.Image.Name, oldTag, cfg.Image.Name, newTag)
	} else {
		fmt.Printf("检查镜像 %s ...\n", imageRef)
	}

	ctx := context.Background()

	if dockerpkg.ImageExists(ctx, imageRef) {
		fmt.Printf("镜像 %s 已存在本地，跳过拉取\n", imageRef)
	} else {
		fmt.Printf("拉取镜像 %s ...\n", imageRef)
		reader, err := dockerpkg.ImagePull(ctx, imageRef)
		if err != nil {
			return fmt.Errorf("拉取镜像失败: %w", err)
		}
		defer reader.Close()
		if _, err := io.Copy(os.Stdout, reader); err != nil {
			return fmt.Errorf("拉取镜像失败: %w", err)
		}
	}

	cfg.Image.Tag = newTag
	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	// 收集需要升级的用户
	var users []string
	if targetUser != "" {
		users = []string{targetUser}
	} else {
		var err error
		users, err = user.GetUserList(cfg)
		if err != nil {
			return err
		}
	}

	// 阶段1: 停止所有容器
	fmt.Println("\n=== 停止容器 ===")
	for _, u := range users {
		fmt.Printf("  [停止] %s ... ", u)
		if err := stopUser(u); err != nil {
			fmt.Printf("失败: %v\n", err)
		} else {
			fmt.Println("完成")
		}
	}

	// 阶段2: 更新 DB 中的镜像并重建容器
	fmt.Println("\n=== 重建容器 ===")
	for _, u := range users {
		rec, err := auth.GetContainerByUsername(u)
		if err != nil || rec == nil {
			fmt.Fprintf(os.Stderr, "  [跳过] %s: 无 DB 记录\n", u)
			continue
		}

		// 更新镜像引用
		rec.Image = imageRef
		if err := auth.UpsertContainer(rec); err != nil {
			fmt.Fprintf(os.Stderr, "  [失败] %s: %v\n", u, err)
			continue
		}

		// 移除旧容器
		if rec.ContainerID != "" {
			_ = dockerpkg.Remove(ctx, rec.ContainerID)
			auth.UpdateContainerID(u, "")
		}

		fmt.Printf("  [更新] %s -> %s\n", u, imageRef)
	}

	// 阶段3: 启动所有容器
	fmt.Println("\n=== 启动容器 ===")
	for _, u := range users {
		fmt.Printf("  [启动] %s ... ", u)
		if err := startUser(cfg, u); err != nil {
			fmt.Printf("失败: %v\n", err)
		} else {
			fmt.Println("完成")
		}
	}

	fmt.Printf("\n升级完成: %s -> %s\n", oldTag, newTag)
	return nil
}
