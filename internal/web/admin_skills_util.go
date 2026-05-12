package web

import (
  "archive/zip"
  "fmt"
  "io"
  "io/fs"
  "os"
  "path/filepath"
  "strings"

  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 技能文件操作工具
// ============================================================

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

func copySkillZipContents(reader *zip.Reader, skillName string) error {
  skillDir := config.SkillsDirPath()
  if err := os.MkdirAll(skillDir, 0755); err != nil {
    return err
  }
  targetDir := filepath.Join(skillDir, skillName)
  if err := os.RemoveAll(targetDir); err != nil {
    return err
  }

  tempDir, err := os.MkdirTemp(filepath.Dir(skillDir), ".skill-upload-*")
  if err != nil {
    return err
  }
  defer os.RemoveAll(tempDir)
  tempRoot, err := os.OpenRoot(tempDir)
  if err != nil {
    return err
  }
  defer tempRoot.Close()

  prefix := commonZipRootDir(reader.File)
  for _, file := range reader.File {
    name := strings.TrimPrefix(filepath.ToSlash(file.Name), "/")
    if name == "" || strings.HasSuffix(name, "/") {
      continue
    }
    if prefix != "" {
      name = strings.TrimPrefix(name, prefix+"/")
    }
    if name == "" {
      continue
    }
    cleanName := filepath.Clean(filepath.FromSlash(name))
    if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) || filepath.IsAbs(cleanName) {
      return fmt.Errorf("zip 包包含不安全路径: %s", file.Name)
    }
    if file.FileInfo().IsDir() {
      if err := tempRoot.MkdirAll(cleanName, file.Mode()); err != nil {
        return err
      }
      continue
    }
    if err := tempRoot.MkdirAll(filepath.Dir(cleanName), 0755); err != nil {
      return err
    }
    src, err := file.Open()
    if err != nil {
      return err
    }
    dst, err := tempRoot.OpenFile(cleanName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
    if err != nil {
      src.Close()
      return err
    }
    _, copyErr := io.Copy(dst, src)
    closeErr := dst.Close()
    src.Close()
    if copyErr != nil {
      return copyErr
    }
    if closeErr != nil {
      return closeErr
    }
  }
  if err := os.Rename(tempDir, targetDir); err != nil {
    return err
  }
  return nil
}

func commonZipRootDir(files []*zip.File) string {
  root := ""
  for _, file := range files {
    name := strings.Trim(filepath.ToSlash(file.Name), "/")
    if name == "" {
      continue
    }
    first := strings.SplitN(name, "/", 2)[0]
    if root == "" {
      root = first
      continue
    }
    if root != first {
      return ""
    }
  }
  return root
}

func syncGitRepoToSkill(repoName string) error {
  repoDir := filepath.Join(skillReposDir(), repoName)
  skillDir := config.SkillsDirPath()
  if err := os.MkdirAll(skillDir, 0755); err != nil {
    return err
  }
  targetDir := filepath.Join(skillDir, repoName)
  if err := os.RemoveAll(targetDir); err != nil {
    return err
  }

  tempDir, err := os.MkdirTemp(filepath.Dir(skillDir), ".skill-git-*")
  if err != nil {
    return err
  }
  defer os.RemoveAll(tempDir)

  err = filepath.WalkDir(repoDir, func(path string, entry os.DirEntry, err error) error {
    if err != nil {
      return err
    }
    relPath, err := filepath.Rel(repoDir, path)
    if err != nil {
      return err
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
    info, err := entry.Info()
    if err != nil {
      return err
    }
    if info.Mode()&os.ModeSymlink != 0 {
      return nil
    }
    targetPath := filepath.Join(tempDir, relPath)
    if entry.IsDir() {
      return os.MkdirAll(targetPath, info.Mode())
    }
    if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
      return err
    }
    src, err := os.Open(path)
    if err != nil {
      return err
    }
    dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
    if err != nil {
      src.Close()
      return err
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
    return err
  }
  return os.Rename(tempDir, targetDir)
}
