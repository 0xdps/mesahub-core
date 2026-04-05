import { useEffect, useState } from 'react'
import { Outlet, Navigate } from 'react-router-dom'

type AuthStatus = 'loading' | 'authed' | 'unauthed'

/**
 * Protects child routes by checking /api/auth/me.
 * Renders nothing while loading, redirects to /login if unauthenticated.
 */
export default function AuthGuard() {
  const [status, setStatus] = useState<AuthStatus>('loading')

  useEffect(() => {
    fetch('/api/auth/me', { credentials: 'include', cache: 'no-store' })
      .then((r) => setStatus(r.ok ? 'authed' : 'unauthed'))
      .catch(() => setStatus('unauthed'))
  }, [])

  if (status === 'loading') return null
  if (status === 'unauthed') return <Navigate to="/login" replace />
  return <Outlet />
}
