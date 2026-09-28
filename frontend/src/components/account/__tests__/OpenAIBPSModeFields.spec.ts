import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import OpenAIBPSModeFields from '../OpenAIBPSModeFields.vue'

describe('OpenAI OAuth BPS mode', () => {
  const render = (enabled: boolean) => mount(OpenAIBPSModeFields, {
    props: { enabled, models: 'gpt-6-astra\ngpt-5.6-sol' },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })] }
  })
  it('is off by default and never requests separate credentials', () => {
    const wrapper = render(false)
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.find('input[type=password]').exists()).toBe(false)
  })
  it('emits a toggle and editable model list without altering OAuth credentials', async () => {
    const wrapper = render(false)
    await wrapper.get('input').setValue(true)
    expect(wrapper.emitted('update:enabled')?.[0]).toEqual([true])
    await wrapper.setProps({ enabled: true })
    const models = wrapper.get('textarea')
    expect(models.element.value).toBe('gpt-6-astra\ngpt-5.6-sol')
    await models.setValue('custom-model')
    expect(wrapper.emitted('update:models')?.[0]).toEqual(['custom-model'])
  })
})
