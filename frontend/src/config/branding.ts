/** Presentation-only branding. Never apply to API payloads or user content. */
export function brandText(text: string): string {
  // Keep working links, package names and protocol examples unchanged.
  return text.split(/(https?:\/\/[^\s<>"']+|X-WeKnora-[\w-]+|\b[\w@/-]*weknora[\w/-]*\b)/g)
    .map((part, index) => index % 2 ? part : part.replace(/WeKnora|Tencent|腾讯|騰訊/g, 'YHSJ'))
    .join('')
}

export function brandMessages<T>(value: T): T {
  if (typeof value === 'string') return brandText(value) as T
  if (Array.isArray(value)) return value.map(brandMessages) as T
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, brandMessages(item)])) as T
  }
  return value
}
