import { describe, expect, it } from 'vitest'
import { effectiveGroupPlatform, hasDisplayPlatformOverride } from '@/utils/groupPlatform'

describe('effectiveGroupPlatform', () => {
  it('falls back to the real platform when no display override is set', () => {
    expect(effectiveGroupPlatform({ platform: 'grok' })).toBe('grok')
  })

  it('prefers display_platform when set', () => {
    expect(
      effectiveGroupPlatform({ platform: 'grok', display_platform: 'anthropic' }),
    ).toBe('anthropic')
  })

  it('treats empty display_platform as no override', () => {
    expect(
      effectiveGroupPlatform({ platform: 'grok', display_platform: '' }),
    ).toBe('grok')
  })

  it('handles null/undefined groups with a safe default', () => {
    expect(effectiveGroupPlatform(null)).toBe('anthropic')
    expect(effectiveGroupPlatform(undefined)).toBe('anthropic')
  })
})

describe('hasDisplayPlatformOverride', () => {
  it('is false without display_platform', () => {
    expect(hasDisplayPlatformOverride({ platform: 'grok' })).toBe(false)
  })

  it('is true when display differs from the real platform', () => {
    expect(
      hasDisplayPlatformOverride({ platform: 'grok', display_platform: 'anthropic' }),
    ).toBe(true)
  })

  it('is false when display equals the real platform', () => {
    expect(
      hasDisplayPlatformOverride({ platform: 'grok', display_platform: 'grok' }),
    ).toBe(false)
  })

  it('is false for null', () => {
    expect(hasDisplayPlatformOverride(null)).toBe(false)
  })
})
