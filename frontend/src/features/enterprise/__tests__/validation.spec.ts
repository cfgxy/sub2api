import { describe, expect, it } from 'vitest'
import { validateBrand, validateDepartmentName, validatePassword } from '@/features/enterprise/validation'

describe('enterprise form validation', () => {
  it('matches backend text and password limits', () => {
    expect(validatePassword('short')).toBeTruthy()
    expect(validatePassword('123456789012')).toBe('')
    expect(validateDepartmentName('部'.repeat(101))).toBeTruthy()
    expect(validateBrand({ title: '企'.repeat(41), body: '', slogan: '', background_url: '', background_content_type: '', background_sha256: '', background_size_bytes: 0 })).toBeTruthy()
  })

  it('accepts only complete JPG/PNG/WebP background metadata', () => {
    expect(validateBrand({
      title: 'Example Enterprise', body: '', slogan: '',
      background_url: 'https://cdn.example.com/login.webp',
      background_content_type: 'image/webp',
      background_sha256: 'a'.repeat(64), background_size_bytes: 1024,
    })).toBe('')
    expect(validateBrand({
      title: 'Example Enterprise', body: '', slogan: '',
      background_url: 'https://cdn.example.com/login.svg',
      background_content_type: 'image/svg+xml',
      background_sha256: 'a'.repeat(64), background_size_bytes: 1024,
    })).toBeTruthy()
  })
})
