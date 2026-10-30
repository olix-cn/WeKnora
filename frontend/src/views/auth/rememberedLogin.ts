export const REMEMBERED_LOGIN_KEY = 'aethcraft_remembered_login'

export interface RememberedLogin {
  email: string
  password: string
}

// Explicit opt-in only. Browser storage is persistent, not a secure password vault.
export function loadRememberedLogin(): RememberedLogin | null {
  try {
    const stored = localStorage.getItem(REMEMBERED_LOGIN_KEY)
    if (!stored) return null
    const value = JSON.parse(stored)
    if (typeof value?.email !== 'string' || !value.email ||
      typeof value?.password !== 'string' || !value.password) return null
    return { email: value.email, password: value.password }
  } catch {
    return null
  }
}

export function saveRememberedLogin(credentials: RememberedLogin | null): boolean {
  try {
    if (credentials) {
      localStorage.setItem(REMEMBERED_LOGIN_KEY, JSON.stringify(credentials))
    } else {
      localStorage.removeItem(REMEMBERED_LOGIN_KEY)
    }
    return true
  } catch {
    return false
  }
}
