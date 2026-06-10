package web

import (
  "encoding/json"
  "fmt"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/scheduler"
  "github.com/picoaide/picoaide/internal/store"
)

// picoaideToolDefs PicoAgent 平台工具列表
var picoaideToolDefs = []ToolDef{
  {
    Name:        "picoaide_user_info",
    Description: "获取当前登录用户的信息，包括用户名、角色、来源等",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_skills_list",
    Description: "获取当前用户已安装的技能列表",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_shared_folders",
    Description: "获取当前用户可访问的团队空间共享文件夹列表",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_cron_create",
    Description: "创建定时任务，到指定时间时 AI 会执行预设的提示词并将结果通过所有已启用的通讯渠道通知你。schedule 格式：'cron 分 时 日 月 周' 或 'every 毫秒数' 或 'at unix毫秒时间戳'",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "schedule": map[string]interface{}{"type": "string", "description": "调度表达式，如 'cron 0 8 * * *'（每天早8点）、'every 86400000'（每24小时）、'at 1718000000000'（指定时间）"},
        "prompt":   map[string]interface{}{"type": "string", "description": "定时执行时 AI 要执行的提示词内容"},
      },
      "required": []string{"schedule", "prompt"},
    },
  },
  {
    Name:        "picoaide_cron_list",
    Description: "查看当前用户的所有定时任务列表",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
  {
    Name:        "picoaide_cron_delete",
    Description: "删除指定的定时任务",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "id": map[string]interface{}{"type": "number", "description": "定时任务 ID"},
      },
      "required": []string{"id"},
    },
  },
}

// picoaideHandlers PicoAgent 平台工具的处理函数映射
var picoaideHandlers = map[string]func(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string){
  "picoaide_user_info":      handlePicoaideUserInfo,
  "picoaide_skills_list":    handlePicoaideSkillsList,
  "picoaide_shared_folders": handlePicoaideSharedFolders,
  "picoaide_cron_create":    handlePicoaideCronCreate,
  "picoaide_cron_list":      handlePicoaideCronList,
  "picoaide_cron_delete":    handlePicoaideCronDelete,
}

// picoaideHandleMCPToolCall 分发 PicoAgent 平台工具调用
func picoaideHandleMCPToolCall(s *Server, c *gin.Context, id json.Number, name string, args map[string]interface{}, username string) {
  handler, ok := picoaideHandlers[name]
  if !ok {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "未知工具: " + name},
      },
      "isError": true,
    })
    return
  }
  handler(s, c, id, args, username)
}

// ============================================================
// 工具 Handler 实现
// ============================================================

// handlePicoaideUserInfo 获取当前用户信息
func handlePicoaideUserInfo(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  engine, err := store.GetEngine()
  if err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "数据库连接失败"},
      },
      "isError": true,
    })
    return
  }

  var user store.LocalUser
  has, err := engine.Where("username = ?", username).Get(&user)
  if err != nil || !has {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "查询用户失败"},
      },
      "isError": true,
    })
    return
  }

  text := fmt.Sprintf("用户名: %s\n角色: %s\n来源: %s\n创建时间: %s", user.Username, user.Role, user.Source, user.CreatedAt)
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": text},
    },
  })
}

// handlePicoaideSkillsList 获取用户技能列表
func handlePicoaideSkillsList(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  skills, err := store.GetUserSkills(username)
  if err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "查询技能失败"},
      },
      "isError": true,
    })
    return
  }

  text := "已安装的技能:\n"
  if len(skills) == 0 {
    text = "暂未安装任何技能"
  } else {
    for _, skill := range skills {
      text += "- " + skill + "\n"
    }
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": text},
    },
  })
}

// handlePicoaideSharedFolders 获取可访问的共享文件夹
func handlePicoaideSharedFolders(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  folders, err := store.GetAccessibleSharedFolders(username)
  if err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "查询共享文件夹失败"},
      },
      "isError": true,
    })
    return
  }

  text := "可访问的共享文件夹:\n"
  if len(folders) == 0 {
    text = "暂无共享文件夹"
  } else {
    for _, f := range folders {
      text += fmt.Sprintf("- %s (%s)\n", f.Name, f.Description)
    }
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": text},
    },
  })
}

// ============================================================
// 定时任务工具
// ============================================================

func handlePicoaideCronCreate(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  schedule, _ := args["schedule"].(string)
  prompt, _ := args["prompt"].(string)
  if schedule == "" || prompt == "" {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "schedule 和 prompt 不能为空"},
      },
      "isError": true,
    })
    return
  }

  if s.agentIntegration == nil || s.agentIntegration.cronStore == nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "定时任务服务未就绪"},
      },
      "isError": true,
    })
    return
  }

  job := &scheduler.CronJob{
    UserID:   username,
    Schedule: schedule,
    Prompt:   prompt,
    AgentID:  "pico",
    Enabled:  true,
  }

  if err := s.agentIntegration.cronStore.Insert(c.Request.Context(), job); err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": fmt.Sprintf("创建定时任务失败: %s", err.Error())},
      },
      "isError": true,
    })
    return
  }

  desc, _ := scheduler.ParseSchedule(schedule)
  text := fmt.Sprintf("已创建定时任务 #%d\n提示词: %s\n调度: %s\n下次执行: %s", job.ID, prompt, desc, job.NextRunAt)
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": text},
    },
  })
}

func handlePicoaideCronList(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  if s.agentIntegration == nil || s.agentIntegration.cronStore == nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "定时任务服务未就绪"},
      },
      "isError": true,
    })
    return
  }

  jobs, err := s.agentIntegration.cronStore.ListByUser(c.Request.Context(), username)
  if err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "查询失败"},
      },
      "isError": true,
    })
    return
  }

  if len(jobs) == 0 {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "暂无定时任务"},
      },
    })
    return
  }

  var text string
  for _, job := range jobs {
    status := "启用"
    if !job.Enabled {
      status = "禁用"
    }
    desc, _ := scheduler.ParseSchedule(job.Schedule)
    text += fmt.Sprintf("#%d [%s] %s\n  调度: %s\n  下次执行: %s\n", job.ID, status, job.Prompt, desc, job.NextRunAt)
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": text},
    },
  })
}

func handlePicoaideCronDelete(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  idFloat, ok := args["id"].(float64)
  if !ok {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "id 必须是数字"},
      },
      "isError": true,
    })
    return
  }

  jobID := int64(idFloat)
  if s.agentIntegration == nil || s.agentIntegration.cronStore == nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "定时任务服务未就绪"},
      },
      "isError": true,
    })
    return
  }

  existing, err := s.agentIntegration.cronStore.GetByID(c.Request.Context(), jobID)
  if err != nil || existing == nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "任务不存在"},
      },
      "isError": true,
    })
    return
  }
  if existing.UserID != username {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "无权删除该任务"},
      },
      "isError": true,
    })
    return
  }

  if err := s.agentIntegration.cronStore.Delete(c.Request.Context(), jobID); err != nil {
    writeMCPResult(c.Writer, id, map[string]interface{}{
      "content": []map[string]interface{}{
        {"type": "text", "text": "删除失败"},
      },
      "isError": true,
    })
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("定时任务 #%d 已删除", jobID)},
    },
  })
}
