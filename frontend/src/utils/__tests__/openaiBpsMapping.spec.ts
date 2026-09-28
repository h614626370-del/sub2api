import { describe, expect, it } from 'vitest'
import { resolveBPSMappedModel } from '../openaiBps'

describe('BPS connection test model mapping', () => {
  it('prefers exact mappings over wildcard mappings', () => {
    expect(resolveBPSMappedModel('alias', { alias: 'gpt-6-astra', '*': 'other' })).toBe('gpt-6-astra')
  })

  it('uses the longest matching prefix', () => {
    expect(resolveBPSMappedModel('alias-sol', { '*': 'other', 'alias-*': 'gpt-5.6-sol' })).toBe('gpt-5.6-sol')
  })

  it('matches trimmed names and preserves unmatched names', () => {
    expect(resolveBPSMappedModel(' alias ', { alias: 'gpt-6-astra' })).toBe('gpt-6-astra')
    expect(resolveBPSMappedModel('other', { alias: 'gpt-6-astra' })).toBe('other')
    expect(resolveBPSMappedModel('other')).toBe('other')
  })
})
