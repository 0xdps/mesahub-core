/**
 * next/link compatibility shim for Vite + React Router.
 * Renders internal links as <RouterLink to={href}> and external links as <a href={href}>.
 */
import React from 'react'
import { Link as RouterLink } from 'react-router-dom'

export interface LinkProps extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string
  children?: React.ReactNode
  prefetch?: boolean
  passHref?: boolean
}

export default function Link({ href, prefetch: _pf, ...props }: LinkProps) {
  const isExternal =
    href.startsWith('http://') ||
    href.startsWith('https://') ||
    href.startsWith('//') ||
    href.startsWith('#') ||
    href.startsWith('mailto:') ||
    href.startsWith('tel:')

  if (isExternal) {
    return <a href={href} {...props} />
  }

  // React Router's Link uses 'to' instead of 'href'
  const { ...rest } = props
  return <RouterLink to={href} {...(rest as any)} />
}
