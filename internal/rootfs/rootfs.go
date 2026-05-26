package rootfs

import (
  "archive/tar"
  "compress/gzip"
  "embed"
  "fmt"
  "io"
  "os"
  "path/filepath"
  "strings"
)

//go:embed bundle
var bundle embed.FS

// Ensure 确保 rootfs 存在，每次启动重新解压 Alpine 并覆盖二进制
func Ensure(rootfsDir string) error {
  // 删除旧的 rootfs 目录
  if _, err := os.Stat(rootfsDir); err == nil {
    os.RemoveAll(rootfsDir)
  }

  // 从嵌入的 bundle 中提取 Alpine mini rootfs
  if err := extractAlpine(rootfsDir); err != nil {
    return fmt.Errorf("提取 Alpine rootfs 失败: %w", err)
  }

  // 覆盖 picoagent
  if data, err := bundle.ReadFile("bundle/picoagent"); err == nil {
    os.WriteFile(filepath.Join(rootfsDir, "bin", "picoagent"), data, 0755)
  }

  // DNS
  os.MkdirAll(filepath.Join(rootfsDir, "etc"), 0755)
  os.WriteFile(filepath.Join(rootfsDir, "etc", "resolv.conf"),
    []byte("nameserver 8.8.8.8\nnameserver 1.1.1.1\n"), 0644)

  return nil
}

// extractAlpine 从嵌入的 bundle 中解压 Alpine mini rootfs tarball
func extractAlpine(rootfsDir string) error {
  f, err := bundle.Open("bundle/alpine-rootfs.tar.gz")
  if err != nil {
    return err
  }
  defer f.Close()

  gr, err := gzip.NewReader(f)
  if err != nil {
    return err
  }
  defer gr.Close()

  tr := tar.NewReader(gr)
  for {
    header, err := tr.Next()
    if err == io.EOF {
      break
    }
    if err != nil {
      return err
    }

    target := filepath.Join(rootfsDir, header.Name)
    if !strings.HasPrefix(target, rootfsDir+string(os.PathSeparator)) {
      continue
    }

    switch header.Typeflag {
    case tar.TypeDir:
      os.MkdirAll(target, os.FileMode(header.Mode))
    case tar.TypeReg:
      os.MkdirAll(filepath.Dir(target), 0755)
      out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
      if err != nil {
        continue
      }
      if _, err := io.Copy(out, tr); err != nil {
        out.Close()
        continue
      }
      if err := out.Close(); err != nil {
        return err
      }
    case tar.TypeSymlink:
      linkTarget := header.Linkname
      resolved := filepath.Clean(filepath.Join(filepath.Dir(target), linkTarget))
      if !strings.HasPrefix(resolved, rootfsDir+string(os.PathSeparator)) {
        continue
      }
      os.MkdirAll(filepath.Dir(target), 0755)
      os.Symlink(linkTarget, target)
    }
  }
  return nil
}
