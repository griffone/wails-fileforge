import {
  Component,
  OnInit,
  computed,
  inject,
  input,
  signal,
} from '@angular/core';

import type { TranslationKey } from '../../../i18n';
import { I18nService } from '../../../services/i18n.service';
import {
  UpdateStateService,
  updateUnavailableKey,
  updateUnavailableReason,
} from '../../../services/update-state.service';
import type { AppInfoResponse } from '../../../services/wails';
import { formatBytes } from '../../../utils/format';

/** Notes longer than this are shown truncated with a show-more control. */
const NOTES_LIMIT = 420;

/** Distinct screens the About panel renders from the backend state machine. */
type UpdateView =
  | 'unavailable'
  | 'idle'
  | 'checking'
  | 'available'
  | 'downloading'
  | 'downloaded'
  | 'installing'
  | 'installed'
  | 'upToDate'
  | 'error';

type InstallMethod = 'appimage' | 'windows' | 'unknown';

/**
 * Updates block inside Settings → About.
 *
 * The backend owns installability, download and install; this component only
 * renders its status snapshots and forwards explicit user actions. There is
 * no automatic download, no automatic install, and no cancel button: the
 * backend exposes no way to abort a download, so the UI does not pretend it
 * can.
 */
@Component({
  selector: 'app-settings-updates',
  standalone: true,
  templateUrl: './settings-updates.html',
  styleUrl: './settings-updates.css',
})
export class SettingsUpdates implements OnInit {
  private readonly updates = inject(UpdateStateService);
  readonly i18n = inject(I18nService);

  /** Build identity from the About panel; a fallback for a missing reason. */
  readonly appInfo = input<AppInfoResponse | null>(null);

  readonly status = this.updates.status;
  readonly progress = this.updates.progress;
  readonly busy = this.updates.busy;

  readonly notesExpanded = signal(false);
  readonly confirmingInstall = signal(false);
  readonly installAcknowledged = signal(false);
  readonly submittingInstall = signal(false);

  readonly view = computed<UpdateView>(() => {
    const status = this.status();
    if (!status) {
      return 'idle';
    }

    const state = (status.state ?? '').trim();
    if (state === 'error') {
      return 'error';
    }
    if (!status.updatesEnabled) {
      return 'unavailable';
    }

    switch (state) {
      case 'checking':
        return 'checking';
      case 'available':
        return 'available';
      case 'downloading':
        return 'downloading';
      case 'ready':
        return 'downloaded';
      case 'installing':
        return 'installing';
      case 'installed':
        return 'installed';
      case 'up-to-date':
        return 'upToDate';
      case 'unavailable':
        return 'unavailable';
      default:
        return 'idle';
    }
  });

  readonly currentVersion = computed(() => {
    const fromStatus = (this.status()?.currentVersion ?? '').trim();
    if (fromStatus) {
      return fromStatus;
    }
    return (this.appInfo()?.version ?? '').trim() || 'dev';
  });

  readonly releaseVersion = computed(() => {
    const version = (this.status()?.release?.version ?? '').trim();
    return version || this.currentVersion();
  });

  readonly unavailableReason = computed<TranslationKey | null>(() => {
    const status = this.status();
    if (!status || status.updatesEnabled || this.view() === 'error') {
      return null;
    }

    const reason = updateUnavailableReason(status.reason ?? '', this.appInfo());
    return updateUnavailableKey(reason);
  });

  readonly notes = computed(() => {
    const notes = (this.status()?.release?.notes ?? '').trim();
    return notes;
  });

  readonly notesTruncated = computed(() => this.notes().length > NOTES_LIMIT);

  readonly displayedNotes = computed(() => {
    const notes = this.notes();
    if (this.notesExpanded() || notes.length <= NOTES_LIMIT) {
      return notes;
    }
    return `${notes.slice(0, NOTES_LIMIT).trimEnd()}…`;
  });

  readonly sizeLabel = computed(() => {
    const size = this.status()?.release?.asset?.size ?? 0;
    if (!Number.isFinite(size) || size <= 0) {
      return '';
    }
    return formatBytes(size);
  });

  readonly downloadPercent = computed(() => {
    const progress = this.progress();
    if (!progress) {
      return 0;
    }

    const reported = Number.isFinite(progress.percent) ? progress.percent : 0;
    const written = Number.isFinite(progress.written) ? progress.written : 0;
    const total = Number.isFinite(progress.total) ? progress.total : 0;
    const derived = total > 0 ? Math.round((written * 100) / total) : 0;
    const percent = reported > 0 ? reported : derived;
    return Math.min(100, Math.max(0, Math.round(percent)));
  });

  readonly downloadIndeterminate = computed(() => {
    const progress = this.progress();
    if (!progress) {
      return true;
    }
    const total = Number.isFinite(progress.total) ? progress.total : 0;
    return total <= 0 && this.downloadPercent() <= 0;
  });

  readonly downloadProgressLabel = computed(() => {
    const progress = this.progress();
    if (!progress) {
      return '';
    }

    const written = formatBytes(
      Number.isFinite(progress.written) ? progress.written : 0,
    );
    const total = Number.isFinite(progress.total) ? progress.total : 0;
    return total > 0
      ? this.i18n.t('settings.updates.progress', {
          written,
          total: formatBytes(total),
        })
      : this.i18n.t('settings.updates.progressUnknown', { written });
  });

  readonly installMethod = computed<InstallMethod>(() => {
    const method = (this.status()?.method ?? '').trim();
    if (method === 'appimage') {
      return 'appimage';
    }
    if (method === 'windows-installer') {
      return 'windows';
    }
    return 'unknown';
  });

  readonly installActionKey = computed<TranslationKey>(() => {
    switch (this.installMethod()) {
      case 'appimage':
        return 'settings.updates.installAppImage';
      case 'windows':
        return 'settings.updates.installWindows';
      default:
        return 'settings.updates.installGeneric';
    }
  });

  readonly consentConsequenceKey = computed<TranslationKey>(() => {
    switch (this.installMethod()) {
      case 'appimage':
        return 'settings.updates.consentAppImage';
      case 'windows':
        return 'settings.updates.consentWindows';
      default:
        return 'settings.updates.consentGeneric';
    }
  });

  readonly acknowledgeKey = computed<TranslationKey>(() => {
    switch (this.installMethod()) {
      case 'appimage':
        return 'settings.updates.ackAppImage';
      case 'windows':
        return 'settings.updates.ackWindows';
      default:
        return 'settings.updates.ackGeneric';
    }
  });

  readonly consentConfirmKey = computed<TranslationKey>(() => {
    switch (this.installMethod()) {
      case 'appimage':
        return 'settings.updates.consentConfirm';
      case 'windows':
        return 'settings.updates.consentConfirmWindows';
      default:
        return 'settings.updates.consentConfirmGeneric';
    }
  });

  readonly stateBadge = computed<{
    key: TranslationKey;
    className: string;
  } | null>(() => {
    switch (this.view()) {
      case 'checking':
        return {
          key: 'settings.updates.state.checking',
          className: 'badge badge-neutral',
        };
      case 'available':
        return {
          key: 'settings.updates.state.available',
          className: 'badge badge-info',
        };
      case 'downloading':
        return {
          key: 'settings.updates.state.downloading',
          className: 'badge badge-info',
        };
      case 'downloaded':
        return {
          key: 'settings.updates.state.downloaded',
          className: 'badge badge-success',
        };
      case 'installing':
        return {
          key: 'settings.updates.state.installing',
          className: 'badge badge-info',
        };
      case 'upToDate':
        return {
          key: 'settings.updates.state.upToDate',
          className: 'badge badge-success',
        };
      case 'error':
        return {
          key: 'settings.updates.state.error',
          className: 'badge badge-danger',
        };
      default:
        return null;
    }
  });

  readonly errorMessage = computed(() => {
    const status = this.status();
    const error = (status?.error ?? '').trim();
    if (error) {
      return error;
    }

    const message = (status?.message ?? '').trim();
    if (message) {
      return message;
    }

    return this.i18n.t('settings.updates.errorGeneric');
  });

  /**
   * What the retry button repeats: a failed download retries the download, any
   * other failure retries the check.
   */
  readonly retryKind = computed<'download' | 'check'>(() => {
    const status = this.status();
    if (!status) {
      return 'check';
    }

    const stage = String(status.stage ?? '');
    if (
      (stage === 'download' || stage === 'verify') &&
      status.release &&
      status.updateAvailable
    ) {
      return 'download';
    }
    return 'check';
  });

  ngOnInit(): void {
    void this.updates.refresh();
  }

  /** Manual check: always reports success or failure in this panel. */
  check(): void {
    if (this.busy()) {
      return;
    }
    this.resetInstallConfirmation();
    void this.updates.checkForUpdates({ manual: true });
  }

  /** Starts the download. No automatic download exists anywhere. */
  download(): void {
    if (this.busy()) {
      return;
    }
    this.resetInstallConfirmation();
    void this.updates.download();
  }

  retry(): void {
    if (this.retryKind() === 'download') {
      this.download();
      return;
    }
    this.check();
  }

  toggleNotes(): void {
    this.notesExpanded.set(!this.notesExpanded());
  }

  /** Opens the confirmation. It calls no backend method. */
  requestInstall(): void {
    if (this.busy() || this.view() !== 'downloaded') {
      return;
    }
    this.installAcknowledged.set(false);
    this.confirmingInstall.set(true);
  }

  /** Dismisses the confirmation. It calls no backend method. */
  cancelInstall(): void {
    this.resetInstallConfirmation();
  }

  onAcknowledge(event: Event): void {
    const input = event.target as HTMLInputElement | null;
    this.installAcknowledged.set(input?.checked === true);
  }

  /**
   * The only place that grants consent. It runs from the confirmation, after
   * the acknowledgement, and installs only when the backend accepted it.
   */
  async confirmInstall(): Promise<void> {
    if (
      !this.confirmingInstall() ||
      !this.installAcknowledged() ||
      this.submittingInstall()
    ) {
      return;
    }

    this.submittingInstall.set(true);
    try {
      const consent = await this.updates.grantConsent();
      if (!consent || consent.success === false) {
        return;
      }
      await this.updates.install();
    } finally {
      this.submittingInstall.set(false);
      this.resetInstallConfirmation();
    }
  }

  private resetInstallConfirmation(): void {
    this.confirmingInstall.set(false);
    this.installAcknowledged.set(false);
  }
}
