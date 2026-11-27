<template>
  <div class="mx-auto max-w-3xl space-y-6">
    <div>
      <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-dark-400">系统管理 / 额度定制</p>
      <h2 class="mt-1 text-xl font-bold text-gray-900 dark:text-white">员工额度分配</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">设置员工管理额度是否可以超过企业当前套餐总池。</p>
    </div>

    <div v-if="loadError" role="status" class="card border-l-4 border-l-amber-500 p-4">
      <p class="font-semibold text-gray-900 dark:text-white">配置暂时不可用</p>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">请刷新后重试；在无法读取配置时，服务端仍按默认禁止超额分配执行。</p>
      <button class="btn btn-secondary btn-sm mt-3" :disabled="loading" @click="load">刷新</button>
    </div>

    <section v-else class="card p-5">
      <div class="flex items-start justify-between gap-6">
        <div>
          <h3 class="font-semibold text-gray-900 dark:text-white">允许员工额度超额分配</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">默认关闭。关闭时，新增或上调后的员工额度总和不能超过企业当前套餐总池。</p>
        </div>
        <el-switch v-model="enabled" :loading="loading || saving" @change="save" />
      </div>
      <div class="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
        <p class="font-semibold">开启前请确认风险</p>
        <p class="mt-1">开启后，员工额度总和可超过企业当前套餐额度。员工额度仅用于内部预算管理，不代表实际可用额度；实际调用仍受企业总池余额、套餐有效期及上游服务规则限制。</p>
      </div>
      <p v-if="enabled" class="mt-4 text-sm text-amber-700 dark:text-amber-300">已开启：现有和后续超额配置不会放松任何请求期总池、套餐、官方账号、上游或并发保护。</p>
      <p v-else class="mt-4 text-sm text-gray-500 dark:text-dark-400">已关闭：不会自动回收既有超额额度；请在“额度分配”中下调后再新增或上调其他员工额度。</p>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { enterpriseAPI } from '@/api/enterprise'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

const auth = useEnterpriseAuthStore()
const enabled = ref(false)
const loading = ref(true)
const saving = ref(false)
const loadError = ref(false)
const enterpriseId = () => auth.principal?.enterprise_id || 0

async function load() {
  loading.value = true
  loadError.value = false
  try {
    const policy = await enterpriseAPI.getAllocationPolicy(enterpriseId(), { suppressUnavailableRedirect: true })
    enabled.value = policy.allow_employee_allocation_overcommit
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

async function save(value: boolean) {
  if (value) {
    try {
      await ElMessageBox.confirm(
        '员工额度将只作为内部预算管理，不保证实际可用额度；实际调用仍受企业总池、套餐和上游规则限制。确认开启？',
        '确认允许超额分配',
        { type: 'warning', confirmButtonText: '确认开启', cancelButtonText: '取消' },
      )
    } catch {
      enabled.value = false
      return
    }
  }
  saving.value = true
  try {
    const policy = await enterpriseAPI.updateAllocationPolicy({
      enterprise_id: enterpriseId(),
      allow_employee_allocation_overcommit: value,
    })
    enabled.value = policy.allow_employee_allocation_overcommit
    ElMessage.success(value ? '已允许员工额度超额分配' : '已禁止后续员工额度超额分配')
  } catch (error) {
    enabled.value = !value
    ElMessage.error((error as { message?: string }).message || '保存失败，请刷新后重试')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
