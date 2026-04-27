package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/config"
	"github.com/picoaide/picoaide/internal/ldap"
	"github.com/picoaide/picoaide/internal/user"
)

func Sync(cfg *config.GlobalConfig) error {
	whitelist, err := user.LoadWhitelist()
	if err != nil {
		return fmt.Errorf("加载白名单失败: %w", err)
	}

	ldapUsers, err := ldap.FetchUsers(cfg)
	if err != nil {
		return fmt.Errorf("获取 LDAP 用户失败: %w", err)
	}

	if whitelist != nil {
		var filtered []string
		for _, u := range ldapUsers {
			if user.IsWhitelisted(whitelist, u) {
				filtered = append(filtered, u)
			}
		}
		fmt.Printf("白名单过滤: %d -> %d 个用户\n", len(ldapUsers), len(filtered))
		ldapUsers = filtered
	}

	ldapSet := make(map[string]bool)
	for _, u := range ldapUsers {
		ldapSet[u] = true
	}

	localUsers, err := user.GetUserList(cfg)
	if err != nil {
		return fmt.Errorf("获取本地用户列表失败: %w", err)
	}
	localSet := make(map[string]bool)
	for _, u := range localUsers {
		localSet[u] = true
	}

	archivedUsers, err := user.GetArchivedUsers(cfg)
	if err != nil {
		return fmt.Errorf("获取归档用户列表失败: %w", err)
	}
	archivedSet := make(map[string]bool)
	for _, u := range archivedUsers {
		archivedSet[u] = true
	}

	var newUsers, restoredUsers, removedUsers, existingUsers []string

	for _, u := range ldapUsers {
		if localSet[u] {
			existingUsers = append(existingUsers, u)
		} else if archivedSet[u] {
			restoredUsers = append(restoredUsers, u)
		} else {
			newUsers = append(newUsers, u)
		}
	}
	for _, u := range localUsers {
		if !ldapSet[u] {
			removedUsers = append(removedUsers, u)
		}
	}

	fmt.Println("=== 同步报告 ===")
	fmt.Printf("LDAP 用户: %d | 本地用户: %d | 归档用户: %d\n", len(ldapUsers), len(localUsers), len(archivedUsers))
	fmt.Printf("新增: %d | 重新入职: %d | 离职: %d | 不变: %d\n", len(newUsers), len(restoredUsers), len(removedUsers), len(existingUsers))

	if len(restoredUsers) > 0 {
		fmt.Println("\n--- 重新入职 ---")
		for _, u := range restoredUsers {
			if err := user.RestoreUser(cfg, u); err != nil {
				fmt.Fprintf(os.Stderr, "恢复 %s 失败: %v\n", u, err)
				continue
			}
			if err := user.InitUser(cfg, u); err != nil {
				fmt.Fprintf(os.Stderr, "初始化 %s 失败: %v\n", u, err)
				continue
			}
			if err := startUser(cfg, u); err != nil {
				fmt.Fprintf(os.Stderr, "启动 %s 失败: %v\n", u, err)
			}
		}
	}

	if len(newUsers) > 0 {
		fmt.Println("\n--- 新增用户 ---")
		for _, u := range newUsers {
			if err := user.InitUser(cfg, u); err != nil {
				fmt.Fprintf(os.Stderr, "初始化 %s 失败: %v\n", u, err)
				continue
			}
			if err := startUser(cfg, u); err != nil {
				fmt.Fprintf(os.Stderr, "启动 %s 失败: %v\n", u, err)
			}
		}
	}

	if len(removedUsers) > 0 {
		fmt.Println("\n--- 离职用户 ---")
		for _, u := range removedUsers {
			fmt.Printf("  [停止] %s ... ", u)
			if err := downUser(u); err != nil {
				fmt.Printf("停止失败: %v\n", err)
			} else {
				fmt.Println("完成")
			}
			if err := user.ArchiveUser(cfg, u); err != nil {
				fmt.Fprintf(os.Stderr, "归档 %s 失败: %v\n", u, err)
			}
			auth.DeleteContainer(u)
		}
	}

	needApplyUsers := append(newUsers, restoredUsers...)
	if len(needApplyUsers) > 0 {
		fmt.Printf("\n--- 等待容器初始化（30 秒）---\n")
		time.Sleep(30 * time.Second)

		fmt.Println("\n--- 应用全局配置 ---")
		for _, u := range needApplyUsers {
			picoclawDir := filepath.Join(user.UserDir(cfg, u), "root", ".picoclaw")
			configJSON := filepath.Join(picoclawDir, "config.json")
			securityYAML := filepath.Join(picoclawDir, ".security.yml")

			if _, err := os.Stat(configJSON); err != nil {
				fmt.Fprintf(os.Stderr, "  [跳过] %s: config.json 尚未生成\n", u)
				continue
			}

			if err := user.ApplyConfigToJSON(cfg, picoclawDir); err != nil {
				fmt.Fprintf(os.Stderr, "  [失败] %s: %v\n", u, err)
				continue
			}
			if _, err := os.Stat(securityYAML); err == nil {
				if err := user.ApplySecurityToYAML(cfg, picoclawDir); err != nil {
					fmt.Fprintf(os.Stderr, "  [失败] %s: %v\n", u, err)
					continue
				}
			}
			fmt.Printf("  [配置] %s\n", u)
		}

		fmt.Println("\n--- 重启已配置用户 ---")
		for _, u := range needApplyUsers {
			ud := user.UserDir(cfg, u)
			if _, err := os.Stat(filepath.Join(ud, "root", ".picoclaw", "config.json")); err != nil {
				continue
			}
			fmt.Printf("  [重启] %s ... ", u)
			if err := restartUser(cfg, u); err != nil {
				fmt.Printf("失败: %v\n", err)
			} else {
				fmt.Println("完成")
			}
		}
	}

	fmt.Println("\n=== 同步完成 ===")
	return nil
}
