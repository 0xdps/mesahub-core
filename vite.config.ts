import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [
      { find: '@', replacement: path.resolve(__dirname, 'src') },
      { find: 'next/link', replacement: path.resolve(__dirname, 'src/lib/next-compat/link.tsx') },
      { find: 'next/navigation', replacement: path.resolve(__dirname, 'src/lib/next-compat/navigation.ts') },
      { find: 'next/image', replacement: path.resolve(__dirname, 'src/lib/next-compat/image.tsx') },
      { find: 'next/dynamic', replacement: path.resolve(__dirname, 'src/lib/next-compat/dynamic.ts') },
      { find: /^next$/, replacement: path.resolve(__dirname, 'src/lib/next-compat/next.ts') },
    ],
  },
  // Replace NEXT_PUBLIC_* env var patterns at build time so existing source
  // files continue to work without modification.
  define: {
    'process.env.NEXT_PUBLIC_ENABLE_FILE_STORAGE': JSON.stringify(process.env.NEXT_PUBLIC_ENABLE_FILE_STORAGE ?? 'false'),
    'process.env.NODE_ENV': JSON.stringify(process.env.NODE_ENV ?? 'production'),
  },
  build: {
    outDir: 'dist',
  },
  server: {
    port: 3001,
    proxy: {
      '/api': { target: 'http://localhost:3000', changeOrigin: true },
    },
  },
})
