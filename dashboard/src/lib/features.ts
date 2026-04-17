const envEnabled = (value: string | undefined) => value === "true";

export const FILES_ENABLED = envEnabled(process.env.NEXT_PUBLIC_ENABLE_FILES);
export const BUCKETS_ENABLED = envEnabled(process.env.NEXT_PUBLIC_ENABLE_BUCKETS);