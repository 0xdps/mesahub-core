/**
 * Minimal structured logger for sqlite-hub.
 * All output goes to stdout/stderr so Railway captures it automatically.
 *
 * Format: [ISO timestamp] LEVEL  message
 */
function ts() {
  return new Date().toISOString();
}

export const logger = {
  info(msg: string) {
    console.log(`[${ts()}] INFO  ${msg}`);
  },
  warn(msg: string) {
    console.warn(`[${ts()}] WARN  ${msg}`);
  },
  error(msg: string) {
    console.error(`[${ts()}] ERROR ${msg}`);
  },
};
