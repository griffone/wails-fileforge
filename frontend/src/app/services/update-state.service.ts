import { DOCUMENT } from '@angular/common';
import {
  DestroyRef,
  Injectable,
  inject,
  signal,
} from '@angular/core';
import { Subscription } from 'rxjs';

import type { TranslationKey } from '../i18n';
import { Wails } from './wails';
import type {
  AppInfoResponse,
  UpdateProgressEvent,
  UpdateStatus,
} from './wails';

/** localStorage key that holds the epoch millis of the last update check. */
export const UPDATE_LAST_CHECK_STORAGE_KEY = 'ff-update-last-check-at';

/** A launch check runs at most once per day. */
export const UPDATE_CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;

/** Reason classes the About panel translates into plain language. */
export type UpdateUnavailableReason =
  | 'dev'
  | 'noKey'
  | 'packageManager'
  | 'platform'
  | 'generic';

const REASON_KEYS: Readonly<Record<UpdateUnavailableReason, TranslationKey>> = {
  dev: 'settings.updates.unavailable.dev',
  noKey: 'settings.updates.unavailable.noKey',
  packageManager: 'settings.updates.unavailable.packageManager',
  platform: 'settings.updates.unavailable.platform',
  generic: 'settings.updates.unavailable.generic',
};

/** Maps a reason class to the translation key the panel renders. */
export function updateUnavailableKey(reason: UpdateUnavailableReason): TranslationKey {
  return REASON_KEYS[reason];
}

/**
 * Classifies the backend gate reason.
 *
 * The gate strings are pinned by Go tests, so they are matched first; the
 * build identity is the fallback when a reason is missing (for example a
 * failed status read on a development build).
 */
export function updateUnavailableReason(
  reason: string,
  info?: AppInfoResponse | null,
): UpdateUnavailableReason {
  const text = (reason ?? '').trim().toLowerCase();
  if (text.includes('development build')) {
    return 'dev';
  }
  if (text.includes('signing key')) {
    return 'noKey';
  }
  if (text.includes('package manager')) {
    return 'packageManager';
  }
  if (text.includes('not supported')) {
    return 'platform';
  }
  if (info) {
    if (info.release !== true) {
      return 'dev';
    }
    if (info.updateKeyConfigured !== true) {
      return 'noKey';
    }
  }
  return 'generic';
}

/**
 * True when a launch check is due. A missing, malformed or future timestamp
 * (clock moved backwards) all mean "check now".
 */
export function isUpdateCheckDue(
  lastCheckAt: number | null,
  now: number = Date.now(),
  intervalMs: number = UPDATE_CHECK_INTERVAL_MS,
): boolean {
  if (
    lastCheckAt === null ||
    !Number.isFinite(lastCheckAt) ||
    lastCheckAt <= 0
  ) {
    return true;
  }
  if (lastCheckAt > now + intervalMs) {
    return true;
  }
  return now - lastCheckAt >= intervalMs;
}

/**
 * Shared auto-update state for the shell and Settings → About.
 *
 * The backend owns the state machine; this service mirrors it from the
 * `update/state` snapshots, forwards the download progress stream, and owns
 * the launch-check policy. It never downloads or installs on its own: every
 * download and every install is an explicit user action.
 */
@Injectable({
  providedIn: 'root',
})
export class UpdateStateService {
  private readonly wails = inject(Wails);
  private readonly document = inject(DOCUMENT);
  private readonly destroyRef = inject(DestroyRef);

  private readonly statusState = signal<UpdateStatus | null>(null);
  private readonly progressState = signal<UpdateProgressEvent | null>(null);
  private readonly busyState = signal(false);

  readonly status = this.statusState.asReadonly();
  readonly progress = this.progressState.asReadonly();
  readonly busy = this.busyState.asReadonly();

  private stateSubscription: Subscription | null = null;
  private progressSubscription: Subscription | null = null;
  private launchCheckStarted = false;
  private checkInFlight = false;

  constructor() {
    try {
      this.stateSubscription = this.wails.updateState$.subscribe((status) =>
        this.applyStatus(status),
      );
    } catch {
      // The event stream is optional; RPC reads still cover the status.
    }

    try {
      this.progressSubscription = this.wails.updateProgress$.subscribe(
        (progress) => this.applyProgress(progress),
      );
    } catch {
      this.progressSubscription = null;
    }

    this.destroyRef.onDestroy(() => this.dispose());
  }

  /** Reads the cached status. It never touches the network. */
  async refresh(): Promise<UpdateStatus | null> {
    try {
      const status = await this.wails.getUpdateStatus();
      if (status?.success) {
        this.applyStatus(status);
        return status;
      }
    } catch {
      // No IPC in the browser preview; leave the status unknown.
    }

    return null;
  }

  /**
   * Checks the release feed. A manual check always reports its outcome,
   * including a failure; the launch check never surfaces one.
   */
  async checkForUpdates(options: { manual?: boolean } = {}): Promise<UpdateStatus | null> {
    const manual = options.manual === true;
    if (this.checkInFlight) {
      return this.statusState();
    }

    this.checkInFlight = true;
    this.busyState.set(true);
    this.recordCheckAttempt();

    try {
      const status = await this.wails.checkForUpdates();
      if (status?.success || manual) {
        this.applyStatus(status);
      }
      return status ?? null;
    } catch {
      // A failed launch check stays invisible; a manual one already reports
      // through the error status the backend returns.
      return null;
    } finally {
      this.checkInFlight = false;
      this.busyState.set(false);
    }
  }

  /** Downloads the checked release. There is no automatic download. */
  async download(): Promise<UpdateStatus | null> {
    this.progressState.set(null);
    this.busyState.set(true);

    try {
      const status = await this.wails.downloadUpdate();
      this.applyStatus(status);
      return status ?? null;
    } catch {
      return null;
    } finally {
      this.busyState.set(false);
    }
  }

  /** Records the user's consent for the verified download. */
  async grantConsent(): Promise<UpdateStatus | null> {
    try {
      const status = await this.wails.grantInstallConsent();
      this.applyStatus(status);
      return status ?? null;
    } catch {
      return null;
    }
  }

  /** Installs the verified download. The backend requires prior consent. */
  async install(): Promise<UpdateStatus | null> {
    this.busyState.set(true);

    try {
      const status = await this.wails.installUpdate();
      this.applyStatus(status);
      return status ?? null;
    } catch {
      return null;
    } finally {
      this.busyState.set(false);
    }
  }

  /**
   * Runs the launch check at most once per session, in production builds only
   * and at most once per 24 hours. Every failure is silent by design.
   */
  async ensureLaunchCheck(): Promise<void> {
    if (this.launchCheckStarted) {
      return;
    }
    this.launchCheckStarted = true;

    try {
      if (!this.wails.isRuntimeAvailable()) {
        return;
      }

      const info = await this.wails.getAppInfo();
      if (!info?.success || info.release !== true) {
        return;
      }

      if (!isUpdateCheckDue(this.readLastCheckAt())) {
        return;
      }

      await this.checkForUpdates({ manual: false });
    } catch {
      // A missing or failed launch check is never shown to the user.
    }
  }

  /** True when the launch check already ran in this session. */
  launchCheckRequested(): boolean {
    return this.launchCheckStarted;
  }

  private applyStatus(status: UpdateStatus): void {
    if (!status) {
      return;
    }

    this.statusState.set(status);

    // A terminal state means no download is in flight; keeping the last
    // progress would draw a stale bar in the next download.
    const state = status.state ?? '';
    if (state !== 'downloading' && this.progressState() !== null) {
      this.progressState.set(null);
    }
  }

  private applyProgress(progress: UpdateProgressEvent): void {
    if (!progress || String(progress.stage) !== 'download') {
      return;
    }

    const written =
      Number.isFinite(progress.written) && progress.written > 0
        ? progress.written
        : 0;
    const total =
      Number.isFinite(progress.total) && progress.total > 0
        ? progress.total
        : 0;
    const reported = Number.isFinite(progress.percent) ? progress.percent : 0;
    const derived = total > 0 ? Math.round((written * 100) / total) : 0;
    const percent = Math.min(100, Math.max(0, reported > 0 ? reported : derived));

    this.progressState.set({
      ...progress,
      written,
      total,
      percent,
    });
  }

  private recordCheckAttempt(): void {
    const now = Date.now();
    const storage = this.storage();
    if (!storage) {
      return;
    }

    try {
      storage.setItem(UPDATE_LAST_CHECK_STORAGE_KEY, String(now));
    } catch {
      // Blocked storage only means the 24 hour window is not persisted.
    }
  }

  private readLastCheckAt(): number | null {
    const storage = this.storage();
    if (!storage) {
      return null;
    }

    try {
      const raw = storage.getItem(UPDATE_LAST_CHECK_STORAGE_KEY);
      if (raw === null) {
        return null;
      }
      const parsed = Number(raw);
      return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
    } catch {
      return null;
    }
  }

  private storage(): Storage | null {
    try {
      return this.document?.defaultView?.localStorage ?? null;
    } catch {
      return null;
    }
  }

  private dispose(): void {
    try {
      this.stateSubscription?.unsubscribe();
    } catch {
      // The runtime may already be gone during teardown.
    }
    try {
      this.progressSubscription?.unsubscribe();
    } catch {
      // ignore
    }
    this.stateSubscription = null;
    this.progressSubscription = null;
  }
}
