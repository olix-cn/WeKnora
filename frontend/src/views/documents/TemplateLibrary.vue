<template>
  <section class="template-library">
    <header class="template-toolbar">
      <h3>{{ t('documentCenter.customTemplates') }}</h3>
      <t-button variant="outline" :loading="loading" @click="load">{{ t('documentCenter.retry') }}</t-button>
    </header>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <div class="published-grid">
      <article v-for="version in publishedVersions" :key="version.id" class="published-card">
        <div class="template-emblem" aria-hidden="true">XLSX</div>
        <div class="published-info">
          <h4>{{ version.name }}</h4>
          <p><span class="version-badge">v{{ version.version }}</span> {{ t('documentCenter.templateStatus.published') }}</p>
        </div>
        <div class="published-actions">
          <t-button variant="outline" :disabled="busy || previewLoading" @click="openPreview(version)">
            {{ t('documentCenter.previewTemplate') }}
          </t-button>
          <t-button v-if="version.can_manage" variant="text" theme="danger" :disabled="busy"
            @click="disable(version)">{{ t('documentCenter.disableTemplate') }}</t-button>
        </div>
      </article>
    </div>
    <p v-if="!loading && !publishedVersions.length">{{ t('documentCenter.noPublishedTemplates') }}</p>
    <t-drawer v-model:visible="previewVisible" :header="previewTitle" size="75%" :footer="false"
      :close-btn="true" :close-on-esc-keydown="true" @close="closePreview">
      <t-loading v-if="previewLoading" />
      <div v-else-if="previewBlob" class="drawer-preview">
        <DocumentPreview :source-blob="previewBlob" file-type="xlsx" :file-name="`${previewTitle}.xlsx`" active fill-height />
      </div>
    </t-drawer>
    <form v-if="canManage" class="template-upload" @submit.prevent="upload">
      <h4>{{ t('documentCenter.addWorkspaceTemplate') }}</h4>
      <p>{{ t('documentCenter.templateLimits') }}</p>
      <label>{{ t('documentCenter.templateNameLabel') }}
        <t-input v-model="name" :maxlength="120" :disabled="busy" />
      </label>
      <label>{{ t('documentCenter.baseVersion') }}
        <t-select v-model="baseVersion" :disabled="busy" clearable
          :options="versions.filter(v => v.can_manage && v.status === 'published').map(v => ({ value: v.id, label: `${v.name} v${v.version}` }))" />
      </label>
      <div class="file-picker">
        <input ref="fileInput" type="file" accept=".xlsx" :disabled="busy" class="native-file-input" tabindex="-1" @change="chooseFile" />
        <t-button variant="outline" :disabled="busy" @click="fileInput?.click()">
          <template #icon><UploadIcon /></template>{{ t('documentCenter.chooseExcelFile') }}
        </t-button>
        <span>{{ file?.name || t('documentCenter.noFileChosen') }}</span>
      </div>
      <t-button type="submit" :disabled="busy || !file || !name.trim()" :loading="uploading">
        {{ t('documentCenter.uploadTemplate') }}
      </t-button>
    </form>
    <p v-if="!loading && !versions.length">{{ t('documentCenter.noCustomTemplates') }}</p>
    <h4 v-if="managedDrafts.length" class="draft-heading">{{ t('documentCenter.draftsAndDisabled') }}</h4>
    <article v-for="version in managedDrafts" :key="version.id" class="version-card">
      <h4>{{ version.name }} · v{{ version.version }}</h4>
      <p>{{ t(`documentCenter.templateStatus.${version.status}`) }}</p>
      <div v-if="version.can_manage" class="template-actions">
        <t-button v-if="['draft', 'configured', 'tested'].includes(version.status)" variant="outline"
          :disabled="busy" @click="edit(version)">{{ t('documentCenter.configureTemplate') }}</t-button>
        <t-button v-if="version.status === 'published'" variant="outline" :disabled="busy"
          @click="disable(version)">{{ t('documentCenter.disableTemplate') }}</t-button>
      </div>
    </article>
    <section v-if="editing && canManage" class="template-editor">
      <h3>{{ editing.name }} · v{{ editing.version }}</h3>
      <ol class="setup-steps">
        <li :class="{ done: saved }">{{ t('documentCenter.confirmHeaderStep') }}</li>
        <li :class="{ done: tested }">{{ t('documentCenter.checkTrialStep') }}</li>
        <li>{{ t('documentCenter.publishStep') }}</li>
      </ol>
      <p>{{ t('documentCenter.guidedInstructions') }}</p>
      <p v-if="localPreview">{{ t('documentCenter.privateUploadPreview') }}</p>
      <div v-if="localPreview" class="workbook-preview">
        <DocumentPreview :source-blob="localPreview" file-type="xlsx" :file-name="file?.name || 'template.xlsx'" active fill-height />
      </div>
      <div v-if="inspection" class="guided-setup">
        <label>{{ t('documentCenter.chooseWorksheet') }}
          <t-select :model-value="layout.sheet" :disabled="busy" :options="inspection.sheets.map(s => ({ value: s, label: s }))"
            @change="chooseSheet" />
        </label>
        <p>{{ t('documentCenter.chooseHeaderHint') }}</p>
        <div class="header-choices">
          <button v-for="candidate in sheetCandidates" :key="candidate.header_row" type="button"
            :disabled="busy" :aria-pressed="layout.header_row === candidate.header_row"
            :class="{ selected: layout.header_row === candidate.header_row }" @click="applyCandidate(candidate)">
            <strong>{{ t('documentCenter.headerChoice', { row: candidate.header_row }) }}</strong>
            <span>{{ candidate.fields.map(f => f.label).join(' · ') }}</span>
          </button>
        </div>
        <p v-if="!sheetCandidates.length">{{ t('documentCenter.noHeaderCandidates') }}</p>
        <div v-if="currentCandidate" class="recognized-columns">
          <p>{{ t('documentCenter.recognizedColumns', { count: layout.fields.length, row: layout.start_row }) }}</p>
          <t-checkbox v-for="field in currentCandidate.fields" :key="field.key"
            :checked="layout.fields.some(f => f.column === field.column)" :disabled="busy"
            @change="(checked: boolean) => toggleColumn(field, checked)">{{ field.label }}</t-checkbox>
        </div>
        <t-checkbox v-model="headerConfirmed" :disabled="busy || !layout.fields.length">
          {{ t('documentCenter.confirmDetectedHeader') }}
        </t-checkbox>
      </div>
      <details class="advanced-setup" :open="!inspection">
        <summary>{{ t('documentCenter.advancedCoordinates') }}</summary>
        <p>{{ t('documentCenter.layoutInstructions') }}</p>
        <label>{{ t('documentCenter.sheetName') }}<t-input v-model="layout.sheet" :disabled="busy" /></label>
      <label>{{ t('documentCenter.headerRow') }}
        <t-input-number v-model="layout.header_row" :min="1" :max="999" :disabled="busy" @change="syncStart" />
      </label>
      <p>{{ t('documentCenter.firstDataRow', { row: layout.header_row + 1 }) }}</p>
      <div class="field-bindings">
        <div v-for="(field, index) in layout.fields" :key="index" class="field-row">
          <t-input v-model="field.key" :placeholder="t('documentCenter.fieldKey')" :disabled="busy" />
          <t-input v-model="field.label" :placeholder="t('documentCenter.exactHeader')" :disabled="busy" />
          <t-input v-model="field.column" :placeholder="t('documentCenter.excelColumn')" :disabled="busy" />
          <t-button variant="text" :disabled="busy || layout.fields.length === 1" @click="layout.fields.splice(index, 1)">
            {{ t('documentCenter.removeField') }}
          </t-button>
        </div>
      </div>
      <t-button variant="text" :disabled="busy || layout.fields.length >= 50" @click="addField">
        {{ t('documentCenter.addField') }}
      </t-button>
      </details>
      <div class="template-actions">
        <t-button :loading="busy" :disabled="busy || !layout.fields.length || (inspection && !headerConfirmed)"
          @click="confirmAndTest">{{ t('documentCenter.confirmAndTry') }}</t-button>
        <t-button variant="outline" :disabled="busy || !layout.fields.length || (inspection && !headerConfirmed)"
          @click="saveLayout">{{ t('documentCenter.saveDraftOnly') }}</t-button>
        <t-button :disabled="busy || !saved" @click="testLayout">{{ t('documentCenter.testLayout') }}</t-button>
      </div>
      <p>{{ t('documentCenter.testBeforePublish') }}</p>
      <t-checkbox v-model="sampleCleared" :disabled="busy || !tested">
        {{ t('documentCenter.sampleCleared') }}
      </t-checkbox>
      <div class="template-actions">
        <t-button :disabled="busy || !tested || !sampleCleared" @click="publish">
          {{ t('documentCenter.publishTemplate') }}
        </t-button>
        <t-button variant="text" :disabled="busy" @click="editing = null">{{ t('common.close') }}</t-button>
      </div>
    </section>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { UploadIcon } from 'tdesign-icons-vue-next'
import { useAuthStore } from '@/stores/auth'
import DocumentPreview from '@/components/document-preview.vue'
import { listCustomTemplates, uploadCustomTemplate, getTemplateLayout, configureCustomTemplate,
  inspectCustomTemplate, previewCustomTemplate, testCustomTemplate, publishCustomTemplate, disableCustomTemplate, type CustomTemplate,
  type TemplateInspection, type TemplateLayout } from '@/api/documentCenter'

const { t } = useI18n()
const emit = defineEmits<{ changed: [] }>()
const auth = useAuthStore()
const canManage = computed(() => auth.hasRole('admin'))
const versions = ref<CustomTemplate[]>([])
const publishedVersions = computed(() => versions.value.filter(v => v.status === 'published'))
const managedDrafts = computed(() => versions.value.filter(v => v.status !== 'published' && v.can_manage))
const previewVisible = ref(false)
const previewLoading = ref(false)
const previewBlob = ref<Blob | null>(null)
const previewTitle = ref('')
let previewTicket = 0
const loading = ref(false)
const busy = ref(false)
const uploading = ref(false)
const error = ref('')
const file = ref<File | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const localPreview = ref<Blob | null>(null)
const name = ref('')
const baseVersion = ref('')
const editing = ref<CustomTemplate | null>(null)
const inspection = ref<TemplateInspection | null>(null)
const headerConfirmed = ref(false)
const sheetCandidates = computed(() => inspection.value?.header_candidates?.filter(c => c.sheet === layout.value.sheet) || [])
const currentCandidate = computed(() => sheetCandidates.value.find(c => c.header_row === layout.value.header_row))
const layout = ref<TemplateLayout>({ sheet: '', header_row: 1, start_row: 2,
  fields: [{ key: 'part_number', label: '', column: 'A' }] })
const saved = ref(false)
const tested = ref(false)
const sampleCleared = ref(false)
let scope = 0
let listTicket = 0
let applying = false
const errorText = (e: unknown) => (e as { message?: string })?.message || t('documentCenter.templateOperationFailed')

watch(() => [auth.currentUserId, auth.effectiveTenantId], () => {
  closePreview()
  scope++; versions.value = []; editing.value = null; localPreview.value = null; file.value = null
  inspection.value = null; headerConfirmed.value = false
  name.value = ''; baseVersion.value = ''; saved.value = false; tested.value = false
  sampleCleared.value = false; error.value = ''; loading.value = false; busy.value = false; uploading.value = false
  if (fileInput.value) fileInput.value.value = ''
  void load()
})
function closePreview() { previewTicket++; previewVisible.value = false; previewLoading.value = false; previewBlob.value = null; previewTitle.value = '' }
async function openPreview(version: CustomTemplate) {
  closePreview()
  const ticket = ++previewTicket; const mine = scope
  previewTitle.value = `${version.name} · v${version.version}`; previewVisible.value = true; previewLoading.value = true
  try {
    const blob = await previewCustomTemplate(version.id)
    if (mine === scope && ticket === previewTicket) previewBlob.value = blob
  } catch (e) { if (mine === scope && ticket === previewTicket) { error.value = errorText(e); closePreview() } }
  finally { if (mine === scope && ticket === previewTicket) previewLoading.value = false }
}
watch(layout, () => {
  if (!applying) { saved.value = false; tested.value = false; sampleCleared.value = false; headerConfirmed.value = false }
}, { deep: true, flush: 'sync' })

async function load() {
  const mine = scope
  const ticket = ++listTicket
  loading.value = true
  try { const r = await listCustomTemplates(); if (mine === scope && ticket === listTicket) versions.value = r.data }
  catch (e) { if (mine === scope && ticket === listTicket) error.value = errorText(e) }
  finally { if (mine === scope && ticket === listTicket) loading.value = false }
}
function chooseFile(event: Event) {
  file.value = (event.target as HTMLInputElement).files?.[0] || null
  localPreview.value = null
  if (file.value && !name.value.trim()) name.value = file.value.name.replace(/\.xlsx$/i, '').slice(0, 120)
}
function applyCandidate(candidate: TemplateLayout) {
  layout.value = JSON.parse(JSON.stringify(candidate)) as TemplateLayout
  headerConfirmed.value = false
}
function chooseSheet(value: unknown) {
  const sheet = String(value)
  const candidates = inspection.value?.header_candidates?.filter(c => c.sheet === sheet) || []
  // Prefer more populated rows; retain the earliest on ties. Never save without confirmation.
  const best = candidates.reduce<TemplateLayout | undefined>((best, c) =>
    !best || c.fields.length > best.fields.length ? c : best, undefined)
  applyCandidate(best || { sheet, header_row: 1, start_row: 2, fields: [] })
}
function toggleColumn(field: TemplateLayout['fields'][number], checked: boolean) {
  const columns = new Set(layout.value.fields.map(f => f.column))
  if (checked) columns.add(field.column)
  else columns.delete(field.column)
  layout.value.fields = (currentCandidate.value?.fields || []).filter(f => columns.has(f.column)).map(f => ({ ...f }))
}
async function upload() {
  if (!canManage.value || busy.value || !file.value) return
  if (file.value.size > 10 * 1024 * 1024) { error.value = t('documentCenter.templateTooLarge'); return }
  const mine = scope
  busy.value = true; uploading.value = true; error.value = ''
  try {
    const r = await uploadCustomTemplate(name.value, file.value, baseVersion.value)
    if (mine !== scope) return
    editing.value = r.data; localPreview.value = file.value
    inspection.value = r.inspection
    applying = true
    layout.value = { sheet: r.inspection.sheets[0] || '', header_row: 1, start_row: 2,
      fields: [{ key: 'part_number', label: '', column: 'A' }] }
    applying = false; saved.value = false; tested.value = false; sampleCleared.value = false
    if (r.inspection.header_candidates) chooseSheet(r.inspection.sheets[0] || '')
    headerConfirmed.value = false
    await load()
  } catch (e) { if (mine === scope) error.value = errorText(e) }
  finally { if (mine === scope) { busy.value = false; uploading.value = false } }
}
async function edit(version: CustomTemplate) {
  if (busy.value) return
  const mine = scope
  busy.value = true; error.value = ''; localPreview.value = null
  try {
    let configured: TemplateLayout = { sheet: '', header_row: 1, start_row: 2,
      fields: [{ key: 'part_number', label: '', column: 'A' }] }
    const info = (await inspectCustomTemplate(version.id)).data
    if (mine !== scope) return
    if (version.status !== 'draft') configured = (await getTemplateLayout(version.id)).data
    if (mine !== scope) return
    editing.value = version; applying = true; layout.value = configured; applying = false
    inspection.value = info
    if (version.status === 'draft') chooseSheet(info.sheets[0] || '')
    headerConfirmed.value = version.status !== 'draft'
    saved.value = version.status !== 'draft'; tested.value = version.status === 'tested'; sampleCleared.value = false
  } catch (e) { if (mine === scope) error.value = errorText(e) }
  finally { if (mine === scope) busy.value = false }
}
function syncStart() { layout.value.start_row = layout.value.header_row + 1 }
function addField() { layout.value.fields.push({ key: `field_${layout.value.fields.length + 1}`, label: '', column: '' }) }
async function operation(action: () => Promise<unknown>) {
  if (busy.value) return
  const mine = scope
  busy.value = true; error.value = ''
  try { await action(); if (mine === scope) { await load(); emit('changed') } }
  catch (e) { if (mine === scope) error.value = errorText(e) }
  finally { if (mine === scope) busy.value = false }
}
async function saveLayout() {
  if (!editing.value || (inspection.value && !headerConfirmed.value)) return
  const id = editing.value.id
  const mine = scope
  const config = JSON.parse(JSON.stringify(layout.value)) as TemplateLayout
  config.start_row = config.header_row + 1
  await operation(async () => {
    await configureCustomTemplate(id, config)
    if (mine === scope) { saved.value = true; tested.value = false; sampleCleared.value = false }
  })
}
function downloadTrial(blob: Blob) {
  const url = URL.createObjectURL(blob); const a = document.createElement('a')
  a.href = url; a.download = 'template-synthetic-test.xlsx'; document.body.appendChild(a); a.click(); a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
async function confirmAndTest() {
  if (!editing.value || !layout.value.fields.length || (inspection.value && !headerConfirmed.value)) return
  const id = editing.value.id; const mine = scope
  const config = JSON.parse(JSON.stringify(layout.value)) as TemplateLayout
  config.start_row = config.header_row + 1
  await operation(async () => {
    await configureCustomTemplate(id, config)
    if (mine !== scope) return
    saved.value = true; tested.value = false; sampleCleared.value = false
    const blob = await testCustomTemplate(id)
    if (mine !== scope) return
    downloadTrial(blob); tested.value = true
  })
}
async function testLayout() {
  if (!editing.value || !saved.value) return
  const id = editing.value.id; const mine = scope
  await operation(async () => {
    const blob = await testCustomTemplate(id)
    if (mine !== scope) return
    downloadTrial(blob); tested.value = true
  })
}
async function publish() {
  if (!editing.value || !tested.value || !sampleCleared.value) return
  const id = editing.value.id; const mine = scope
  await operation(async () => { await publishCustomTemplate(id); if (mine === scope) editing.value = null })
}
async function disable(version: CustomTemplate) {
  if (!window.confirm(t('documentCenter.disableConfirm'))) return
  await operation(() => disableCustomTemplate(version.id))
}
onMounted(load)
onBeforeUnmount(() => { scope++; closePreview() })
</script>

<style scoped>
.template-library { margin-top: 24px; }
.template-toolbar, .template-actions, .field-row { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
.template-upload, .template-editor { padding: 16px; border: 1px solid var(--td-component-border); border-radius: var(--app-radius-md); }
.template-upload label, .template-editor label { display: block; margin-bottom: 12px; }
.version-card { padding: 12px 0; border-bottom: 1px solid var(--td-component-border); }
.version-card h4 { margin: 0; overflow-wrap: anywhere; }
.field-row { margin-bottom: 8px; }
.field-row :deep(.t-input__wrap) { flex: 1; min-width: 120px; }
.workbook-preview { height: 360px; margin-bottom: 16px; }
.template-actions { margin-top: 12px; }
.error { color: var(--td-error-color); white-space: pre-wrap; }
.setup-steps { display: flex; flex-wrap: wrap; gap: 12px 32px; padding-left: 24px; }
.setup-steps .done { color: var(--td-success-color); }
.header-choices { display: grid; gap: 8px; margin: 12px 0; }
.header-choices button { display: flex; flex-direction: column; gap: 6px; text-align: left; padding: 12px;
  background: var(--td-bg-color-container); color: var(--td-text-color-primary); cursor: pointer;
  border: 1px solid var(--td-component-border); border-radius: var(--app-radius-md); overflow-wrap: anywhere; }
.header-choices button.selected { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
.recognized-columns :deep(.t-checkbox) { margin: 0 16px 12px 0; }
.advanced-setup { margin: 16px 0; }
.advanced-setup summary { cursor: pointer; margin-bottom: 12px; }
.published-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 360px), 1fr)); gap: 16px; margin: 16px 0 24px; }
.published-card { display: flex; gap: 16px; align-items: center; padding: 20px; border: 1px solid var(--td-component-border);
  border-radius: var(--app-radius-md); background: var(--td-bg-color-container); }
.template-emblem { display: grid; place-items: center; width: 56px; height: 64px; flex-shrink: 0; font-weight: 700; font-size: var(--td-font-size-body-small);
  color: var(--td-success-color); background: var(--td-success-color-light); border-radius: var(--app-radius-md); }
.published-info { flex: 1; min-width: 0; }
.published-info h4 { margin: 0; overflow-wrap: anywhere; font-size: var(--td-font-size-title-medium); }
.published-info p { margin: 8px 0; color: var(--td-success-color); }
.published-info small { color: var(--td-text-color-secondary); }
.version-badge { margin-right: 8px; padding: 2px 8px; background: var(--td-bg-color-secondarycontainer); border-radius: var(--app-radius-md); }
.published-actions { display: flex; flex-direction: column; gap: 8px; }
.file-picker { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin: 16px 0; }
.file-picker span { overflow-wrap: anywhere; color: var(--td-text-color-secondary); }
.native-file-input { display: none; }
.drawer-preview { height: calc(100vh - 120px); }
.draft-heading { margin-top: 24px; }
</style>
