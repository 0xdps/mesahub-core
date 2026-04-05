/**
 * next/image compatibility shim for Vite.
 * Renders as a regular <img> element.
 */
import React from 'react'

interface ImageProps {
  src: string | { src: string; width?: number; height?: number }
  alt: string
  width?: number | string
  height?: number | string
  className?: string
  style?: React.CSSProperties
  priority?: boolean
  fill?: boolean
  sizes?: string
  quality?: number
}

export default function Image({ src, alt, width, height, className, style, fill }: ImageProps) {
  const srcStr = typeof src === 'object' && 'src' in src ? src.src : (src as string)
  const resolvedWidth = typeof src === 'object' && 'width' in src && !width ? src.width : width
  const resolvedHeight = typeof src === 'object' && 'height' in src && !height ? src.height : height

  return (
    <img
      src={srcStr}
      alt={alt}
      width={resolvedWidth as number | undefined}
      height={resolvedHeight as number | undefined}
      className={className}
      style={
        fill
          ? { position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover', ...style }
          : style
      }
    />
  )
}
