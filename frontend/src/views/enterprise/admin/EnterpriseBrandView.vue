<template>
  <section>
    <div class="page-heading">
      <div>
        <h1>品牌设置</h1>
        <p>配置企业登录页品牌信息；未配置字段将逐项回退到 Sub2API 默认值。</p>
      </div>
      <div class="heading-actions">
        <el-button :disabled="loading || saving || uploading || restoring" @click="load">放弃更改</el-button>
        <el-button type="primary" :loading="saving" :disabled="loading || uploading || restoring" @click="save">保存设置</el-button>
      </div>
    </div>
    <el-alert
      v-if="savedSummary"
      type="success"
      :title="`品牌设置已保存`"
      :description="savedSummary"
      show-icon
      :closable="false"
      class="saved-banner"
      data-testid="brand-saved-banner"
    />
    <div class="brand-grid">
      <div class="content-panel">
        <div class="panel-head"><h2>登录页内容</h2><span class="tag">逐字段回退</span></div>
        <el-form :model="brand" label-position="top" :disabled="loading || saving || uploading || restoring">
          <el-form-item>
            <template #label><span class="field-label">登录背景图<em>可选</em></span></template>
            <label class="dropzone" :class="{ busy: uploading }">
              <input
                class="file-input"
                type="file"
                accept="image/jpeg,image/png,image/webp"
                :disabled="loading || saving || uploading || restoring"
                @change="uploadFile"
              >
              <strong>{{ uploading ? '上传中…' : '上传背景图' }}</strong>
              <span>JPG / PNG / WebP，文件不超过 5 MB</span>
              <small>未上传或图片加载失败时，回退平台默认背景。</small>
            </label>
            <el-button
              v-if="brand.background_url !== '/logo.svg'"
              class="restore-btn"
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
          </el-form-item>
          <el-form-item label="企业名称">
            <el-input v-model="brand.enterprise_name" maxlength="255" show-word-limit />
          </el-form-item>
          <el-form-item label="标题">
            <el-input v-model="brand.title" maxlength="40" show-word-limit />
          </el-form-item>
          <el-form-item label="正文">
            <el-input v-model="brand.body" type="textarea" :rows="4" maxlength="120" show-word-limit />
          </el-form-item>
          <el-form-item label="Slogan">
            <el-input v-model="brand.slogan" maxlength="60" show-word-limit />
          </el-form-item>
          <el-alert v-if="validationError" :title="validationError" type="error" :closable="false" data-testid="brand-validation-error" />
        </el-form>
        <p class="field-fallback-hint">清空任一字段只会让该字段回退，不影响其他已保存内容。</p>
      </div>
      <div class="preview-panel">
        <div class="panel-head"><h2>登录页预览</h2><span class="tag">桌面端</span></div>
        <div class="preview" :style="previewStyle">
          <div class="preview-shade" />
          <div class="preview-copy">
            <small>{{ brand.enterprise_name || 'Sub2API' }}</small>
            <h2>{{ brand.title || '企业工作台' }}</h2>
            <strong>{{ brand.slogan || '安全、统一的企业访问入口' }}</strong>
            <p>{{ brand.body || '使用企业管理员或员工账号安全访问组织资源。' }}</p>
          </div>
        </div>
        <dl class="preview-summary">
          <div><dt>图片回退</dt><dd>{{ usingDefaultBackground ? '平台默认背景' : '企业自定义背景' }}</dd></div>
          <div><dt>标题上限</dt><dd>40 字符</dd></div>
          <div><dt>正文上限</dt><dd>120 字符</dd></div>
          <div><dt>Slogan 上限</dt><dd>60 字符</dd></div>
        </dl>
        <el-alert type="info" :closable="false" show-icon class="preview-note">
          <template #default>{{ usingDefaultBackground ? '当前预览使用默认背景' : '当前预览使用企业自定义背景' }} · 新图片提交并校验成功后替换。</template>
        </el-alert>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
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
const savedSummary = ref('')
const brand = reactive<EnterpriseBrand>({ ...defaultBrand })
const previewStyle = computed(() => {
  const url = controlledBackgroundURLs.has(brand.background_url) ? brand.background_url : defaultBrand.background_url
  const version = url === '/api/v1/enterprise/brand/background' && /^[a-f\d]{64}$/i.test(brand.background_sha256)
    ? `?v=${brand.background_sha256}`
    : ''
  return { backgroundImage: `url("${url}${version}")` }
})
const usingDefaultBackground = computed(() => brand.background_url !== '/api/v1/enterprise/brand/background')

function customizedFieldCount() {
  return ([
    [brand.enterprise_name, defaultBrand.enterprise_name],
    [brand.title, defaultBrand.title],
    [brand.body, defaultBrand.body],
    [brand.slogan, defaultBrand.slogan],
  ] as const).filter(([value, fallback]) => value !== fallback).length
}

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
  savedSummary.value = ''
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
    const count = customizedFieldCount()
    savedSummary.value = count
      ? `已自定义 ${count} 项文案字段${usingDefaultBackground.value ? '，背景仍为平台默认' : '，含自定义背景图'}。`
      : `全部文案字段均为平台默认值${usingDefaultBackground.value ? '，背景仍为平台默认' : '，含自定义背景图'}。`
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
.page-heading{display:flex;justify-content:space-between;gap:20px;margin-bottom:16px}
.page-heading h1{margin:0 0 6px;font-size:26px}
.page-heading p{margin:0;color:#64748b}
.heading-actions{display:flex;gap:12px;align-items:flex-start}
.saved-banner{margin-bottom:18px}
.brand-grid{display:grid;grid-template-columns:minmax(0,520px) minmax(320px,1fr);gap:36px;align-items:start}
.content-panel,.preview-panel{background:white;border:1px solid #ebeef5;border-radius:8px;padding:20px}
.panel-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px}
.panel-head h2{margin:0;font-size:16px}
.panel-head .tag{font-size:12px;color:#64748b;background:#f1f5f9;padding:2px 8px;border-radius:999px}
.field-label{display:flex;align-items:center;gap:8px}
.field-label em{font-style:normal;font-size:12px;color:#94a3b8}
.dropzone{display:flex;flex-direction:column;gap:4px;border:1px dashed #dcdfe6;border-radius:6px;padding:14px;cursor:pointer;position:relative}
.dropzone.busy{opacity:.6;cursor:progress}
.dropzone strong{font-size:14px}
.dropzone span{font-size:13px;color:#64748b}
.dropzone small{font-size:12px;color:#94a3b8}
.file-input{position:absolute;inset:0;opacity:0;cursor:pointer}
.restore-btn{margin-top:10px}
.metadata{display:grid;grid-template-columns:1fr 1fr;gap:8px 16px;margin:14px 0 4px;color:#64748b;font-size:13px}
.metadata code{grid-column:1/-1;overflow-wrap:anywhere}
.field-fallback-hint{margin:14px 0 0;color:#94a3b8;font-size:13px}
.preview{position:relative;aspect-ratio:16/10;border-radius:8px;background:#173f3d center/cover;color:white;overflow:hidden}
.preview-shade{position:absolute;inset:0;background:rgba(9,31,31,.72)}
.preview-copy{position:absolute;left:0;right:0;bottom:0;padding:28px}
.preview-copy small{display:block;margin:0 0 8px;color:#a7f3d0}
.preview-copy h2{margin:0 0 10px;font-size:28px;overflow-wrap:anywhere}
.preview-copy strong{display:block;margin-bottom:8px;color:#a7f3d0}
.preview-copy p{color:#d1fae5;line-height:1.6;margin:0}
.preview-summary{display:grid;grid-template-columns:1fr 1fr;gap:10px 16px;margin:18px 0;font-size:13px}
.preview-summary dt{color:#94a3b8}
.preview-summary dd{margin:2px 0 0;color:#1e293b;font-weight:500}
.preview-note{margin-top:4px}
@media(max-width:900px){.brand-grid{grid-template-columns:1fr}}
@media(max-width:640px){.page-heading{flex-direction:column}.heading-actions{width:100%}.heading-actions .el-button{flex:1}.metadata,.preview-summary{grid-template-columns:1fr}}
</style>
