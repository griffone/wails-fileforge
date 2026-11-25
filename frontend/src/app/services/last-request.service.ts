import { DOCUMENT } from '@angular/common';
import { Injectable, inject } from '@angular/core';

import type { JobRequest } from './wails';

/**
 * Prefix of the per-tool last-submitted request documents.
 *
 * These live outside `ff-preferences` and outside the per-tool option payload:
 * a request carries inputs and a destination, while options carry only the
 * tool's settings.
 */
export const LAST_REQUEST_STORAGE_PREFIX = 'ff-last-request:';

/** Envelope version for the stored request document. */
export const LAST_REQUEST_SCHEMA_VERSION = 1;

export interface LastSubmittedRequest {
  toolId: string;
  /** Epoch milliseconds of the submission that produced this snapshot. */
  submittedAt: number;
  request: JobRequest;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function storageKey(toolId: string): string {
  const trimmed = typeof toolId === 'string' ? toolId.trim() : '';
  return trimmed ? `${LAST_REQUEST_STORAGE_PREFIX}${trimmed}` : '';
}

function normalizePaths(value: unknown): string[] | null {
  if (!Array.isArray(value)) {
    return null;
  }
  const paths = value
    .map((entry) => (typeof entry === 'string' ? entry.trim() : ''))
    .filter((entry) => entry.length > 0);
  return paths.length === value.length ? paths : null;
}

/**
 * Remembers the request that produced a job, keyed by tool id, so the result
 * card can offer "run again" even after a restart.
 *
 * The payload is validated on read; anything malformed is treated as absent
 * instead of throwing or producing a half-valid request.
 */
@Injectable({
  providedIn: 'root',
})
export class LastRequestService {
  private readonly document = inject(DOCUMENT);

  /** Stores the submitted request, replacing any previous one for the tool. */
  save(request: JobRequest): void {
    const key = storageKey(request?.toolId ?? '');
    if (!key) {
      return;
    }

    const inputPaths = normalizePaths(request.inputPaths);
    if (!inputPaths) {
      return;
    }

    const payload = {
      schemaVersion: LAST_REQUEST_SCHEMA_VERSION,
      toolId: request.toolId.trim(),
      submittedAt: Date.now(),
      request: {
        toolId: request.toolId.trim(),
        mode: request.mode,
        inputPaths,
        outputDir: typeof request.outputDir === 'string' ? request.outputDir : '',
        options: isRecord(request.options) ? { ...request.options } : {},
      },
    };

    try {
      this.document?.defaultView?.localStorage?.setItem(
        key,
        JSON.stringify(payload),
      );
    } catch {
      // Storage can be unavailable; "run again" simply stays hidden.
    }
  }

  /** Returns the last submitted request for the tool, or `null`. */
  get(toolId: string): LastSubmittedRequest | null {
    const key = storageKey(toolId);
    if (!key) {
      return null;
    }

    let raw: string | null = null;
    try {
      raw = this.document?.defaultView?.localStorage?.getItem(key) ?? null;
    } catch {
      return null;
    }
    if (!raw) {
      return null;
    }

    try {
      const parsed: unknown = JSON.parse(raw);
      if (
        !isRecord(parsed) ||
        parsed['schemaVersion'] !== LAST_REQUEST_SCHEMA_VERSION
      ) {
        return null;
      }

      const storedToolId =
        typeof parsed['toolId'] === 'string' ? parsed['toolId'].trim() : '';
      const submittedAt =
        typeof parsed['submittedAt'] === 'number' &&
        Number.isFinite(parsed['submittedAt'])
          ? parsed['submittedAt']
          : 0;
      const request = parsed['request'];
      if (!storedToolId || !isRecord(request)) {
        return null;
      }

      const requestToolId =
        typeof request['toolId'] === 'string' ? request['toolId'].trim() : '';
      const inputPaths = normalizePaths(request['inputPaths']);
      const outputDir =
        typeof request['outputDir'] === 'string' ? request['outputDir'] : '';
      const mode = request['mode'];
      if (
        requestToolId !== storedToolId ||
        storedToolId !== toolId.trim() ||
        !inputPaths ||
        inputPaths.length === 0 ||
        (mode !== 'single' && mode !== 'batch')
      ) {
        return null;
      }

      return {
        toolId: storedToolId,
        submittedAt,
        request: {
          toolId: storedToolId,
          mode,
          inputPaths,
          outputDir,
          options: isRecord(request['options'])
            ? { ...request['options'] }
            : {},
        },
      };
    } catch {
      return null;
    }
  }

  /** Forgets the last request for one tool. */
  clear(toolId: string): void {
    const key = storageKey(toolId);
    if (!key) {
      return;
    }

    try {
      this.document?.defaultView?.localStorage?.removeItem(key);
    } catch {
      // Storage can be unavailable; there is nothing else to clear.
    }
  }
}
