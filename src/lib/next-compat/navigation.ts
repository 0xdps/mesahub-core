/**
 * next/navigation compatibility shim for Vite + React Router.
 * Provides useRouter, usePathname, useSearchParams, useParams, notFound, redirect.
 */
import { useNavigate, useLocation, useSearchParams as rrUseSearchParams, useParams as rrUseParams } from 'react-router-dom'

export function useRouter() {
  const navigate = useNavigate()
  return {
    push: (href: string) => navigate(href),
    replace: (href: string) => navigate(href, { replace: true }),
    refresh: () => window.location.reload(),
    back: () => navigate(-1),
    forward: () => navigate(1),
    prefetch: () => {},
  }
}

export function usePathname(): string {
  return useLocation().pathname
}

export function useSearchParams(): URLSearchParams {
  const [params] = rrUseSearchParams()
  return params
}

export function useParams<T extends Record<string, string | undefined> = Record<string, string | undefined>>(): T {
  return rrUseParams() as T
}

export function notFound(): never {
  throw new Error('Not found')
}

export function redirect(href: string): never {
  window.location.replace(href)
  throw new Error('redirect')
}
