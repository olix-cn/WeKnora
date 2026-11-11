const ACCOUNT_KEY = 'yhsj_saved_login_account'

type AccountStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export function savedLoginAccount(storage: AccountStorage): string {
  try { return storage.getItem(ACCOUNT_KEY) || '' } catch { return '' }
}

export function rememberLoginAccount(storage: AccountStorage, account: string): void {
  try {
    if (account.trim()) storage.setItem(ACCOUNT_KEY, account.trim())
    else storage.removeItem(ACCOUNT_KEY)
  } catch { /* Private browsing may disable storage; login must still work. */ }
}

/** Call only after authentication, and only when the user opted in. */
export async function saveVerifiedPassword(account: string, password: string): Promise<boolean> {
  const PasswordCredential = (window as unknown as {
    PasswordCredential?: new (data: { id: string; password: string }) => Credential
  }).PasswordCredential
  if (!window.isSecureContext || !PasswordCredential || !navigator.credentials?.store) return false
  try {
    await navigator.credentials.store(new PasswordCredential({ id: account, password }))
    return true
  } catch {
    // Browser policy or the user may reject the save. Never interrupt login.
    return false
  }
}
