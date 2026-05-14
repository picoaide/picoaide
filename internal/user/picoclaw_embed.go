package user

import (
  "embed"
  "io"
  "io/fs"
  "os"
  "path/filepath"
)

//go:embed all:picoclaw_rules
var picoclawRulesEmbed embed.FS

const picoclawRulesEmbedDir = "picoclaw_rules"

func releasePicoClawAdapterFromEmbed(dstRoot string) error {
  if err := os.MkdirAll(dstRoot, 0755); err != nil {
    return err
  }
  return fs.WalkDir(picoclawRulesEmbed, picoclawRulesEmbedDir, func(embedPath string, entry fs.DirEntry, err error) error {
    if err != nil {
      return err
    }
    if embedPath == picoclawRulesEmbedDir {
      return nil
    }
    rel, err := filepath.Rel(picoclawRulesEmbedDir, embedPath)
    if err != nil {
      return err
    }
    dst := filepath.Join(dstRoot, filepath.FromSlash(rel))
    if entry.IsDir() {
      return os.MkdirAll(dst, 0755)
    }
    src, err := picoclawRulesEmbed.Open(embedPath)
    if err != nil {
      return err
    }
    defer src.Close()
    data, err := io.ReadAll(src)
    if err != nil {
      return err
    }
    return os.WriteFile(dst, data, 0644)
  })
}

func picoclawAdapterEmbedExists() bool {
  entries, err := fs.ReadDir(picoclawRulesEmbed, picoclawRulesEmbedDir)
  return err == nil && len(entries) > 0
}
