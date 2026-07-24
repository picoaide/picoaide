<template>
  <div class="chat-page">
    <div class="chat-container">
      <!-- 侧边栏：历史对话列表 -->
      <div class="chat-sidebar">
        <a-button type="primary" block @click="createNewChat" style="margin-bottom: 12px">
          <template #icon><PlusOutlined /></template>
          新对话
        </a-button>
        <div class="chat-list">
          <div
            v-for="chat in chatList"
            :key="chat.id"
            class="chat-item"
            :class="{ active: currentChatId === chat.id }"
            @click="switchChat(chat.id)"
          >
            <span class="chat-title">{{ chat.title || '新对话' }}</span>
            <a-button
              type="text"
              size="small"
              class="delete-btn"
              @click.stop="deleteChat(chat.id)"
            >
              <DeleteOutlined />
            </a-button>
          </div>
        </div>
      </div>

      <!-- 主界面：聊天区域 -->
      <div class="chat-main">
        <div class="chat-header">
          <h3>{{ currentChat?.title || 'AI 对话' }}</h3>
        </div>

        <!-- 消息展示区 -->
        <div class="chat-messages" ref="messagesRef">
          <div
            v-for="msg in currentChat?.messages || []"
            :key="msg.id"
            class="message"
            :class="msg.role"
          >
            <div class="message-avatar">
              <a-avatar v-if="msg.role === 'user'" style="background: #1890ff">
                {{ username.charAt(0).toUpperCase() }}
              </a-avatar>
              <a-avatar v-else style="background: #52c41a">AI</a-avatar>
            </div>
            <div class="message-content">
              <div class="message-meta">
                {{ msg.role === 'user' ? '你' : 'AI 助手' }}
                <span class="message-time">{{ formatTime(msg.timestamp) }}</span>
              </div>
              <div class="message-body">
                <!-- AI 刚建立连接但尚未返回任何文本时，显示加载动画 -->
                <div v-if="!msg.content && msg.role === 'assistant'" class="typing-indicator">
                  <span></span><span></span><span></span>
                </div>
                <!-- 收到数据后实时逐字渲染 Markdown -->
                <div v-else class="markdown-body" v-html="renderMarkdown(msg.content)"></div>
              </div>
            </div>
          </div>
        </div>

        <!-- 底部输入框区域 -->
        <div class="chat-input-area">
          <a-textarea
            v-model:value="inputMessage"
            placeholder="输入消息... (Shift+Enter 换行)"
            :auto-size="{ minRows: 1, maxRows: 4 }"
            :disabled="isStreaming"
            @keydown="handleKeyDown"
          />
          <a-button
            v-if="!isStreaming"
            type="primary"
            :loading="isSending"
            @click="sendMessage"
            :disabled="!inputMessage.trim()"
          >
            发送
          </a-button>
          <a-button
            v-else
            danger
            @click="stopStream"
          >
            停止
          </a-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { api } from '../../composables/api'
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

// Markdown 解析器初始化
const md = new MarkdownIt({ html: false, linkify: true, breaks: true })
const renderMarkdown = (content: string) => DOMPurify.sanitize(md.render(content || ''))

// 基础状态定义
const username = ref(localStorage.getItem('username') || 'User')
const inputMessage = ref('')
const isSending = ref(false)
const isStreaming = ref(false)
const messagesRef = ref<HTMLElement>()

let evtSource: EventSource | null = null

interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  timestamp: number
}

interface Chat {
  id: string
  title: string
  messages: Message[]
}

const chatList = ref<Chat[]>([])
const currentChatId = ref<string>('')
const currentChat = computed(() => chatList.value.find(c => c.id === currentChatId.value))

// 新增对话
const createNewChat = () => {
  const id = Date.now().toString()
  chatList.value.unshift({ id, title: '', messages: [] })
  currentChatId.value = id
}

// 切换对话
const switchChat = (id: string) => {
  currentChatId.value = id
}

// 删除对话
const deleteChat = (id: string) => {
  const idx = chatList.value.findIndex(c => c.id === id)
  if (idx > -1) {
    chatList.value.splice(idx, 1)
    if (currentChatId.value === id) {
      currentChatId.value = chatList.value[0]?.id || ''
    }
  }
}

// 时间格式化
const formatTime = (ts: number) =>
  new Date(ts).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })

// 自动滚动到底部
const scrollToBottom = () => {
  nextTick(() => {
    if (messagesRef.value) {
      messagesRef.value.scrollTop = messagesRef.value.scrollHeight
    }
  })
}

// 快捷键发送 (Enter 发送，Shift+Enter 换行)
const handleKeyDown = (e: KeyboardEvent) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendMessage()
  }
}

// 加载历史对话
const loadHistory = async () => {
  try {
    const res = await fetch('/api/user/chat/history')
    if (!res.ok) return
    const data = await res.json()
    if (data.messages?.length) {
      chatList.value = [{
        id: Date.now().toString(),
        title: data.messages[0]?.content?.slice(0, 20) || '历史对话',
        messages: data.messages.map((m: any) => ({
          id: m.id || `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
          role: m.role || 'assistant',
          content: m.content || '',
          timestamp: m.timestamp || Date.now(),
        })),
      }]
      currentChatId.value = chatList.value[0]?.id || ''
    }
  } catch { /* 忽略历史记录获取异常 */ }
}

// 检查是否存在活跃流
const checkActive = async () => {
  try {
    const res = await fetch('/api/user/chat/active')
    if (res.ok) {
      const data = await res.json()
      if (data.run_id) {
        connectSSE(data.run_id)
      }
    }
  } catch { /* 忽略激活状态获取异常 */ }
}

// 发送消息
const sendMessage = async () => {
  const content = inputMessage.value.trim()
  if (!content || isStreaming.value) return

  if (!currentChatId.value) createNewChat()
  const chat = currentChat.value!

  // 追加用户消息
  const userMsg: Message = {
    id: Date.now().toString(),
    role: 'user',
    content,
    timestamp: Date.now(),
  }
  chat.messages.push(userMsg)

  if (!chat.title) {
    chat.title = content.slice(0, 20) + (content.length > 20 ? '...' : '')
  }
  inputMessage.value = ''
  scrollToBottom()

  isSending.value = true
  try {
    const data = await api.post('/user/chat/send', { message: content })
    if (data.run_id) {
      connectSSE(data.run_id)
    }
  } catch (e: any) {
    message.error(e.message || '网络错误，请重试')
  } finally {
    isSending.value = false
  }
}

// 建立 SSE 流式连接（核心打字机逻辑）
const connectSSE = (runId: string) => {
  // 关闭上一个连接，防止泄漏
  if (evtSource) evtSource.close()

  isStreaming.value = true
  scrollToBottom()

  const chat = currentChat.value!

  // 先在消息列表中压入一条空的 AI 消息，用于占位和后续文本追加
  const assistantMsg: Message = {
    id: `ai-${Date.now()}`,
    role: 'assistant',
    content: '',
    timestamp: Date.now(),
  }
  chat.messages.push(assistantMsg)

  // 引用该条消息，后续在 onmessage 中直接追加文本
  const currentMsg = chat.messages[chat.messages.length - 1]

  // 打字机节流：缓冲累积 + requestAnimationFrame 刷出
  let textBuffer = ''
  let rafId: number | null = null
  let hasRendered = false

  const flushText = () => {
    if (textBuffer) {
      currentMsg.content += textBuffer
      textBuffer = ''
      scrollToBottom()
    }
    rafId = null
  }

  const scheduleFlush = () => {
    if (!rafId) {
      rafId = requestAnimationFrame(flushText)
    }
  }

  evtSource = new EventSource(`/api/user/chat/stream?run_id=${encodeURIComponent(runId)}`)

  evtSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data)

      // 1. 匹配后端数据格式 {"type":"text_delta","data":"可以"}
      if (data.type === 'text_delta' || data.type === 'delta' || data.type === 'content') {
        const textChunk = data.data || data.content || data.delta || ''
        if (textChunk) {
          if (!hasRendered) {
            // 第一块立即显示（切换掉 typing-indicator）
            currentMsg.content += textChunk
            hasRendered = true
            scrollToBottom()
          } else {
            // 后续累积到 buffer，在 rAF 回调中刷出
            textBuffer += textChunk
            scheduleFlush()
          }
        }
      } 
      // 2. 匹配传输结束指令
      else if (data.type === 'done' || data.type === 'end' || data.type === 'finish') {
        flushText()
        if (!currentMsg.content) currentMsg.content = '（空回复）'
        closeSSE()
      } 
      // 3. 匹配后端抛出的错误信息
      else if (data.type === 'error') {
        flushText()
        currentMsg.content += `\n\n**错误:** ${data.data || '生成中断'}`
        closeSSE()
      }
    } catch {
      // 容错机制：如果后端没有传 JSON，直接发送了纯文本
      if (event.data) {
        if (!hasRendered) {
          currentMsg.content += event.data
          hasRendered = true
          scrollToBottom()
        } else {
          textBuffer += event.data
          scheduleFlush()
        }
      }
    }
  }

  evtSource.onerror = () => {
    flushText()
    closeSSE()
    if (currentMsg && !currentMsg.content) {
      currentMsg.content = '连接中断，请重试'
    }
  }
}

// 关闭 SSE 连接
const closeSSE = () => {
  if (evtSource) {
    evtSource.close()
    evtSource = null
  }
  isStreaming.value = false
  scrollToBottom()
}

// 中断/停止生成
const stopStream = async () => {
  try {
    await fetch('/api/user/chat/stop', { method: 'POST' })
  } catch { /* 忽略中断请求报错 */ }
  closeSSE()
}

// 生命周期挂载
onMounted(() => {
  loadHistory()
  checkActive()
  if (!chatList.value.length) createNewChat()
})

// 组件卸载时安全清理连接
onUnmounted(() => {
  closeSSE()
})
</script>

<style scoped>
.chat-page { 
  height: calc(100vh - 56px - 48px); 
}
.chat-container { 
  display: flex; 
  height: 100%; 
  background: #fff; 
  border-radius: 8px; 
  overflow: hidden; 
}

/* 侧边栏样式 */
.chat-sidebar { 
  width: 240px; 
  border-right: 1px solid #f0f0f0; 
  padding: 16px; 
  display: flex; 
  flex-direction: column; 
}
.chat-list { 
  flex: 1; 
  overflow-y: auto; 
}
.chat-item { 
  display: flex; 
  align-items: center; 
  justify-content: space-between; 
  padding: 10px 12px; 
  border-radius: 6px; 
  cursor: pointer; 
  margin-bottom: 4px; 
  transition: background 0.2s; 
}
.chat-item:hover { 
  background: #f5f5f5; 
}
.chat-item.active { 
  background: #e6f7ff; 
}
.chat-title { 
  flex: 1; 
  overflow: hidden; 
  text-overflow: ellipsis; 
  white-space: nowrap; 
  font-size: 14px; 
}
.delete-btn { 
  opacity: 0; 
  transition: opacity 0.2s; 
}
.chat-item:hover .delete-btn { 
  opacity: 1; 
}

/* 主对话窗口 */
.chat-main { 
  flex: 1; 
  display: flex; 
  flex-direction: column; 
}
.chat-header { 
  padding: 16px 24px; 
  border-bottom: 1px solid #f0f0f0; 
}
.chat-header h3 { 
  margin: 0; 
  font-size: 16px; 
  font-weight: 500; 
}
.chat-messages { 
  flex: 1; 
  overflow-y: auto; 
  padding: 24px; 
}
.message { 
  display: flex; 
  gap: 12px; 
  margin-bottom: 24px; 
}
.message.user { 
  flex-direction: row-reverse; 
}
.message-content { 
  max-width: 70%; 
}
.message-meta { 
  font-size: 12px; 
  color: #999; 
  margin-bottom: 4px; 
}
.message.user .message-meta { 
  text-align: right; 
}
.message-time { 
  margin-left: 8px; 
}
.message-body { 
  padding: 12px 16px; 
  border-radius: 12px; 
  line-height: 1.6; 
  word-break: break-word;
}
.message.user .message-body { 
  background: #1890ff; 
  color: #fff; 
  border-top-right-radius: 4px; 
}
.message.assistant .message-body { 
  background: #f5f5f5; 
  border-top-left-radius: 4px; 
  color: #333;
}

/* markdown 样式修正 */
.markdown-body :deep(p:last-child) {
  margin-bottom: 0;
}

/* 打字等待提示动画 */
.typing-indicator { 
  display: flex; 
  gap: 4px; 
  padding: 4px 0; 
}
.typing-indicator span { 
  width: 8px; 
  height: 8px; 
  border-radius: 50%; 
  background: #999; 
  animation: typing 1.4s infinite; 
}
.typing-indicator span:nth-child(2) { animation-delay: 0.2s; }
.typing-indicator span:nth-child(3) { animation-delay: 0.4s; }

@keyframes typing { 
  0%, 60%, 100% { transform: translateY(0); opacity: 0.4; } 
  30% { transform: translateY(-6px); opacity: 1; } 
}

/* 输入框区域 */
.chat-input-area { 
  padding: 16px 24px; 
  border-top: 1px solid #f0f0f0; 
  display: flex; 
  gap: 12px; 
  align-items: flex-end; 
}
.chat-input-area :deep(.ant-input) { 
  flex: 1; 
}
</style>