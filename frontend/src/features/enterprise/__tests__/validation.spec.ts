import { describe, expect, it } from 'vitest'
import { validateBrand, validateDepartmentName, validateImageFile, validatePassword } from '@/features/enterprise/validation'

describe('enterprise form validation', () => {
  it('matches backend text and password limits', () => {
    expect(validatePassword('short')).toBeTruthy()
    expect(validatePassword('123456789012')).toBe('')
    expect(validateDepartmentName('部'.repeat(101))).toBeTruthy()
    expect(validateBrand({ enterprise_name: 'Acme', title: '企'.repeat(41), body: '', slogan: '', background_url: '', background_content_type: '', background_sha256: '', background_size_bytes: 0 })).toBeTruthy()
  })

  it('accepts only platform-controlled background locations and ignores server-owned metadata', () => {
    expect(validateBrand({
      enterprise_name: 'Example Enterprise', title: 'Workspace', body: '', slogan: '',
      background_url: '/api/v1/enterprise/brand/background',
      background_content_type: 'untrusted/server-owned-value',
      background_sha256: 'not-client-editable', background_size_bytes: -1,
    })).toBe('')
    expect(validateBrand({
      enterprise_name: 'Example Enterprise', title: 'Workspace', body: '', slogan: '',
      background_url: 'https://cdn.example.com/background.webp',
      background_content_type: 'image/webp', background_sha256: 'a'.repeat(64), background_size_bytes: 1024,
    })).toBeTruthy()
  })

  it('accepts only non-empty JPG, PNG, or WebP files up to 5MB', () => {
    expect(validateImageFile(new File([new Uint8Array([1])], 'background.jpg', { type: 'image/jpeg' }))).toBe('')
    expect(validateImageFile(new File([new Uint8Array(5 * 1024 * 1024)], 'background.webp', { type: 'image/webp' }))).toBe('')
    expect(validateImageFile(new File([new Uint8Array(0)], 'background.png', { type: 'image/png' }))).toBeTruthy()
    expect(validateImageFile(new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'background.png', { type: 'image/png' }))).toBeTruthy()
    expect(validateImageFile(new File([new Uint8Array([1])], 'background.svg', { type: 'image/svg+xml' }))).toBeTruthy()
  })
})
