<template>
  <div class="chat-page">
    <div class="chat-container">
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
      <div class="chat-main">
        <div class="chat-header">
          <h3>{{ currentChat?.title || 'AI 对话' }}</h3>
        </div>
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
              <div class="message-body" v-html="renderMarkdown(msg.content)"></div>
            </div>
          </div>
          <div v-if="isStreaming" class="message assistant">
            <div class="message-avatar">
              <a-avatar style="background: #52c41a">AI</a-avatar>
            </div>
            <div class="message-content">
              <div class="typing-indicator">
                <span></span><span></span><span></span>
              </div>
            </div>
          </div>
        </div>
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

const md = new MarkdownIt({ html: false, linkify: true, breaks: true })
const renderMarkdown = (content: string) => DOMPurify.sanitize(md.render(content || ''))

const username = ref(localStorage.getItem('username') || '')
const inputMessage = ref('')
const isSending = ref(false)
const isStreaming = ref(false)
const messagesRef = ref<HTMLElement>()
let abortController: AbortController | null = null

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

const createNewChat = () => {
  const id = Date.now().toString()
  chatList.value.unshift({ id, title: '', messages: [] })
  currentChatId.value = id
}

const switchChat = (id: string) => {
  currentChatId.value = id
}

const deleteChat = (id: string) => {
  const idx = chatList.value.findIndex(c => c.id === id)
  if (idx > -1) {
    chatList.value.splice(idx, 1)
    if (currentChatId.value === id) {
      currentChatId.value = chatList.value[0]?.id || ''
    }
  }
}

const formatTime = (ts: number) =>
  new Date(ts).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })

const scrollToBottom = () => {
  nextTick(() => {
    if (messagesRef.value) messagesRef.value.scrollTop = messagesRef.value.scrollHeight
  })
}

const handleKeyDown = (e: KeyboardEvent) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendMessage()
  }
}

const loadHistory = async () => {
  try {
    const res = await fetch('/api/user/chat/history')
    if (!res.ok) return
    const data = await res.json()
    if (data.messages?.length) {
      chatList.value = [{
        id: Date.now().toString(),
        title: data.messages[0]?.content?.slice(0, 20) || '历史对话',
        messages: data.messages.map((m: any, i: number) => ({
          id: `${Date.now()}-${i}`,
          role: m.role || 'assistant',
          content: m.content || '',
          timestamp: Date.now(),
        })),
      }]
      currentChatId.value = chatList.value[0]?.id || ''
    }
  } catch { /* ignore */ }
}

const checkActive = async () => {
  try {
    const res = await fetch('/api/user/chat/active')
    if (res.ok) {
      const data = await res.json()
      if (data.run_id) {
        connectSSE(data.run_id)
      }
    }
  } catch { /* ignore */ }
}

const sendMessage = async () => {
  const content = inputMessage.value.trim()
  if (!content || isStreaming.value) return

  if (!currentChatId.value) createNewChat()
  const chat = currentChat.value!

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

const connectSSE = (runId: string) => {
  isStreaming.value = true
  scrollToBottom()

  const chat = currentChat.value!
  const assistantMsg: Message = {
    id: `ai-${Date.now()}`,
    role: 'assistant',
    content: '',
    timestamp: Date.now(),
  }
  chat.messages.push(assistantMsg)

  abortController = new AbortController()
  const evtSource = new EventSource(`/api/user/chat/stream?run_id=${encodeURIComponent(runId)}`)

  evtSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data)
      if (data.type === 'delta' || data.type === 'content') {
        assistantMsg.content += data.content || data.delta || ''
        scrollToBottom()
      } else if (data.type === 'done' || data.type === 'end') {
        evtSource.close()
        isStreaming.value = false
        abortController = null
      } else if (data.type === 'error') {
        assistantMsg.content += `\n\n错误: ${data.data || '未知错误'}`
        evtSource.close()
        isStreaming.value = false
        abortController = null
      }
    } catch { /* ignore parse errors */ }
  }

  evtSource.onerror = () => {
    evtSource.close()
    isStreaming.value = false
    abortController = null
    if (!assistantMsg.content) {
      assistantMsg.content = '连接中断，请重试'
    }
  }
}

const stopStream = async () => {
  try {
    await fetch('/api/user/chat/stop', { method: 'POST' })
  } catch { /* ignore */ }
  if (abortController) {
    abortController.abort()
    abortController = null
  }
  isStreaming.value = false
}

onMounted(() => {
  loadHistory()
  checkActive()
  if (!chatList.value.length) createNewChat()
})

onUnmounted(() => {
  if (abortController) abortController.abort()
})
</script>

<style scoped>
.chat-page { height: calc(100vh - 56px - 48px); }
.chat-container { display: flex; height: 100%; background: #fff; border-radius: 8px; overflow: hidden; }
.chat-sidebar { width: 240px; border-right: 1px solid #f0f0f0; padding: 16px; display: flex; flex-direction: column; }
.chat-list { flex: 1; overflow-y: auto; }
.chat-item { display: flex; align-items: center; justify-content: space-between; padding: 10px 12px; border-radius: 6px; cursor: pointer; margin-bottom: 4px; transition: background 0.2s; }
.chat-item:hover { background: #f5f5f5; }
.chat-item.active { background: #e6f7ff; }
.chat-title { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; }
.delete-btn { opacity: 0; transition: opacity 0.2s; }
.chat-item:hover .delete-btn { opacity: 1; }
.chat-main { flex: 1; display: flex; flex-direction: column; }
.chat-header { padding: 16px 24px; border-bottom: 1px solid #f0f0f0; }
.chat-header h3 { margin: 0; font-size: 16px; font-weight: 500; }
.chat-messages { flex: 1; overflow-y: auto; padding: 24px; }
.message { display: flex; gap: 12px; margin-bottom: 24px; }
.message.user { flex-direction: row-reverse; }
.message-content { max-width: 70%; }
.message-meta { font-size: 12px; color: #999; margin-bottom: 4px; }
.message.user .message-meta { text-align: right; }
.message-time { margin-left: 8px; }
.message-body { padding: 12px 16px; border-radius: 12px; line-height: 1.6; }
.message.user .message-body { background: #1890ff; color: #fff; border-top-right-radius: 4px; }
.message.assistant .message-body { background: #f5f5f5; border-top-left-radius: 4px; }
.typing-indicator { display: flex; gap: 4px; padding: 8px 0; }
.typing-indicator span { width: 8px; height: 8px; border-radius: 50%; background: #999; animation: typing 1.4s infinite; }
.typing-indicator span:nth-child(2) { animation-delay: 0.2s; }
.typing-indicator span:nth-child(3) { animation-delay: 0.4s; }
@keyframes typing { 0%, 60%, 100% { transform: translateY(0); opacity: 0.4; } 30% { transform: translateY(-8px); opacity: 1; } }
.chat-input-area { padding: 16px 24px; border-top: 1px solid #f0f0f0; display: flex; gap: 12px; align-items: flex-end; }
.chat-input-area :deep(.ant-input) { flex: 1; }
</style>
