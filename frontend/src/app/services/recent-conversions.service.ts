import { DOCUMENT } from '@angular/common';
import {
  Injectable,
  Signal,
  WritableSignal,
  inject,
  signal,
} from '@angular/core';

import { ToolDefinition } from '../config/tools';
import { JobResult } from './wails';
import { ToolRegistryService } from './tool-registry.service';
import { isTerminalJobStatus } from '../utils/job-status';

const STORAGE_KEY = 'ff-recent-conversions';
const MAX_RECENT = 8;
const MAX_OUTPUTS = 20;

/** One finished job, shaped for the Home history. */
export interface RecentConversion {
  /** Job id; also the dedupe key. */
  id: string;
  toolId: string;
  toolName: string;
  route: string;
  /** Terminal status only: success | partial_success | failed | cancelled | interrupted. */
  status: string;
  /** Output file or folder paths, newest job first, capped per job. */
  outputs: string[];
  inputCount: number;
  timestamp: number;
}

/** True for statuses that represent a finished job. */
export { isTerminalJobStatus };

/**
 * Remembers the last finished conversions for the Home history.
 *
 * Only stable metadata is persisted; tool names and routes are resolved through
 * the registry on load so renamed or removed tools cannot leave stale entries.
 */
@Injectable({
  providedIn: 'root',
})
export class RecentConversionsService {
  private readonly registry = inject(ToolRegistryService);
  private readonly document = inject(DOCUMENT);

  private readonly recentState: WritableSignal<RecentConversion[]>;
  readonly recent: Signal<RecentConversion[]>;

  constructor() {
    this.recentState = signal<RecentConversion[]>(this.loadRecent());
    this.recent = this.recentState.asReadonly();
  }

  /** Records a terminal job; non-terminal or unknown-tool jobs are ignored. */
  record(job: JobResult): void {
    if (!job || !isTerminalJobStatus(job.status) || !job.jobId) {
      return;
    }

    const tool = this.registry.findById(job.toolId);
    if (!tool || !this.registry.isTool(tool)) {
      return;
    }

    const itemCount = job.items?.length ?? 0;
    const entry: RecentConversion = {
      id: job.jobId,
      toolId: tool.id,
      toolName: tool.name,
      route: tool.route,
      status: job.status,
      outputs: this.collectOutputs(job),
      inputCount: itemCount > 0 ? itemCount : 1,
      timestamp:
        typeof job.endedAt === 'number' && Number.isFinite(job.endedAt) && job.endedAt > 0
          ? job.endedAt
          : Date.now(),
    };

    const next = [
      entry,
      ...this.recentState().filter((item) => item.id !== entry.id),
    ].slice(0, MAX_RECENT);

    this.recentState.set(next);
    this.persist(next);
  }

  /** Removes every entry in memory and in storage. */
  clear(): void {
    this.recentState.set([]);
    this.persist([]);
  }

  private collectOutputs(job: JobResult): string[] {
    const collected: string[] = [];

    for (const item of job.items ?? []) {
      const candidates =
        item.outputs && item.outputs.length > 0
          ? item.outputs
          : item.outputPath
            ? [item.outputPath]
            : [];

      for (const path of candidates) {
        if (typeof path !== 'string' || !path) {
          continue;
        }
        if (collected.includes(path)) {
          continue;
        }
        collected.push(path);
        if (collected.length === MAX_OUTPUTS) {
          return collected;
        }
      }
    }

    return collected;
  }

  private persist(entries: readonly RecentConversion[]): void {
    try {
      const payload = entries.map((entry) => ({
        id: entry.id,
        toolId: entry.toolId,
        route: entry.route,
        status: entry.status,
        outputs: entry.outputs,
        inputCount: entry.inputCount,
        timestamp: entry.timestamp,
      }));
      this.document?.defaultView?.localStorage?.setItem(
        STORAGE_KEY,
        JSON.stringify(payload),
      );
    } catch {
      // Storage can be unavailable; the in-memory history still works.
    }
  }

  private loadRecent(): RecentConversion[] {
    const raw = this.readStorage();
    if (!raw) {
      return [];
    }

    try {
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) {
        return [];
      }

      const entries: RecentConversion[] = [];
      const seen = new Set<string>();

      for (const value of parsed) {
        const entry = this.toEntry(value);
        if (!entry || seen.has(entry.id)) {
          continue;
        }

        seen.add(entry.id);
        entries.push(entry);
        if (entries.length === MAX_RECENT) {
          break;
        }
      }

      return entries;
    } catch {
      // Corrupted JSON is treated as an empty history.
      return [];
    }
  }

  private toEntry(value: unknown): RecentConversion | null {
    if (!isRecord(value)) {
      return null;
    }

    const id = asString(value['id']);
    const toolId = asString(value['toolId']);
    const status = asString(value['status']);
    if (!id || !toolId || !status || !isTerminalJobStatus(status)) {
      return null;
    }

    const tool = this.resolveTool(toolId, asString(value['route']));
    if (!tool) {
      return null;
    }

    const inputCountValue = value['inputCount'];
    const inputCount =
      typeof inputCountValue === 'number' &&
      Number.isFinite(inputCountValue) &&
      inputCountValue > 0
        ? Math.floor(inputCountValue)
        : 1;

    const timestampValue = value['timestamp'];
    if (
      typeof timestampValue !== 'number' ||
      !Number.isFinite(timestampValue) ||
      timestampValue <= 0
    ) {
      return null;
    }

    return {
      id,
      toolId: tool.id,
      toolName: tool.name,
      route: tool.route,
      status,
      outputs: this.cleanOutputs(value['outputs']),
      inputCount,
      timestamp: timestampValue,
    };
  }

  private resolveTool(toolId: string, route: string | null): ToolDefinition | null {
    const tool =
      this.registry.findById(toolId) ?? (route ? this.registry.findByRoute(route) : null);

    return tool && this.registry.isTool(tool) ? tool : null;
  }

  private cleanOutputs(value: unknown): string[] {
    if (!Array.isArray(value)) {
      return [];
    }

    const outputs: string[] = [];
    for (const item of value) {
      if (typeof item !== 'string' || !item || outputs.includes(item)) {
        continue;
      }
      outputs.push(item);
      if (outputs.length === MAX_OUTPUTS) {
        break;
      }
    }

    return outputs;
  }

  private readStorage(): string | null {
    try {
      return this.document?.defaultView?.localStorage?.getItem(STORAGE_KEY) ?? null;
    } catch {
      return null;
    }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function asString(value: unknown): string | null {
  return typeof value === 'string' && value.length > 0 ? value : null;
}
