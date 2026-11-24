<template>
  <section class="kb-permissions-center">
    <header class="permissions-heading"><div><h2>{{ text.title }}</h2><p>{{ text.description }}</p></div>
      <t-button variant="outline" :disabled="loading || editorVisible" @click="load(0)">{{ text.refresh }}</t-button>
    </header>
    <div class="permissions-toolbar">
      <t-input v-model="keyword" clearable :placeholder="text.search" :aria-label="text.search" />
      <span>{{ text.total }} {{ total }}</span>
    </div>
    <t-alert v-if="error" theme="error" :message="error" />
    <t-table row-key="id" :data="rows" :columns="columns" :loading="loading" table-layout="fixed" hover>
      <template #library="{ row }"><div class="library-cell"><strong>{{ row.name }}</strong><small>{{ row.id }}</small></div></template>
      <template #mode="{ row }"><t-tag :theme="row.mode === 'restricted' ? 'warning' : 'success'" variant="light">{{ row.mode === 'restricted' ? text.restricted : text.inherit }}</t-tag></template>
      <template #actions="{ row }"><t-button variant="text" @click="openEditor(row)">{{ text.configure }}</t-button></template>
    </t-table>
    <div class="permissions-pager"><t-button variant="text" :disabled="!offset || loading" @click="load(offset - 20)">{{ text.previous }}</t-button>
      <span>{{ Math.floor(offset / 20) + 1 }}</span><t-button variant="text" :disabled="offset + rows.length >= total || loading" @click="load(offset + 20)">{{ text.next }}</t-button></div>
    <t-dialog :visible="editorVisible" :header="selected?.name" width="min(1060px, calc(100vw - 48px))" :footer="false" :close-btn="!editorSaving"
      :close-on-overlay-click="false" :close-on-esc-keydown="false" :on-close="closeEditor" destroy-on-close>
      <template v-if="editorVisible && selected">
        <div class="permission-library-context"><span>{{ text.workspace }}：{{ selected.tenant_name }}</span><span>{{ text.version }}：{{ selected.version }}</span></div>
        <KBMemberAccessPanel :key="selected.id" :kb-id="selected.id" :creator-id="selected.creator_id" platform
          @saving="editorSaving = $event" @dirty="editorDirty = $event" @saved="saved" />
      </template>
    </t-dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import KBMemberAccessPanel from '../knowledge/components/KBMemberAccessPanel.vue'
import { listKBPermissions, type PlatformKBPermissionRow } from '@/api/system/kbPermissions'
const emit = defineEmits<{ dirty: [boolean]; saving: [boolean] }>()
const { locale } = useI18n()
const text = computed(() => locale.value.startsWith('zh') ? {
  title: '知识库权限', description: '统一管理全部空间的知识库访问范围。此页面仅配置授权，不授予管理员资料读取权限。',
  search: '搜索知识库名称或所属空间', total: '知识库总数', refresh: '刷新', library: '知识库', workspace: '所属空间',
  inherit: '继承空间权限', restricted: '指定成员', members: '显式授权人数', mode: '访问范围', configure: '配置权限',
  previous: '上一页', next: '下一页', failed: '无法加载知识库权限，请重试。', discard: '有未保存的权限修改，确认放弃？', version: '策略版本',
} : {
  title: 'Knowledge-base permissions', description: 'Manage access policies across workspaces. Policy administration does not grant document access.',
  search: 'Search knowledge base or workspace', total: 'Knowledge bases', refresh: 'Refresh', library: 'Knowledge base', workspace: 'Workspace',
  inherit: 'Workspace permissions', restricted: 'Selected members', members: 'Explicit grants', mode: 'Access scope', configure: 'Configure',
  previous: 'Previous', next: 'Next', failed: 'Unable to load policies. Retry.', discard: 'Discard unsaved permission changes?', version: 'Policy version',
})
const columns = computed(() => [
  { colKey: 'library', title: text.value.library, minWidth: 240 }, { colKey: 'tenant_name', title: text.value.workspace, width: 180, ellipsis: true },
  { colKey: 'mode', title: text.value.mode, width: 170 }, { colKey: 'member_count', title: text.value.members, width: 140 },
  { colKey: 'actions', title: '', width: 120 },
])
const rows = ref<PlatformKBPermissionRow[]>([]), selected = ref<PlatformKBPermissionRow | null>(null)
const keyword = ref(''), error = ref(''), offset = ref(0), total = ref(0), loading = ref(false)
const editorVisible = ref(false), editorDirty = ref(false), editorSaving = ref(false)
watch(editorDirty, value => emit('dirty', value))
watch(editorSaving, value => emit('saving', value))
let sequence = 0, timer: ReturnType<typeof setTimeout> | undefined
async function load(nextOffset = 0) {
  const n = ++sequence; loading.value = true; error.value = ''
  try { const data = await listKBPermissions(keyword.value.trim(), nextOffset); if (n !== sequence) return
    rows.value = data.data; total.value = data.total; offset.value = nextOffset
  } catch { if (n === sequence) error.value = text.value.failed } finally { if (n === sequence) loading.value = false }
}
function openEditor(row: PlatformKBPermissionRow) { selected.value = row; editorDirty.value = false; editorVisible.value = true }
function closeEditor() { if (editorSaving.value) return; if (editorDirty.value && !window.confirm(text.value.discard)) return; editorVisible.value = false; selected.value = null; editorDirty.value = false }
function saved() { editorDirty.value = false; editorVisible.value = false; selected.value = null; void load(offset.value) }
watch(keyword, () => { sequence++; clearTimeout(timer); timer = setTimeout(() => load(0), 250) })
void load()
onBeforeUnmount(() => { sequence++; clearTimeout(timer); emit('dirty', false); emit('saving', false) })
</script>
<style scoped lang="less">
.permissions-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:20px; margin-bottom:24px; h2 { margin:0 0 8px; } p { margin:0; color:var(--td-text-color-secondary); } }
.permissions-toolbar { display:flex; align-items:center; gap:20px; margin-bottom:16px; .t-input__wrap { max-width:420px; } span { white-space:nowrap; color:var(--td-text-color-secondary); } }
.library-cell { display:grid; gap:5px; min-width:0; strong,small { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; } small { color:var(--td-text-color-placeholder); } }
.permissions-pager { display:flex; justify-content:flex-end; align-items:center; gap:12px; margin-top:16px; }
.permission-library-context { display:flex; gap:24px; flex-wrap:wrap; padding-bottom:16px; color:var(--td-text-color-secondary); }
</style>
