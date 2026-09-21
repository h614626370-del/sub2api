<template>
  <section class="space-y-6" aria-labelledby="codex-ticket-details-title">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h1 id="codex-ticket-details-title" class="text-xl font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.ticketDetails.title') }}
        </h1>
        <p class="mt-1 max-w-4xl text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.ticketDetails.description') }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-danger" :disabled="loading || discardingAll" @click="discardAllTickets">
          <Icon name="trash" size="sm" :class="['mr-1.5', discardingAll ? 'animate-pulse' : '']" />
          {{ t('admin.accounts.ticketDetails.discardAllTickets') }}
        </button>
        <button type="button" class="btn btn-secondary" :disabled="loading || discardingAll" @click="load">
          <Icon name="refresh" size="sm" :class="['mr-1.5', loading ? 'animate-spin' : '']" />
          {{ t('admin.accounts.ticketDetails.refresh') }}
        </button>
      </div>
    </div>

    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <div class="card p-5">
        <div class="flex items-center justify-between gap-3">
          <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ticketDetails.featureStatus') }}</span>
          <span class="h-2.5 w-2.5 rounded-full" :class="settings.enabled ? 'bg-emerald-500' : 'bg-gray-400'" />
        </div>
        <p class="mt-2 text-lg font-semibold" :class="settings.enabled ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-600 dark:text-gray-300'">
          {{ settings.enabled ? t('admin.accounts.ticketDetails.enabled') : t('admin.accounts.ticketDetails.disabled') }}
        </p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ settings.proxyConfigured ? t('admin.accounts.ticketDetails.proxyConfigured') : t('admin.accounts.ticketDetails.proxyNotConfigured') }}
        </p>
      </div>

      <div class="card p-5">
        <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ticketDetails.eligibleAccounts') }}</span>
        <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ eligibleAccounts.length }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ticketDetails.eligibleHint') }}</p>
      </div>

      <div class="card p-5">
        <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ticketDetails.readyTickets') }}</span>
        <p class="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">{{ readyCount }} / {{ ticketRows.length }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ticketDetails.readyHint', { models: modelNames.length }) }}</p>
      </div>

      <div class="card p-5">
        <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ticketDetails.blockedTickets') }}</span>
        <p class="mt-2 text-2xl font-semibold text-amber-600 dark:text-amber-400">{{ blockedCount }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ticketDetails.blockedHint') }}</p>
      </div>
    </div>

    <div class="card overflow-hidden">
      <div class="flex flex-col gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.accounts.ticketDetails.tableTitle') }}</h2>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ lastLoadedAt ? t('admin.accounts.ticketDetails.lastUpdated', { time: formatDateTime(lastLoadedAt) }) : t('admin.accounts.ticketDetails.notLoaded') }}
          </p>
        </div>
        <div class="flex flex-col gap-2 sm:flex-row">
          <input
            v-model="search"
            type="search"
            class="input min-w-0 sm:w-64"
            :placeholder="t('admin.accounts.ticketDetails.searchPlaceholder')"
          />
          <select v-model="stateFilter" class="select sm:w-44">
            <option value="all">{{ t('admin.accounts.ticketDetails.filters.all') }}</option>
            <option value="ready">{{ t('admin.accounts.ticketDetails.filters.ready') }}</option>
            <option value="missing">{{ t('admin.accounts.ticketDetails.filters.missing') }}</option>
            <option value="blocked">{{ t('admin.accounts.ticketDetails.filters.blocked') }}</option>
          </select>
        </div>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-16 text-sm text-gray-500 dark:text-gray-400">
        <Icon name="refresh" size="sm" class="mr-2 animate-spin" />
        {{ t('common.loading') }}
      </div>
      <div v-else-if="errorMessage" class="px-5 py-12 text-center text-sm text-red-600 dark:text-red-400">
        {{ errorMessage }}
      </div>
      <div v-else-if="filteredRows.length === 0" class="px-5 py-16 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ settings.enabled ? t('admin.accounts.ticketDetails.empty') : t('admin.accounts.ticketDetails.disabledEmpty') }}
      </div>
      <div v-else class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
          <thead class="bg-gray-50 text-left text-xs font-medium uppercase tracking-wide text-gray-500 dark:bg-dark-800/70 dark:text-gray-400">
            <tr>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.account') }}</th>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.model') }}</th>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.status') }}</th>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.length') }}</th>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.attempts') }}</th>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.remaining') }}</th>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.expiresAt') }}</th>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.scheduling') }}</th>
              <th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.columns.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="row in filteredRows" :key="`${row.account.id}-${row.ticket.model}`" class="align-top hover:bg-gray-50/70 dark:hover:bg-dark-800/50">
              <td class="whitespace-nowrap px-5 py-4">
                <div class="font-medium text-gray-900 dark:text-white">{{ row.account.name || `#${row.account.id}` }}</div>
                <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">#{{ row.account.id }} · {{ row.account.type }}</div>
              </td>
              <td class="px-5 py-4 font-mono text-xs text-gray-700 dark:text-gray-300">{{ row.ticket.model }}</td>
              <td class="whitespace-nowrap px-5 py-4">
                <span class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium" :class="statusClass(row.ticket)">
                  {{ statusLabel(row.ticket) }}
                </span>
              </td>
              <td class="whitespace-nowrap px-5 py-4 text-gray-700 dark:text-gray-300">
                <span v-if="row.ticket.length">{{ row.ticket.length }}</span>
                <span v-else class="text-gray-400">-</span>
                <span class="ml-1 text-xs text-gray-400">/ 292</span>
              </td>
              <td class="whitespace-nowrap px-5 py-4 text-gray-700 dark:text-gray-300">{{ row.ticket.attempts || 0 }}</td>
              <td class="whitespace-nowrap px-5 py-4 text-gray-700 dark:text-gray-300">
                <span v-if="row.ticket.ready">{{ formatRemaining(row.ticket.remaining_seconds) }}</span>
                <span v-else class="text-gray-400">-</span>
              </td>
              <td class="whitespace-nowrap px-5 py-4 text-gray-700 dark:text-gray-300">
                <span v-if="row.ticket.expires_at">{{ formatDateTime(row.ticket.expires_at) }}</span>
                <span v-else class="text-gray-400">-</span>
              </td>
              <td class="px-5 py-4">
                <div class="flex flex-wrap gap-1.5">
                  <span class="rounded-md px-2 py-1 text-xs" :class="row.account.schedulable ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">
                    {{ row.account.schedulable ? t('admin.accounts.ticketDetails.schedulable') : t('admin.accounts.ticketDetails.notSchedulable') }}
                  </span>
                  <span class="rounded-md px-2 py-1 text-xs" :class="(row.account.group_ids?.length ?? 0) > 0 ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'">
                    {{ (row.account.group_ids?.length ?? 0) > 0 ? t('admin.accounts.ticketDetails.grouped') : t('admin.accounts.ticketDetails.ungrouped') }}
                  </span>
                </div>
              </td>
              <td class="whitespace-nowrap px-5 py-4">
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="loading || discardingAll || discardingKey === `${row.account.id}-${row.ticket.model}`"
                  @click="discardAccountTicket(row.account.id, row.ticket.model)"
                >
                  <Icon v-if="discardingKey === `${row.account.id}-${row.ticket.model}`" name="refresh" size="sm" class="mr-1 animate-spin" />
                  {{ t('admin.accounts.ticketDetails.discardTicket') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <p class="text-xs text-gray-400 dark:text-gray-500">
      {{ t('admin.accounts.ticketDetails.limitations') }}
    </p>

    <div class="card overflow-hidden">
      <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.accounts.ticketDetails.auditTitle') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ticketDetails.auditHint') }}</p>
          </div>
          <button type="button" class="btn btn-danger btn-sm self-start" :disabled="auditLoading || clearingAudits" @click="clearAuditRecords">
            <Icon name="trash" size="sm" :class="['mr-1.5', clearingAudits ? 'animate-pulse' : '']" />
            {{ t('admin.accounts.ticketDetails.clearAudits') }}
          </button>
        </div>
      </div>
      <div v-if="auditLoading" class="px-5 py-10 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="audits.length === 0" class="px-5 py-10 text-center text-sm text-gray-500">{{ t('admin.accounts.ticketDetails.auditEmpty') }}</div>
      <div v-else class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
          <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800/70 dark:text-gray-400">
            <tr><th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.auditColumns.time') }}</th><th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.auditColumns.account') }}</th><th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.auditColumns.result') }}</th><th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.auditColumns.reason') }}</th><th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.auditColumns.attempts') }}</th><th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.auditColumns.ip') }}</th><th class="px-5 py-3">{{ t('admin.accounts.ticketDetails.auditColumns.detail') }}</th></tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <template v-for="audit in audits" :key="audit.id">
              <tr class="align-top">
                <td class="whitespace-nowrap px-5 py-4 text-xs text-gray-600 dark:text-gray-300">{{ formatDateTime(audit.created_at) }}<div class="mt-1 text-gray-400">{{ audit.duration_ms }}ms</div></td>
                <td class="px-5 py-4 text-xs"><div class="font-medium">#{{ audit.account_id }}</div><div class="font-mono text-gray-500">{{ audit.model }}</div></td>
                <td class="px-5 py-4"><span class="rounded-full px-2 py-1 text-xs" :class="audit.outcome === 'success' ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-red-700'">{{ audit.outcome }}</span><div v-if="audit.http_status" class="mt-1 text-xs text-gray-500">HTTP {{ audit.http_status }}</div></td>
                <td class="max-w-xs px-5 py-4 text-xs text-gray-600 dark:text-gray-300">{{ audit.reason || '-' }}</td>
                <td class="px-5 py-4 text-xs text-gray-600 dark:text-gray-300">{{ audit.attempts || 0 }}</td>
                <td class="px-5 py-4 font-mono text-xs text-gray-600 dark:text-gray-300">{{ audit.egress_ip || '-' }}</td>
                <td class="px-5 py-4"><button type="button" class="btn btn-secondary btn-sm" @click="expandedAudit = expandedAudit === audit.id ? null : audit.id">{{ expandedAudit === audit.id ? t('admin.accounts.ticketDetails.collapse') : t('admin.accounts.ticketDetails.expand') }}</button></td>
              </tr>
              <tr v-if="expandedAudit === audit.id"><td colspan="7" class="bg-gray-50 px-5 py-4 dark:bg-dark-800/50"><div v-if="auditDetailLoading" class="text-sm text-gray-500">{{ t('common.loading') }}</div><div v-else-if="auditDetailError" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.accounts.ticketDetails.detailLoadFailed') }}</div><div v-else class="grid gap-4 lg:grid-cols-2"><div><div class="mb-1 text-xs font-semibold text-gray-500">{{ t('admin.accounts.ticketDetails.requestBody') }}</div><pre class="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded bg-gray-900 p-3 text-xs leading-5 text-gray-100">{{ auditDetail?.id === audit.id ? formatPayload(auditDetail.request_body) : '-' }}</pre></div><div><div class="mb-1 text-xs font-semibold text-gray-500">{{ t('admin.accounts.ticketDetails.responseHeaders') }}</div><pre class="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded bg-gray-900 p-3 text-xs leading-5 text-gray-100">{{ auditDetail?.id === audit.id ? formatHeaders(auditDetail.response_headers) : '-' }}</pre></div></div></td></tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import type { AccountListItem } from '@/types'
import type { CodexTicketAudit } from '@/api/admin/codexTicket'

type Ticket = NonNullable<AccountListItem['codex_turn_tickets']>[number]
type TicketState = 'all' | 'ready' | 'missing' | 'blocked'

interface TicketRow {
  account: AccountListItem
  ticket: Ticket
}

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const errorMessage = ref('')
const search = ref('')
const stateFilter = ref<TicketState>('all')
const lastLoadedAt = ref<string | null>(null)
const audits = ref<CodexTicketAudit[]>([])
const auditDetail = ref<CodexTicketAudit | null>(null)
const auditDetailLoading = ref(false)
const auditDetailError = ref(false)
const auditLoading = ref(false)
const expandedAudit = ref<number | null>(null)
const discardingAll = ref(false)
const discardingKey = ref<string | null>(null)
const clearingAudits = ref(false)
const accounts = ref<AccountListItem[]>([])
const settings = reactive({
  enabled: false,
  proxyConfigured: false,
})

const eligibleAccounts = computed(() => accounts.value.filter(isCurrentlyHarvestable))

const ticketRows = computed<TicketRow[]>(() => accounts.value.flatMap((account) => (
  (account.codex_turn_tickets ?? []).map((ticket) => ({ account, ticket }))
)))

const modelNames = computed(() => [...new Set(ticketRows.value.map((row) => row.ticket.model))])
const readyCount = computed(() => ticketRows.value.filter((row) => row.ticket.ready).length)
const blockedCount = computed(() => ticketRows.value.filter((row) => row.ticket.blocked).length)

const filteredRows = computed(() => {
  const needle = search.value.trim().toLowerCase()
  return ticketRows.value.filter((row) => {
    const state = row.ticket.blocked ? 'blocked' : row.ticket.ready ? 'ready' : 'missing'
    if (stateFilter.value !== 'all' && state !== stateFilter.value) return false
    if (!needle) return true
    return [row.account.name, String(row.account.id), row.ticket.model].some((value) => value.toLowerCase().includes(needle))
  })
})

function statusLabel(ticket: Ticket): string {
  if (ticket.blocked) return t('admin.accounts.ticketDetails.statuses.blocked')
  if (ticket.ready) return t('admin.accounts.ticketDetails.statuses.ready')
  return t('admin.accounts.ticketDetails.statuses.missing')
}

function statusClass(ticket: Ticket): string {
  if (ticket.blocked) return 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  if (ticket.ready) return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}

function formatRemaining(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds || 0))
  if (value < 60) return `${value}s`
  const minutes = Math.floor(value / 60)
  if (minutes < 60) return `${minutes}m ${value % 60}s`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ${minutes % 60}m`
}

function formatPayload(value: string | null | undefined): string {
  const raw = value?.trim()
  if (!raw) return '-'
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return value || '-'
  }
}

function formatHeaders(headers: Record<string, string> | null | undefined): string {
  if (!headers || Object.keys(headers).length === 0) return t('admin.accounts.ticketDetails.responseHeadersUnavailable')
  return Object.entries(headers)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([name, value]) => `${name}: ${value}`)
    .join('\n')
}

function toTimestamp(value: string | number | null | undefined): number | null {
  if (value === null || value === undefined || value === '') return null
  if (typeof value === 'number') return value < 10_000_000_000 ? value * 1000 : value
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? parsed : null
}

function isFuture(value: string | null | undefined): boolean {
  const timestamp = toTimestamp(value)
  return timestamp !== null && timestamp > Date.now()
}

function isCurrentlyHarvestable(account: AccountListItem): boolean {
  if (
    account.platform !== 'openai'
    || (account.type !== 'oauth' && account.type !== 'setup-token')
    || account.parent_account_id
    || account.status !== 'active'
    || !account.schedulable
    || (account.group_ids?.length ?? 0) === 0
  ) return false

  if (account.auto_pause_on_expired && toTimestamp(account.expires_at) !== null && toTimestamp(account.expires_at)! <= Date.now()) return false
  return ![account.overload_until, account.rate_limit_reset_at, account.temp_unschedulable_until].some(isFuture)
}

async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    const currentSettings = await adminAPI.settings.getSettings()
    auditLoading.value = true
    adminAPI.codexTicket.listAudits({ page: 1, page_size: 50 }).then((response) => { audits.value = response.items }).catch(() => { audits.value = [] }).finally(() => { auditLoading.value = false })
    settings.enabled = Boolean(currentSettings.openai_codex_ticket_enabled)
    settings.proxyConfigured = Boolean(currentSettings.openai_codex_ticket_harvest_proxy_configured)

    const pageSize = 1000
    const allAccounts: AccountListItem[] = []
    let page = 1
    let total = 0
    do {
      const response = await adminAPI.accounts.list(page, pageSize, {
        platform: 'openai',
        lite: '1',
        sort_by: 'name',
        sort_order: 'asc',
      })
      allAccounts.push(...response.items)
      total = response.total
      page += 1
      if (response.items.length === 0) break
    } while (allAccounts.length < total)

    accounts.value = allAccounts
    lastLoadedAt.value = new Date().toISOString()
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error, t('admin.accounts.ticketDetails.loadFailed'))
    appStore.showError(errorMessage.value)
  } finally {
    loading.value = false
  }
}

async function discardAccountTicket(accountId: number, model: string) {
  const key = `${accountId}-${model}`
  if (!window.confirm(t('admin.accounts.ticketDetails.discardConfirm', { account: `#${accountId}`, model }))) return
  discardingKey.value = key
  try {
    await adminAPI.codexTicket.discard(accountId)
    appStore.showSuccess(t('admin.accounts.ticketDetails.discardSuccess'))
    await load()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.ticketDetails.discardFailed')))
  } finally {
    discardingKey.value = null
  }
}

async function discardAllTickets() {
  if (!window.confirm(t('admin.accounts.ticketDetails.discardAllConfirm'))) return
  discardingAll.value = true
  try {
    await adminAPI.codexTicket.discard()
    appStore.showSuccess(t('admin.accounts.ticketDetails.discardSuccess'))
    await load()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.ticketDetails.discardFailed')))
  } finally {
    discardingAll.value = false
  }
}

async function clearAuditRecords() {
  if (!window.confirm(t('admin.accounts.ticketDetails.clearAuditsConfirm'))) return
  clearingAudits.value = true
  try {
    await adminAPI.codexTicket.clearAudits()
    audits.value = []
    expandedAudit.value = null
    auditDetail.value = null
    appStore.showSuccess(t('admin.accounts.ticketDetails.clearAuditsSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.ticketDetails.clearAuditsFailed')))
  } finally {
    clearingAudits.value = false
  }
}

watch(expandedAudit, async (id) => {
  auditDetail.value = null
  auditDetailError.value = false
  if (id === null) { auditDetailLoading.value = false; return }
  auditDetailLoading.value = true
  try {
    auditDetail.value = await adminAPI.codexTicket.getAudit(id)
  } catch {
    auditDetailError.value = true
  } finally {
    auditDetailLoading.value = false
  }
})

onMounted(load)
</script>
