// CSS module declarations for TypeScript 6
// TS6 added TS2882 which flags side-effect imports of non-TS files.
// Next.js handles CSS via its webpack/turbopack bundler at build time.
declare module '*.css' {}
