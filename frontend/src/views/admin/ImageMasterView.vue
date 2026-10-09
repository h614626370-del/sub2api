<template>
  <AppLayout>
    <div class="image-master">
      <header class="heading">
        <div><p class="text-xs text-gray-500">{{ t('imageMaster.operations') }}</p><h1>{{ t('imageMaster.title') }}</h1></div>
        <div class="actions">
          <span class="text-xs text-gray-500">{{ t('imageMaster.currentInstance') }}</span>
          <span v-if="remote" class="state" :class="remote.config.enabled ? 'good' : 'muted'">
            {{ t('imageMaster.primaryState', { state: t(remote.config.enabled ? 'imageMaster.running' : 'imageMaster.disabled') }) }}
          </span>
          <button class="tool" :title="t('imageMaster.refresh')" :aria-label="t('imageMaster.refresh')" :disabled="loading || busy" @click="load">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          </button>
        </div>
      </header>
      <p v-if="error" role="alert" class="alert">{{ error }}</p>
      <p v-if="remote?.storage_error" role="alert" class="alert">{{ t('imageMaster.storageError') }}</p>
      <p v-if="notice" role="status" class="notice">{{ notice }}</p>
      <nav class="tabs" role="tablist" :aria-label="t('imageMaster.title')">
        <button v-for="name in (['monitor', 'settings', 'shadow'] as const)" :key="name" role="tab" :aria-selected="tab === name" :class="{ selected: tab === name }" @click="tab = name">
          <Icon :name="name === 'monitor' ? 'monitorPulse' : 'cog'" size="sm" />{{ t(`imageMaster.${name}`) }}
        </button>
      </nav>
      <p v-if="!remote && loading" role="status" class="empty">{{ t('imageMaster.loading') }}</p>
      <template v-if="remote">
        <ImageShadowPanel v-if="tab === 'shadow'" />
        <section v-show="tab === 'monitor'" role="tabpanel">
          <div class="metrics">
            <div><span>{{ t('imageMaster.active') }}</span><strong>{{ remote.active }}</strong></div>
            <div><span>{{ t('imageMaster.total') }}</span><strong>{{ remote.items.length }}</strong></div>
            <div><span>{{ t('imageMaster.successRate') }}</span><strong>{{ successRate }}</strong></div>
            <div><span>{{ t('imageMaster.latency') }}</span><strong>{{ seconds(median) }}</strong></div>
          </div>
          <div class="toolbar">
            <h2>{{ t('imageMaster.recent') }}</h2>
            <div class="actions">
              <label class="check"><input v-model="autoRefresh" type="checkbox" />{{ t('imageMaster.autoRefresh') }}</label>
              <button class="tool" :disabled="busy" :title="t('imageMaster.export')" :aria-label="t('imageMaster.export')" @click="exportRecords"><Icon name="download" size="sm" /></button>
              <button class="tool danger" :disabled="busy || !finished.length" :title="t('imageMaster.clear')" :aria-label="t('imageMaster.clear')" @click="confirmAction('clear')"><Icon name="trash" size="sm" /></button>
            </div>
          </div>
          <div class="filters">
            <input v-model="search" class="input search" :placeholder="t('imageMaster.search')" :aria-label="t('imageMaster.search')" />
            <select v-model="statusFilter" class="input" :aria-label="t('imageMaster.status')">
              <option value="">{{ t('imageMaster.all') }}</option>
              <option v-for="state in states" :key="state" :value="state">{{ t(`imageMaster.states.${state}`) }}</option>
            </select>
            <select v-model="routeFilter" class="input" :aria-label="t('imageMaster.model')">
              <option value="">{{ t('imageMaster.allRoutes') }}</option>
              <option value="images-generations">{{ t('imageMaster.routes.images-generations') }}</option>
              <option value="images-edits">{{ t('imageMaster.routes.images-edits') }}</option>
              <option value="responses">{{ t('imageMaster.routes.responses') }}</option>
            </select>
          </div>
          <div class="table-scroll">
            <table>
              <thead><tr><th v-for="key in ['request', 'model', 'status', 'duration', 'images', 'actions']" :key="key">{{ t(`imageMaster.${key}`) }}</th></tr></thead>
              <tbody>
                <tr v-for="row in paged" :key="row.id">
                  <td><button class="request-link" :title="row.id" @click="openDetail(row)">{{ row.id.slice(0, 8) }}</button><small>{{ date(row.started_at) }}</small></td>
                  <td><span class="model-name" :title="row.model">{{ row.model }}</span><small>{{ t(`imageMaster.routes.${row.route}`) }}</small></td>
                  <td><span class="state" :class="row.outcome === 'completed' ? 'good' : row.outcome === 'failed' ? 'bad' : 'muted'">{{ t(`imageMaster.states.${row.outcome}`) }}</span></td>
                  <td class="numeric">{{ seconds(row.duration_ms) }}</td>
                  <td class="numeric">{{ row.source_images }} / {{ row.image_count }}</td>
                  <td><div class="actions">
                    <button class="tool" :title="t('imageMaster.detail')" :aria-label="t('imageMaster.detail')" @click="openDetail(row)"><Icon name="eye" size="sm" /></button>
                    <button v-if="!row.finished_at" class="tool danger" :disabled="busy" :title="t('imageMaster.cancel')" :aria-label="t('imageMaster.cancel')" @click="confirmAction('cancel', row.id)"><Icon name="xCircle" size="sm" /></button>
                    <button v-else class="tool danger" :disabled="busy" :title="t('imageMaster.remove')" :aria-label="t('imageMaster.remove')" @click="confirmAction('delete', row.id)"><Icon name="trash" size="sm" /></button>
                  </div></td>
                </tr>
                <tr v-if="!filtered.length"><td colspan="6" class="empty"><Icon name="inbox" size="lg" class="mx-auto mb-3 text-gray-400" />{{ t('imageMaster.empty') }}</td></tr>
              </tbody>
            </table>
          </div>
          <footer class="pagination">
            <span>{{ t('imageMaster.page', { page, pages, total: filtered.length }) }}</span>
            <div class="actions">
              <button class="tool" :disabled="page <= 1" :title="t('imageMaster.previous')" :aria-label="t('imageMaster.previous')" @click="page--"><Icon name="chevronLeft" size="sm" /></button>
              <button class="tool" :disabled="page >= pages" :title="t('imageMaster.next')" :aria-label="t('imageMaster.next')" @click="page++"><Icon name="chevronRight" size="sm" /></button>
            </div>
          </footer>
        </section>
        <form v-if="tab === 'settings' && draft" class="settings" @submit.prevent="save">
          <fieldset :disabled="busy">
            <label class="enable"><span>{{ t('imageMaster.enabled') }}</span><input v-model="draft.enabled" data-testid="image-master-enabled" type="checkbox" role="switch" /></label>
            <div class="form-grid">
              <label v-for="field in numericFields" :key="field.key">{{ t(`imageMaster.${field.label}`) }}<input v-model.number="draft[field.key]" class="input" type="number" :min="field.min" :max="field.max" step="1" required /></label>
            </div>
            <div class="switches">
              <label class="check"><input v-model="draft.done_sentinel" type="checkbox" />{{ t('imageMaster.done') }}</label>
              <label class="check"><input v-model="draft.raw_request_logging" type="checkbox" />{{ t('imageMaster.rawLogging') }}</label>
              <p v-if="draft.raw_request_logging" class="text-sm text-amber-700 dark:text-amber-400">{{ t('imageMaster.rawWarning') }}</p>
            </div>
            <div class="save-row"><button class="btn btn-primary" type="submit" :disabled="!dirty || busy"><Icon name="check" size="sm" />{{ t('imageMaster.save') }}</button></div>
          </fieldset>
        </form>
      </template>
    </div>
    <BaseDialog :show="!!selected" :title="t('imageMaster.detail')" width="wide" @close="closeDetail">
      <template v-if="selected">
        <dl class="details">
          <template v-for="[key, value] in detailFields" :key="key"><dt>{{ t(`imageMaster.${key}`) }}</dt><dd>{{ value === '' ? '--' : value ?? '--' }}</dd></template>
        </dl>
        <h3 class="my-4 font-semibold">{{ t('imageMaster.timeline') }}</h3>
        <ol class="timeline"><li v-for="(stage, index) in selected.stages" :key="index"><span>{{ t(`imageMaster.stages.${stage.name}`) }}</span><time>{{ date(stage.at) }}</time></li></ol>
        <button v-if="selected.raw_saved && !rawText" class="btn btn-secondary mt-4" :disabled="busy" @click="loadRaw">{{ t('imageMaster.raw') }}</button>
        <template v-if="rawText"><h3 class="mt-4 text-sm font-semibold text-amber-700">{{ t('imageMaster.rawTitle') }}</h3><pre class="raw">{{ rawText }}</pre></template>
      </template>
    </BaseDialog>
    <BaseDialog :show="!!pending" :title="t('imageMaster.confirm')" width="narrow" @close="pending = null">
      <p>{{ pending ? t(`imageMaster.${pending.kind}Confirm`) : '' }}</p>
      <template #footer><button class="btn btn-secondary" :disabled="busy" @click="pending = null">{{ t('imageMaster.back') }}</button><button class="btn btn-primary" :disabled="busy" @click="performConfirmed">{{ t('imageMaster.confirmAction') }}</button></template>
    </BaseDialog>
    <TotpStepUpDialog :controller="stepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ImageShadowPanel from './ImageShadowPanel.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpCancelled } from '@/composables/useStepUp'
import { imageMasterAPI, type ImageMasterConfig, type ImageMasterRecord, type ImageMasterStatus } from '@/api/admin/imageMaster'

const { t } = useI18n()
const stepUp = useStepUp()
const tab = ref<'monitor' | 'settings' | 'shadow'>('monitor')
const remote = ref<ImageMasterStatus | null>(null), draft = ref<ImageMasterConfig | null>(null)
const loading = ref(false), busy = ref(false), autoRefresh = ref(true)
const error = ref(''), notice = ref(''), search = ref(''), statusFilter = ref(''), routeFilter = ref('')
const page = ref(1), selected = ref<ImageMasterRecord | null>(null), rawText = ref('')
const pending = ref<{ kind: 'clear' | 'delete' | 'cancel' | 'raw'; id?: string } | null>(null)
const states = ['running', 'completed', 'failed', 'incomplete', 'canceled', 'client_disconnected']
const numericFields = [
  { key: 'timeout_seconds', label: 'timeout', min: 1, max: 3600 },
  { key: 'heartbeat_seconds', label: 'heartbeat', min: 1, max: 60 },
  { key: 'max_body_mib', label: 'requestLimit', min: 1, max: 128 },
  { key: 'max_response_mib', label: 'responseLimit', min: 1, max: 128 }
] as const
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== JSON.stringify(remote.value?.config))
const finished = computed(() => remote.value?.items.filter(row => row.finished_at) || [])
const successful = computed(() => finished.value.filter(row => row.outcome === 'completed'))
const successRate = computed(() => finished.value.length ? `${Math.round(successful.value.length / finished.value.length * 100)}%` : '--')
const median = computed(() => {
  const values = successful.value.map(row => row.duration_ms).sort((a, b) => a - b)
  if (!values.length) return 0
  const middle = Math.floor(values.length / 2)
  return values.length % 2 ? values[middle] : (values[middle - 1] + values[middle]) / 2
})
const filtered = computed(() => (remote.value?.items || []).filter(row =>
  (!statusFilter.value || row.outcome === statusFilter.value) &&
  (!routeFilter.value || row.route === routeFilter.value) &&
  `${row.id} ${row.model} ${row.requested_model} ${row.error_code || ''}`.toLowerCase().includes(search.value.trim().toLowerCase())))
const pages = computed(() => Math.max(1, Math.ceil(filtered.value.length / 25)))
const paged = computed(() => filtered.value.slice((page.value - 1) * 25, page.value * 25))
watch([search, statusFilter, routeFilter], () => { page.value = 1 })
watch(pages, n => { if (page.value > n) page.value = n })
const date = (value: number) => new Date(value).toLocaleString()
const seconds = (value: number) => t('imageMaster.seconds', { value: (value / 1000).toFixed(1) })
const detailFields = computed(() => selected.value ? [
  ['requestID', selected.value.id], ['gatewayID', selected.value.gateway_request_id],
  ['user', selected.value.user_id], ['apiKey', selected.value.api_key_id],
  ['requestedModel', selected.value.requested_model], ['imageModel', selected.value.model], ['heartbeatCount', selected.value.heartbeats],
  ['gatewayStatus', selected.value.gateway_status], ['errorCode', selected.value.error_code]
] as [string, unknown][] : [])
let alive = true, revision = 0, timer: ReturnType<typeof setInterval> | undefined
const message = (e: unknown) => (e as { message?: string })?.message || t('imageMaster.error')
async function load() {
  if (loading.value || busy.value) return
  const request = ++revision
  loading.value = true
  try {
    const data = await imageMasterAPI.status()
    if (!alive || request !== revision) return
    const keepDraft = dirty.value
    remote.value = data
    if (!keepDraft) draft.value = { ...data.config }
    if (selected.value) selected.value = data.items.find(r => r.id === selected.value?.id) || selected.value
    error.value = ''
  } catch (e) { if (alive && request === revision) error.value = message(e) }
  finally { if (alive && request === revision) loading.value = false }
}
async function run(action: () => Promise<unknown>) {
  if (busy.value) return
  busy.value = true
  revision++
  loading.value = false
  error.value = ''
  try { await action() } catch (e) { if (alive && !isStepUpCancelled(e)) error.value = message(e) }
  finally { busy.value = false }
  if (alive && !error.value) await load()
}
async function save() {
  if (!draft.value || busy.value) return
  if (draft.value.raw_request_logging && !remote.value?.config.raw_request_logging) {
    pending.value = { kind: 'raw' }; return
  }
  await saveConfig()
}
async function saveConfig() {
  const config = { ...draft.value! }
  await run(async () => {
    const saved = await imageMasterAPI.save(config)
    if (alive && remote.value) { remote.value.config = saved; draft.value = { ...saved }; notice.value = t('imageMaster.saved') }
  })
}
function confirmAction(kind: 'clear' | 'delete' | 'cancel', id?: string) { pending.value = { kind, id } }
async function performConfirmed() {
  const action = pending.value
  if (!action || busy.value) return
  if (action.kind === 'raw') await saveConfig()
  else await run(() => action.kind === 'cancel' ? imageMasterAPI.cancel(action.id!) : imageMasterAPI.remove(action.id))
  pending.value = null
  if (!error.value && (action.kind === 'clear' || action.kind === 'delete')) closeDetail()
}
function openDetail(row: ImageMasterRecord) { selected.value = row; rawText.value = '' }
function closeDetail() { selected.value = null; rawText.value = '' }
async function loadRaw() {
  const id = selected.value?.id
  if (!id) return
  await run(async () => {
    const raw = await stepUp.run(() => imageMasterAPI.raw(id))
    if (alive && selected.value?.id === id) rawText.value = JSON.stringify(raw, null, 2)
  })
}
async function exportRecords() {
  await run(async () => {
    const blob = await imageMasterAPI.export()
    if (!alive) return
    const url = URL.createObjectURL(blob), link = document.createElement('a')
    link.href = url; link.download = 'image-master-requests.json'; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  })
}
onMounted(() => {
  void load()
  timer = setInterval(() => { if (autoRefresh.value && !document.hidden) void load() }, 3000)
})
onBeforeUnmount(() => { alive = false; revision++; clearInterval(timer); rawText.value = '' })
</script>

<style scoped>
.image-master { min-width: 0; color: inherit; }
.heading, .toolbar, .pagination { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.heading { padding: 4px 0 20px; }
h1 { font-size: 22px; font-weight: 650; line-height: 1.5; margin-top: 4px; }
h2 { font-size: 15px; font-weight: 600; }
.actions, .check { display: flex; align-items: center; gap: 8px; }
.actions { flex-wrap: wrap; }
.check { font-size: 13px; cursor: pointer; }
.tool { width: 34px; height: 34px; display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0; border-radius: 6px; border: 1px solid #dfe5e7; }
.tool:hover { background: #f0f5f4; }
.tool:disabled { opacity: .4; cursor: not-allowed; }
.danger { color: #c13a42; }
.tabs { display: flex; border-bottom: 1px solid #dfe5e7; gap: 24px; }
.tabs button { display: flex; align-items: center; gap: 7px; padding: 12px 0; font-size: 14px; border-bottom: 2px solid transparent; }
.tabs .selected { color: #008577; border-color: #008577; }
.metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); padding: 24px 0; gap: 20px; border-bottom: 1px solid #dfe5e7; }
.metrics span { display: block; color: #68767c; font-size: 12px; }
.metrics strong { display: block; font-size: 24px; font-weight: 600; margin-top: 6px; font-variant-numeric: tabular-nums; }
.toolbar { padding: 22px 0 14px; }
.filters { display: flex; gap: 10px; padding-bottom: 16px; flex-wrap: wrap; }
.filters select { width: 170px; }
.search { flex: 1; min-width: 190px; }
.table-scroll { width: 100%; overflow-x: auto; border-top: 1px solid #dfe5e7; }
table { width: 100%; border-collapse: collapse; font-size: 13px; white-space: nowrap; }
th, td { text-align: left; padding: 13px 12px; border-bottom: 1px solid #edf0f1; }
th { color: #68767c; font-size: 12px; font-weight: 500; background: #f6f8f8; }
td small { display: block; color: #819097; margin-top: 4px; font-size: 11px; }
.model-name { display: block; max-width: 220px; overflow: hidden; text-overflow: ellipsis; }
.request-link { font-family: monospace; color: #008577; }
.numeric { font-variant-numeric: tabular-nums; }
.state { display: inline-block; font-size: 12px; border-radius: 4px; padding: 3px 7px; white-space: nowrap; }
.good { color: #07705f; background: #e5f5ee; }
.muted { color: #67777c; background: #edf1f2; }
.bad { color: #bc303f; background: #fcecef; }
.empty { text-align: center; padding: 60px 20px; color: #819097; }
.pagination { font-size: 12px; color: #68767c; padding: 16px 0; }
.settings { max-width: 800px; padding-top: 24px; }
.enable { display: flex; justify-content: space-between; align-items: center; padding-bottom: 22px; border-bottom: 1px solid #dfe5e7; font-weight: 600; }
input[type=checkbox] { width: 16px; height: 16px; accent-color: #009b8b; flex-shrink: 0; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px 24px; padding: 24px 0; }
.form-grid label { display: flex; flex-direction: column; gap: 8px; font-size: 13px; min-width: 0; }
.form-grid .wide { grid-column: 1 / -1; }
.switches { display: grid; gap: 18px; padding: 20px 0; border-top: 1px solid #dfe5e7; }
.save-row { display: flex; justify-content: flex-end; padding: 20px 0; }
.btn { display: inline-flex; align-items: center; gap: 7px; }
.alert, .notice { padding: 12px 16px; margin-bottom: 16px; border-left: 3px solid; font-size: 13px; overflow-wrap: anywhere; }
.alert { color: #b52d3c; background: #fff1f1; }.notice { color: #08745e; background: #eaf8f2; }
.details { display: grid; grid-template-columns: 140px minmax(0, 1fr); gap: 10px; font-size: 13px; }
.details dt { color: #819097; }.details dd { overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
.timeline { font-size: 13px; }.timeline li { display: flex; justify-content: space-between; gap: 12px; padding: 9px 0; border-bottom: 1px solid #edf0f1; }
.timeline time { color: #819097; }
.raw { max-height: 400px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 11px; padding: 12px; }
.dark .image-master th { background: #202427; }
.dark .image-master .tool:hover { background: #273136; }
.dark .image-master :is(.tabs, .metrics, .enable, .switches, .table-scroll, .tool, th, td) { border-color: #343c41; }
@media (max-width: 640px) {
  .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; }
  .metrics strong { font-size: 21px; }.form-grid { grid-template-columns: minmax(0, 1fr); }
  .filters select { flex: 1; min-width: 120px; }.search { flex-basis: 100%; }
  .details { grid-template-columns: 100px minmax(0, 1fr); }
  .heading .actions { gap: 6px; }.timeline li { flex-wrap: wrap; }
}
</style>
