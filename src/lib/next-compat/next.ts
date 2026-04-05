/**
 * next compatibility shim for Vite.
 * Provides type stubs for Next.js top-level exports.
 */

export type Metadata = {
  title?: string;
  description?: string;
  [key: string]: unknown;
};

export declare namespace MetadataRoute {
  type Robots = {
    rules?: { userAgent?: string | string[]; allow?: string | string[]; disallow?: string | string[] }[];
    sitemap?: string | string[];
    host?: string;
  };
  type Sitemap = { url: string; lastModified?: Date | string; changeFrequency?: string; priority?: number }[];
}
