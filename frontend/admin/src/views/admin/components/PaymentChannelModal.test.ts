// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive, type App } from 'vue'
import PaymentChannelModal from './PaymentChannelModal.vue'

const api = vi.hoisted(() => ({ getPaymentChannel: vi.fn(), getMemberLevels: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/components/admin/MediaPicker.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/ui/dialog', async () => {
  const { h } = await import('vue')
  const stub = { setup: (_: unknown, { slots }: any) => () => h('div', slots.default?.()) }
  return { Dialog: stub, DialogScrollContent: stub, DialogHeader: stub, DialogTitle: stub }
})
vi.mock('@/components/ui/select', async () => {
  const { h } = await import('vue')
  const stub = { setup: (_: unknown, { slots }: any) => () => h('div', slots.default?.()) }
  return { Select: stub, SelectContent: stub, SelectItem: stub, SelectTrigger: stub, SelectValue: stub }
})
vi.mock('@/components/ui/multi-select', () => ({ MultiSelect: { render: () => null } }))
vi.mock('@/components/ui/switch', () => ({ Switch: { render: () => null } }))

const channel = (id: number, secret: string) => ({ data: { data: {
  id, name: `channel-${id}`, provider_type: 'epusdt', channel_type: 'epusdt',
  interaction_mode: 'redirect', is_active: false,
  config_json: { gateway_url: 'https://example.invalid', pid: '1000', secret_key: secret, token: 'usdt', network: 'tron' },
} } })

const flush = async () => { await Promise.resolve(); await nextTick(); await Promise.resolve(); await nextTick() }
let app: App
let host: HTMLDivElement
let state: { modelValue: boolean; channelId: number | null }
const secretInput = () => host.querySelector<HTMLInputElement>('[placeholder="admin.paymentChannels.modal.epusdtSecretKeyPlaceholder"]')!

beforeEach(() => {
  api.getPaymentChannel.mockReset()
  api.getMemberLevels.mockResolvedValue({ data: { data: [] } })
  state = reactive({ modelValue: false, channelId: null })
  host = document.createElement('div')
  document.body.append(host)
  app = createApp({ setup: () => () => h(PaymentChannelModal, state) })
  app.mount(host)
})
afterEach(() => { app.unmount(); host.remove() })

describe('payment channel secret lifecycle', () => {
  it('renders a native password input even for a newly entered secret', async () => {
    api.getPaymentChannel.mockResolvedValue(channel(1, 'test-only-secret'))
    state.channelId = 1
    state.modelValue = true
    await flush()
    expect(secretInput().type).toBe('password')
    expect(secretInput().value).toBe('test-only-secret')
    expect(host.textContent).not.toContain('test-only-secret')
  })

  it('clears credentials on close and reloads when reopening the same channel', async () => {
    api.getPaymentChannel.mockResolvedValue(channel(1, 'test-only-secret'))
    state.channelId = 1
    state.modelValue = true
    await flush()
    state.modelValue = false
    await flush()
    expect(secretInput().value).toBe('')
    state.modelValue = true
    await flush()
    expect(api.getPaymentChannel).toHaveBeenCalledTimes(2)
    expect(secretInput().value).toBe('test-only-secret')
  })

  it('ignores a stale channel response after another channel loads', async () => {
    let resolveOld!: (value: ReturnType<typeof channel>) => void
    api.getPaymentChannel.mockImplementation((id: number) => id === 1
      ? new Promise(resolve => { resolveOld = resolve })
      : Promise.resolve(channel(2, 'new-test-secret')))
    state.channelId = 1
    state.modelValue = true
    await flush()
    state.channelId = 2
    await flush()
    expect(secretInput().value).toBe('new-test-secret')
    resolveOld(channel(1, 'old-test-secret'))
    await flush()
    expect(secretInput().value).toBe('new-test-secret')
  })

  it('does not restore credentials when a pending load finishes after close', async () => {
    let resolvePending!: (value: ReturnType<typeof channel>) => void
    api.getPaymentChannel.mockImplementation(() => new Promise(resolve => { resolvePending = resolve }))
    state.channelId = 1
    state.modelValue = true
    await flush()
    state.modelValue = false
    await flush()
    resolvePending(channel(1, 'late-test-secret'))
    await flush()
    expect(secretInput()).toBeNull()
  })
})
