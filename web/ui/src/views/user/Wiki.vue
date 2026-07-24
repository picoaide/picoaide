<template>
  <div class="wiki-container">
    <div class="wiki-topbar">
      <a-input-search v-model:value="searchQuery" placeholder="搜索知识库..." style="width: 400px" @search="doSearch" />
    </div>

    <div class="wiki-body">
      <div class="wiki-sidebar">
        <h4>目录</h4>
        <a-spin :spinning="treeLoading">
          <a-tree
            v-if="folderTree.length > 0"
            :tree-data="folderTree"
            :default-expand-all="true"
            @select="onFolderSelect"
          />
          <a-empty v-else description="暂无目录" />
        </a-spin>

        <h4 style="margin-top: 16px">标签</h4>
        <div v-if="tags.length > 0">
          <a-tag v-for="t in tags" :key="t.tag" color="blue" style="cursor:pointer" @click="handleTagClick(t.tag)">
            {{ t.tag }}
          </a-tag>
        </div>
        <span v-else style="color: #999; font-size: 12px">暂无标签</span>
      </div>

      <div class="wiki-content">
        <div v-if="searchQuery">
          <h3>搜索结果: "{{ searchQuery }}"</h3>
          <a-list v-if="searchResults.length > 0" :data-source="searchResults">
            <template #renderItem="{ item }">
              <a-list-item @click="loadDocument(item.doc_id)" style="cursor:pointer">
                <a-list-item-meta :title="item.title" :description="item.snippet" />
              </a-list-item>
            </template>
          </a-list>
          <a-empty v-else description="未找到相关文档" />
        </div>

        <div v-else-if="documents.length > 0 && !currentDoc">
          <h3>文件夹内容</h3>
          <a-list :data-source="documents">
            <template #renderItem="{ item }">
              <a-list-item @click="loadDocument(item.id)" style="cursor:pointer">
                <a-list-item-meta :title="item.title" :description="item.file_type" />
              </a-list-item>
            </template>
          </a-list>
        </div>

        <div v-else-if="currentDoc">
          <a-page-header :title="currentDoc.title" @back="currentDoc = null" />
          <div v-if="currentDoc.tags?.length" style="margin-bottom: 8px">
            <a-tag v-for="t in currentDoc.tags" :key="t.tag" color="blue">{{ t.tag }}</a-tag>
          </div>
          <div class="wiki-doc-content" @click="onDocContentClick" v-html="renderedContent"></div>

          <a-divider />
          <a-row :gutter="16">
            <a-col :span="12">
              <h4>相关文档</h4>
              <ul v-if="currentDoc.links?.length" class="wiki-link-list">
                <li v-for="link in currentDoc.links" :key="link.id" @click="loadDocument(link.target_doc)">
                  {{ link.keyword }}
                </li>
              </ul>
              <span v-else style="color:#999">无</span>
            </a-col>
            <a-col :span="12">
              <h4>被引用自</h4>
              <ul v-if="currentDoc.backlinks?.length" class="wiki-link-list">
                <li v-for="bl in currentDoc.backlinks" :key="bl.id" @click="loadDocument(bl.source_doc)">
                  {{ bl.keyword }}
                </li>
              </ul>
              <span v-else style="color:#999">无</span>
            </a-col>
          </a-row>
        </div>

        <div v-else class="wiki-empty">
          <a-empty description="选择一个文档开始阅读" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'
import { api } from '../../composables/api'

interface KB {
  id: number
  name: string
  description?: string
}

interface TreeNode {
  key: string
  title: string
  children?: TreeNode[]
  isLeaf?: boolean
  doc_id?: number
}

interface DocListItem {
  id: number
  title: string
  doc_type?: string
}

interface Document {
  id: number
  title: string
  content: string
  tags?: { tag: string }[]
  links?: { id: number; keyword: string; target_doc: number }[]
  backlinks?: { id: number; keyword: string; source_doc: number }[]
}

interface SearchResult {
  doc_id: number
  title: string
  snippet: string
}

interface TagInfo {
  tag: string
  count: number
}

const md = new MarkdownIt({ html: false, breaks: true, linkify: true })

const kbs = ref<KB[]>([])
const currentKB = ref<KB | null>(null)
const folderTree = ref<TreeNode[]>([])
const currentFolder = ref<number | undefined>()
const documents = ref<DocListItem[]>([])
const currentDoc = ref<Document | null>(null)
const searchQuery = ref('')
const searchResults = ref<SearchResult[]>([])
const tags = ref<TagInfo[]>([])
const loading = ref(false)
const treeLoading = ref(false)

const renderedContent = computed(() => {
  if (!currentDoc.value) return ''
  const html = md.render(currentDoc.value.content)
  const withLinks = html.replace(/\[\[([^\]]+)\]\]/g, '<a href="javascript:void(0)" class="wiki-link" data-keyword="$1">$1</a>')
  const withTags = withLinks.replace(/(?:\s|^)#(\w[\w-]*)/g, ' <a href="javascript:void(0)" class="wiki-tag" data-tag="$1">#$1</a>')
  return DOMPurify.sanitize(withTags)
})

const loadKBs = async () => {
  try {
    const res = await api.get<{ success: boolean; data: KB[] }>('/user/knowledge-bases')
    if (res.success && res.data.length > 0) {
      kbs.value = res.data
      currentKB.value = res.data[0]
      loadFolderTree()
    }
  } catch (e: any) {
    message.error('加载知识库失败: ' + e.message)
  }
}

const loadFolderTree = async () => {
  if (!currentKB.value) return
  treeLoading.value = true
  try {
    const res = await api.get<{ success: boolean; data: any }>('/user/knowledge-bases/' + currentKB.value.id)
    if (res.success) {
      folderTree.value = buildTree(res.data)
    }
  } catch (e: any) {
    message.error('加载目录失败: ' + e.message)
  } finally {
    treeLoading.value = false
  }
}

const buildTree = (data: any): TreeNode[] => {
  if (data.folders) {
    const roots: TreeNode[] = []
    const folderMap = new Map<number, TreeNode>()
    for (const f of data.folders) {
      const node: TreeNode = {
        key: 'folder-' + f.id,
        title: f.name,
        children: [],
      }
      folderMap.set(f.id, node)
    }
    for (const f of data.folders) {
      if (f.parent_id && folderMap.has(f.parent_id)) {
        folderMap.get(f.parent_id)!.children!.push(folderMap.get(f.id)!)
      } else {
        roots.push(folderMap.get(f.id)!)
      }
    }
    if (data.documents) {
      const attachDocs = (nodes: TreeNode[]) => {
        for (const node of nodes) {
          const folderId = parseInt(node.key.replace('folder-', ''), 10)
          const docs = data.documents.filter((d: any) => d.folder_id === folderId)
          for (const d of docs) {
            node.children!.push({
              key: 'doc-' + d.id,
              title: d.title,
              isLeaf: true,
              doc_id: d.id,
            })
          }
          if (node.children) attachDocs(node.children)
        }
      }
      attachDocs(roots)
    }
    return roots
  }
  return []
}

const onFolderSelect = async (_keys: any, info: any) => {
  const node = info.node
  if (node.isLeaf && node.doc_id) {
    loadDocument(node.doc_id)
  } else {
    currentDoc.value = null
    searchQuery.value = ''
    currentFolder.value = parseInt(node.key.replace('folder-', ''), 10)
    if (!isNaN(currentFolder.value)) {
      browseFolder(currentFolder.value)
    }
  }
}

const browseFolder = async (folderId: number) => {
  loading.value = true
  try {
    const res = await api.get<{ success: boolean; data: any }>('/user/knowledge-bases/' + currentKB.value!.id + '/navigate', {
      folder: String(folderId),
      page: '1',
      size: '50',
    })
    if (res.success) {
      documents.value = res.data.documents || []
      tags.value = res.data.tags || []
    }
  } catch (e: any) {
    message.error('浏览文件夹失败: ' + e.message)
  } finally {
    loading.value = false
  }
}

const loadDocument = async (docId: number) => {
  loading.value = true
  try {
    const res = await api.get<{ success: boolean; data: Document }>('/user/knowledge-bases/documents/' + docId)
    if (res.success) {
      currentDoc.value = res.data
      searchQuery.value = ''
    }
  } catch (e: any) {
    message.error('加载文档失败: ' + e.message)
  } finally {
    loading.value = false
  }
}

const doSearch = async () => {
  const q = searchQuery.value.trim()
  if (!q) return
  loading.value = true
  currentDoc.value = null
  try {
    const res = await api.get<{ success: boolean; data: { results: SearchResult[] } }>('/user/knowledge-bases/search', {
      q,
      page: '1',
      size: '20',
    })
    if (res.success) {
      searchResults.value = res.data.results || []
    }
  } catch (e: any) {
    message.error('搜索失败: ' + e.message)
  } finally {
    loading.value = false
  }
}

const onDocContentClick = async (e: MouseEvent) => {
  const target = e.target as HTMLElement
  if (target.classList.contains('wiki-link')) {
    const keyword = target.getAttribute('data-keyword')
    if (keyword) handleWikiLinkClick(keyword)
  } else if (target.classList.contains('wiki-tag')) {
    const tag = target.getAttribute('data-tag')
    if (tag) handleTagClick(tag)
  }
}

const handleWikiLinkClick = async (keyword: string) => {
  try {
    const res = await api.get<{ success: boolean; data: { results: SearchResult[] } }>('/user/knowledge-bases/search', { q: keyword })
    if (res.success && res.data.results?.length > 0) {
      loadDocument(res.data.results[0].doc_id)
    }
  } catch {
    // silently ignore
  }
}

const handleTagClick = (tag: string) => {
  searchQuery.value = tag
  doSearch()
}

onMounted(() => {
  loadKBs()
})
</script>

<style scoped>
.wiki-container { display: flex; flex-direction: column; height: 100%; }
.wiki-topbar { display: flex; justify-content: space-between; align-items: center; padding: 8px 16px; border-bottom: 1px solid #f0f0f0; }
.wiki-body { display: flex; flex: 1; overflow: hidden; }
.wiki-sidebar { width: 300px; border-right: 1px solid #f0f0f0; padding: 16px; overflow-y: auto; flex-shrink: 0; }
.wiki-content { flex: 1; padding: 16px; overflow-y: auto; }
.wiki-doc-content { line-height: 1.8; }
.wiki-doc-content h1 { font-size: 24px; margin-bottom: 16px; }
.wiki-doc-content h2 { font-size: 20px; margin: 24px 0 12px; }
.wiki-doc-content p { margin-bottom: 12px; }
.wiki-doc-content code { background: #f5f5f5; padding: 2px 6px; border-radius: 3px; font-size: 0.9em; }
.wiki-doc-content pre code { display: block; padding: 12px; overflow-x: auto; }
.wiki-doc-content :deep(.wiki-link) { color: #1890ff; cursor: pointer; text-decoration: underline; }
.wiki-doc-content :deep(.wiki-tag) { color: #1890ff; cursor: pointer; }
.wiki-link-list { list-style: none; padding: 0; }
.wiki-link-list li { color: #1890ff; cursor: pointer; margin-bottom: 4px; }
.wiki-link-list li:hover { text-decoration: underline; }
.wiki-empty { display: flex; justify-content: center; align-items: center; height: 100%; }
@media (max-width: 768px) { .wiki-sidebar { display: none; } }
</style>
