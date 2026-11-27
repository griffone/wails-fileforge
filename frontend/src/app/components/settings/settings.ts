import { DOCUMENT } from '@angular/common';
import {
  Component,
  OnInit,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import {
  FolderOpen,
  History,
  LucideAngularModule,
  Moon,
  RefreshCw,
  Sun,
  TriangleAlert,
} from 'lucide-angular';

import { PreferencesService } from '../../services/preferences.service';
import { RecentConversionsService } from '../../services/recent-conversions.service';
import { I18nService } from '../../services/i18n.service';
import type { Locale, TranslationKey } from '../../i18n';
import { Theme, ThemeService } from '../../services/theme.service';
import {
  isUnsupportedStatus,
  ToolchainStateService,
} from '../../services/toolchain-state.service';
import {
  AppInfoResponse,
  LocalCapability,
  LocalCapabilitiesResponse,
  ToolchainComponent,
  TranscriptionCapabilitiesResponse,
  Wails,
} from '../../services/wails';
import { formatBytes } from '../../utils/format';
import { SettingsUpdates } from './settings-updates/settings-updates';
import { TrashHistory } from './trash-history/trash-history';

/** Shared with the Home onboarding strip. */
const ONBOARDING_STORAGE_KEY = 'ff-onboarding-dismissed';

const TOOLCHAIN_MODELS = ['medium', 'small', 'large-v3'] as const;

const TOOLCHAIN_OCR_LANGUAGES = ['eng', 'spa'] as const;

const DEFAULT_OCR_LANGUAGES = ['eng', 'spa'];

@Component({
  selector: 'app-settings',
  standalone: true,
  imports: [LucideAngularModule, TrashHistory, SettingsUpdates],
  templateUrl: './settings.html',
  styleUrl: './settings.css',
})
export class Settings implements OnInit {
  private readonly preferences = inject(PreferencesService);
  private readonly themeService = inject(ThemeService);
  private readonly recentConversions = inject(RecentConversionsService);
  private readonly wails = inject(Wails);
  private readonly document = inject(DOCUMENT);
  private readonly toolchain = inject(ToolchainStateService);

  readonly i18n = inject(I18nService);

  readonly folderIcon = FolderOpen;
  readonly sunIcon = Sun;
  readonly moonIcon = Moon;
  readonly historyIcon = History;
  readonly refreshIcon = RefreshCw;
  readonly alertIcon = TriangleAlert;

  readonly toolchainModels = TOOLCHAIN_MODELS;
  readonly toolchainOcrOptions = TOOLCHAIN_OCR_LANGUAGES;

  readonly defaultOutputDir = this.preferences.defaultOutputDir;
  readonly isDark = this.themeService.isDark;
  readonly isLight = computed(() => !this.isDark());
  readonly locales = this.i18n.locales;

  readonly recentCount = computed(() => this.recentConversions.recent().length);
  readonly historySummary = computed(() => {
    const count = this.recentCount();
    if (count === 0) {
      return this.i18n.t('settings.history.none');
    }
    return count === 1
      ? this.i18n.t('settings.history.one')
      : this.i18n.t('settings.history.many', { count });
  });

  readonly hasDefaultDestination = computed(
    () => this.defaultOutputDir().length > 0,
  );
  readonly isNextToOriginal = computed(() => !this.hasDefaultDestination());

  readonly actionMessage = signal('');

  readonly appInfo = signal<AppInfoResponse | null>(null);
  readonly appInfoLoading = signal(false);
  readonly appInfoError = signal('');

  readonly aboutVersion = computed(() => this.aboutValue(this.appInfo()?.version));
  readonly aboutCommit = computed(() => this.aboutValue(this.appInfo()?.commit));
  readonly aboutBuiltAt = computed(() =>
    this.aboutValue(this.appInfo()?.builtAt),
  );
  readonly aboutRelease = computed(() => this.appInfo()?.release === true);
  readonly aboutUpdateKey = computed(() =>
    this.appInfo()?.updateKeyConfigured
      ? this.i18n.t('settings.aboutInfo.updateKeyConfigured')
      : this.i18n.t('settings.aboutInfo.updateKeyDisabled'),
  );

  readonly capabilities = signal<LocalCapabilitiesResponse | null>(null);
  readonly capabilitiesLoading = signal(false);
  readonly capabilitiesError = signal('');

  readonly transcriptionCapabilities =
    signal<TranscriptionCapabilitiesResponse | null>(null);
  readonly transcriptionLoading = signal(false);
  readonly transcriptionError = signal('');

  readonly whisperPath = this.preferences.whisperPath;
  readonly whisperModelPath = this.preferences.whisperModelPath;

  readonly upscaleAnimeModelPath = this.preferences.upscaleAnimeModelPath;
  readonly hasUpscaleAnimeModel = computed(
    () => this.upscaleAnimeModelPath().trim().length > 0,
  );

  readonly transcriptionReady = computed(
    () => this.transcriptionCapabilities()?.available === true,
  );

  readonly hasWhisperOverrides = computed(
    () => this.whisperPath().length > 0 || this.whisperModelPath().length > 0,
  );

  readonly toolchainStatus = this.toolchain.status;
  readonly toolchainEvent = this.toolchain.event;
  readonly toolchainLoading = this.toolchain.loading;

  readonly toolchainReady = computed(
    () => this.toolchainStatus()?.ready === true,
  );
  readonly toolchainComponents = computed(
    () => this.toolchainStatus()?.components ?? [],
  );

  /**
   * True when an optional pack is missing even though the required toolchain
   * is ready. Without this the Install / repair button disappeared after the
   * first run and there was no way to install background removal or upscaling
   * later.
   */
  readonly optionalPackMissing = computed(() => {
    const optionalPackIDs = new Set([
      'pdf2docx',
      'rembg',
      'rembg-model',
      'upscale',
      'upscale-model',
      'upscale-anime-model',
    ]);

    return this.toolchainComponents().some(
      (component) => optionalPackIDs.has(component.id) && !component.ready,
    );
  });
  readonly toolchainBusy = this.toolchain.isBusy;

  readonly toolchainModel = computed(() => {
    const model = this.toolchainStatus()?.model ?? '';
    const known = TOOLCHAIN_MODELS.some((option) => option === model);
    return known ? model : TOOLCHAIN_MODELS[0];
  });

  readonly toolchainOcrLanguages = computed<string[]>(() => {
    const languages = this.toolchainStatus()?.ocrLanguages ?? [];
    const selected = TOOLCHAIN_OCR_LANGUAGES.filter((language) =>
      languages.includes(language),
    );
    return selected.length > 0 ? selected : [...DEFAULT_OCR_LANGUAGES];
  });

  readonly toolchainError = computed(() => {
    const status = this.toolchainStatus();
    if (!status) {
      return '';
    }

    const error = (status.error ?? '').trim();
    if (error) {
      return error;
    }
    return status.success
      ? ''
      : status.message || this.i18n.t('settings.toolchain.unavailable');
  });

  readonly toolchainUnsupported = computed(() => {
    const status = this.toolchainStatus();
    return (
      !!status &&
      status.success &&
      !status.error &&
      !status.installing &&
      isUnsupportedStatus(status)
    );
  });

  readonly toolchainPercent = computed(() => {
    const value = this.toolchainStatus()?.percent ?? 0;
    if (!Number.isFinite(value)) {
      return 0;
    }
    return Math.min(100, Math.max(0, value));
  });

  readonly toolchainPhaseLabel = computed(() =>
    this.phaseLabel(this.toolchainStatus()?.phase ?? ''),
  );

  readonly toolchainIndeterminate = computed(() => {
    const status = this.toolchainStatus();
    if (!status?.installing) {
      return false;
    }

    return (
      this.toolchainEvent()?.indeterminate === true ||
      status.phase === 'environment' ||
      this.toolchainPercent() <= 0
    );
  });

  readonly toolchainMessage = computed(() => {
    const status = this.toolchainStatus();
    return (
      status?.detail ||
      this.toolchainEvent()?.message ||
      status?.message ||
      ''
    );
  });

  readonly toolchainStorageDir = computed(
    () => this.toolchainStatus()?.storageDir?.trim() ?? '',
  );

  readonly installButtonLabel = computed(() =>
    this.toolchainReady() && this.optionalPackMissing()
      ? this.i18n.t('settings.toolchain.installRepair')
      : this.i18n.t('settings.toolchain.install'),
  );

  private lastToolchainReady = false;

  constructor() {
    // A finished install changes what the transcription and capability checks
    // can see; refresh the dependent panels once it becomes ready.
    effect(() => {
      const ready = this.toolchainStatus()?.ready === true;
      if (ready && !this.lastToolchainReady) {
        void this.loadTranscriptionCapabilities();
        void this.loadCapabilities();
      }
      this.lastToolchainReady = ready;
    });
  }

  ngOnInit(): void {
    void this.loadAppInfo();
    void this.loadCapabilities();
    void this.loadTranscriptionCapabilities();
    void this.toolchain.refresh();
  }

  /** Reads the build identity stamped into the desktop binary. */
  async loadAppInfo(): Promise<void> {
    this.appInfoLoading.set(true);
    this.appInfoError.set('');

    try {
      const response = await this.wails.getAppInfo();
      if (!response.success) {
        this.appInfo.set(null);
        this.appInfoError.set(
          response.message || this.i18n.t('settings.aboutInfo.unavailable'),
        );
        return;
      }

      this.appInfo.set(response);
    } catch {
      this.appInfo.set(null);
      this.appInfoError.set(this.i18n.t('settings.aboutInfo.unavailable'));
    } finally {
      this.appInfoLoading.set(false);
    }
  }

  private aboutValue(value: string | undefined): string {
    const text = (value ?? '').trim();
    return text || this.i18n.t('settings.aboutInfo.notReported');
  }

  async loadCapabilities(): Promise<void> {
    this.capabilitiesLoading.set(true);
    this.capabilitiesError.set('');

    try {
      const response = await this.wails.getLocalCapabilities();
      if (!response.success) {
        this.capabilities.set(null);
        this.capabilitiesError.set(
          this.friendlyCapabilitiesError(response.message),
        );
        return;
      }

      this.capabilities.set(response);
    } catch {
      this.capabilities.set(null);
      this.capabilitiesError.set(
        this.i18n.t('settings.capabilities.unavailable'),
      );
    } finally {
      this.capabilitiesLoading.set(false);
    }
  }

  refreshCapabilities(): void {
    void this.loadCapabilities();
  }

  /** Reads the Whisper runtime with the persisted overrides (empty = defaults). */
  async loadTranscriptionCapabilities(): Promise<void> {
    this.transcriptionLoading.set(true);
    this.transcriptionError.set('');

    try {
      const response = await this.wails.getTranscriptionCapabilities(
        this.preferences.whisperPath(),
        this.preferences.whisperModelPath(),
      );
      if (!response.success) {
        this.transcriptionCapabilities.set(null);
        this.transcriptionError.set(
          this.friendlyCapabilitiesError(
            response.message,
            'settings.transcription.unavailable',
          ),
        );
        return;
      }

      this.transcriptionCapabilities.set(response);
    } catch {
      this.transcriptionCapabilities.set(null);
      this.transcriptionError.set(
        this.i18n.t('settings.transcription.unavailable'),
      );
    } finally {
      this.transcriptionLoading.set(false);
    }
  }

  refreshTranscription(): void {
    void this.loadTranscriptionCapabilities();
  }

  refreshToolchain(): void {
    void this.toolchain.refresh();
  }

  /** Badge for the Tools & Models panel header. */
  toolchainBadgeClass(): string {
    const status = this.toolchainStatus();
    if (!status) {
      return 'badge badge-neutral';
    }
    if (status.ready) {
      return 'badge badge-success';
    }
    if (status.installing) {
      return 'badge badge-info';
    }
    if (this.toolchainError()) {
      return 'badge badge-danger';
    }
    return 'badge badge-warning';
  }

  toolchainStateLabel(): string {
    const status = this.toolchainStatus();
    if (!status) {
      return this.toolchainLoading()
        ? this.i18n.t('settings.status.checking')
        : this.i18n.t('settings.status.unknown');
    }
    if (status.ready) {
      return this.i18n.t('settings.status.ready');
    }
    if (status.installing) {
      return status.cancelling
        ? this.i18n.t('settings.status.cancelling')
        : this.i18n.t('settings.status.installing');
    }
    if (this.toolchainError()) {
      return this.i18n.t('settings.status.error');
    }
    return this.i18n.t('settings.status.setupNeeded');
  }

  toolchainComponentBadgeClass(component: ToolchainComponent): string {
    if (component.ready) {
      return 'badge badge-success';
    }
    return component.required ? 'badge badge-danger' : 'badge badge-neutral';
  }

  toolchainComponentStateLabel(component: ToolchainComponent): string {
    if (component.ready) {
      return this.i18n.t('settings.status.ready');
    }
    if (component.required) {
      return this.i18n.t('settings.status.missing');
    }
    return component.standard
      ? this.i18n.t('settings.status.needsInstall')
      : this.i18n.t('settings.status.optional');
  }

  toolchainComponentDotState(component: ToolchainComponent): string {
    if (component.ready) {
      return 'success';
    }
    return component.required ? 'danger' : 'idle';
  }

  toolchainComponentScopeLabel(component: ToolchainComponent): string {
    if (component.required) {
      return this.i18n.t('settings.status.required');
    }
    return component.standard
      ? this.i18n.t('settings.status.standard')
      : this.i18n.t('settings.status.optional');
  }

  /** Version + path when known, otherwise the human detail line. */
  toolchainComponentMeta(component: ToolchainComponent): string {
    const parts: string[] = [];
    if (component.version?.trim()) {
      parts.push(component.version.trim());
    }
    if (component.path?.trim()) {
      parts.push(component.path.trim());
    }
    if (parts.length === 0 && component.detail?.trim()) {
      parts.push(component.detail.trim());
    }
    return parts.join(' · ');
  }

  isToolchainLanguageSelected(language: string): boolean {
    return this.toolchainOcrLanguages().includes(language);
  }

  toolchainModelLabel(model: string): string {
    switch (model) {
      case 'small':
        return this.i18n.t('settings.model.small');
      case 'large-v3':
        return this.i18n.t('settings.model.largeV3');
      default:
        return this.i18n.t('settings.model.medium');
    }
  }

  toolchainOcrLanguageLabel(language: string): string {
    return language === 'spa'
      ? this.i18n.t('settings.ocrLanguage.spa')
      : this.i18n.t('settings.ocrLanguage.eng');
  }

  onToolchainModelChange(event: Event): void {
    if (this.toolchainBusy()) {
      return;
    }

    const model = (event.target as HTMLSelectElement).value.trim();
    if (!model || model === this.toolchainModel()) {
      return;
    }

    void this.toolchain.install({
      model,
      ocrLanguages: [...this.toolchainOcrLanguages()],
      force: false,
    });
  }

  onToolchainLanguageChange(language: string, event: Event): void {
    if (this.toolchainBusy()) {
      return;
    }

    const input = event.target as HTMLInputElement;
    const selected = new Set(this.toolchainOcrLanguages());
    if (input.checked) {
      selected.add(language);
    } else {
      selected.delete(language);
    }

    const next = TOOLCHAIN_OCR_LANGUAGES.filter((id) => selected.has(id));
    if (next.length === 0) {
      // At least one OCR language must stay selected.
      input.checked = true;
      return;
    }

    if (next.join(',') === this.toolchainOcrLanguages().join(',')) {
      return;
    }

    void this.toolchain.install({
      model: this.toolchainModel(),
      ocrLanguages: next,
      force: false,
    });
  }

  installToolchain(): void {
    void this.toolchain.install({
      model: this.toolchainModel(),
      ocrLanguages: [...this.toolchainOcrLanguages()],
      force: false,
    });
  }

  cancelToolchain(): void {
    void this.toolchain.cancel();
  }

  async openToolchainStorage(): Promise<void> {
    const directory = this.toolchainStorageDir();
    if (!directory) {
      return;
    }

    await this.wails.openPath(directory);
  }

  async chooseWhisperBinary(): Promise<void> {
    const path = (await this.wails.openFileDialog()).trim();
    if (!path) {
      return;
    }

    this.preferences.setWhisperPath(path);
    this.actionMessage.set('');
    await this.loadTranscriptionCapabilities();
  }

  async chooseWhisperModel(): Promise<void> {
    const path = (await this.wails.openFileDialog()).trim();
    if (!path) {
      return;
    }

    this.preferences.setWhisperModelPath(path);
    this.actionMessage.set('');
    await this.loadTranscriptionCapabilities();
  }

  async chooseUpscaleAnimeModel(): Promise<void> {
    const path = (await this.wails.openFileDialog()).trim();
    if (!path) {
      return;
    }

    this.preferences.setUpscaleAnimeModelPath(path);
    this.actionMessage.set('');
  }

  clearUpscaleAnimeModel(): void {
    if (!this.hasUpscaleAnimeModel()) {
      return;
    }

    this.preferences.clearUpscaleAnimeModelPath();
    this.actionMessage.set('');
  }

  upscaleAnimeModelLabel(): string {
    return (
      this.upscaleAnimeModelPath().trim() ||
      this.i18n.t('settings.upscale.animeModelNotSet')
    );
  }

  async useDetectedWhisperPaths(): Promise<void> {
    if (!this.hasWhisperOverrides()) {
      return;
    }

    this.preferences.clearWhisperPaths();
    this.actionMessage.set('');
    await this.loadTranscriptionCapabilities();
  }

  transcriptionBadgeClass(): string {
    return this.transcriptionReady()
      ? 'badge badge-success'
      : 'badge badge-neutral';
  }

  transcriptionStateLabel(): string {
    if (!this.transcriptionCapabilities()) {
      return this.i18n.t('settings.status.unknown');
    }
    return this.transcriptionReady()
      ? this.i18n.t('settings.status.ready')
      : this.i18n.t('settings.status.setupNeeded');
  }

  transcriptionModelLine(): string {
    const capabilities = this.transcriptionCapabilities();
    if (!capabilities) {
      return this.i18n.t('settings.transcription.noModel');
    }

    const name =
      capabilities.modelName?.trim() ||
      this.i18n.t('settings.transcription.modelFallback');
    if (!capabilities.modelPresent) {
      return this.i18n.t('settings.transcription.modelMissing', { name });
    }
    if (
      typeof capabilities.modelSizeBytes === 'number' &&
      capabilities.modelSizeBytes > 0
    ) {
      return this.i18n.t('settings.transcription.modelInfo', {
        name,
        size: formatBytes(capabilities.modelSizeBytes),
      });
    }
    return name;
  }

  transcriptionBinaryPath(): string {
    return (
      this.transcriptionCapabilities()?.whisperPath?.trim() ||
      this.i18n.t('settings.status.notFound')
    );
  }

  transcriptionModelPathLabel(): string {
    return (
      this.transcriptionCapabilities()?.modelPath?.trim() ||
      this.i18n.t('settings.status.notFound')
    );
  }

  transcriptionLanguagesLabel(): string {
    const languages = this.transcriptionCapabilities()?.languages ?? [];
    if (languages.length === 0) {
      return this.i18n.t('settings.transcription.languagesNone');
    }

    const labels = languages.map((language) =>
      this.transcriptionLanguageLabel(language),
    );
    return this.i18n.t('settings.transcription.languages', {
      languages: labels.join(' · '),
    });
  }

  transcriptionGuidance(): string {
    const capabilities = this.transcriptionCapabilities();
    if (!capabilities || capabilities.available) {
      return '';
    }
    return this.i18n.t('settings.transcription.installHelp');
  }

  capabilityBadgeClass(check: LocalCapability): string {
    if (check.available) {
      return 'badge badge-success';
    }
    return check.required ? 'badge badge-danger' : 'badge badge-neutral';
  }

  capabilityStateLabel(check: LocalCapability): string {
    if (check.available) {
      return this.i18n.t('settings.status.ready');
    }
    return check.required
      ? this.i18n.t('settings.status.missing')
      : this.i18n.t('settings.status.optional');
  }

  capabilityDotState(check: LocalCapability): string {
    if (check.available) {
      return 'success';
    }
    return check.required ? 'danger' : 'idle';
  }

  ocrLanguageLabel(): string {
    const capabilities = this.capabilities();
    const languages = capabilities?.ocrLanguages ?? [];
    if (languages.length === 0) {
      return this.i18n.t('settings.capabilities.noOcrData');
    }

    const installed = languages
      .map((language) => language.toUpperCase())
      .join(' · ');
    const recommended = (capabilities?.recommendedOcrLanguage ?? '')
      .trim()
      .toUpperCase();

    return recommended
      ? this.i18n.t('settings.capabilities.ocrLanguagesRecommended', {
          installed,
          recommended,
        })
      : this.i18n.t('settings.capabilities.ocrLanguages', { installed });
  }

  private friendlyCapabilitiesError(
    message: string,
    fallbackKey: TranslationKey = 'settings.capabilities.unavailable',
  ): string {
    const text = (message ?? '').trim();
    if (
      !text ||
      /<\/?[a-z][\s\S]*>/i.test(text) ||
      /Cannot POST|DOCTYPE html/i.test(text)
    ) {
      return this.i18n.t(fallbackKey);
    }

    return text.length > 240 ? `${text.slice(0, 237)}…` : text;
  }

  private transcriptionLanguageLabel(language: string): string {
    switch (language) {
      case 'auto':
        return this.i18n.t('settings.status.auto');
      case 'en':
        return this.i18n.t('settings.languageName.english');
      case 'es':
        return this.i18n.t('settings.languageName.spanish');
      default:
        return language.toUpperCase();
    }
  }

  private phaseLabel(phase: string): string {
    switch (phase) {
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
      case 'rembg':
        return this.i18n.t('toolchain.phase.installingBackgroundRemoval');
      case 'upscale':
        return this.i18n.t('toolchain.phase.installingUpscale');
      case 'finalize':
        return this.i18n.t('toolchain.phase.verifying');
      case 'ready':
        return this.i18n.t('toolchain.phase.ready');
      case 'cancelling':
        return this.i18n.t('toolchain.phase.cancelling');
      case 'cancelled':
        return this.i18n.t('toolchain.phase.cancelled');
      case 'idle':
        return this.i18n.t('settings.status.notStarted');
      default:
        return this.i18n.t('toolchain.phase.preparing');
    }
  }

  async chooseFolder(): Promise<void> {
    const directory = (await this.wails.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }

    this.preferences.setDefaultOutputDir(directory);
    this.actionMessage.set('');
  }

  useOriginalFolders(): void {
    if (!this.hasDefaultDestination()) {
      return;
    }

    this.preferences.clearDefaultOutputDir();
    this.actionMessage.set('');
  }

  setTheme(theme: Theme): void {
    this.themeService.set(theme);
  }

  setLocale(locale: Locale): void {
    this.i18n.setLocale(locale);
  }

  clearHistory(): void {
    if (this.recentCount() === 0) {
      return;
    }

    this.recentConversions.clear();
    this.actionMessage.set(this.i18n.t('settings.action.historyCleared'));
  }

  resetOnboarding(): void {
    try {
      this.document?.defaultView?.localStorage?.removeItem(
        ONBOARDING_STORAGE_KEY,
      );
    } catch {
      // Storage can be unavailable; there is nothing to remove.
    }

    this.actionMessage.set(this.i18n.t('settings.action.onboardingReset'));
  }
}
