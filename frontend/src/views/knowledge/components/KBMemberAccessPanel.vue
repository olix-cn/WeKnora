<template>
  <section class="kb-member-access" :aria-busy="busy">
    <div class="access-summary"><h3>{{ text.title }}</h3><p class="notice">{{ text.adminNotice }}</p></div>
    <t-alert v-if="error" theme="error" :message="error" role="alert" />
    <t-button v-if="!policy || conflict" :disabled="busy" @click="load">{{ text.reload }}</t-button>
    <template v-if="policy">
      <div class="access-mode-cards" role="group" :aria-label="text.range">
        <button v-for="option in modeOptions" :key="option.value" type="button" class="access-mode-card"
          :class="{ selected: mode === option.value }" :aria-pressed="mode === option.value" :disabled="busy || conflict"
          @click="changeMode(option.value)"><strong>{{ option.label }}</strong>
          <span>{{ option.value === 'inherit' ? text.inheritShort : text.restrictedShort }}</span></button>
      </div>
      <p class="access-boundary">{{ mode === 'inherit' ? text.inheritNotice : text.restrictedNotice }}</p>
      <div class="member-picker-layout">
        <section class="member-picker-column">
          <h4>{{ text.available }}</h4>
          <form @submit.prevent="searchMembers(1)">
            <t-input v-model="search" :placeholder="text.search" :disabled="busy || conflict" />
            <t-button type="submit" variant="outline" :disabled="busy || conflict">{{ text.searchAction }}</t-button>
          </form>
          <p v-if="!searched" class="notice">{{ text.searchHint }}</p>
          <p v-if="searched && !candidates.length" class="notice">{{ text.noMembers }}</p>
          <ul class="candidates">
            <li v-for="member in candidates" :key="member.user_id">
              <div class="member-identity"><strong>{{ member.username }}</strong><small>{{ member.role }}</small></div>
              <t-button variant="text" :disabled="busy || conflict || members.some(m => m.user_id === member.user_id)"
                @click="addMember(member)">{{ members.some(m => m.user_id === member.user_id) ? text.added : text.add }}</t-button>
            </li>
          </ul>
          <t-button v-if="candidates.length < total" variant="text" :disabled="busy || conflict"
            @click="searchMembers(page + 1)">{{ text.more }}</t-button>
        </section>
        <section class="member-picker-column">
          <h4>{{ text.granted }} <span>{{ members.length }}</span></h4>
          <p v-if="!members.length" class="notice">{{ mode === 'restricted' ? text.adminOnly : text.noGrants }}</p>
          <ul class="grants">
            <li v-for="member in members" :key="member.user_id">
              <div class="member-identity"><strong>{{ names[member.user_id]?.username || member.user_id }}</strong>
                <small v-if="names[member.user_id]?.role === 'viewer'">{{ text.viewerNotice }}</small></div>
              <t-select :model-value="member.permission" :disabled="busy || conflict" :aria-label="text.permission"
                :options="permissionOptions(member.user_id)" @change="(value: unknown) => setPermission(member.user_id, value)" />
              <t-button variant="text" theme="danger" :disabled="busy || conflict" @click="removeMember(member.user_id)">{{ text.remove }}</t-button>
            </li>
          </ul>
        </section>
      </div>
      <div class="access-save-bar">
        <t-checkbox v-if="requiresConfirmation" v-model="confirmed" :disabled="busy || conflict">{{ text.confirm }}</t-checkbox>
        <t-button :loading="saving" :disabled="busy || conflict || !canSave" @click="save">{{ text.save }}</t-button>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { getKBAccessPolicy, replaceKBAccessPolicy, searchKBAccessMembers } from '@/api/knowledge-base/memberAccess'
import type { KBAccessMode, KBAccessPolicy, KBMemberCandidate, KBMemberGrant, KBMemberPermission } from '@/api/knowledge-base/memberAccess'

// Mounted in the KB detail dialog. Other locales currently fall back to English.
const props = defineProps<{ kbId: string; creatorId?: string; platform?: boolean }>()
const emit = defineEmits<{ saved: []; dirty: [boolean]; saving: [boolean] }>()
const auth = useAuthStore()
const { locale } = useI18n()
const zh = {
  available: '可选空间成员', granted: '已授权成员', added: '已添加', searchAction: '搜索', searchHint: '搜索此知识库所属空间的有效成员。', noGrants: '未设置额外授权，按空间角色访问。', permission: '成员权限', inheritShort: '沿用所属空间的角色权限', restrictedShort: '仅指定成员及空间管理员可访问',
  title: '访问权限', adminNotice: '空间所有者和管理员始终保留管理权限；此设置不对他们保密。',
  reload: '重新加载', range: '谁可以访问', inherit: '继承空间权限', restricted: '指定成员',
  inheritNotice: '空间成员按原有角色访问；可在下方额外指定维护人员。',
  restrictedNotice: '普通成员仅在授权名单内可访问。已有知识库或智能体分享须先解除。来源不完整的旧会话会保守隐藏：无法读取本空间任一受限库的成员将不能读取这些旧会话。已下载内容及未到期云直链无法撤回。',
  search: '搜索空间成员', noMembers: '没有符合条件的有效成员', add: '添加', remove: '移除', more: '加载更多',
  viewer: '可查看', editor: '可编辑', admin: '可管理', viewerNotice: '空间只读成员最多可查看；编辑需先调整空间角色。',
  adminOnly: '当前仅空间所有者和管理员可以访问。',
  confirm: '我已检查名单，确认此变更可能使成员（包括我自己或创建者）失去访问权限。',
  save: '保存权限', failed: '暂时无法读取或保存权限，请重试。',
  changed: '权限已被其他人修改，请重新加载后检查，不能直接覆盖。',
  shared: '此库仍有知识库或智能体分享，请先解除相关分享。', invalid: '名单中有无效或已离开空间的成员，请重新加载并检查。',
  forbidden: '当前已无此库的管理权限，请刷新知识库页面。',
  transferBusy: '此库有正在执行或结果未确认的转存任务，请先完成或由管理员核对恢复。',
}
const en: typeof zh = {
  available: 'Workspace members', granted: 'Granted members', added: 'Added', searchAction: 'Search', searchHint: 'Search active members of this library’s workspace.', noGrants: 'No explicit grants. Workspace roles apply.', permission: 'Member permission', inheritShort: 'Use existing workspace roles', restrictedShort: 'Selected members and workspace administrators only',
  title: 'Access permissions', adminNotice: 'Workspace owners and administrators always retain management access.',
  reload: 'Reload', range: 'Who can access', inherit: 'Inherit workspace permissions', restricted: 'Selected members',
  inheritNotice: 'Workspace roles still apply. Add explicit maintainers below.',
  restrictedNotice: 'Other members need an explicit grant. Remove existing KB or agent shares first. Legacy conversations with incomplete provenance are conservatively hidden from members who cannot read every restricted KB in this workspace. Downloads and unexpired cloud URLs cannot be recalled.',
  search: 'Search workspace members', noMembers: 'No active matching members', add: 'Add', remove: 'Remove', more: 'Load more',
  viewer: 'Can view', editor: 'Can edit', admin: 'Can manage', viewerNotice: 'Workspace viewers remain read-only.',
  adminOnly: 'Only workspace owners and administrators can currently access this KB.',
  confirm: 'I checked the list and understand this change may revoke access, including my own or the creator’s.',
  save: 'Save permissions', failed: 'Unable to load or save permissions. Try again.',
  changed: 'Permissions changed. Reload and review before saving; changes will not be overwritten.',
  shared: 'Remove existing knowledge-base or agent shares first.', invalid: 'A member is unavailable. Reload and review the list.',
  forbidden: 'You no longer manage this knowledge base. Refresh the knowledge-base page.',
  transferBusy: 'A transfer is running or unresolved. Complete it or ask an administrator to recover its outcome.',
}
const text = computed(() => locale.value.startsWith('zh') ? zh : en)
const modeOptions = computed(() => [
  { value: 'inherit', label: text.value.inherit }, { value: 'restricted', label: text.value.restricted },
])
const policy = ref<KBAccessPolicy | null>(null)
const mode = ref<KBAccessMode>('inherit')
const members = ref<KBMemberGrant[]>([])
const candidates = ref<KBMemberCandidate[]>([])
const names = ref<Record<string, KBMemberCandidate>>({})
const total = ref(0)
const page = ref(1)
const search = ref('')
const searched = ref(false)
const loading = ref(false)
const searching = ref(false)
const saving = ref(false)
const conflict = ref(false)
const confirmed = ref(false)
const error = ref('')
let generation = 0
let searchGeneration = 0
const busy = computed(() => loading.value || searching.value || saving.value)
const normalized = (rows: KBMemberGrant[]) => JSON.stringify(
  rows.map(({ user_id, permission }) => ({ user_id, permission })).sort((a, b) => a.user_id.localeCompare(b.user_id)))
const dirty = computed(() => !!policy.value &&
  (mode.value !== policy.value.mode || normalized(members.value) !== normalized(policy.value.members)))
const requiresConfirmation = computed(() => dirty.value &&
  (mode.value === 'restricted' || policy.value?.mode === 'restricted'))
const canSave = computed(() => dirty.value && (!requiresConfirmation.value || confirmed.value))

function reset() {
  generation++; searchGeneration++
  policy.value = null; members.value = []; candidates.value = []; names.value = {}
  mode.value = 'inherit'; search.value = ''; total.value = 0; page.value = 1
  loading.value = false; searching.value = false; saving.value = false
  conflict.value = false; confirmed.value = false; error.value = ''; searched.value = false
}
function showError(cause: any) {
  const code = cause?.response?.data?.code ?? cause?.code ?? cause?.error?.code
  if (code === 'policy_conflict') { conflict.value = true; error.value = text.value.changed }
  else if (code === 'policy_shared') error.value = text.value.shared
  else if (code === 'policy_invalid') error.value = text.value.invalid
  else if (code === 'policy_transfer_busy') error.value = text.value.transferBusy
  else if (code === 'policy_forbidden') { reset(); error.value = text.value.forbidden }
  else error.value = text.value.failed
}
async function load() {
  if (!props.kbId || busy.value) return
  reset()
  const current = generation
  loading.value = true
  try {
    const response = await getKBAccessPolicy(props.kbId, props.platform)
    if (current !== generation) return
    policy.value = response.data
    mode.value = response.data.mode
    members.value = response.data.members.map(m => ({ ...m }))
    for (const member of response.data.member_details || []) names.value[member.user_id] = member
  } catch (cause) { if (current === generation) showError(cause) }
  finally { if (current === generation) loading.value = false }
}
function changeMode(value: unknown) {
  if (busy.value || conflict.value || (value !== 'inherit' && value !== 'restricted')) return
  mode.value = value; confirmed.value = false
  if (value === 'restricted' && props.creatorId && !members.value.some(m => m.user_id === props.creatorId)) {
    members.value.push({ user_id: props.creatorId, permission: 'admin' })
  }
}
async function searchMembers(nextPage = 1) {
  if (!policy.value || busy.value || conflict.value) return
  const current = generation, request = ++searchGeneration
  searching.value = true; error.value = ''
  try {
    const response = await searchKBAccessMembers(props.kbId, search.value.trim(), nextPage, props.platform)
    if (current !== generation || request !== searchGeneration) return
    candidates.value = nextPage === 1 ? response.data : [...candidates.value, ...response.data]
    for (const member of response.data) {
      names.value[member.user_id] = member
      if (member.role === 'viewer') {
        const grant = members.value.find(m => m.user_id === member.user_id)
        if (grant && grant.permission !== 'viewer') { grant.permission = 'viewer'; confirmed.value = false }
      }
    }
    page.value = nextPage; total.value = response.total; searched.value = true
  } catch (cause) { if (current === generation) showError(cause) }
  finally { if (current === generation && request === searchGeneration) searching.value = false }
}
function addMember(member: KBMemberCandidate) {
  if (busy.value || conflict.value || members.value.length >= 500 || members.value.some(m => m.user_id === member.user_id)) return
  names.value[member.user_id] = member
  members.value.push({ user_id: member.user_id, permission: 'viewer' }); confirmed.value = false
}
function removeMember(userId: string) {
  if (busy.value || conflict.value) return
  members.value = members.value.filter(m => m.user_id !== userId); confirmed.value = false
}
function permissionOptions(userId: string) {
  return (['viewer', 'editor', 'admin'] as const).map(value => ({
    value, label: text.value[value], disabled: names.value[userId]?.role === 'viewer' && value !== 'viewer',
  }))
}
function setPermission(userId: string, value: unknown) {
  if (busy.value || conflict.value || !['viewer', 'editor', 'admin'].includes(String(value))) return
  if (names.value[userId]?.role === 'viewer' && value !== 'viewer') return
  const member = members.value.find(m => m.user_id === userId)
  if (member) { member.permission = value as KBMemberPermission; confirmed.value = false }
}
async function save() {
  if (!policy.value || busy.value || conflict.value || !canSave.value) return
  const current = generation
  saving.value = true; error.value = ''
  try {
    await replaceKBAccessPolicy(props.kbId, { mode: mode.value, version: policy.value.version,
      members: members.value.map(m => ({ ...m })) }, props.platform)
    if (current !== generation) return
    // Saving can revoke this manager's own access. Clear private state and
    // let the parent refresh rather than assuming the new roster is readable.
    reset(); emit('saved')
  } catch (cause) { if (current === generation) showError(cause) }
  finally { if (current === generation) saving.value = false }
}
watch(() => [props.kbId, auth.currentUserId, auth.effectiveTenantId, props.platform], () => { reset(); void load() }, { immediate: true })
watch(search, () => {
  searchGeneration++; searching.value = false
  candidates.value = []; total.value = 0; page.value = 1; searched.value = false
})
watch(dirty, value => emit('dirty', value))
watch(saving, value => emit('saving', value))
onBeforeUnmount(reset)
</script>

<style scoped>
.kb-member-access { display:grid; gap:16px; }
.access-summary h3 { margin:0 0 8px; }
.notice,.access-boundary { color:var(--td-text-color-secondary); font-size:var(--app-text-md); line-height:1.65; margin:0; }
.access-mode-cards { display:grid; grid-template-columns:1fr 1fr; gap:12px; }
.access-mode-card { display:grid; gap:6px; text-align:left; padding:16px; border:1px solid var(--td-component-border); border-radius:var(--app-radius-md); background:var(--td-bg-color-container); color:inherit; cursor:pointer; }
.access-mode-card.selected { border-color:var(--td-brand-color); background:var(--td-brand-color-light); }
.access-mode-card span { color:var(--td-text-color-secondary); font-size:var(--app-text-sm); }
.member-picker-layout { display:grid; grid-template-columns:minmax(240px,0.9fr) minmax(340px,1.1fr); border:1px solid var(--td-component-border); border-radius:var(--app-radius-md); }
.member-picker-column { min-width:0; padding:16px; }
.member-picker-column + .member-picker-column { border-left:1px solid var(--td-component-border); }
h4 { margin:0 0 12px; font-size:var(--app-text-base); } h4 span { color:var(--td-text-color-secondary); margin-left:8px; }
form { display:flex; gap:8px; margin-bottom:12px; }
ul { padding:0; margin:0; list-style:none; max-height:340px; overflow:auto; }
li { display:flex; gap:8px; align-items:center; padding:10px 0; border-bottom:1px solid var(--td-component-border); }
.member-identity { flex:1; min-width:0; display:grid; gap:4px; overflow-wrap:anywhere; }
.member-identity strong { font-weight:500; } .member-identity small { font-size:var(--app-text-sm); color:var(--td-text-color-secondary); }
.grants .t-select__wrap { flex:0 0 110px; width:110px; }
.access-save-bar { display:flex; justify-content:flex-end; align-items:center; gap:16px; padding-top:16px; border-top:1px solid var(--td-component-border); }
.access-save-bar .t-checkbox { margin-right:auto; }
@media (max-width:800px) { .member-picker-layout { grid-template-columns:1fr; } .member-picker-column + .member-picker-column { border-left:0; border-top:1px solid var(--td-component-border); } .access-save-bar { flex-wrap:wrap; } }
</style>
