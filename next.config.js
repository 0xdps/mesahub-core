/* eslint-disable @typescript-eslint/no-var-requires */
const withMDX = require("@next/mdx")();
const pkg = require("./package.json");

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  reactStrictMode: false,
  pageExtensions: ["js", "jsx", "mdx", "ts", "tsx"],
  // Keep better-sqlite3 as a server-side external so its native .node
  // bindings are preserved and bundled correctly in the standalone output.
  serverExternalPackages: ["better-sqlite3"],
  env: {
    NEXT_PUBLIC_STUDIO_VERSION: pkg.version,
  },
};

module.exports = { ...withMDX(nextConfig), output: "standalone" };
