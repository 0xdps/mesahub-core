/**
 * next/dynamic compatibility shim for Vite.
 * Wraps dynamic imports with React.lazy.
 */
import React from 'react'

type Loader<T> = () => Promise<React.ComponentType<T> | { default: React.ComponentType<T> }>

interface DynamicOptions {
  ssr?: boolean
  loading?: React.ComponentType
}

export default function dynamic<T>(
  loader: Loader<T>,
  _options?: DynamicOptions
): React.ComponentType<T> {
  return React.lazy(() =>
    loader().then((mod) => {
      if (typeof mod === 'function') {
        return { default: mod as React.ComponentType<T> }
      }
      return mod as { default: React.ComponentType<T> }
    })
  ) as React.ComponentType<T>
}
