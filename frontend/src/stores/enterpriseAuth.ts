import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { enterpriseAPI, onEnterpriseAuthSession, syncEnterpriseAuthSession } from '@/api/enterprise'
import type { EnterprisePrincipal, EnterpriseTokenPair } from '@/types/enterprise'

const ACCESS_KEY = 'enterprise_access_token'
const REFRESH_KEY = 'enterprise_refresh_token'
const PRINCIPAL_KEY = 'enterprise_principal'

export const useEnterpriseAuthStore = defineStore('enterprise-auth', () => {
  const accessToken = ref<string | null>(null)
  const refreshToken = ref<string | null>(null)
  const principal = ref<EnterprisePrincipal | null>(null)
  const initialized = ref(false)

  const isAuthenticated = computed(() => !!accessToken.value && !!refreshToken.value && !!principal.value)
  const isAdmin = computed(() => principal.value?.role === 'enterprise_admin')
  const mustChangePassword = computed(() => principal.value?.force_password_change === true)

  function applySession(pair: EnterpriseTokenPair) {
    accessToken.value = pair.access_token
    refreshToken.value = pair.refresh_token
    principal.value = pair.principal
  }

  onEnterpriseAuthSession(applySession)

  function restore() {
    if (initialized.value) return
    initialized.value = true
    accessToken.value = localStorage.getItem(ACCESS_KEY)
    refreshToken.value = localStorage.getItem(REFRESH_KEY)
    try {
      const saved = localStorage.getItem(PRINCIPAL_KEY)
      principal.value = saved ? JSON.parse(saved) as EnterprisePrincipal : null
    } catch {
      clear()
    }
  }

  function clear() {
    accessToken.value = null
    refreshToken.value = null
    principal.value = null
    localStorage.removeItem(ACCESS_KEY)
    localStorage.removeItem(REFRESH_KEY)
    localStorage.removeItem(PRINCIPAL_KEY)
  }

  async function login(credentials: { email: string; password: string }) {
    const pair = await enterpriseAPI.login(credentials)
    syncEnterpriseAuthSession(pair)
    return pair.principal
  }

  async function logout() {
    const token = refreshToken.value
    try {
      if (token) await enterpriseAPI.logout(token)
    } finally {
      clear()
    }
  }

  async function changeInitialPassword(currentPassword: string, newPassword: string) {
    await enterpriseAPI.changeInitialPassword(currentPassword, newPassword)
    clear()
  }

  return { accessToken, principal, initialized, isAuthenticated, isAdmin, mustChangePassword, restore, login, logout, clear, changeInitialPassword }
})
