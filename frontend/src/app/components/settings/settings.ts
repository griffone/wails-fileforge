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
import { Theme, ThemeService } from '../../services/theme.service';
import {
  isUnsupportedStatus,
  ToolchainStateService,
} from '../../services/toolchain-state.service';
import {
  LocalCapabilityV1,
  LocalCapabilitiesResponseV1,
  ToolchainComponentV1,
  TranscriptionCapabilitiesResponseV1,
  Wails,
} from '../../services/wails';
import { formatBytes } from '../../utils/format';

/** Shared with the Home onboarding strip. */
const ONBOARDING_STORAGE_KEY = 'ff-onboarding-dismissed';

const CAPABILITIES_ERROR_FALLBACK =
  'The local capability check did not run. Open FileForge in the desktop app, then retry.';

const TRANSCRIPTION_ERROR_FALLBACK =
  'The transcription check did not run. Open FileForge in the desktop app, then retry.';

const TOOLCHAIN_MODELS = [
  { id: 'medium', label: 'Medium — recommended' },
  { id: 'small', label: 'Small — lighter download' },
  { id: 'large-v3', label: 'Large v3 — maximum quality' },
] as const;

const TOOLCHAIN_OCR_LANGUAGES = [
  { id: 'eng', label: 'English' },
  { id: 'spa', label: 'Spanish' },
] as const;

const DEFAULT_OCR_LANGUAGES = ['eng', 'spa'];

@Component({
  selector: 'app-settings',
  standalone: true,
  imports: [LucideAngularModule],
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

  readonly recentCount = computed(() => this.recentConversions.recent().length);
  readonly historySummary = computed(() => {
    const count = this.recentCount();
    if (count === 0) {
      return 'No finished conversions yet.';
    }
    return count === 1 ? '1 finished conversion' : `${count} finished conversions`;
  });

  readonly hasDefaultDestination = computed(
    () => this.defaultOutputDir().length > 0,
  );
  readonly isNextToOriginal = computed(() => !this.hasDefaultDestination());

  readonly actionMessage = signal('');

  readonly capabilities = signal<LocalCapabilitiesResponseV1 | null>(null);
  readonly capabilitiesLoading = signal(false);
  readonly capabilitiesError = signal('');

  readonly transcriptionCapabilities =
    signal<TranscriptionCapabilitiesResponseV1 | null>(null);
  readonly transcriptionLoading = signal(false);
  readonly transcriptionError = signal('');

  readonly whisperPath = this.preferences.whisperPath;
  readonly whisperModelPath = this.preferences.whisperModelPath;

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
  readonly toolchainBusy = this.toolchain.isBusy;

  readonly toolchainModel = computed(() => {
    const model = this.toolchainStatus()?.model ?? '';
    const known = TOOLCHAIN_MODELS.some((option) => option.id === model);
    return known ? model : TOOLCHAIN_MODELS[0].id;
  });

  readonly toolchainOcrLanguages = computed<string[]>(() => {
    const languages = this.toolchainStatus()?.ocrLanguages ?? [];
    const selected = TOOLCHAIN_OCR_LANGUAGES.map((option) => option.id).filter(
      (language) => languages.includes(language),
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
      : status.message || 'The local toolchain is unavailable.';
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
    toolchainPhaseLabel(this.toolchainStatus()?.phase ?? ''),
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
    void this.loadCapabilities();
    void this.loadTranscriptionCapabilities();
    void this.toolchain.refresh();
  }

  async loadCapabilities(): Promise<void> {
    this.capabilitiesLoading.set(true);
    this.capabilitiesError.set('');

    try {
      const response = await this.wails.getLocalCapabilitiesV1();
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
      this.capabilitiesError.set(CAPABILITIES_ERROR_FALLBACK);
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
      const response = await this.wails.getTranscriptionCapabilitiesV1(
        this.preferences.whisperPath(),
        this.preferences.whisperModelPath(),
      );
      if (!response.success) {
        this.transcriptionCapabilities.set(null);
        this.transcriptionError.set(
          this.friendlyCapabilitiesError(
            response.message,
            TRANSCRIPTION_ERROR_FALLBACK,
          ),
        );
        return;
      }

      this.transcriptionCapabilities.set(response);
    } catch {
      this.transcriptionCapabilities.set(null);
      this.transcriptionError.set(TRANSCRIPTION_ERROR_FALLBACK);
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
      return this.toolchainLoading() ? 'Checking…' : 'Unknown';
    }
    if (status.ready) {
      return 'Ready';
    }
    if (status.installing) {
      return status.cancelling ? 'Cancelling' : 'Installing';
    }
    if (this.toolchainError()) {
      return 'Error';
    }
    return 'Setup needed';
  }

  toolchainComponentBadgeClass(component: ToolchainComponentV1): string {
    if (component.ready) {
      return 'badge badge-success';
    }
    return component.required ? 'badge badge-danger' : 'badge badge-neutral';
  }

  toolchainComponentStateLabel(component: ToolchainComponentV1): string {
    if (component.ready) {
      return 'Ready';
    }
    return component.required ? 'Missing' : 'Optional';
  }

  toolchainComponentDotState(component: ToolchainComponentV1): string {
    if (component.ready) {
      return 'success';
    }
    return component.required ? 'danger' : 'idle';
  }

  /** Version + path when known, otherwise the human detail line. */
  toolchainComponentMeta(component: ToolchainComponentV1): string {
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

    const next = TOOLCHAIN_OCR_LANGUAGES.map((option) => option.id).filter(
      (id) => selected.has(id),
    );
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
      return 'Unknown';
    }
    return this.transcriptionReady() ? 'Ready' : 'Setup needed';
  }

  transcriptionModelLine(): string {
    const capabilities = this.transcriptionCapabilities();
    if (!capabilities) {
      return 'No Whisper model detected.';
    }

    const name = capabilities.modelName?.trim() || 'Whisper model';
    if (!capabilities.modelPresent) {
      return `${name} not found`;
    }
    if (
      typeof capabilities.modelSizeBytes === 'number' &&
      capabilities.modelSizeBytes > 0
    ) {
      return `${name} · ${formatBytes(capabilities.modelSizeBytes)}`;
    }
    return name;
  }

  transcriptionBinaryPath(): string {
    return this.transcriptionCapabilities()?.whisperPath?.trim() || 'Not found';
  }

  transcriptionModelPathLabel(): string {
    return this.transcriptionCapabilities()?.modelPath?.trim() || 'Not found';
  }

  transcriptionLanguagesLabel(): string {
    const languages = this.transcriptionCapabilities()?.languages ?? [];
    if (languages.length === 0) {
      return 'Languages: not reported';
    }

    const labels = languages.map((language) => {
      switch (language) {
        case 'auto':
          return 'Auto';
        case 'en':
          return 'English';
        case 'es':
          return 'Spanish';
        default:
          return language.toUpperCase();
      }
    });
    return `Languages: ${labels.join(' · ')}`;
  }

  transcriptionGuidance(): string {
    const capabilities = this.transcriptionCapabilities();
    if (!capabilities || capabilities.available) {
      return '';
    }
    return 'Install whisper.cpp and choose a ggml model file. FileForge never downloads models automatically.';
  }

  capabilityBadgeClass(check: LocalCapabilityV1): string {
    if (check.available) {
      return 'badge badge-success';
    }
    return check.required ? 'badge badge-danger' : 'badge badge-neutral';
  }

  capabilityStateLabel(check: LocalCapabilityV1): string {
    if (check.available) {
      return 'Ready';
    }
    return check.required ? 'Missing' : 'Optional';
  }

  capabilityDotState(check: LocalCapabilityV1): string {
    if (check.available) {
      return 'success';
    }
    return check.required ? 'danger' : 'idle';
  }

  ocrLanguageLabel(): string {
    const capabilities = this.capabilities();
    const languages = capabilities?.ocrLanguages ?? [];
    if (languages.length === 0) {
      return 'No OCR language data installed';
    }

    const installed = languages
      .map((language) => language.toUpperCase())
      .join(' · ');
    const recommended = (capabilities?.recommendedOcrLanguage ?? '')
      .trim()
      .toUpperCase();

    return recommended
      ? `OCR languages: ${installed} · recommended ${recommended}`
      : `OCR languages: ${installed}`;
  }

  private friendlyCapabilitiesError(
    message: string,
    fallback: string = CAPABILITIES_ERROR_FALLBACK,
  ): string {
    const text = (message ?? '').trim();
    if (
      !text ||
      /<\/?[a-z][\s\S]*>/i.test(text) ||
      /Cannot POST|DOCTYPE html/i.test(text)
    ) {
      return fallback;
    }

    return text.length > 240 ? `${text.slice(0, 237)}…` : text;
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

  clearHistory(): void {
    if (this.recentCount() === 0) {
      return;
    }

    this.recentConversions.clear();
    this.actionMessage.set('Recent conversions cleared.');
  }

  resetOnboarding(): void {
    try {
      this.document?.defaultView?.localStorage?.removeItem(
        ONBOARDING_STORAGE_KEY,
      );
    } catch {
      // Storage can be unavailable; there is nothing to remove.
    }

    this.actionMessage.set('Onboarding will show again on Home.');
  }
}

function toolchainPhaseLabel(phase: string): string {
  switch (phase) {
    case 'starting':
      return 'Preparing the installer';
    case 'micromamba':
      return 'Downloading micromamba';
    case 'environment':
      return 'Installing tool packages';
    case 'whisper':
      return 'Installing Whisper';
    case 'model':
      return 'Downloading the model';
    case 'tessdata':
      return 'Installing OCR languages';
    case 'finalize':
      return 'Verifying';
    case 'ready':
      return 'Ready';
    case 'cancelling':
      return 'Cancelling';
    case 'cancelled':
      return 'Cancelled';
    case 'idle':
      return 'Not started';
    default:
      return 'Preparing';
  }
}
