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
  if newTag == "" {
    return fmt.Errorf("请使用 -tag 指定要升级的版本标签")
  }

  pullRef := cfg.Image.PullRef(newTag)
  unifiedRef := cfg.Image.UnifiedRef(newTag)
  fmt.Printf("升级镜像: %s\n", pullRef)
  if cfg.Image.IsTencent() && pullRef != unifiedRef {
    fmt.Printf("（腾讯云模式，拉取后重命名为 %s）\n", unifiedRef)
  }

  ctx := context.Background()

  if dockerpkg.ImageExists(ctx, unifiedRef) {
    fmt.Printf("镜像 %s 已存在本地，跳过拉取\n", unifiedRef)
  } else {
    fmt.Printf("拉取镜像 %s ...\n", pullRef)
    reader, err := dockerpkg.ImagePull(ctx, pullRef)
    if err != nil {
      return fmt.Errorf("拉取镜像失败: %w", err)
    }
    defer reader.Close()
    if _, err := io.Copy(os.Stdout, reader); err != nil {
      return fmt.Errorf("拉取镜像失败: %w", err)
    }

    // 腾讯云模式：retag 为统一名称
    if cfg.Image.IsTencent() && pullRef != unifiedRef {
      fmt.Printf("重命名镜像: %s -> %s\n", pullRef, unifiedRef)
      if err := dockerpkg.RetagImage(ctx, pullRef, unifiedRef); err != nil {
        return fmt.Errorf("重命名镜像失败: %w", err)
      }
    }
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
    rec.Image = unifiedRef
    if err := auth.UpsertContainer(rec); err != nil {
      fmt.Fprintf(os.Stderr, "  [失败] %s: %v\n", u, err)
      continue
    }

    // 移除旧容器
    if rec.ContainerID != "" {
      _ = dockerpkg.Remove(ctx, rec.ContainerID)
      auth.UpdateContainerID(u, "")
    }

    fmt.Printf("  [更新] %s -> %s\n", u, unifiedRef)
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

  fmt.Printf("\n升级完成: %s\n", unifiedRef)
  return nil
}
