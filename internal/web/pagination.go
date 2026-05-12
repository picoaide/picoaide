package web

import (
  "strconv"
  "strings"

  "github.com/gin-gonic/gin"
)

type paginationQuery struct {
  Page     int
  PageSize int
  Search   string
  Enabled  bool
}

func parsePagination(c *gin.Context, defaultPageSize, maxPageSize int) paginationQuery {
  p := paginationQuery{
    Page:     1,
    PageSize: 0,
    Search:   strings.ToLower(strings.TrimSpace(c.Query("search"))),
  }
  if raw := strings.TrimSpace(c.Query("page")); raw != "" {
    if n, err := strconv.Atoi(raw); err == nil && n > 0 {
      p.Page = n
      p.Enabled = true
    }
  }
  if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
    if n, err := strconv.Atoi(raw); err == nil && n > 0 {
      p.PageSize = n
      p.Enabled = true
    }
  }
  if p.Search != "" {
    p.Enabled = true
  }
  if p.Enabled {
    if p.PageSize <= 0 {
      p.PageSize = defaultPageSize
    }
    if p.PageSize <= 0 {
      p.PageSize = 20
    }
    if maxPageSize <= 0 {
      maxPageSize = 100
    }
    if p.PageSize > maxPageSize {
      p.PageSize = maxPageSize
    }
  }
  return p
}

func paginateSlice[T any](items []T, p paginationQuery) ([]T, int, int, int, int) {
  total := len(items)
  if !p.Enabled {
    totalPages := 1
    if total == 0 {
      totalPages = 1
    }
    return items, total, totalPages, 1, 0
  }
  totalPages := (total + p.PageSize - 1) / p.PageSize
  if totalPages < 1 {
    totalPages = 1
  }
  page := p.Page
  if page > totalPages {
    page = totalPages
  }
  start := (page - 1) * p.PageSize
  if start > total {
    start = total
  }
  end := start + p.PageSize
  if end > total {
    end = total
  }
  return append([]T(nil), items[start:end]...), total, totalPages, page, p.PageSize
}
