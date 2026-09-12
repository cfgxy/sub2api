<template>
  <section>
    <div class="page-heading">
      <div>
        <h1>企业品牌</h1>
        <p>品牌内容会直接显示在企业登录页首屏。</p>
      </div>
      <el-button type="primary" :loading="saving" :disabled="loading || uploading || restoring" @click="save">保存品牌</el-button>
    </div>
    <div class="brand-grid">
      <el-form :model="brand" label-position="top" :disabled="loading || saving || uploading || restoring">
        <el-form-item label="企业名称">
          <el-input v-model="brand.enterprise_name" maxlength="255" show-word-limit />
        </el-form-item>
        <el-form-item label="品牌标题">
          <el-input v-model="brand.title" maxlength="40" show-word-limit />
        </el-form-item>
        <el-form-item label="品牌标语">
          <el-input v-model="brand.slogan" maxlength="60" show-word-limit />
        </el-form-item>
        <el-form-item label="品牌正文">
          <el-input v-model="brand.body" type="textarea" :rows="4" maxlength="120" show-word-limit />
        </el-form-item>
        <el-divider>登录背景资源</el-divider>
        <el-form-item label="上传背景图片">
          <input
            class="file-input"
            type="file"
            accept="image/jpeg,image/png,image/webp"
            :disabled="loading || saving || uploading || restoring"
            @change="uploadFile"
          >
        </el-form-item>
        <el-button
          v-if="brand.background_url !== '/logo.svg'"
          :loading="restoring"
          :disabled="loading || saving || uploading"
          @click="useDefaultBackground"
        >
          恢复平台默认背景
        </el-button>
        <div v-if="brand.background_url" class="metadata">
          <span>{{ brand.background_content_type }}</span>
          <span>{{ brand.background_size_bytes }} bytes</span>
          <code>{{ brand.background_sha256 }}</code>
        </div>
        <el-alert v-if="validationError" :title="validationError" type="error" :closable="false" />
      </el-form>
      <div class="preview" :style="previewStyle">
        <div class="preview-shade" />
        <div class="preview-copy">
          <Icon name="shield" size="xl" />
          <small>{{ brand.enterprise_name || 'Sub2API' }}</small>
          <h2>{{ brand.title || '企业工作台' }}</h2>
          <strong>{{ brand.slogan || '安全、统一的企业访问入口' }}</strong>
          <p>{{ brand.body || '使用企业管理员或员工账号安全访问组织资源。' }}</p>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import Icon from '@/components/icons/Icon.vue'
import { enterpriseAPI } from '@/api/enterprise'
import { validateBrand, validateImageFile } from '@/features/enterprise/validation'
import type { EnterpriseBrand } from '@/types/enterprise'

const defaultBrand: EnterpriseBrand = {
  enterprise_name: 'Sub2API',
  title: '企业工作台',
  body: '使用企业管理员或员工账号安全访问组织资源。',
  slogan: '安全、统一的企业访问入口',
  background_url: '/logo.svg',
  background_content_type: 'image/svg+xml',
  background_sha256: 'ce1f2ac07efcfff80904a9582578b5db8fdd14a3118a5c7b58f408ed06df18e1',
  background_size_bytes: 2010,
}
const controlledBackgroundURLs = new Set(['/logo.svg', '/api/v1/enterprise/brand/background'])
const loading = ref(false)
const saving = ref(false)
const uploading = ref(false)
const restoring = ref(false)
const validationError = ref('')
const brand = reactive<EnterpriseBrand>({ ...defaultBrand })
const previewStyle = computed(() => {
  const url = controlledBackgroundURLs.has(brand.background_url) ? brand.background_url : defaultBrand.background_url
  const version = url === '/api/v1/enterprise/brand/background' && /^[a-f\d]{64}$/i.test(brand.background_sha256)
    ? `?v=${brand.background_sha256}`
    : ''
  return { backgroundImage: `url("${url}${version}")` }
})

function applyBrand(response: EnterpriseBrand) {
  brand.enterprise_name = response.enterprise_name?.trim() || defaultBrand.enterprise_name
  brand.title = response.title?.trim() || defaultBrand.title
  brand.body = response.body?.trim() || defaultBrand.body
  brand.slogan = response.slogan?.trim() || defaultBrand.slogan
  brand.background_url = controlledBackgroundURLs.has(response.background_url) ? response.background_url : defaultBrand.background_url
  brand.background_content_type = response.background_content_type || defaultBrand.background_content_type
  brand.background_sha256 = response.background_sha256 || defaultBrand.background_sha256
  brand.background_size_bytes = response.background_size_bytes > 0 ? response.background_size_bytes : defaultBrand.background_size_bytes
}

function editableBrand(backgroundURL = brand.background_url) {
  return {
    enterprise_name: brand.enterprise_name,
    title: brand.title,
    body: brand.body,
    slogan: brand.slogan,
    background_url: backgroundURL,
  }
}

async function load() {
  loading.value = true
  validationError.value = ''
  try {
    applyBrand(await enterpriseAPI.getAdminBrand())
  } catch {
    validationError.value = '品牌信息加载失败，已显示平台默认值'
  } finally {
    loading.value = false
  }
}

async function uploadFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const error = validateImageFile(file)
  if (error) {
    validationError.value = error
    input.value = ''
    return
  }
  uploading.value = true
  validationError.value = ''
  try {
    const background = await enterpriseAPI.uploadBrandBackground(file)
    if (!controlledBackgroundURLs.has(background.background_url)) throw new Error('Unexpected background URL')
    Object.assign(brand, background)
    ElMessage.success('背景图片已上传')
  } catch {
    validationError.value = '背景图片上传失败，当前预览未更改'
    ElMessage.error('背景图片上传失败')
  } finally {
    uploading.value = false
    input.value = ''
  }
}

async function useDefaultBackground() {
  restoring.value = true
  validationError.value = ''
  try {
    applyBrand(await enterpriseAPI.updateBrand(editableBrand(defaultBrand.background_url)))
    ElMessage.success('已恢复平台默认背景')
  } catch {
    validationError.value = '恢复平台默认背景失败，当前预览未更改'
    ElMessage.error('恢复平台默认背景失败')
  } finally {
    restoring.value = false
  }
}

async function save() {
  validationError.value = validateBrand(brand)
  if (validationError.value) return
  saving.value = true
  try {
    applyBrand(await enterpriseAPI.updateBrand(editableBrand()))
    ElMessage.success('企业品牌已保存')
  } catch {
    validationError.value = '企业品牌保存失败，请重试'
    ElMessage.error('企业品牌保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page-heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:22px}.page-heading h1{margin:0 0 6px;font-size:26px}.page-heading p{margin:0;color:#64748b}.brand-grid{display:grid;grid-template-columns:minmax(0,520px) minmax(320px,1fr);gap:36px;align-items:start}.file-input{width:100%;padding:10px;border:1px solid #dcdfe6;border-radius:4px;background:white}.metadata{display:grid;grid-template-columns:1fr 1fr;gap:8px 16px;margin-bottom:18px;color:#64748b;font-size:13px}.metadata code{grid-column:1/-1;overflow-wrap:anywhere}.preview{position:sticky;top:92px;min-height:520px;border-radius:8px;background:#173f3d center/cover;color:white;overflow:hidden}.preview-shade{position:absolute;inset:0;background:rgba(9,31,31,.72)}.preview-copy{position:absolute;left:0;right:0;bottom:0;padding:42px}.preview-copy small{display:block;margin:20px 0 8px;color:#a7f3d0}.preview-copy h2{margin:0 0 16px;font-size:38px;overflow-wrap:anywhere}.preview-copy p{color:#d1fae5;line-height:1.6}@media(max-width:900px){.brand-grid{grid-template-columns:1fr}.preview{position:relative;top:0;min-height:380px}}@media(max-width:640px){.page-heading{flex-direction:column}.page-heading .el-button{width:100%}.metadata{grid-template-columns:1fr}}
</style>
