<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { notifyError, notifySuccess } from '@/utils/notify'

const { t } = useI18n()

const loading = ref(false)
const submitting = ref(false)

const form = reactive({
  interval_minutes: 5,
  price_strategy: 'upstream_price' as 'upstream_price' | 'min_sku',
  pre_order_stock_check_enabled: true,
  sync_page_size: 50,
  sync_max_pages: 200,
  sync_conn_concurrency: 3,
})

const clamp = (value: unknown, min: number, max: number, fallback: number) => {
  const parsed = Number(value)
  if (Number.isNaN(parsed)) return fallback
  if (parsed < min) return min
  if (parsed > max) return max
  return parsed
}

const loadConfig = async () => {
  loading.value = true
  try {
    const res = await adminAPI.getSettings({ key: 'upstream_sync_config' })
    const data = res.data?.data as Record<string, unknown> | undefined
    if (data) {
      form.interval_minutes = clamp(data.interval_minutes, 5, 1440, 5)
      form.price_strategy = data.price_strategy === 'min_sku' ? 'min_sku' : 'upstream_price'
      form.pre_order_stock_check_enabled = data.pre_order_stock_check_enabled !== false
      form.sync_page_size = clamp(data.sync_page_size, 10, 50, 50)
      form.sync_max_pages = clamp(data.sync_max_pages, 10, 500, 200)
      form.sync_conn_concurrency = clamp(data.sync_conn_concurrency, 1, 10, 3)
    }
  } catch {
    // ignore load error, use defaults
  } finally {
    loading.value = false
  }
}

const save = async () => {
  submitting.value = true
  try {
    const normalized = {
      interval_minutes: clamp(form.interval_minutes, 5, 1440, 5),
      price_strategy: form.price_strategy,
      pre_order_stock_check_enabled: form.pre_order_stock_check_enabled,
      sync_page_size: clamp(form.sync_page_size, 10, 50, 50),
      sync_max_pages: clamp(form.sync_max_pages, 10, 500, 200),
      sync_conn_concurrency: clamp(form.sync_conn_concurrency, 1, 10, 3),
    }
    form.interval_minutes = normalized.interval_minutes
    form.sync_page_size = normalized.sync_page_size
    form.sync_max_pages = normalized.sync_max_pages
    form.sync_conn_concurrency = normalized.sync_conn_concurrency
    await adminAPI.updateSettings({
      key: 'upstream_sync_config',
      value: normalized,
    })
    notifySuccess(t('admin.settings.alerts.saveSuccess'))
  } catch (err) {
    const known = err as Error & { __notified?: boolean }
    if (!known?.__notified) {
      notifyError(known?.message || t('admin.settings.alerts.saveFailed'))
    }
  } finally {
    submitting.value = false
  }
}

defineExpose({ save, submitting })

onMounted(() => {
  loadConfig()
})
</script>

<template>
  <div class="space-y-6">
    <div class="rounded-lg border p-6">
      <h2 class="text-lg font-semibold">{{ t('admin.settings.upstreamSync.title') }}</h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.subtitle') }}</p>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.upstreamSync.interval.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.interval.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.upstreamSync.interval.minutesLabel') }}</label>
        <Input v-model.number="form.interval_minutes" type="number" min="5" max="1440" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.interval.minutesHint') }}</p>
      </div>
      <div class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
        {{ t('admin.settings.upstreamSync.interval.restartHint') }}
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.upstreamSync.priceStrategy.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.priceStrategy.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.upstreamSync.priceStrategy.label') }}</label>
        <Select v-model="form.price_strategy">
          <SelectTrigger>
            <SelectValue :placeholder="t('admin.settings.upstreamSync.priceStrategy.placeholder')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="upstream_price">{{ t('admin.settings.upstreamSync.priceStrategy.upstreamPrice') }}</SelectItem>
            <SelectItem value="min_sku">{{ t('admin.settings.upstreamSync.priceStrategy.minSku') }}</SelectItem>
          </SelectContent>
        </Select>
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.priceStrategy.hint') }}</p>
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.upstreamSync.preOrderCheck.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.preOrderCheck.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-3">
        <Switch v-model="form.pre_order_stock_check_enabled" />
        <Label class="text-sm font-medium">{{ t('admin.settings.upstreamSync.preOrderCheck.enabled') }}</Label>
      </div>
      <p class="text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.preOrderCheck.enabledHint') }}</p>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.upstreamSync.pageSize.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.pageSize.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.upstreamSync.pageSize.label') }}</label>
        <Input v-model.number="form.sync_page_size" type="number" min="10" max="50" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.pageSize.hint') }}</p>
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.upstreamSync.maxPages.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.maxPages.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.upstreamSync.maxPages.label') }}</label>
        <Input v-model.number="form.sync_max_pages" type="number" min="10" max="500" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.maxPages.hint') }}</p>
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.upstreamSync.concurrency.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.concurrency.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.upstreamSync.concurrency.label') }}</label>
        <Input v-model.number="form.sync_conn_concurrency" type="number" min="1" max="10" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.upstreamSync.concurrency.hint') }}</p>
      </div>
    </div>
  </div>
</template>
