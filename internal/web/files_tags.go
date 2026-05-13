package web

// picoclawWorkspaceTag 根据 Picoclaw 官方工作区规范返回用途标记。
// 参考: https://github.com/sipeed/picoclaw/blob/main/docs/guides/configuration.md
func picoclawWorkspaceTag(name string, isDir bool) string {
  if isDir {
    switch name {
    case "sessions":
      return "会话"
    case "memory":
      return "记忆"
    case "state":
      return "状态"
    case "cron":
      return "定时"
    case "skills":
      return "技能"
    }
    return ""
  }
  switch name {
  case "AGENT.md":
    return "行为"
  case "HEARTBEAT.md":
    return "心跳"
  case "IDENTITY.md":
    return "身份"
  case "SOUL.md":
    return "灵魂"
  case "USER.md":
    return "偏好"
  }
  return ""
}
