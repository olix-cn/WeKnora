<template>
  <main class="document-center">
    <header class="document-center-header">
      <h2><ResourceIcon type="artifact" :size="24" />{{ t('documentCenter.title') }}</h2>
      <p>{{ t('documentCenter.subtitle') }}</p>
    </header>
    <nav class="document-center-tabs" :aria-label="t('documentCenter.title')">
      <button v-for="tab in DOCUMENT_CENTER_TABS" :key="tab" type="button"
        :aria-current="activeTab === tab ? 'page' : undefined" :class="{ active: activeTab === tab }"
        @click="selectTab(tab)">{{ t(`documentCenter.tabs.${tab}`) }}</button>
    </nav>
    <!-- Keep the existing library and its filters/download ownership checks. -->
    <ArtifactLibrary v-if="activeTab === 'files'" :key="workspaceKey" embedded />
    <section v-else-if="activeTab === 'templates'" class="document-center-content">
      <p v-if="!loading && !loadFailed && !customTemplatesAvailable" class="availability-note">{{ t('documentCenter.notAvailable') }}</p>
      <t-loading v-if="loading" />
      <EmptyState v-else-if="loadFailed" icon="error-circle" :title="t('documentCenter.failed')">
        <t-button variant="outline" @click="loadCatalog">{{ t('documentCenter.retry') }}</t-button>
      </EmptyState>
      <TemplateLibrary v-if="customTemplatesAvailable" :key="workspaceKey" @changed="templateRevision++" />
    </section>
    <section v-else-if="loadFailed" class="document-center-content">
      <EmptyState icon="error-circle" :title="t('documentCenter.failed')">
        <t-button variant="outline" @click="loadCatalog">{{ t('documentCenter.retry') }}</t-button>
      </EmptyState>
    </section>
    <t-loading v-else-if="loading" />
    <GenerationPanel v-else-if="generationAvailable" mode="tasks" />
    <EmptyState v-else icon="time" :title="t('documentCenter.tabs.tasks')"
      :description="t('documentCenter.tasksNotice')" />
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import ArtifactLibrary from '@/views/artifacts/ArtifactLibrary.vue'
import EmptyState from '@/components/EmptyState.vue'
import ResourceIcon from '@/components/icons/ResourceIcon.vue'
import GenerationPanel from './GenerationPanel.vue'
import TemplateLibrary from './TemplateLibrary.vue'
import { getDocumentTemplates } from '@/api/documentCenter'
import { DOCUMENT_CENTER_TABS, parseDocumentCenterTab, parseDocumentCatalog,
  type DocumentCenterTab, type DocumentTemplate } from '@/config/documentCenter'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const workspaceKey = computed(() => `${auth.currentUserId}:${auth.effectiveTenantId}`)
const activeTab = computed(() => parseDocumentCenterTab(route.query.tab))
const templates = ref<DocumentTemplate[]>([])
const loading = ref(false)
const loadFailed = ref(false)
let loaded = false
let catalogTicket = 0
const generationAvailable = ref(false)
const customTemplatesAvailable = ref(false)
const templateRevision = ref(0)

function selectTab(tab: DocumentCenterTab) {
  const query = { ...route.query }
  if (tab === 'files') delete query.tab
  else query.tab = tab
  void router.push({ query, hash: route.hash })
}

async function loadCatalog() {
  if (loading.value) return
  const ticket = ++catalogTicket
  loading.value = true
  loadFailed.value = false
  try {
    const response = await getDocumentTemplates()
    if (ticket !== catalogTicket) return
    if (response.success !== true) throw new Error('Unavailable catalog')
    templates.value = parseDocumentCatalog(response.data)
    generationAvailable.value = !!response.data && typeof response.data === 'object'
      && (response.data as { generation_available?: unknown }).generation_available === true
    customTemplatesAvailable.value = !!response.data && typeof response.data === 'object'
      && (response.data as { custom_templates_available?: unknown }).custom_templates_available === true
    loaded = true
  } catch {
    if (ticket !== catalogTicket) return
    templates.value = []
    generationAvailable.value = false
    customTemplatesAvailable.value = false
    loadFailed.value = true
  } finally {
    if (ticket === catalogTicket) loading.value = false
  }
}

watch(activeTab, (tab) => {
  if (tab !== 'files' && !loaded) void loadCatalog()
})
watch(workspaceKey, () => {
  catalogTicket++
  templates.value = []; generationAvailable.value = false; customTemplatesAvailable.value = false; loaded = false
  loading.value = false; loadFailed.value = false
  if (activeTab.value !== 'files') void loadCatalog()
})
onMounted(() => {
  if (activeTab.value !== 'files') void loadCatalog()
})
</script>

<style scoped lang="less">
.document-center { display: flex; flex: 1; flex-direction: column; height: 100%; min-height: 0; min-width: 0; }
.document-center-header { padding: 24px 32px 12px; }
.document-center-header h2 { display: flex; align-items: center; gap: 10px; margin: 0; }
.document-center-header p { color: var(--td-text-color-secondary); }
.document-center-tabs { display: flex; gap: 8px; padding: 0 32px 12px; border-bottom: 1px solid var(--td-component-border); }
.document-center-tabs button { background: none; border: 0; border-radius: 6px; padding: 10px 16px; cursor: pointer;
  color: var(--td-text-color-secondary); font: inherit; }
.document-center-tabs button.active { color: var(--td-brand-color); background: var(--td-brand-color-light); }
.document-center-content { padding: 24px 32px; overflow: auto; }
.availability-note, .sample-note { color: var(--td-text-color-secondary); }
.template-card { max-width: 560px; padding: 24px; border: 1px solid var(--td-component-border); border-radius: 10px; }
.template-card h3 { margin-top: 0; }
@media (max-width: 600px) {
  .document-center-header, .document-center-content { padding: 16px; }
  .document-center-tabs { padding: 0 12px 12px; gap: 0; }
  .document-center-tabs button { padding: 10px 12px; }
}
</style>
