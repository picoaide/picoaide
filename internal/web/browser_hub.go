package web

// BrowserExtra 浏览器服务额外数据
type BrowserExtra struct {
  TabID int
}

// browserSvc 浏览器服务的连接管理器
var browserSvc = NewServiceHub("browser")

// 兼容旧代码引用
var browserHub = browserSvc
