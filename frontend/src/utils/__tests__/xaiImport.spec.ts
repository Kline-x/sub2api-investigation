import { describe, expect, it } from 'vitest'
import { extractXaiAccounts } from '../xaiImport'

describe('extractXaiAccounts', () => {
  it('识别单个 xai 账号对象', () => {
    const parsed = { access_token: 'at', refresh_token: 'rt', email: 'a@x.ai' }
    expect(extractXaiAccounts(parsed)).toEqual([parsed])
  })

  it('识别 xai 账号数组', () => {
    const parsed = [{ access_token: 'a1' }, { access_token: 'a2' }]
    expect(extractXaiAccounts(parsed)).toEqual(parsed)
  })

  it('数组含无 access_token 元素时整体拒绝', () => {
    expect(extractXaiAccounts([{ access_token: 'a1' }, { refresh_token: 'r2' }])).toBeNull()
  })

  it('拒绝 sub2api-data payload / 空数组 / 非对象', () => {
    expect(extractXaiAccounts({ type: 'sub2api-data', proxies: [], accounts: [] })).toBeNull()
    expect(extractXaiAccounts([])).toBeNull()
    expect(extractXaiAccounts('text')).toBeNull()
    expect(extractXaiAccounts(null)).toBeNull()
    expect(extractXaiAccounts({ access_token: '' })).toBeNull()
  })
})