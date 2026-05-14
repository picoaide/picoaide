package skill

import (
  "archive/zip"
  "crypto/sha256"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "net/url"
  "os"
  "path/filepath"
  "strings"
  "time"

  "github.com/picoaide/picoaide/internal/util"
)

// ============================================================
// 注册源客户端 — SkillHub 兼容协议
// ============================================================

// RegistrySkill 注册源中的技能条目
type RegistrySkill struct {
  Slug        string   `json:"slug"`
  Name        string   `json:"name"`
  Description string   `json:"description"`
  Summary     string   `json:"summary,omitempty"`
  Version     string   `json:"version"`
  Categories  []string `json:"categories,omitempty"`
  Downloads   int      `json:"downloads,omitempty"`
  Stars       int      `json:"stars,omitempty"`
  Score       int      `json:"score,omitempty"`
  Rank        int      `json:"rank,omitempty"`
  Homepage    string   `json:"homepage,omitempty"`
  Sha256      string   `json:"sha256,omitempty"`
}

// IndexResponse skills.json 响应
type IndexResponse struct {
  Total  int             `json:"total"`
  Skills []RegistrySkill `json:"skills"`
}

// SearchResponse 搜索 API 响应
type SearchResponse struct {
  Results []SearchResult `json:"results"`
}

type SearchResult struct {
  Slug        string `json:"slug"`
  Name        string `json:"name"`
  DisplayName string `json:"displayName"`
  Summary     string `json:"summary"`
  Description string `json:"description"`
  Version     string `json:"version"`
}

// FetchIndex 从 indexURL 拉取并解析 skills.json
func FetchIndex(indexURL string) (*IndexResponse, error) {
  client := &http.Client{Timeout: 20 * time.Second}
  req, err := http.NewRequest("GET", indexURL, nil)
  if err != nil {
    return nil, fmt.Errorf("创建请求失败: %w", err)
  }
  req.Header.Set("User-Agent", "picoaide/1.0")
  req.Header.Set("Accept", "application/json")

  resp, err := client.Do(req)
  if err != nil {
    return nil, fmt.Errorf("请求索引失败: %w", err)
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    return nil, fmt.Errorf("索引返回 HTTP %d", resp.StatusCode)
  }

  body, err := io.ReadAll(resp.Body)
  if err != nil {
    return nil, fmt.Errorf("读取响应失败: %w", err)
  }

  var index IndexResponse
  // 兼容数组格式
  if err := json.Unmarshal(body, &index); err != nil {
    var skills []RegistrySkill
    if err2 := json.Unmarshal(body, &skills); err2 != nil {
      return nil, fmt.Errorf("解析索引失败: %w", err)
    }
    index.Skills = skills
    index.Total = len(skills)
  }
  return &index, nil
}

// SearchRegistry 调用远程搜索 API
func SearchRegistry(searchURL, query string, limit int) ([]RegistrySkill, error) {
  if searchURL == "" || query == "" {
    return nil, nil
  }
  u, err := url.Parse(searchURL)
  if err != nil {
    return nil, err
  }
  q := u.Query()
  q.Set("q", query)
  if limit > 0 {
    q.Set("limit", fmt.Sprintf("%d", limit))
  }
  u.RawQuery = q.Encode()

  client := &http.Client{Timeout: 15 * time.Second}
  req, err := http.NewRequest("GET", u.String(), nil)
  if err != nil {
    return nil, err
  }
  req.Header.Set("User-Agent", "picoaide/1.0")
  req.Header.Set("Accept", "application/json")

  resp, err := client.Do(req)
  if err != nil {
    return nil, err
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    return nil, fmt.Errorf("搜索返回 HTTP %d", resp.StatusCode)
  }

  body, err := io.ReadAll(resp.Body)
  if err != nil {
    return nil, err
  }

  var sr SearchResponse
  if err := json.Unmarshal(body, &sr); err != nil {
    return nil, err
  }

  var skills []RegistrySkill
  for _, r := range sr.Results {
    name := r.Name
    if name == "" {
      name = r.Slug
    }
    desc := r.Description
    if desc == "" {
      desc = r.Summary
    }
    skills = append(skills, RegistrySkill{
      Slug:        r.Slug,
      Name:        name,
      Description: desc,
      Summary:     r.Summary,
      Version:     r.Version,
    })
  }
  return skills, nil
}

// DownloadAndInstall 从注册源下载 ZIP 并安装到 skills/<source>/<slug>/
// primaryURL 优先尝试，失败后 fallback 到 fallbackTemplate（{slug} 会被替换）
func DownloadAndInstall(source, slug, primaryURL, fallbackTemplate, expectedSha256 string) error {
  if err := util.SafePathSegment(source); err != nil {
    return fmt.Errorf("源名称不合法: %w", err)
  }
  if err := util.SafePathSegment(slug); err != nil {
    return fmt.Errorf("技能名不合法: %w", err)
  }
  targetDir := filepath.Join(SkillsRootDir(), source, slug)
  if _, err := os.Stat(targetDir); err == nil {
    if err := os.RemoveAll(targetDir); err != nil {
      return fmt.Errorf("清理旧目录失败: %w", err)
    }
  }

  var zipData []byte

  if primaryURL != "" {
    downloadURL := strings.ReplaceAll(primaryURL, "{slug}", url.QueryEscape(slug))
    data, err := downloadFile(downloadURL)
    if err == nil {
      zipData = data
    }
  }

  if zipData == nil && fallbackTemplate != "" {
    fallbackURL := strings.ReplaceAll(fallbackTemplate, "{slug}", url.PathEscape(slug))
    data, err := downloadFile(fallbackURL)
    if err == nil {
      zipData = data
    }
  }

  if zipData == nil {
    return fmt.Errorf("所有下载通道均失败（slug: %s）", slug)
  }

  if expectedSha256 != "" {
    actual := fmt.Sprintf("%x", sha256.Sum256(zipData))
    if actual != strings.ToLower(expectedSha256) {
      return fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", expectedSha256, actual)
    }
  }

  if err := extractZipToDir(zipData, targetDir); err != nil {
    return fmt.Errorf("解压失败: %w", err)
  }

  skmdPath := filepath.Join(targetDir, "SKILL.md")
  if _, err := os.Stat(skmdPath); err != nil {
    os.RemoveAll(targetDir)
    return fmt.Errorf("技能包缺少 SKILL.md")
  }

  return nil
}

func downloadFile(downloadURL string) ([]byte, error) {
  parsed, err := url.Parse(downloadURL)
  if err != nil {
    return nil, fmt.Errorf("无效下载 URL: %w", err)
  }
  if parsed.Scheme != "https" && parsed.Scheme != "http" {
    return nil, fmt.Errorf("不支持的 URL 协议: %s", parsed.Scheme)
  }

  client := &http.Client{Timeout: 60 * time.Second}
  req, err := http.NewRequest("GET", downloadURL, nil)
  if err != nil {
    return nil, err
  }
  req.Header.Set("User-Agent", "picoaide/1.0")

  resp, err := client.Do(req)
  if err != nil {
    return nil, err
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
  }

  return io.ReadAll(resp.Body)
}

func extractZipToDir(data []byte, targetDir string) error {
  r := strings.NewReader(string(data))
  reader, err := zip.NewReader(r, int64(len(data)))
  if err != nil {
    return err
  }

  for _, f := range reader.File {
    fpath := filepath.Join(targetDir, f.Name)
    if !strings.HasPrefix(filepath.Clean(fpath), filepath.Clean(targetDir)+string(os.PathSeparator)) {
      return fmt.Errorf("压缩包包含非法路径: %s", f.Name)
    }

    if f.FileInfo().IsDir() {
      os.MkdirAll(fpath, 0755)
      continue
    }

    os.MkdirAll(filepath.Dir(fpath), 0755)
    rc, err := f.Open()
    if err != nil {
      return err
    }

    out, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
    if err != nil {
      rc.Close()
      return err
    }

    io.Copy(out, rc)
    out.Close()
    rc.Close()
  }

  return nil
}
