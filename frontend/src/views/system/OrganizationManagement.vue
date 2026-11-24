<template>
  <section class="corporate-directory">
    <header class="directory-header">
      <h2>{{ t('corporateDirectory.title') }}</h2>
      <p>{{ t('corporateDirectory.notice') }}</p>
    </header>
    <t-alert v-if="error" theme="error" :message="error" />
    <div class="directory-layout">
      <aside class="department-panel" :aria-label="t('corporateDirectory.department')">
        <div class="department-panel__header">
          <strong :title="rootName">{{ rootName }}</strong>
          <t-button theme="primary" size="small" :disabled="busy || loading" @click="editDepartment()">
            <template #icon><t-icon name="add" /></template>
            {{ t('corporateDirectory.addDepartment') }}
          </t-button>
        </div>
        <div class="department-list">
          <button type="button" class="department-node" :class="{ selected: selected === '' }" :aria-pressed="selected === ''"
            @click="selectDepartment('')">{{ t('corporateDirectory.all') }}</button>
          <button type="button" class="department-node" :class="{ selected: selected === 'unassigned' }" :aria-pressed="selected === 'unassigned'"
            @click="selectDepartment('unassigned')">{{ t('corporateDirectory.unassigned') }}</button>
          <button v-for="item in flatDepartments" :key="item.id" type="button" class="department-node"
            :class="{ selected: selected === item.id }" :aria-pressed="selected === item.id"
            :style="{ paddingInlineStart: `${12 + Math.min(item.depth, 7) * 12}px` }" :title="item.name" @click="selectDepartment(item.id)">
            <span>{{ item.name }}</span><small>{{ item.member_count }}</small>
          </button>
        </div>
        <div v-if="selectedDepartment" class="department-actions">
          <t-button variant="outline" size="small" :disabled="busy || loading" @click="editDepartment(selectedDepartment)">
            <template #icon><t-icon name="edit" /></template>
            {{ t('common.edit') }}
          </t-button>
          <t-button variant="outline" size="small" :disabled="busy || loading" @click="editDepartment(undefined, selectedDepartment.id)">
            <template #icon><t-icon name="add" /></template>
            {{ t('corporateDirectory.addChild') }}
          </t-button>
          <t-popconfirm :key="selectedDepartment.id" :content="t('corporateDirectory.deleteConfirm')" theme="danger"
            :popup-props="{ overlayClassName: 'wk-popover user-management-confirm-popup' }" @confirm="removeDepartment">
            <t-button variant="outline" theme="danger" size="small" shape="square" :title="t('common.delete')"
              :aria-label="t('common.delete')" :disabled="busy || loading">
              <template #icon><t-icon name="delete" /></template>
            </t-button>
          </t-popconfirm>
        </div>
      </aside>
      <div class="directory-members">
        <UserManagement :active="true" :department-id="selected" :include-descendants="includeDescendants" :refresh-key="refreshKey"
          @loaded="refresh" @refresh="refresh" @selection="selection = $event" @announced="announcement = $event">
          <template #filters>
            <t-checkbox v-if="selectedDepartment" v-model="includeDescendants">{{ t('corporateDirectory.includeDescendants') }}</t-checkbox>
          </template>
          <template #actions="{ locked }">
            <t-button variant="outline" size="small" :disabled="!selection.length || busy || loading || locked" @click="moveVisible = true">
              <template #icon><t-icon name="swap" /></template>
              {{ t('corporateDirectory.moveMembers', { count: selection.length }) }}
            </t-button>
          </template>
        </UserManagement>
      </div>
    </div>
    <span class="sr-only" aria-live="polite">{{ announcement }}</span>
    <t-dialog v-model:visible="departmentVisible" :close-btn="!busy" :close-on-esc-keydown="!busy" :header="editingId ? t('corporateDirectory.editDepartment') : t('corporateDirectory.addDepartment')"
      :confirm-loading="busy" :close-on-overlay-click="false" :on-confirm="save" :cancel-btn="busy ? null : undefined">
      <t-form label-align="top">
        <t-form-item :label="t('corporateDirectory.departmentName')"><t-input v-model="form.name" :maxlength="100" :disabled="busy" /></t-form-item>
        <p class="department-code-note">{{ t('corporateDirectory.autoCode') }}</p>
        <t-form-item :label="t('corporateDirectory.parent')"><t-select v-model="form.parent_id" :options="parentOptions" :disabled="busy" /></t-form-item>
        <t-form-item :label="t('corporateDirectory.sort')"><t-input-number v-model="form.sort_order" :min="0" :max="1000000" :disabled="busy" /></t-form-item>
        <t-form-item v-if="editingId" :label="t('corporateDirectory.leader')">
          <t-select v-model="form.leader_id" filterable :options="leaderOptions" :disabled="busy" @search="searchLeaders" />
        </t-form-item>
      </t-form>
    </t-dialog>
    <t-dialog v-model:visible="moveVisible" :close-btn="!busy" :close-on-esc-keydown="!busy" :header="t('corporateDirectory.moveTitle')" :confirm-loading="busy"
      :close-on-overlay-click="false" :on-confirm="move" :cancel-btn="busy ? null : undefined">
      <p>{{ t('corporateDirectory.moveNotice') }}</p>
      <t-select v-model="moveTarget" :options="moveOptions" :disabled="busy" />
    </t-dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import UserManagement from './UserManagement.vue'
import { getDirectory, saveDepartment, deleteDepartment, movePersonnel, listPersonnel, type Department, type DepartmentInput, type Personnel } from '@/api/system/directory'
const { t } = useI18n()
const rootName = ref('')
const departments = ref<Department[]>([])
const selected = ref('')
const includeDescendants = ref(false)
const selection = ref<Personnel[]>([])
const loading = ref(false), busy = ref(false), error = ref(''), announcement = ref('')
const refreshKey = ref(0)
const departmentVisible = ref(false), moveVisible = ref(false), editingId = ref(''), moveTarget = ref('')
const form = reactive({ name: '', parent_id: '', sort_order: 0, leader_id: '', expected_version: 0 })
const leaderChoices = ref<Personnel[]>([])
let sequence = 0, leaderSequence = 0, alive = true
const selectedDepartment = computed(() => departments.value.find(d => d.id === selected.value))
const flatDepartments = computed(() => {
  const out: Array<Department & { depth: number }> = [], seen = new Set<string>()
  function walk(parent: string | null, depth: number) {
    for (const d of departments.value.filter(d => d.parent_id === parent)) {
      if (seen.has(d.id)) continue
      seen.add(d.id); out.push({ ...d, depth }); walk(d.id, depth + 1)
    }
  }
  walk(null, 0); return out
})
const descendants = computed(() => {
  const ids = new Set([editingId.value]); let changed = true
  while (changed) { changed = false; for (const d of departments.value) if (d.parent_id && ids.has(d.parent_id) && !ids.has(d.id)) { ids.add(d.id); changed = true } }
  return ids
})
const parentOptions = computed(() => [{ label: rootName.value, value: '' }, ...flatDepartments.value.filter(d => !descendants.value.has(d.id)).map(d => ({ label: `${'　'.repeat(d.depth)}${d.name}`, value: d.id }))])
const moveOptions = computed(() => [{ label: t('corporateDirectory.unassigned'), value: '' }, ...flatDepartments.value.map(d => ({ label: `${'　'.repeat(d.depth)}${d.name}`, value: d.id }))])
const leaderOptions = computed(() => [{ label: t('corporateDirectory.noLeader'), value: '' }, ...leaderChoices.value.map(u => ({ label: u.display_name || u.username, value: u.id })),
  ...(form.leader_id && !leaderChoices.value.some(u => u.id === form.leader_id) ? [{ label: form.leader_id, value: form.leader_id }] : [])])
function report(e: any) {
  const code = e?.response?.data?.code || e?.code
  const key = ['directory_conflict', 'department_occupied', 'department_leader', 'directory_invalid'].includes(code) ? code : 'failed'
  error.value = t(`corporateDirectory.errors.${key}`); MessagePlugin.error(error.value)
}
function selectDepartment(id: string) {
  if (id === selected.value) return
  selected.value = id
  selection.value = []
}
async function refresh() {
  const n = ++sequence; loading.value = true
  try { const data = await getDirectory(); if (!alive || n !== sequence) return
    rootName.value = data.directory.name; departments.value = data.departments
    if (selected.value && selected.value !== 'unassigned' && !departments.value.some(d => d.id === selected.value)) selected.value = ''
    refreshKey.value++; error.value = ''
  } catch(e: any) { if (alive && n === sequence) report(e) } finally { if (alive && n === sequence) loading.value = false }
}
async function searchLeaders(keyword = '') {
  const n = ++leaderSequence, id = editingId.value
  if (!id) return
  try { const data = await listPersonnel({ department_id: id, keyword, status: 'active', offset: 0, limit: 100 }); if (alive && n === leaderSequence && id === editingId.value) leaderChoices.value = data.users } catch(e: any) { if (alive && n === leaderSequence) report(e) }
}
function editDepartment(d?: Department, parent = '') {
  editingId.value = d?.id || ''; leaderChoices.value = []
  Object.assign(form, { name: d?.name || '', parent_id: d?.parent_id || parent, sort_order: d?.sort_order || 0, leader_id: d?.leader_id || '', expected_version: d?.version || 0 })
  departmentVisible.value = true; void searchLeaders()
}
async function save() {
  if (busy.value || !form.name.trim()) return
  busy.value = true
  try { const input: DepartmentInput = { ...form, parent_id: form.parent_id || null, leader_id: form.leader_id || null }
    await saveDepartment(editingId.value, input); if (!alive) return; departmentVisible.value = false; await refresh()
  } catch(e: any) { if (alive) { if ((e?.response?.status || e?.$httpStatus) === 409) departmentVisible.value = false; await refresh(); report(e) } } finally { busy.value = false }
}
async function removeDepartment() {
  const d = selectedDepartment.value; if (!d || busy.value) return
  busy.value = true
  try { await deleteDepartment(d.id, d.version); if (alive) await refresh() } catch(e: any) { if (alive) report(e) } finally { busy.value = false }
}
async function move() {
  if (busy.value || !selection.value.length) return
  busy.value = true
  try { await movePersonnel(moveTarget.value || null, selection.value.map(u => ({ user_id: u.id, expected_version: u.version }))); if (!alive) return
    moveVisible.value = false; selection.value = []; await refresh()
  } catch(e: any) { if (alive) { moveVisible.value = false; await refresh(); report(e) } } finally { busy.value = false }
}
onMounted(refresh)
onBeforeUnmount(() => { alive = false; sequence++; leaderSequence++ })
</script>
<style scoped lang="less">
.directory-header {
  margin-bottom: 20px;
  h2 { margin: 0 0 8px; }
  p { color: var(--td-text-color-secondary); margin: 0; }
}
.directory-layout { display: grid; grid-template-columns: 252px minmax(0, 1fr); align-items: start; gap: 20px; }
.department-panel {
  display: flex;
  flex-direction: column;
  min-width: 0;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-sm);
  background: var(--td-bg-color-container);
}
.department-panel__header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px;
  strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; }
}
.department-list { padding: 0 8px 8px; max-height: min(60vh, 520px); overflow-y: auto; }
.department-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 10px;
  border-top: 1px solid var(--td-component-stroke);
}
.directory-members { min-width: 0; }
.department-code-note { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.department-node {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  text-align: start;
  border: 0;
  background: transparent;
  color: inherit;
  padding: 9px 12px;
  border-radius: var(--td-radius-default);
  cursor: pointer;
  span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  small { flex: none; color: var(--td-text-color-secondary); }
  &:hover { background: var(--td-bg-color-container-hover); }
  &.selected { background: var(--td-brand-color-light); color: var(--td-brand-color); }
}
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0,0,0,0); }
@media (max-width: 950px) {
  .directory-layout { grid-template-columns: minmax(0, 1fr); }
  .department-list { max-height: 180px; }
}
</style>
