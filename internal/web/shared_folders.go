package web

import (
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
)

// handleSharedFolders 返回当前用户可访问的共享文件夹列表
func (s *Server) handleSharedFolders(c *gin.Context) {
  username := s.requireAuth(c)
  if username == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  folders, err := auth.GetAccessibleSharedFolders(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }

  type userFolderView struct {
    ID            int    `json:"id"`
    Name          string `json:"name"`
    Description   string `json:"description"`
    IsPublic      bool   `json:"is_public"`
    MemberCount   int    `json:"member_count"`
    ContainerPath string `json:"container_path"`
  }

  result := make([]userFolderView, 0, len(folders))
  for _, f := range folders {
    members, _ := auth.GetSharedFolderMembers(f.ID)
    result = append(result, userFolderView{
      ID:            int(f.ID),
      Name:          f.Name,
      Description:   f.Description,
      IsPublic:      f.IsPublic,
      MemberCount:   len(members),
      ContainerPath: "workspace/share/" + f.Name + "/",
    })
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "folders": result,
  })
}
