<template>
  <PlatformEnterpriseShell>
    <div class="mx-auto max-w-5xl space-y-6">
      <div>
        <RouterLink class="text-sm text-gray-500 hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-400" to="/admin/enterprises">
          ‹ {{ t('admin.enterprise.create.backToList') }}
        </RouterLink>
        <h1 class="mt-3 text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.enterprise.create.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.create.description') }}</p>
      </div>

      <div v-if="success && created" class="card border-green-200 dark:border-green-900" data-testid="enterprise-create-success">
        <div class="card-body space-y-4">
          <h2 class="text-base font-semibold text-green-700 dark:text-green-400">{{ t('admin.enterprise.create.successTitle') }}</h2>
          <dl class="grid gap-3 sm:grid-cols-2">
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.name') }}</dt>
              <dd class="text-sm font-semibold text-gray-900 dark:text-white">{{ created.name }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.portalHost') }}</dt>
              <dd class="text-sm font-semibold text-gray-900 dark:text-white">{{ created.portal_host }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.create.dedicatedAccount') }}</dt>
              <dd class="text-sm font-semibold text-gray-900 dark:text-white">{{ created.admin_email || t('admin.enterprise.create.configured') }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.common.upstreamUserId') }}</dt>
              <dd class="text-sm font-semibold text-gray-900 dark:text-white">{{ created.dedicated_upstream_user_id }}</dd>
            </div>
          </dl>
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.create.successNote') }}</p>
          <button class="btn btn-secondary btn-sm" data-testid="enterprise-create-view-detail" @click="router.push(`/admin/enterprises/${created.id}`)">
            {{ t('admin.enterprise.create.viewDetail') }} ›
          </button>
        </div>
      </div>

      <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_300px] lg:items-start">
        <form class="card" @submit.prevent="submit">
          <section class="card-body space-y-4 border-b border-gray-200 dark:border-dark-700">
            <div class="flex gap-3">
              <span class="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-primary-50 text-xs font-bold text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">1</span>
              <div>
                <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.enterprise.create.basicTitle') }}</h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.create.basicDescription') }}</p>
              </div>
            </div>
            <div data-testid="enterprise-create-name">
              <Input
                v-model="form.name"
                :label="t('admin.enterprise.common.name')"
                :placeholder="t('admin.enterprise.create.namePlaceholder')"
                :error="errors.name"
                required
              />
            </div>
            <div data-testid="enterprise-create-host">
              <Input
                v-model="form.portal_host"
                :label="t('admin.enterprise.create.portalLabel')"
                placeholder="enterprise.example.com"
                :error="errors.portal_host"
                required
              />
            </div>
          </section>

          <section class="card-body space-y-4 border-b border-gray-200 dark:border-dark-700">
            <div class="flex gap-3">
              <span class="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-primary-50 text-xs font-bold text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">2</span>
              <div>
                <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.enterprise.create.accountTitle') }}</h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.create.accountDescription') }}</p>
              </div>
            </div>
            <div data-testid="enterprise-create-account">
              <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-dark-200">
                {{ t('admin.enterprise.create.dedicatedAccount') }}
              </label>
              <Select
                :model-value="form.dedicated_upstream_user_id || null"
                :options="accountOptions"
                remote
                clearable
                :loading="searching"
                :disabled="!enterprisesLoaded"
                :placeholder="t('admin.enterprise.create.searchAccount')"
                :search-placeholder="t('admin.enterprise.create.searchAccount')"
                :error="!!errors.dedicated_upstream_user_id"
                @search="searchUsers"
                @update:model-value="selectUser"
              />
            </div>
            <p v-if="sourceError" class="text-xs text-red-600 dark:text-red-400" role="alert" data-testid="enterprise-create-source-error">
              {{ sourceError }}
            </p>
            <p v-else-if="selectedReason" class="text-xs text-red-600 dark:text-red-400" data-testid="enterprise-create-account-reason">
              {{ selectedReason }}
            </p>
            <!-- Select 的 error 只驱动样式，校验文案在此单独渲染 -->
            <p
              v-else-if="errors.dedicated_upstream_user_id"
              class="text-xs text-red-600 dark:text-red-400"
              role="alert"
              data-testid="enterprise-create-account-error"
            >
              {{ errors.dedicated_upstream_user_id }}
            </p>
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <p class="text-xs font-semibold text-gray-700 dark:text-dark-200">{{ t('admin.enterprise.create.sourceTitle') }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.create.sourceDescription') }}</p>
            </div>
          </section>

          <section class="card-body space-y-4">
            <div class="flex gap-3">
              <span class="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-primary-50 text-xs font-bold text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">3</span>
              <div>
                <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.enterprise.create.reasonTitle') }}</h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.enterprise.create.reasonDescription') }}</p>
              </div>
            </div>
            <div data-testid="enterprise-create-reason">
              <TextArea
                v-model="form.reason"
                :label="t('admin.enterprise.create.reasonLabel')"
                :placeholder="t('admin.enterprise.create.reasonPlaceholder')"
                :error="errors.reason"
                :rows="3"
                required
              />
            </div>
          </section>

          <div class="flex justify-end gap-3 border-t border-gray-200 px-6 py-4 dark:border-dark-700">
            <button type="button" class="btn btn-secondary btn-md" @click="router.push('/admin/enterprises')">
              {{ t('admin.enterprise.common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-md" :disabled="saving" data-testid="enterprise-create-submit">
              {{ t('admin.enterprise.create.title') }}
            </button>
          </div>
        </form>

        <aside class="card">
          <div class="card-body">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.enterprise.create.checksTitle') }}</h3>
            <ul class="mt-4 list-disc space-y-3 pl-5 text-xs leading-5 text-gray-500 dark:text-dark-400">
              <li>{{ t('admin.enterprise.create.checksName') }}</li>
              <li>{{ t('admin.enterprise.create.checksActive') }}</li>
              <li>{{ t('admin.enterprise.create.checksSubscription') }}</li>
              <li>{{ t('admin.enterprise.create.checksPrivacy') }}</li>
            </ul>
            <p class="mt-5 rounded-lg bg-amber-50 p-3 text-xs leading-5 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
              {{ t('admin.enterprise.create.note') }}
            </p>
          </div>
        </aside>
      </div>
    </div>
  </PlatformEnterpriseShell>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Input from '@/components/common/Input.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import PlatformEnterpriseShell from '@/components/admin/PlatformEnterpriseShell.vue'
import { enterprisePlatformAPI, type PlatformEnterprise } from '@/api/enterprisePlatform'
import { list as listUsers } from '@/api/admin/users'
import { useAppStore } from '@/stores/app'
import type { AdminUser } from '@/types'

const router = useRouter()
const { t, locale } = useI18n({ useScope: 'global' })
const appStore = useAppStore()
const saving = ref(false)
const success = ref(false)
const created = ref<PlatformEnterprise>()
const searching = ref(false)
const enterprisesLoaded = ref(false)
const sourceError = ref('')
const existingEnterprises = ref<PlatformEnterprise[]>([])
const candidates = ref<AdminUser[]>([])
const selectedUser = ref<AdminUser>()
let searchSequence = 0
const form = reactive<{ name: string; portal_host: string; dedicated_upstream_user_id: number | ''; reason: string }>({ name: '', portal_host: '', dedicated_upstream_user_id: '', reason: '' })
const errors = reactive({ name: '', portal_host: '', dedicated_upstream_user_id: '', reason: '' })
const userOptions = computed(() => candidates.value.filter((user) => Number.isInteger(user.id) && user.id > 0).map((user) => ({ user, reason: ineligibleReason(user) })))
const accountOptions = computed<SelectOption[]>(() => userOptions.value.map((option) => ({
  value: option.user.id,
  label: optionLabel(option),
  disabled: !!option.reason,
})))
const selectedReason = computed(() => selectedUser.value ? ineligibleReason(selectedUser.value) : '')

function optionLabel(option: { user: AdminUser; reason: string }) {
  const account = `${option.user.username} · ${option.user.email}`
  if (!option.reason) return account
  return locale.value === 'zh' ? `${account}（${option.reason}）` : `${account} (${option.reason})`
}

onMounted(async () => {
  try {
    existingEnterprises.value = await enterprisePlatformAPI.list()
    enterprisesLoaded.value = true
  } catch {
    sourceError.value = t('admin.enterprise.create.listFailed')
  }
})

function ineligibleReason(user: AdminUser): string {
  if (!enterprisesLoaded.value) return t('admin.enterprise.create.loadingAccount')
  if (user.status !== 'active') return t('admin.enterprise.create.disabledAccount')
  if (existingEnterprises.value.some((item) => item.dedicated_upstream_user_id === user.id)) return t('admin.enterprise.create.linkedAccount')
  const now = Date.now()
  if (!user.subscriptions?.some((item) => item.status === 'active'
    && Date.parse(item.starts_at) <= now && Date.parse(item.expires_at || '') > now
    && item.group?.status === 'active' && Number(item.group.weekly_limit_usd) > 0)) {
    return t('admin.enterprise.create.noWeeklySubscription')
  }
  return ''
}

async function searchUsers(query: string) {
  const sequence = ++searchSequence
  candidates.value = []
  if (!query.trim() || !enterprisesLoaded.value) return
  sourceError.value = ''
  searching.value = true
  try {
    const response = await listUsers(1, 20, { search: query.trim(), include_subscriptions: true })
    if (sequence === searchSequence) candidates.value = response.items
  } catch {
    if (sequence === searchSequence) sourceError.value = t('admin.enterprise.create.searchFailed')
  } finally {
    if (sequence === searchSequence) searching.value = false
  }
}

function selectUser(value: SelectOption['value']) {
  const id = typeof value === 'number' ? value : Number(value)
  form.dedicated_upstream_user_id = Number.isInteger(id) && id > 0 ? id : ''
  selectedUser.value = userOptions.value.find((option) => option.user.id === form.dedicated_upstream_user_id)?.user
}

function validate() {
  errors.name = form.name.trim() ? '' : t('admin.enterprise.create.nameRequired')
  errors.portal_host = !form.portal_host.trim()
    ? t('admin.enterprise.create.hostRequired')
    : /^[a-z0-9.-]+$/.test(form.portal_host.trim()) ? '' : t('admin.enterprise.create.hostInvalid')
  errors.dedicated_upstream_user_id = Number(form.dedicated_upstream_user_id) >= 1 ? '' : t('admin.enterprise.create.accountRequired')
  errors.reason = form.reason.trim() ? '' : t('admin.enterprise.create.reasonRequired')
  return !Object.values(errors).some(Boolean)
}

async function submit() {
  if (!validate()) return
  // 二次校验：下拉选中值必须与已核验的候选账号一致，且该账号无不合格理由
  if (!enterprisesLoaded.value || !selectedUser.value || form.dedicated_upstream_user_id !== selectedUser.value.id || selectedReason.value) {
    appStore.showError(selectedReason.value || sourceError.value || t('admin.enterprise.create.accountRequired'))
    return
  }
  saving.value = true
  success.value = false
  try {
    created.value = await enterprisePlatformAPI.create({ ...form, dedicated_upstream_user_id: selectedUser.value.id })
    success.value = true
    appStore.showSuccess(t('admin.enterprise.create.createSuccess'))
    window.scrollTo({ top: 0, behavior: 'smooth' })
  } catch {
    appStore.showError(t('admin.enterprise.create.createFailed'))
  } finally {
    saving.value = false
  }
}
</script>
