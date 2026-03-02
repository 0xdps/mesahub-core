import { SavedConnectionRawLocalStorage } from "@/app/(theme)/connect/saved-connection-storage";
import TursoDriver from "./database/turso";
import { SqliteLikeBaseDriver } from "./sqlite-base-driver";

// Minimal driver factory — only Turso connections remain after cloud-driver strip.
export function createLocalDriver(conn: SavedConnectionRawLocalStorage) {
  return new TursoDriver(conn.url!, conn.token!, true) as SqliteLikeBaseDriver;
}
