// CPA(xai-*.json)导入格式检测(定制)。
// 识别 GROK CPA 导出的账号 JSON:单个对象或数组,特征是含非空字符串 access_token。
// 只做检测与透传,字段解析(JWT/base_url 等)在后端 convertXaiAccount 完成。

const isXaiAccountObject = (value: unknown): value is Record<string, unknown> => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const accessToken = (value as Record<string, unknown>).access_token
  return typeof accessToken === 'string' && accessToken.trim() !== ''
}

export const extractXaiAccounts = (parsed: unknown): Record<string, unknown>[] | null => {
  if (isXaiAccountObject(parsed)) return [parsed]
  if (Array.isArray(parsed) && parsed.length > 0 && parsed.every(isXaiAccountObject)) {
    return parsed
  }
  return null
}