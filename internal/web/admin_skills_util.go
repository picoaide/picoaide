package web

import (
  "errors"
  "fmt"
  "io"
  "io/fs"
  "os"
  "path/filepath"
  "strings"

  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/skill"
  "github.com/picoaide/picoaide/internal/util"
)

// renameFallback 先尝试原子重命名，跨文件系统时回退到复制+删除
func renameFallback(src, dst string) error {
  if err := os.Rename(src, dst); err == nil {
    return nil
  } else if errors.Is(err, os.ErrExist) {
    return err
  }
  // 跨设备（EXDEV）：改用复制
  if err := util.CopyDir(src, dst); err != nil {
    return err
  }
  return os.RemoveAll(src)
}

// ============================================================
// 技能文件操作工具（单技能仓库）
// ============================================================

func skillReposDir() string {
  if hcfg, err := config.LoadHome(); err == nil && hcfg != nil && hcfg.WorkDir != "" {
    return filepath.Join(hcfg.WorkDir, "skill-repos")
  }
  wd, err := os.Getwd()
  if err != nil {
    return "./skill-repos"
  }
  return filepath.Join(wd, "skill-repos")
}

func cleanPathSegment(value string) (string, error) {
  cleaned := filepath.Base(strings.TrimSpace(value))
  if cleaned != strings.TrimSpace(value) {
    return "", fmt.Errorf("名称不合法")
  }
  if err := util.SafePathSegment(cleaned); err != nil {
    return "", err
  }
  return cleaned, nil
}

func copyDirBetweenRoots(source *os.Root, sourceDir string, target *os.Root, targetDir string) error {
  return fs.WalkDir(source.FS(), sourceDir, func(path string, entry fs.DirEntry, err error) error {
    if err != nil {
      return err
    }
    relPath, err := filepath.Rel(sourceDir, path)
    if err != nil {
      return err
    }
    targetPath := filepath.Join(targetDir, relPath)
    info, err := entry.Info()
    if err != nil {
      return err
    }
    if entry.IsDir() {
      return target.MkdirAll(targetPath, info.Mode())
    }
    in, err := source.Open(path)
    if err != nil {
      return err
    }
    defer in.Close()
    if err := target.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
      return err
    }
    out, err := target.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
    if err != nil {
      return err
    }
    defer out.Close()
    _, err = io.Copy(out, in)
    return err
  })
}

// syncGitRepoToSkill 将单技能仓库同步到 skill/ 目录，返回解析到的元数据
func syncGitRepoToSkill(repoName string) (*skill.Metadata, error) {
  repoDir := filepath.Join(skillReposDir(), repoName)
  skillDir := config.SkillsDirPath()
  if err := os.MkdirAll(skillDir, 0755); err != nil {
    return nil, err
  }

  meta, err := skill.ParseAndValidate(repoDir)
  if err != nil {
    return nil, fmt.Errorf("SKILL.md 校验失败: %w", err)
  }
  skillName := meta.Name

  targetDir := filepath.Join(skillDir, skillName)
  if err := os.RemoveAll(targetDir); err != nil {
    return nil, fmt.Errorf("删除旧技能目录失败: %w", err)
  }

  tempDir, err := os.MkdirTemp(filepath.Dir(skillDir), ".skill-git-*")
  if err != nil {
    return nil, err
  }
  defer os.RemoveAll(tempDir)

  err = filepath.WalkDir(repoDir, func(path string, entry os.DirEntry, walkErr error) error {
    if walkErr != nil {
      return walkErr
    }
    relPath, rErr := filepath.Rel(repoDir, path)
    if rErr != nil {
      return rErr
    }
    if relPath == "." {
      return nil
    }
    relSlash := filepath.ToSlash(relPath)
    if relSlash == ".git" || strings.HasPrefix(relSlash, ".git/") {
      if entry.IsDir() {
        return filepath.SkipDir
      }
      return nil
    }
    if entry.Type()&os.ModeSymlink != 0 {
      return nil
    }
    info, iErr := entry.Info()
    if iErr != nil {
      return iErr
    }
    targetPath := filepath.Join(tempDir, relPath)
    if entry.IsDir() {
      return os.MkdirAll(targetPath, info.Mode())
    }
    if mkErr := os.MkdirAll(filepath.Dir(targetPath), 0755); mkErr != nil {
      return mkErr
    }
    src, sErr := os.Open(path)
    if sErr != nil {
      return sErr
    }
    dst, dErr := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
    if dErr != nil {
      src.Close()
      return dErr
    }
    _, copyErr := io.Copy(dst, src)
    closeErr := dst.Close()
    src.Close()
    if copyErr != nil {
      return copyErr
    }
    return closeErr
  })
  if err != nil {
    return nil, err
  }

  if err := renameFallback(tempDir, targetDir); err != nil {
    return nil, err
  }
  return meta, nil
}
