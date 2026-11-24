import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { LucideAngularModule, TriangleAlert, Wrench } from 'lucide-angular';

import { I18nService } from '../../services/i18n.service';
import { ToolchainStateService } from '../../services/toolchain-state.service';
import { ToolchainInstallOptions } from '../../services/wails';

const FALLBACK_OCR_LANGUAGES = ['eng', 'spa'];

/**
 * Blocking first-run setup screen for the managed local toolchain.
 *
 * The backend starts installing on startup; this overlay makes the progress
 * visible, offers Retry after a failure and lets the user continue with a
 * partial install instead of getting stuck. Hidden when the toolchain is ready
 * or when the runtime is unavailable (browser preview).
 */
@Component({
  selector: 'app-toolchain-setup',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './toolchain-setup.html',
  styleUrl: './toolchain-setup.css',
})
export class ToolchainSetup implements OnInit {
  private readonly state = inject(ToolchainStateService);
  private readonly router = inject(Router);

  readonly i18n = inject(I18nService);

  readonly status = this.state.status;
  readonly event = this.state.event;

  readonly toolIcon = Wrench;
  readonly alertIcon = TriangleAlert;

  readonly dismissed = signal(false);

  readonly visible = computed(() => {
    if (this.dismissed()) {
      return false;
    }

    const status = this.status();
    if (!status || status.ready) {
      return false;
    }

    if (status.success) {
      return true;
    }

    // A failed install keeps the actionable retry surface; a preview/unknown
    // status (no components, no error) must never block the app.
    return Boolean(status.error?.trim()) || status.components.length > 0;
  });

  readonly installing = computed(() => this.status()?.installing === true);

  readonly phase = computed(() => {
    const event = this.event();
    if (event && !event.done) {
      return event.phase;
    }
    return this.status()?.phase || event?.phase || '';
  });

  readonly phaseLabel = computed(() => {
    switch (this.phase()) {
      case 'starting':
        return this.i18n.t('toolchain.phase.preparingInstaller');
      case 'micromamba':
        return this.i18n.t('toolchain.phase.downloadingMicromamba');
      case 'environment':
        return this.i18n.t('toolchain.phase.installingPackages');
      case 'whisper':
        return this.i18n.t('toolchain.phase.installingWhisper');
      case 'model':
        return this.i18n.t('toolchain.phase.downloadingModel');
      case 'tessdata':
        return this.i18n.t('toolchain.phase.installingLanguages');
      case 'finalize':
        return this.i18n.t('toolchain.phase.verifying');
      case 'ready':
        return this.i18n.t('toolchain.phase.ready');
      case 'cancelling':
        return this.i18n.t('toolchain.phase.cancelling');
      case 'cancelled':
        return this.i18n.t('toolchain.phase.cancelled');
      default:
        return this.i18n.t('toolchain.phase.preparing');
    }
  });

  readonly percent = computed(() => {
    const status = this.status();
    const event = this.event();
    const raw =
      status?.installing && event && !event.done
        ? event.percent
        : (status?.percent ?? 0);
    return clampPercent(raw);
  });

  readonly percentLabel = computed(() => `${Math.round(this.percent())}%`);

  readonly message = computed(() => {
    const event = this.event();
    if (event?.message) {
      return event.message;
    }
    return this.status()?.detail || this.status()?.message || '';
  });

  readonly errorMessage = computed(() => {
    const statusError = (this.status()?.error ?? '').trim();
    if (statusError) {
      return statusError;
    }
    return (this.event()?.error ?? '').trim();
  });

  readonly cancelling = computed(
    () =>
      this.status()?.cancelling === true ||
      this.phase() === 'cancelling' ||
      this.event()?.phase === 'cancelling',
  );

  readonly cancelled = computed(
    () =>
      !this.installing() &&
      !this.errorMessage() &&
      this.phase() === 'cancelled',
  );

  readonly unsupported = computed(() => {
    const status = this.status();
    if (
      !status ||
      !status.success ||
      status.ready ||
      status.installing ||
      status.error
    ) {
      return false;
    }

    const message = (status.message ?? '').toLowerCase();
    return (
      message.includes('not available for') || message.includes('not supported')
    );
  });

  readonly indeterminate = computed(() => {
    if (!this.installing()) {
      return false;
    }

    return (
      this.event()?.indeterminate === true ||
      this.phase() === 'environment' ||
      this.percent() <= 0
    );
  });

  ngOnInit(): void {
    void this.state.refresh().then(() => {
      if (!this.unsupported()) {
        void this.state.ensureStarted();
      }
    });
  }

  retry(): void {
    void this.state.install(this.defaultOptions());
  }

  cancel(): void {
    void this.state.cancel();
  }

  continueWithoutTools(): void {
    this.dismissed.set(true);
  }

  openSettings(): void {
    this.dismissed.set(true);
    void this.router.navigateByUrl('/settings');
  }

  private defaultOptions(): ToolchainInstallOptions {
    const status = this.status();
    return {
      model: status?.model || 'medium',
      ocrLanguages:
        status?.ocrLanguages && status.ocrLanguages.length > 0
          ? [...status.ocrLanguages]
          : [...FALLBACK_OCR_LANGUAGES],
    };
  }
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.min(100, Math.max(0, value));
}
