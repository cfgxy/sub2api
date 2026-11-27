<template>
  <div class="mx-auto max-w-3xl space-y-6">
    <div>
      <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-dark-400">{{ t('enterprise.allocationSettings.breadcrumb') }}</p>
      <h2 class="mt-1 text-xl font-bold text-gray-900 dark:text-white">{{ t('enterprise.allocationSettings.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.allocationSettings.subtitle') }}</p>
    </div>

    <div v-if="loadError" role="status" class="card border-l-4 border-l-amber-500 p-4">
      <p class="font-semibold text-gray-900 dark:text-white">{{ t('enterprise.allocationSettings.unavailableTitle') }}</p>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.allocationSettings.unavailableHint') }}</p>
      <button class="btn btn-secondary btn-sm mt-3" :disabled="loading" @click="load">{{ t('enterprise.common.refresh') }}</button>
    </div>

    <section v-else class="card p-5">
      <div class="flex items-start justify-between gap-6">
        <div>
          <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('enterprise.allocationSettings.enabledTitle') }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.allocationSettings.disabledHint') }}</p>
        </div>
        <el-switch v-model="enabled" :loading="loading || saving" @change="save" />
      </div>
      <div class="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
        <p class="font-semibold">{{ t('enterprise.allocationSettings.riskTitle') }}</p>
        <p class="mt-1">{{ t('enterprise.allocationSettings.riskHint') }}</p>
      </div>
      <p v-if="enabled" class="mt-4 text-sm text-amber-700 dark:text-amber-300">{{ t('enterprise.allocationSettings.enabledHint') }}</p>
      <p v-else class="mt-4 text-sm text-gray-500 dark:text-dark-400">{{ t('enterprise.allocationSettings.disabledRecoveryHint') }}</p>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { enterpriseAPI } from '@/api/enterprise'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

const { t } = useI18n()
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
        t('enterprise.allocationSettings.confirmMessage'),
        t('enterprise.allocationSettings.confirmTitle'),
        { type: 'warning', confirmButtonText: t('enterprise.allocationSettings.confirm'), cancelButtonText: t('enterprise.allocationSettings.cancel') },
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
    ElMessage.success(value ? t('enterprise.allocationSettings.enabledSuccess') : t('enterprise.allocationSettings.disabledSuccess'))
  } catch (error) {
    enabled.value = !value
    ElMessage.error((error as { message?: string }).message || t('enterprise.allocationSettings.saveFailure'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
