import { LAST_REQUEST_STORAGE_PREFIX } from '../app/services/last-request.service';
import { TOOL_OPTIONS_STORAGE_PREFIX } from '../app/services/preferences.service';

/**
 * Removes the per-tool option and last-request documents from localStorage.
 *
 * Jasmine randomizes spec order, so a spec that runs a job can leave a tool's
 * remembered options behind for the next spec. Clearing them in `beforeEach`
 * keeps every spec independent without touching its assertions.
 */
export function clearToolStorage(): void {
  const prefixes = [TOOL_OPTIONS_STORAGE_PREFIX, LAST_REQUEST_STORAGE_PREFIX];
  const keys: string[] = [];
  for (let index = 0; index < localStorage.length; index += 1) {
    const key = localStorage.key(index);
    if (key && prefixes.some((prefix) => key.startsWith(prefix))) {
      keys.push(key);
    }
  }
  for (const key of keys) {
    localStorage.removeItem(key);
  }
}
