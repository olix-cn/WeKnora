<template>
  <section class="kb-member-access" :aria-busy="busy">
    <h3>{{ text.title }}</h3>
    <p class="notice">{{ text.adminNotice }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <t-button v-if="!policy || conflict" :disabled="busy" @click="load">{{ text.reload }}</t-button>
    <template v-if="policy">
      <label>{{ text.range }}
        <t-select :model-value="mode" :disabled="busy || conflict" :options="modeOptions" @change="changeMode" />
      </label>
      <p>{{ mode === 'inherit' ? text.inheritNotice : text.restrictedNotice }}</p>
      <form @submit.prevent="searchMembers(1)">
        <t-input v-model="search" :placeholder="text.search" :disabled="busy || conflict" />
        <t-button type="submit" :disabled="busy || conflict">{{ text.search }}</t-button>
      </form>
      <p v-if="searched && !candidates.length">{{ text.noMembers }}</p>
      <ul class="candidates">
        <li v-for="member in candidates" :key="member.user_id">
          <span>{{ member.username }}</span>
          <t-button variant="text" :disabled="busy || conflict || members.some(m => m.user_id === member.user_id)"
            @click="addMember(member)">{{ text.add }}</t-button>
        </li>
      </ul>
      <t-button v-if="candidates.length < total" variant="text" :disabled="busy || conflict"
        @click="searchMembers(page + 1)">{{ text.more }}</t-button>
      <ul class="grants">
        <li v-for="member in members" :key="member.user_id">
          <span>{{ names[member.user_id]?.username || member.user_id }}</span>
          <t-select :model-value="member.permission" :disabled="busy || conflict"
            :options="permissionOptions(member.user_id)" @change="(value: unknown) => setPermission(member.user_id, value)" />
          <t-button variant="text" :disabled="busy || conflict" @click="removeMember(member.user_id)">
            {{ text.remove }}
          </t-button>
          <small v-if="names[member.user_id]?.role === 'viewer'">{{ text.viewerNotice }}</small>
        </li>
      </ul>
      <p v-if="mode === 'restricted' && !members.length">{{ text.adminOnly }}</p>
      <t-checkbox v-if="requiresConfirmation" v-model="confirmed" :disabled="busy || conflict">
        {{ text.confirm }}
      </t-checkbox>
      <t-button :loading="saving" :disabled="busy || conflict || !canSave" @click="save">{{ text.save }}</t-button>
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
const props = defineProps<{ kbId: string; creatorId?: string }>()
const emit = defineEmits<{ saved: [] }>()
const auth = useAuthStore()
const { locale } = useI18n()
const zh = {
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
    const response = await getKBAccessPolicy(props.kbId)
    if (current !== generation) return
    policy.value = response.data
    mode.value = response.data.mode
    members.value = response.data.members.map(m => ({ ...m }))
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
    const response = await searchKBAccessMembers(props.kbId, search.value.trim(), nextPage)
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
      members: members.value.map(m => ({ ...m })) })
    if (current !== generation) return
    // Saving can revoke this manager's own access. Clear private state and
    // let the parent refresh rather than assuming the new roster is readable.
    reset(); emit('saved')
  } catch (cause) { if (current === generation) showError(cause) }
  finally { if (current === generation) saving.value = false }
}
watch(() => [props.kbId, auth.currentUserId, auth.effectiveTenantId], () => { reset(); void load() }, { immediate: true })
watch(search, () => {
  searchGeneration++; searching.value = false
  candidates.value = []; total.value = 0; page.value = 1; searched.value = false
})
onBeforeUnmount(reset)
</script>

<style scoped>
.kb-member-access { display: grid; gap: 12px; }
.notice { color: var(--td-text-color-secondary); }
form, li { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
li { padding: 8px 0; }
ul { padding: 0; list-style: none; }
li > span { min-width: 120px; overflow-wrap: anywhere; }
li > .t-select__wrap { width: 140px; }
[role="alert"] { color: var(--td-error-color); }
</style>
