import { reactive } from 'vue'

export function usePagination() {
  const pagination = reactive({ current: 1, pageSize: 20, total: 0, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` })
  const handleTableChange = (pag: any) => {
    pagination.current = pag.current
    pagination.pageSize = pag.pageSize || 20
  }
  return { pagination, handleTableChange }
}
