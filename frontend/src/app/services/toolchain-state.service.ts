import { DestroyRef, Injectable, computed, inject, signal } from '@angular/core';
import { Subscription } from 'rxjs';

import {
  ToolchainInstallEvent,
  ToolchainInstallOptions,
  ToolchainStatus,
  Wails,
} from './wails';

const POLL_INTERVAL_MS = 1500;
const DEFAULT_OCR_LANGUAGES = ['eng', 'spa'];

/**
 * Shared managed-toolchain state for the first-run overlay and Settings.
 *
 * Events drive live progress; the status endpoint is the source of truth on
 * load, on terminal events and while polling. Failures are ignored on read so
 * the browser preview (no Wails IPC) never shows a broken installer.
 */
@Injectable({
  providedIn: 'root',
})
export class ToolchainStateService {
  private readonly wails = inject(Wails);
  private readonly destroyRef = inject(DestroyRef);

  private readonly statusState = signal<ToolchainStatus | null>(null);
  private readonly eventState = signal<ToolchainInstallEvent | null>(null);
  private readonly loadingState = signal(false);

  readonly status = this.statusState.asReadonly();
  readonly event = this.eventState.asReadonly();
  readonly loading = this.loadingState.asReadonly();
  readonly isBusy = computed(() => this.statusState()?.installing === true);

  private subscription: Subscription | null = null;
  private pollTimer: ReturnType<typeof setInterval> | null = null;
  private pollInFlight = false;
  private startRequested = false;

  constructor() {
    // The event stream is optional: a missing runtime simply means the UI
    // falls back to polling / status reads.
    try {
      this.subscription = this.wails.toolchainInstall$.subscribe({
        next: (event) => this.handleEvent(event),
        error: () => {
          // Events are optional; status polling still covers progress.
        },
      });
    } catch {
      this.subscription = null;
    }

    this.destroyRef.onDestroy(() => this.dispose());
  }

  /** Reads the current status. Failures (preview) leave the last value alone. */
  async refresh(): Promise<void> {
    this.loadingState.set(true);

    try {
      const response = await this.wails.getToolchainStatus();
      if (response?.success) {
        this.applyStatus(response);
      }
    } catch {
      // No IPC in the browser preview; keep the current state.
    } finally {
      this.loadingState.set(false);
    }
  }

  /**
   * Fallback first-run start: the backend also starts automatically, so this
   * only covers a status that says "not ready, not installing, no error".
   * Runs at most once per session.
   */
  async ensureStarted(): Promise<void> {
    if (this.startRequested) {
      return;
    }

    const status = this.statusState();
    if (
      !status ||
      !status.success ||
      status.ready ||
      status.installing ||
      status.error ||
      isUnsupportedStatus(status)
    ) {
      return;
    }

    this.startRequested = true;
    await this.install(this.defaultOptions(status));
  }

  /** Starts or repairs the install; ignored while one is already running. */
  async install(
    options: ToolchainInstallOptions,
  ): Promise<ToolchainStatus | null> {
    if (this.statusState()?.installing) {
      return this.statusState();
    }

    this.eventState.set(null);
    this.loadingState.set(true);

    try {
      const response = await this.wails.startToolchainInstall(options);
      this.applyStatus(response);
      return response;
    } catch {
      return null;
    } finally {
      this.loadingState.set(false);
    }
  }

  /** Requests cancellation of the active install. */
  async cancel(): Promise<ToolchainStatus | null> {
    if (!this.statusState()?.installing) {
      return this.statusState();
    }

    try {
      const response = await this.wails.cancelToolchainInstall();
      this.applyStatus(response);
      return response;
    } catch {
      return null;
    }
  }

  private handleEvent(event: ToolchainInstallEvent): void {
    this.eventState.set(event);

    if (!event.done) {
      this.updateStatusFromEvent(event);
    }

    if (event.done || event.error) {
      void this.refresh();
    }
  }

  private updateStatusFromEvent(event: ToolchainInstallEvent): void {
    const current = this.statusState();
    if (!current) {
      return;
    }

    this.applyStatus({
      ...current,
      installing: true,
      cancelling: event.phase === 'cancelling' || current.cancelling,
      percent: event.percent,
      phase: event.phase,
      detail: event.message,
      message: event.message || current.message,
    });
  }

  private applyStatus(status: ToolchainStatus): void {
    this.statusState.set(status);
    this.syncPolling();
  }

  private syncPolling(): void {
    if (this.statusState()?.installing) {
      this.startPolling();
    } else {
      this.stopPolling();
    }
  }

  private startPolling(): void {
    if (this.pollTimer !== null) {
      return;
    }

    this.pollTimer = setInterval(() => void this.poll(), POLL_INTERVAL_MS);
  }

  private stopPolling(): void {
    if (this.pollTimer === null) {
      return;
    }

    clearInterval(this.pollTimer);
    this.pollTimer = null;
  }

  /** Covers progress events missed before the UI subscribed. */
  private async poll(): Promise<void> {
    if (this.pollInFlight) {
      return;
    }

    this.pollInFlight = true;

    try {
      const response = await this.wails.getToolchainInstallState();
      if (response?.success) {
        this.applyStatus(response);
      }
    } catch {
      // Transient IPC failures are retried by the next tick.
    } finally {
      this.pollInFlight = false;
    }
  }

  private defaultOptions(
    status: ToolchainStatus,
  ): ToolchainInstallOptions {
    return {
      model: status.model || 'medium',
      ocrLanguages:
        status.ocrLanguages?.length > 0
          ? [...status.ocrLanguages]
          : [...DEFAULT_OCR_LANGUAGES],
    };
  }

  private dispose(): void {
    this.stopPolling();
    try {
      this.subscription?.unsubscribe();
    } catch {
      // The runtime may already be gone during teardown.
    }
    this.subscription = null;
  }
}

/** True when the platform has no verified prebuilt toolchain. */
export function isUnsupportedStatus(status: ToolchainStatus): boolean {
  const message = (status.message ?? '').toLowerCase();
  return message.includes('not available for') || message.includes('not supported');
}
