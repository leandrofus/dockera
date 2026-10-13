// The logged-in user, shared by the whole UI.
export interface Me {
  user: string
  role: 'admin' | 'viewer'
  method: 'pam' | 'token'
  version?: string
}

let current: Me | null = null
export const getMe = () => current
export const setMe = (m: Me | null) => {
  current = m
}
export const isViewer = () => current?.role === 'viewer'
