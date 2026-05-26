package agent

// ============================================================
// 溢出检测（参考 OpenCode overflow.ts 设计）
// ============================================================

const (
  ContextBuffer  = 20000 // 保留 token 数
  MaxContextTokens = 200000 // 默认 context window
)

// IsOverflow 判断是否溢出
func IsOverflow(tokenCount int, contextLimit int) bool {
  if contextLimit <= 0 {
    contextLimit = MaxContextTokens
  }
  usable := contextLimit - ContextBuffer
  return tokenCount >= usable
}

// UsableTokens 计算可用 token 数
func UsableTokens(contextLimit int) int {
  if contextLimit <= 0 {
    contextLimit = MaxContextTokens
  }
  return contextLimit - ContextBuffer
}
