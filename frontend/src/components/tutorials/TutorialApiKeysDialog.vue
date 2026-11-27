<template>
  <BaseDialog :show="true" :title="t('tutorials.apiKeys.title')" width="wide" @close="close">
    <div class="space-y-5" :aria-busy="loading || busy">
      <div v-if="secret" class="space-y-2 rounded-lg border border-amber-300 p-4 dark:border-amber-700">
        <p class="text-sm text-amber-800 dark:text-amber-200">{{ t('adminAccessKeys.secretWarning') }}</p>
        <code class="block select-all break-all">{{ secret }}</code>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary btn-sm" @click="copyToClipboard(secret)">
            <Icon name="copy" size="sm" class="mr-2" />{{ t('adminAccessKeys.copySecret') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" @click="secret = ''">
            {{ t('adminAccessKeys.dismissSecret') }}
          </button>
        </div>
      </div>

      <form class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end" @submit.prevent="generate">
        <label class="min-w-0 text-sm font-medium">
          {{ t('adminAccessKeys.name') }}
          <input v-model.trim="name" class="input mt-1" required maxlength="100" autocomplete="off" :disabled="busy" />
        </label>
        <label class="min-w-0 text-sm font-medium">
          {{ t('adminAccessKeys.expiration') }}
          <input v-model="expiration" class="input mt-1" type="datetime-local" :disabled="busy" />
        </label>
        <button type="submit" class="btn btn-primary" :disabled="!name || !!secret || busy || loading">
          <Icon name="plus" size="sm" class="mr-2" />{{ t('tutorials.apiKeys.generate') }}
        </button>
      </form>

      <div v-if="error" role="alert" class="flex flex-wrap items-center gap-2 break-words text-sm text-red-600 dark:text-red-400">
        <span>{{ error }}</span>
        <button type="button" class="underline" :disabled="loading || busy" @click="load">
          {{ t('tutorials.apiKeys.refresh') }}
        </button>
      </div>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('adminAccessKeys.loading') }}</p>
      <p v-else-if="!items.length && !error" class="text-sm text-gray-500 dark:text-dark-300">
        {{ t('tutorials.apiKeys.empty') }}
      </p>
      <ul v-else-if="items.length" class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <li v-for="item in items" :key="item.id" class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-3">
          <div class="min-w-0 space-y-1">
            <p class="break-words font-medium">{{ item.name }}</p>
            <div class="flex flex-wrap gap-x-3 gap-y-1 text-sm text-gray-500 dark:text-dark-300">
              <code class="break-all">{{ item.key_prefix }}&hellip;</code>
              <span>{{ status(item) }}</span>
              <span>{{ t('adminAccessKeys.expires', { value: item.expires_at ? formatDate(item.expires_at) : t('adminAccessKeys.never') }) }}</span>
              <span>{{ t('adminAccessKeys.lastUsed', { value: item.last_used_at ? formatDate(item.last_used_at) : t('adminAccessKeys.neverUsed') }) }}</span>
            </div>
          </div>
          <button v-if="!item.revoked_at" type="button" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400"
            :disabled="busy || !!secret" @click="pendingRevoke = item">
            {{ t('adminAccessKeys.revoke') }}
          </button>
        </li>
      </ul>
    </div>
  </BaseDialog>
  <ConfirmDialog
    :show="!!pendingRevoke"
    :title="t('adminAccessKeys.revoke')"
    :message="t('adminAccessKeys.revokeConfirm', { name: pendingRevoke?.name })"
    :confirm-text="t('adminAccessKeys.revoke')"
    danger
    @cancel="pendingRevoke = null"
    @confirm="revoke"
  />
  <TotpStepUpDialog :controller="stepUp" />
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { createTutorialKey, listTutorialKeys, revokeTutorialKey, type TutorialAdminKey } from '@/api/admin/adminAccessKeys'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { isStepUpCancelled, useStepUp } from '@/composables/useStepUp'

const emit = defineEmits<{ close: [] }>()
const { t, locale } = useI18n()
const stepUp = useStepUp()
const { copyToClipboard } = useClipboard()
const items = ref<TutorialAdminKey[]>([])
const pendingRevoke = ref<TutorialAdminKey | null>(null)
const name = ref('')
const expiration = ref('')
const secret = ref('')
const error = ref('')
const loading = ref(true)
const busy = ref(false)

function close() {
  if (!busy.value && !pendingRevoke.value) emit('close')
}

function formatDate(value: string) {
  return new Date(value).toLocaleString(locale.value)
}

function status(item: TutorialAdminKey) {
  if (item.revoked_at) return t('adminAccessKeys.statusRevoked')
  if (!item.enabled) return t('adminAccessKeys.statusDisabled')
  if (item.expires_at && Date.parse(item.expires_at) <= Date.now()) return t('adminAccessKeys.statusExpired')
  return t('adminAccessKeys.statusEnabled')
}

function report(cause: unknown) {
  if (!isStepUpCancelled(cause)) {
    error.value = (cause as { message?: string })?.message || t('adminAccessKeys.operationFailed')
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    items.value = await listTutorialKeys()
  } catch (cause) {
    report(cause)
  } finally {
    loading.value = false
  }
}

async function mutate(action: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try {
    await stepUp.run(action)
  } catch (cause) {
    report(cause)
  } finally {
    busy.value = false
  }
}

async function generate() {
  if (busy.value || loading.value || secret.value || !name.value) return
  const keyName = name.value
  const expiry = expiration.value ? new Date(expiration.value) : null
  if (expiry && !(expiry.getTime() > Date.now())) {
    error.value = t('adminAccessKeys.expirationInvalid')
    return
  }
  await mutate(async () => {
    const result = await createTutorialKey(keyName, expiry?.toISOString() ?? null)
    items.value = [result.item, ...items.value]
    name.value = ''
    expiration.value = ''
    secret.value = result.key
  })
}

async function revoke() {
  if (!pendingRevoke.value || busy.value) return
  const item = pendingRevoke.value
  pendingRevoke.value = null
  await mutate(async () => {
    await revokeTutorialKey(item.id)
    items.value = items.value.map(key => key.id === item.id
      ? { ...key, enabled: false, revoked_at: new Date().toISOString() }
      : key)
  })
}

onMounted(load)
</script>
