<template>
  <section class="generation-panel">
    <template v-if="mode === 'create'">
      <h3>{{ t('documentCenter.useStoredData') }}</h3>
      <p>{{ t('documentCenter.importNotice') }}</p>
      <form @submit.prevent="findSources">
        <label for="document-session">{{ t('documentCenter.sourceConversation') }}</label>
        <div class="session-input">
          <t-input id="document-session" v-model="sessionId" :placeholder="t('documentCenter.sessionPlaceholder')"
            :disabled="busy" />
            <t-button type="submit" :loading="searching" :disabled="busy">{{ t('documentCenter.findSources') }}</t-button>
        </div>
      </form>
      <p v-if="error" role="alert" class="operation-error">{{ error }}</p>
      <p v-if="searched && !sources.length && !error">{{ t('documentCenter.noSources') }}</p>
      <article v-for="source in sources" :key="`${source.message_id}:${source.result_id}`" class="source-card">
        <p>{{ t('documentCenter.sourceShape', { rows: source.row_count, columns: source.column_count }) }}</p>
        <p>{{ source.partial ? t('documentCenter.partialNotice') : t('documentCenter.completeNotice') }}</p>
        <t-button :disabled="busy" :loading="importing" @click="selectSource(source)">
          {{ t('documentCenter.selectData') }}
        </t-button>
      </article>
      <article v-if="dataset" class="source-card selected-source">
        <h4>{{ t('documentCenter.selectedData') }}</h4>
        <p>{{ t('documentCenter.sourceShape', { rows: dataset.row_count, columns: dataset.column_count }) }}</p>
        <label>{{ t('documentCenter.chooseTemplate') }}
          <t-select v-model="templateId" :options="templateOptions" :disabled="busy || uncertain || !!result"
            @change="selectTemplate" />
        </label>
        <p v-if="templateId !== 'engineering_bom_v1'">{{ t('documentCenter.bindingNotice') }}</p>
        <p v-if="templateError" role="alert" class="operation-error">{{ templateError }}</p>
        <label v-for="field in templateLayout?.fields || []" :key="field.key" class="binding-field">
          {{ field.label }} ({{ field.column }})
          <t-select v-model="bindings[field.key]" :disabled="busy || uncertain || !!result"
            :options="(dataset.fields || []).map(f => ({ value: f.id, label: `${f.title} [${f.id}]` }))" />
        </label>
        <p>{{ t('documentCenter.columnPolicy') }}</p>
        <p>{{ t('documentCenter.snapshotNotice') }}</p>
        <ul><li v-for="issue in dataset.issues" :key="issue">{{ issue }}</li></ul>
        <t-checkbox v-if="dataset.partial" v-model="acceptPartial" :disabled="busy || uncertain || !!result">
          {{ t('documentCenter.acceptPartial') }}
        </t-checkbox>
        <div class="generate-actions">
          <t-button :loading="generating" :disabled="busy || !bindingsReady || (dataset.partial && !acceptPartial)" @click="generate">
            {{ uncertain ? t('documentCenter.retrySameRequest')
              : dataset.partial ? t('documentCenter.generatePartial') : t('documentCenter.generate') }}
          </t-button>
        </div>
        <p v-if="uncertain">{{ t('documentCenter.uncertainNotice') }}</p>
      </article>
      <article v-if="result" class="source-card" aria-live="polite">
        <p>{{ result.file_name }}</p>
        <p>{{ statusText(result.status) }}</p>
        <t-button v-if="result.status === 'completed'" :loading="downloading" @click="download(result)">
          {{ t('artifactLibrary.download') }}
        </t-button>
      </article>
    </template>
    <template v-else>
      <div class="task-toolbar">
        <p>{{ t('documentCenter.taskScope') }}</p>
        <t-button variant="outline" :loading="loadingTasks" @click="reloadTasks">
          {{ t('documentCenter.retry') }}
        </t-button>
      </div>
      <p v-if="error" role="alert" class="operation-error">{{ error }}</p>
      <p v-if="!loadingTasks && !tasks.length && !error">{{ t('documentCenter.noTasks') }}</p>
      <article v-for="task in tasks" :key="task.id" class="source-card" :class="{ selected: task.id === route.query.task }">
        <h4>{{ task.file_name }}</h4>
        <p>{{ statusText(task.status) }} · {{ formatArtifactDateTime(task.created_at) }}</p>
        <p v-if="task.partial">{{ t('documentCenter.partial') }}</p>
        <p v-if="task.error_code">{{ t('documentCenter.taskFailure', { code: task.error_code }) }}</p>
        <t-button v-if="task.status === 'completed'" variant="outline" :loading="downloading" @click="download(task)">
          {{ t('artifactLibrary.download') }}
        </t-button>
        <p v-if="task.status === 'interrupted'">{{ t('documentCenter.interruptedNotice') }}</p>
      </article>
      <t-button v-if="tasks.length < total" variant="text" :loading="loadingTasks" @click="loadNextTasks">
        {{ t('artifactLibrary.loadMore') }}
      </t-button>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { formatArtifactDateTime } from '@/utils/sessionArtifacts'
import { newGenerationRequestKey } from '@/config/documentCenter'
import { listDocumentSources, importDocumentDataset, generateDocument, listGenerationTasks, downloadGenerationFile,
  listCustomTemplates, getTemplateLayout, type CustomTemplate, type TemplateLayout,
  type DocumentSource, type DocumentDataset, type GenerationTask } from '@/api/documentCenter'

const props = defineProps<{ mode: 'create' | 'tasks'; customTemplatesAvailable?: boolean; templateRevision?: number }>()
const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
const sessionId = ref('')
const sources = ref<DocumentSource[]>([])
const dataset = ref<DocumentDataset | null>(null)
const result = ref<GenerationTask | null>(null)
const acceptPartial = ref(false)
const searched = ref(false)
const searching = ref(false)
const importing = ref(false)
const generating = ref(false)
const downloading = ref(false)
const loadingTasks = ref(false)
const uncertain = ref(false)
const error = ref('')
const tasks = ref<GenerationTask[]>([])
const total = ref(0)
const customTemplates = ref<CustomTemplate[]>([])
const templateId = ref('engineering_bom_v1')
const templateLayout = ref<TemplateLayout | null>(null)
const bindings = ref<Record<string, string>>({})
const templateError = ref('')
let layoutTicket = 0
let templateTicket = 0
const templateOptions = computed(() => [
  { value: 'engineering_bom_v1', label: `${t('documentCenter.templateName')} · v1` },
  ...customTemplates.value.map(v => ({ value: v.id, label: `${v.name} · v${v.version}` })),
])
const bindingsReady = computed(() => templateId.value === 'engineering_bom_v1' ||
  (!!templateLayout.value?.fields.length && templateLayout.value.fields.every(f =>
    dataset.value?.fields?.some(source => source.id === bindings.value[f.key]))))
let taskPage = 0
let scope = 0
let requestKey = ''
let requestAccept = false
let timer: ReturnType<typeof setInterval> | undefined
const busy = computed(() => searching.value || importing.value || generating.value)
const statusText = (status: string) => t(`documentCenter.status.${[
  'completed', 'failed', 'generating', 'file_deleted', 'unavailable', 'interrupted',
].includes(status) ? status : 'unavailable'}`)

function reset() {
  scope++
  sources.value = []; dataset.value = null; result.value = null; tasks.value = []
  total.value = 0; taskPage = 0; searched.value = false; acceptPartial.value = false
  error.value = ''; requestKey = ''; uncertain.value = false
  layoutTicket++; templateTicket++; customTemplates.value = []; templateId.value = 'engineering_bom_v1'
  templateLayout.value = null; bindings.value = {}; templateError.value = ''
  searching.value = false; importing.value = false; generating.value = false; loadingTasks.value = false
}

watch(() => [auth.currentUserId, auth.effectiveTenantId], () => {
  reset()
  void loadTemplates()
  if (props.mode === 'tasks') void reloadTasks()
})
watch(() => [props.customTemplatesAvailable, props.templateRevision], loadTemplates)

async function loadTemplates() {
  if (props.mode !== 'create' || !props.customTemplatesAvailable) return
  const mine = scope; const ticket = ++templateTicket
  try {
    const r = await listCustomTemplates()
    if (mine === scope && ticket === templateTicket) customTemplates.value = r.data.filter(v => v.status === 'published')
  } catch { if (mine === scope && ticket === templateTicket) templateError.value = t('documentCenter.templateOperationFailed') }
}
async function selectTemplate() {
  const ticket = ++layoutTicket; const mine = scope
  bindings.value = {}; templateLayout.value = null; templateError.value = ''
  requestKey = ''; result.value = null
  if (templateId.value === 'engineering_bom_v1') return
  try {
    const r = await getTemplateLayout(templateId.value)
    if (ticket === layoutTicket && mine === scope) templateLayout.value = r.data
  } catch { if (ticket === layoutTicket && mine === scope) templateError.value = t('documentCenter.templateOperationFailed') }
}
watch(sessionId, () => {
  scope++
  dataset.value = null; sources.value = []; result.value = null; searched.value = false
  requestKey = ''; uncertain.value = false; acceptPartial.value = false
  searching.value = false; importing.value = false; generating.value = false
})

async function findSources() {
  if (busy.value) return
  error.value = ''
  let id = sessionId.value.trim()
  // Accept a copied conversation URL as well as its identifier.
  const match = id.match(/(?:^|\/chat\/)([a-f\d-]{36})(?:[?#].*)?$/i)
  if (!match) { error.value = t('documentCenter.invalidSession'); return }
  id = match[1]!
  const mine = scope
  dataset.value = null; result.value = null; acceptPartial.value = false; requestKey = ''; uncertain.value = false
  searching.value = true
  try {
    const response = await listDocumentSources(id)
    if (mine !== scope) return
    sources.value = response.data || []; searched.value = true
  } catch { if (mine === scope) error.value = t('documentCenter.sourceFailed') }
  finally { if (mine === scope) searching.value = false }
}

async function selectSource(source: DocumentSource) {
  if (busy.value) return
  const mine = scope
  importing.value = true; error.value = ''
  try {
    const response = await importDocumentDataset(source)
    if (mine !== scope) return
    dataset.value = response.data; acceptPartial.value = false; result.value = null
    bindings.value = {}
    requestKey = ''; uncertain.value = false
  } catch { if (mine === scope) error.value = t('documentCenter.sourceFailed') }
  finally { if (mine === scope) importing.value = false }
}

async function generate() {
  if (busy.value || !dataset.value || !bindingsReady.value || (dataset.value.partial && !acceptPartial.value)) return
  const mine = scope
  // Preserve the request key on network failure: retry cannot duplicate a file.
  if (!requestKey) {
    requestKey = newGenerationRequestKey()
    requestAccept = acceptPartial.value
  }
  generating.value = true; error.value = ''
  try {
    const response = templateId.value === 'engineering_bom_v1'
      ? await generateDocument(dataset.value.id, requestKey, requestAccept)
      : await generateDocument(dataset.value.id, requestKey, requestAccept, templateId.value, { ...bindings.value })
    if (mine !== scope) return
    result.value = response.data; uncertain.value = false
  } catch {
    if (mine === scope) { uncertain.value = true; error.value = t('documentCenter.generateFailed') }
  } finally { if (mine === scope) generating.value = false }
}

async function loadNextTasks() {
  if (loadingTasks.value) return
  const mine = scope
  loadingTasks.value = true; error.value = ''
  try {
    const response = await listGenerationTasks(taskPage + 1)
    if (mine !== scope) return
    tasks.value = taskPage ? [...tasks.value, ...response.data] : response.data
    total.value = response.total; taskPage++
  } catch { if (mine === scope) error.value = t('documentCenter.tasksFailed') }
  finally { if (mine === scope) loadingTasks.value = false }
}
async function reloadTasks() {
  if (loadingTasks.value) return
  taskPage = 0
  await loadNextTasks()
}

async function download(task: GenerationTask) {
  if (downloading.value) return
  downloading.value = true
  const mine = scope
  try {
    const blob = await downloadGenerationFile(task.id)
    if (mine !== scope) return
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a'); a.href = url; a.download = task.file_name
    document.body.appendChild(a); a.click(); a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch { MessagePlugin.error(t('artifactLibrary.downloadFailed')) }
  finally { downloading.value = false }
}

onMounted(() => {
  void loadTemplates()
  if (props.mode === 'tasks') void reloadTasks()
  timer = setInterval(() => {
    if (props.mode === 'tasks' && tasks.value.some(task => task.status === 'generating')) void reloadTasks()
  }, 5000)
})
onBeforeUnmount(() => { scope++; if (timer) clearInterval(timer) })
</script>

<style scoped>
.generation-panel { padding: 24px 32px; overflow: auto; min-height: 0; }
.session-input, .task-toolbar { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.session-input { margin-top: 8px; }
.session-input :deep(.t-input__wrap) { flex: 1; min-width: 200px; }
.source-card { padding: 16px; margin: 16px 0; border: 1px solid var(--td-component-border); border-radius: var(--app-radius-md); }
.source-card h4 { margin: 0; overflow-wrap: anywhere; }
.selected, .selected-source { border-color: var(--td-brand-color); }
.operation-error { color: var(--td-error-color); }
.generate-actions { margin-top: 12px; }
.binding-field { display: block; margin-top: 12px; }
@media(max-width:600px) { .generation-panel { padding: 16px; } }
</style>
