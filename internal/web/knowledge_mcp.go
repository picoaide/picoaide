package web

import (
  "encoding/json"
  "fmt"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/store"
)

func init() {
  picoaideHandlers["kb_search"] = handleKBSearch
}

func handleKBSearch(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  scope, _ := args["scope"].(string)
  switch scope {
  case "search":
    query, _ := args["query"].(string)
    if query == "" {
      writeMCPResult(c.Writer, id, map[string]interface{}{
        "content": []map[string]interface{}{{"type": "text", "text": "query is required for search scope"}},
        "isError": true,
      })
      return
    }
    page := 1
    pageSize := 10
    if p, ok := args["page"].(float64); ok && p > 0 {
      page = int(p)
    }
    if ps, ok := args["page_size"].(float64); ok && ps > 0 {
      pageSize = int(ps)
    }

    results, total, err := store.SearchKB(username, query, page, pageSize)
    if err != nil {
      writeMCPResult(c.Writer, id, map[string]interface{}{
        "content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("search failed: %v", err)}},
        "isError": true,
      })
      return
    }
    store.CreateAuditLog(username, "search", fmt.Sprintf(`{"q":"%s","total":%d}`, query, total), "picoagent")
    data, _ := json.Marshal(map[string]interface{}{
      "results": results, "total": total, "page": page,
    })
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{{"type": "text", "text": string(data)}},
    })

  case "read":
    docID, _ := args["doc_id"].(float64)
    if docID == 0 {
      writeMCPResult(c.Writer, id, map[string]interface{}{
        "content": []map[string]interface{}{{"type": "text", "text": "doc_id is required for read scope"}},
        "isError": true,
      })
      return
    }
    maxLen := 4000
    if ml, ok := args["max_length"].(float64); ok && ml > 0 {
      maxLen = int(ml)
    }

    doc, err := store.GetDocumentByID(username, int64(docID))
    if err != nil {
      writeMCPResult(c.Writer, id, map[string]interface{}{
        "content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("read failed: %v", err)}},
        "isError": true,
      })
      return
    }
    if doc == nil {
      writeMCPResult(c.Writer, id, map[string]interface{}{
        "content": []map[string]interface{}{{"type": "text", "text": "document not found"}},
        "isError": true,
      })
      return
    }
    truncated := len(doc.Content) > maxLen
    if truncated {
      doc.Content = doc.Content[:maxLen]
    }
    links, _ := store.GetDocumentLinks(doc.ID)
    backlinks, _ := store.GetDocumentBacklinks(doc.ID)
    tags, _ := store.GetDocumentTags(doc.ID)
    store.CreateAuditLog(username, "read", fmt.Sprintf(`{"doc_id":%d}`, int64(docID)), "picoagent")
    data, _ := json.Marshal(map[string]interface{}{
      "title": doc.Title, "content": doc.Content, "truncated": truncated,
      "tags": tags, "links": links, "backlinks": backlinks,
    })
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{{"type": "text", "text": string(data)}},
    })

  case "browse":
    var folderID int64
    if fid, ok := args["folder_id"].(float64); ok && fid > 0 {
      folderID = int64(fid)
    }
    if folderID == 0 {
      ids, err := store.GetAccessibleFolderIDs(username)
      if err == nil && len(ids) > 0 {
        folderID = ids[0]
      }
      if folderID == 0 {
        writeMCPResult(c.Writer, id, map[string]interface{}{
          "content": []map[string]interface{}{{"type": "text", "text": "没有可访问的文件夹"}},
          "isError": true,
        })
        return
      }
    }
    folders, docs, err := store.BrowseFolder(username, folderID)
    if err != nil {
      writeMCPResult(c.Writer, id, map[string]interface{}{
        "content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("browse failed: %v", err)}},
        "isError": true,
      })
      return
    }
    data, _ := json.Marshal(map[string]interface{}{
      "folders": folders, "docs": docs,
    })
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{{"type": "text", "text": string(data)}},
    })

  default:
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("invalid scope: %s", scope)}},
      "isError": true,
    })
  }
}
