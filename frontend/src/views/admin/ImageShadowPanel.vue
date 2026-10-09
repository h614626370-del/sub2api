<template>
  <section class="shadow-panel" role="tabpanel" :aria-label="t('imageMaster.shadow')">
    <header class="intro">
      <div><h2>{{ t('imageMaster.shadowTitle') }}</h2><p>{{ t('imageMaster.shadowIntro') }}</p></div>
      <span class="status-label">{{ t(data?.config.enabled ? 'imageMaster.shadowEnabled' : 'imageMaster.disabled') }}</span>
    </header>
    <p v-if="error" class="feedback error" role="alert">{{ error }}</p>
    <p v-if="notice" class="feedback" role="status">{{ notice }}</p>
    <p v-if="data?.storage_error" class="feedback error" role="alert">{{ t('imageMaster.storageError') }}</p>
    <p v-if="!data" role="status">{{ t('imageMaster.loading') }}</p>
    <template v-if="data && draft">
      <form class="configuration" @submit.prevent="save">
        <fieldset :disabled="saving">
          <label class="toggle"><input v-model="draft.enabled" type="checkbox" role="switch" data-testid="shadow-enabled" />{{ t('imageMaster.shadowEnable') }}</label>
          <div class="fields">
            <label>{{ t('imageMaster.shadowGroup') }}
              <select v-model.number="draft.group_id" class="input" :required="draft.enabled" data-testid="shadow-group">
                <option :value="0" disabled>{{ t('imageMaster.shadowSelectGroup') }}</option>
                <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }} · #{{ group.id }}</option>
                <option v-if="draft.group_id && !groups.some(g => g.id === draft!.group_id)" :value="draft.group_id">#{{ draft.group_id }} · {{ t('imageMaster.shadowUnavailable') }}</option>
              </select>
            </label>
            <label>{{ t('imageMaster.shadowKey') }}<input v-model.number="draft.api_key_id" class="input" type="number" :min="draft.enabled ? 1 : 0" step="1" :required="draft.enabled" data-testid="shadow-key" /></label>
          </div>
          <p class="help">{{ t('imageMaster.shadowBilling') }}</p>
          <p class="help">{{ t('imageMaster.shadowLimits') }}</p>
          <div class="save-row"><button class="btn btn-primary" :disabled="saving || loading || !dirty" type="submit">{{ t(saving ? 'imageMaster.loading' : 'imageMaster.save') }}</button></div>
        </fieldset>
      </form>
      <div class="section-heading"><div><h2>{{ t('imageMaster.shadowComparison') }}</h2><p class="help">{{ t('imageMaster.shadowDenominator', { count: paired.length }) }}</p></div><button class="btn btn-secondary" :disabled="loading || saving" @click="load">{{ t('imageMaster.refresh') }}</button></div>
      <div class="comparison-scroll">
        <table class="comparison"><caption class="sr-only">{{ t('imageMaster.shadowComparison') }}</caption>
          <thead><tr><th>{{ t('imageMaster.shadowMetric') }}</th><th>{{ t('imageMaster.shadowOriginal') }}</th><th>{{ t('imageMaster.shadowTest') }}</th></tr></thead>
          <tbody>
            <tr><th>{{ t('imageMaster.successRate') }}</th><td>{{ rate('original') }}</td><td>{{ rate('test') }}</td></tr>
            <tr><th>{{ t('imageMaster.latency') }}</th><td>{{ median('original') }}</td><td>{{ median('test') }}</td></tr>
          </tbody>
        </table>
      </div>
      <p class="counters">{{ t('imageMaster.shadowCounts', { active: data.active, unsupported: unsupported, unknown: unknown, large: data.large_skipped }) }}</p>
      <div class="section-heading"><h2>{{ t('imageMaster.shadowRecent') }}</h2><button class="btn btn-secondary" :disabled="!data.items.length" @click="download">{{ t('imageMaster.export') }}</button></div>
      <div class="filters">
        <input v-model="search" class="input" :aria-label="t('imageMaster.search')" :placeholder="t('imageMaster.search')" />
        <select v-model="filter" class="input" :aria-label="t('imageMaster.status')"><option value="">{{ t('imageMaster.all') }}</option><option value="difference">{{ t('imageMaster.shadowDifference') }}</option><option value="unsupported">{{ t('imageMaster.shadowStates.unsupported') }}</option><option value="failed">{{ t('imageMaster.shadowStates.failed') }}</option></select>
      </div>
      <div class="comparison-scroll">
        <table class="records"><thead><tr><th>{{ t('imageMaster.request') }}</th><th>{{ t('imageMaster.model') }}</th><th>{{ t('imageMaster.shadowOriginal') }}</th><th>{{ t('imageMaster.shadowTest') }}</th><th>{{ t('imageMaster.errorCode') }}</th></tr></thead>
          <tbody><tr v-for="row in paged" :key="row.id">
            <td><span class="mono" :title="row.request_id || row.id">{{ (row.request_id || row.id).slice(0, 12) }}</span><small>{{ new Date(row.started_at).toLocaleString() }}</small><small>{{ t('imageMaster.user') }} #{{ row.user_id }} · {{ t('imageMaster.shadowGroupPair', { source: row.source_group_id, target: row.group_id }) }}</small></td>
            <td>{{ row.requested_model }}<small>→ {{ row.model || '—' }}</small><small>{{ t(`imageMaster.routes.${row.route}`) }}</small></td>
            <td v-for="side in (['original', 'test'] as const)" :key="side"><span class="result" :class="row[side].outcome">{{ t(`imageMaster.shadowStates.${row[side].outcome}`) }}</span><small>{{ seconds(row[side].duration_ms) }} · {{ row[side].images }} {{ t('imageMaster.shadowImages') }}</small><small v-if="row[side].status">HTTP {{ row[side].status }}</small></td>
            <td class="shadow-reason">{{ reason(row.test.code || row.original.code) }}<small v-if="row.test.code || row.original.code">{{ row.test.code || row.original.code }}</small><small>#{{ row.id }}</small><small>{{ t('imageMaster.shadowKey') }} #{{ row.api_key_id }}</small></td>
          </tr><tr v-if="!filtered.length"><td colspan="5" class="empty">{{ t('imageMaster.shadowEmpty') }}</td></tr></tbody>
        </table>
      </div>
      <footer class="section-heading"><span class="help">{{ t('imageMaster.page', { page, pages, total: filtered.length }) }}</span><div class="pager"><button class="btn btn-secondary" :disabled="page <= 1" @click="page--">{{ t('imageMaster.previous') }}</button><button class="btn btn-secondary" :disabled="page >= pages" @click="page++">{{ t('imageMaster.next') }}</button></div></footer>
      <p class="help">{{ t('imageMaster.shadowRetention') }}</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { imageMasterAPI, type ShadowConfig, type ShadowRecord, type ShadowStatus } from '@/api/admin/imageMaster'
import { getAll } from '@/api/admin/groups'
const { t } = useI18n()
const data = ref<ShadowStatus | null>(null), draft = ref<ShadowConfig | null>(null)
const groups = ref<{ id: number; name: string }[]>([])
const loading = ref(false), saving = ref(false), error = ref(''), notice = ref('')
const search = ref(''), filter = ref(''), page = ref(1)
let alive = true, timer: ReturnType<typeof setInterval> | undefined
const dirty = computed(() => JSON.stringify(draft.value) !== JSON.stringify(data.value?.config))
const known = (value: string) => ['completed', 'failed', 'no_image', 'disconnected'].includes(value)
const paired = computed(() => data.value?.items.filter(r => known(r.original.outcome) && ['completed', 'failed'].includes(r.test.outcome)) || [])
const unsupported = computed(() => data.value?.items.filter(r => r.test.outcome === 'unsupported').length || 0)
const unknown = computed(() => data.value?.items.filter(r => r.original.outcome === 'unknown' || r.test.outcome === 'unknown').length || 0)
const seconds = (ms: number) => ms ? t('imageMaster.seconds', { value: (ms / 1000).toFixed(1) }) : '—'
const reasonCodes = ['direct_image_unsupported', 'invalid_image_model', 'unsupported_response_option', 'unsupported_image_option', 'file_id_not_supported', 'image_missing', 'image_url_blocked', 'shadow_timeout_or_canceled', 'observation_limit', 'process_restarted', 'gateway_http_403', 'gateway_http_401', 'gateway_http_429', 'image_direct_required']
const reason = (code?: string) => code ? (reasonCodes.includes(code) ? t(`imageMaster.shadowReasons.${code}`) : code) : '—'
function rate(side: 'original' | 'test') {
  const count = paired.value.filter(r => r[side].outcome === 'completed').length
  return paired.value.length ? `${(100 * count / paired.value.length).toFixed(1)}% · ${count}/${paired.value.length}` : '—'
}
function median(side: 'original' | 'test') {
  const values = paired.value.filter(r => r[side].outcome === 'completed').map(r => r[side].duration_ms).sort((a,b) => a-b)
  const n = values.length
  return n ? seconds(n % 2 ? values[Math.floor(n/2)] : (values[n/2-1] + values[n/2]) / 2) : '—'
}
const filtered = computed(() => (data.value?.items || []).filter(r => {
  const match = !filter.value || (filter.value === 'difference' ? known(r.original.outcome) && known(r.test.outcome) && (r.original.outcome === 'completed') !== (r.test.outcome === 'completed') : r.test.outcome === filter.value)
  return match && `${r.id} ${r.request_id} ${r.requested_model} ${r.model} ${r.test.code || ''} ${r.original.code || ''}`.toLowerCase().includes(search.value.trim().toLowerCase())
}))
const pages = computed(() => Math.max(1, Math.ceil(filtered.value.length / 25)))
const paged = computed<ShadowRecord[]>(() => filtered.value.slice((page.value-1)*25, page.value*25))
watch([search, filter], () => { page.value = 1 })
watch(pages, n => { page.value = Math.min(page.value, n) })
const message = (e: unknown) => (e as { message?: string })?.message || t('imageMaster.error')
async function load() {
  if (loading.value || saving.value) return
  loading.value = true
  try {
    const next = await imageMasterAPI.shadow()
    if (!alive) return
    const keep = draft.value && dirty.value
    data.value = next
    if (!keep) draft.value = { ...next.config }
    error.value = ''
  } catch (e) { if (alive) error.value = message(e) }
  finally { loading.value = false }
}
async function save() {
  if (!draft.value || saving.value || loading.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const saved = await imageMasterAPI.saveShadow({ ...draft.value })
    if (alive && data.value) { data.value.config = saved; draft.value = { ...saved }; notice.value = t('imageMaster.saved') }
  } catch (e) { if (alive) error.value = message(e) }
  finally { saving.value = false }
}
function download() {
  const url = URL.createObjectURL(new Blob([JSON.stringify(data.value?.items || [], null, 2)], { type: 'application/json' }))
  const a = document.createElement('a'); a.href = url; a.download = 'image-shadow-comparison.json'; a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
onMounted(async () => {
  void load()
  timer = setInterval(() => { if (!document.hidden) void load() }, 5000)
  try { const all = await getAll('openai'); if (alive) groups.value = all }
  catch (e) { if (alive) error.value = message(e) }
})
onBeforeUnmount(() => { alive = false; clearInterval(timer) })
</script>

<style scoped>
.shadow-panel { padding: 24px 0; min-width: 0; }
.intro, .section-heading { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; }
h2 { font-size: 16px; font-weight: 600; }
.intro p, .help, small { color: #52616b; font-size: 12px; line-height: 1.7; }
.intro p { max-width: 75ch; margin-top: 8px; }
.status-label { font-size: 12px; font-weight: 600; }
.configuration { padding: 24px 0; border-bottom: 1px solid #dfe5e7; }
.toggle { display: flex; gap: 9px; align-items: center; font-size: 14px; font-weight: 600; }
.toggle input { width: 16px; height: 16px; accent-color: #008577; }
.fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin: 20px 0 12px; }
.fields label { display: flex; flex-direction: column; gap: 8px; font-size: 13px; min-width: 0; }
.save-row { margin-top: 16px; display: flex; justify-content: flex-end; }
.section-heading { margin: 24px 0 12px; }.pager { display: flex; gap: 8px; }
.comparison-scroll { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { text-align: left; padding: 12px; border-bottom: 1px solid #dfe5e7; vertical-align: top; }
th { font-weight: 500; color: #52616b; }
.comparison td { font-variant-numeric: tabular-nums; font-size: 16px; }.comparison th { width: 33%; }
.records { min-width: 840px; }small { display: block; margin-top: 4px; }
.mono, .shadow-reason small { font-family: ui-monospace, monospace; }.shadow-reason { max-width: 260px; overflow-wrap: anywhere; }
.result { display: inline-block; padding: 3px 7px; border-radius: 4px; background: #edf1f2; color: #46565e; font-size: 12px; }
.completed { background: #e5f5ee; color: #07624f; }.failed, .disconnected { background: #fcecef; color: #a32437; }
.counters { margin-top: 14px; font-size: 12px; line-height: 1.8; color: #52616b; }
.filters { display: flex; gap: 12px; margin: 16px 0; }.filters input { flex: 1; min-width: 0; }.filters select { width: 170px; }
.empty { text-align: center; padding: 40px 20px; white-space: normal; color: #52616b; }
.feedback { padding: 12px; background: #e5f5ee; color: #07624f; border-radius: 6px; margin-top: 16px; font-size: 13px; }.feedback.error { background: #fcecef; color: #a32437; }
.dark .shadow-panel :is(.help, small, .intro p, .counters, th, .empty) { color: #b5c2ca; }
.dark .shadow-panel :is(.configuration, th, td) { border-color: #37444c; }
@media (max-width: 850px) { .fields { grid-template-columns: 1fr 1fr; } }
@media (max-width: 480px) { .fields { grid-template-columns: 1fr; }.filters { flex-wrap: wrap; }.filters select { width: 100%; } }
</style>
