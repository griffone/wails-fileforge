import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import {
  FolderOpen,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { LastRequestService } from '../../services/last-request.service';
import { ActivityService } from '../../services/activity.service';
import {
  FileInfo,
  JobRequest,
  TranscriptionCapabilitiesResponse,
  Wails,
} from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
  formatDuration,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';
import type { TranslationKey } from '../../i18n';

const TRANSCRIBE_TOOL_ID = 'tool.video.transcribe';
const TRANSCRIBE_ROUTE = '/video-transcribe';
const PLAN_DEBOUNCE_MS = 250;
const OPTIONS_SCHEMA_VERSION = 1;

/** Every format the backend can publish; the UI order is part of the plan. */
const FORMAT_IDS = ['srt', 'vtt', 'txt'] as const;
const DEFAULT_FORMATS = [...FORMAT_IDS];

const ACCEPTED_EXTENSIONS = new Set([
  'mp4',
  'mov',
  'mkv',
  'webm',
  'avi',
  'mp3',
  'm4a',
  'wav',
  'ogg',
  'flac',
]);

const LANGUAGE_OPTIONS: readonly {
  value: string;
  labelKey: TranslationKey;
}[] = [
  { value: 'auto', labelKey: 'videoTranscribe.language.auto' },
  { value: 'en', labelKey: 'videoTranscribe.language.en' },
  { value: 'es', labelKey: 'videoTranscribe.language.es' },
];

type CapabilityStatus = 'loading' | 'ok' | 'error';

type TranscribeOptions = {
  language: string;
  formats: string[];
};

const LANGUAGE_VALUES = new Set(LANGUAGE_OPTIONS.map((option) => option.value));

function defaultOptions(): TranscribeOptions {
  return { language: 'auto', formats: [...DEFAULT_FORMATS] };
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<TranscribeOptions> {
  const result: Partial<TranscribeOptions> = {};

  const language = stored['language'];
  if (typeof language === 'string' && LANGUAGE_VALUES.has(language)) {
    result.language = language;
  }

  const rawFormats = stored['formats'];
  if (Array.isArray(rawFormats)) {
    const chosen = FORMAT_IDS.filter((id) => rawFormats.includes(id));
    if (chosen.length > 0) {
      result.formats = [...chosen];
    }
  }

  return result;
}

export interface CapabilityBanner {
  title: string;
  detail: string;
}

export interface TranscriptSummary {
  tone: 'success' | 'warning';
  text: string;
}

/** `['a']` → `A`; `['a','b']` → `A and B`; more use commas. */
function joinWords(values: readonly string[], conjunction: string): string {
  if (values.length <= 1) {
    return values[0] ?? '';
  }
  if (values.length === 2) {
    return `${values[0]} ${conjunction} ${values[1]}`;
  }
  return `${values.slice(0, -1).join(', ')} ${conjunction} ${values[values.length - 1]}`;
}

@Component({
  selector: 'app-video-transcribe',
  standalone: true,
  imports: [
    RouterLink,
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './video-transcribe.html',
  styleUrl: './video-transcribe.css',
})
export class VideoTranscribe implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly lastRequests = inject(LastRequestService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: TRANSCRIBE_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: TRANSCRIBE_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    initialOutputDir: this.preferences.defaultOutputDir().trim(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) =>
      ACCEPTED_EXTENSIONS.has(fileExtension(path).toLowerCase()),
    canPlan: () =>
      this.selectedPaths().length > 0 &&
      !this.transcriptionBlocked() &&
      !this.formatsMissing(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.plan()?.success === false,
    loadThumbnails: false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: () => this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS),
    messages: {
      start: 'videoTranscribe.error.start',
      planFailed: 'videoTranscribe.error.plan',
      waiting: 'videoTranscribe.status.waiting',
      inspectFailed: 'videoTranscribe.error.inspect',
      statusMissing: 'videoTranscribe.error.status',
      cancelFailed: 'videoTranscribe.error.cancel',
      openFileFailed: 'videoTranscribe.error.openFile',
      revealFailed: 'videoTranscribe.error.openFolder',
    },
    notice: {
      rejectedOne: 'videoTranscribe.notice.rejectedOne',
      rejectedMany: 'videoTranscribe.notice.rejectedMany',
      duplicateOne: 'videoTranscribe.notice.duplicateOne',
      duplicateMany: 'videoTranscribe.notice.duplicateMany',
      rejectedManyFormat: 'names',
    },
    onRunAccepted: (request) => {
      this.settings.save(this.currentOptions());
      this.lastRequests.save(request);
    },
    onRunAgainAdopted: (request) => this.applyRequestOptions(request.options),
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly accept = Array.from(ACCEPTED_EXTENSIONS)
    .map((extension) => `.${extension}`)
    .join(',');

  readonly languageOptions = computed(() =>
    LANGUAGE_OPTIONS.map((option) => ({
      value: option.value,
      label: this.i18n.t(option.labelKey),
    })),
  );
  readonly formatOptions = [
    { id: 'srt', label: 'SRT' },
    { id: 'vtt', label: 'VTT' },
    { id: 'txt', label: 'TXT' },
  ];

  readonly selectedPaths = this.job.selectedPaths;
  readonly fileItems = this.job.fileItems;
  readonly outputDir = this.job.outputDir;
  readonly plan = this.job.plan;
  readonly planError = this.job.planError;
  readonly planLoading = this.job.planLoading;
  readonly jobResult = this.job.jobResult;
  readonly jobError = this.job.jobError;
  readonly outputInfos = this.job.outputInfos;
  readonly isSubmitting = this.job.isSubmitting;
  readonly isPolling = this.job.isPolling;
  readonly activeJobId = this.job.activeJobId;
  readonly notice = this.job.notice;
  readonly actionMessage = this.job.actionMessage;
  readonly terminalJob = this.job.terminalJob;
  readonly showProgress = this.job.showProgress;
  readonly jobErrorCopy = this.job.jobErrorCopy;
  readonly verification = this.job.verification;
  readonly verificationLoading = this.job.verificationLoading;
  readonly verificationUnavailable = this.job.verificationUnavailable;

  /** `auto` lets the backend detect the spoken language. */
  readonly language = signal('auto');
  readonly formats = signal<string[]>([...DEFAULT_FORMATS]);

  readonly capabilities =
    signal<TranscriptionCapabilitiesResponse | null>(null);
  readonly capabilitiesStatus = signal<CapabilityStatus>('loading');

  readonly transcribeAvailable = computed(() => {
    const capabilities = this.capabilities();
    return !!capabilities && capabilities.success && capabilities.available;
  });

  /**
   * A real backend answer (or a failed IPC call) blocks the run: transcription
   * cannot work without the binary and the model.
   */
  readonly transcriptionBlocked = computed(
    () => this.capabilitiesStatus() !== 'loading' && !this.transcribeAvailable(),
  );

  readonly modelSummary = computed(() => {
    const capabilities = this.capabilities();
    if (!capabilities || !this.transcribeAvailable()) {
      return '';
    }

    const parts = [this.i18n.t('videoTranscribe.model.ready')];
    const modelName = capabilities.modelName?.trim() ?? '';
    if (modelName) {
      parts.push(modelName);
    }
    if (
      typeof capabilities.modelSizeBytes === 'number' &&
      capabilities.modelSizeBytes > 0
    ) {
      parts.push(formatBytes(capabilities.modelSizeBytes));
    }
    parts.push(
      this.languageOptions()
        .map((option) => option.label)
        .join('/'),
    );
    return parts.join(' · ');
  });

  readonly capabilityBanner = computed<CapabilityBanner | null>(() => {
    if (!this.transcriptionBlocked()) {
      return null;
    }

    const capabilities = this.capabilities();
    const whisperPath =
      capabilities?.whisperPath?.trim() ||
      this.i18n.t('videoTranscribe.capability.notFound');
    const modelPath =
      capabilities?.modelPath?.trim() ||
      this.i18n.t('videoTranscribe.capability.notFound');

    const missing: string[] = [];
    if (capabilities && !capabilities.whisperPresent) {
      missing.push(
        this.i18n.t('videoTranscribe.capability.missingWhisper'),
      );
    }
    if (capabilities && !capabilities.modelPresent) {
      missing.push(
        this.i18n.t('videoTranscribe.capability.missingModel'),
      );
    }

    const guidance =
      missing.length > 0
        ? this.i18n.t('videoTranscribe.capability.installMissing', {
            missing: joinWords(
              missing,
              this.i18n.t('videoTranscribe.list.and'),
            ),
          })
        : capabilities?.message?.trim() ||
          this.i18n.t('videoTranscribe.capability.installFallback');

    return {
      title: this.i18n.t('videoTranscribe.capability.title'),
      detail: this.i18n.t('videoTranscribe.capability.detail', {
        whisper: whisperPath,
        model: modelPath,
        guidance,
      }),
    };
  });

  readonly formatsMissing = computed(() => this.formats().length === 0);

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      this.transcribeAvailable() &&
      !this.formatsMissing() &&
      !this.planBlocked() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('videoTranscribe.action.transcribing');
    }
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('videoTranscribe.action.transcribeFiles');
    }
    return count === 1
      ? this.i18n.t('videoTranscribe.action.transcribeOne')
      : this.i18n.t('videoTranscribe.action.transcribeMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('videoTranscribe.output.destinationEach'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('videoTranscribe.helper.addOne');
    }
    if (this.transcriptionBlocked()) {
      return this.i18n.t('videoTranscribe.helper.unavailable');
    }
    if (this.formatsMissing()) {
      return this.i18n.t('videoTranscribe.helper.chooseFormat');
    }
    return '';
  });

  /**
   * The tool reports segments, characters and warnings in the first
   * successful item message; `No speech` means the media stayed silent.
   */
  readonly transcriptSummary = computed<TranscriptSummary | null>(() => {
    const job = this.terminalJob();
    if (!job) {
      return null;
    }

    const item = (job.items ?? []).find(
      (candidate) => candidate.success && candidate.message?.trim(),
    );
    if (!item) {
      return null;
    }

    const text = item.message.trim();
    return {
      tone: /no speech/i.test(text) ? 'warning' : 'success',
      text,
    };
  });

  ngOnInit(): void {
    this.restoreOptions();
    void this.loadCapabilities();

    this.job.takePendingDrop(TRANSCRIBE_ROUTE);
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.fileOne'),
      this.i18n.t('common.fileMany'),
    );
  }

  onPathsSelected(paths: string[]): void {
    this.job.onPathsSelected(paths);
  }

  async chooseFiles(): Promise<void> {
    const paths = await this.wailsService.openMultipleFilesDialog();
    if (paths.length > 0) {
      this.job.onPathsSelected(paths);
    }
  }

  clear(): void {
    this.job.clear();
  }

  removeFile(path: string): void {
    this.job.removeFile(path);
  }

  onLanguageChange(event: Event): void {
    const value = (event.target as HTMLSelectElement | null)?.value ?? '';
    if (value === this.language()) {
      return;
    }
    this.language.set(value);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  hasFormat(id: string): boolean {
    return this.formats().includes(id);
  }

  onFormatToggle(id: string, event: Event): void {
    const checked = !!(event.target as HTMLInputElement | null)?.checked;
    const current = this.formats();
    if (checked === current.includes(id)) {
      return;
    }

    const next = checked
      ? [...current, id]
      : current.filter((format) => format !== id);
    this.formats.set(FORMAT_IDS.filter((format) => next.includes(format)));
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  useDefaultOutputFolder(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  async run(): Promise<void> {
    await this.job.run();
  }

  async cancel(): Promise<void> {
    await this.job.cancel();
  }

  async onOpen(path: string): Promise<void> {
    await this.job.openPath(path);
  }

  async onReveal(path: string): Promise<void> {
    await this.job.revealPath(path);
  }

  reset(): void {
    this.job.reset();
    this.restoreOptions();
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.job.adoptRunAgain(event);
  }

  private currentOptions(): TranscribeOptions {
    return { language: this.language(), formats: [...this.formats()] };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.language.set(options.language);
    this.formats.set([...options.formats]);
  }

  private applyRequestOptions(options: Record<string, unknown>): void {
    const stored = normalizeOptions(options);
    if (stored.language) {
      this.language.set(stored.language);
    }
    if (stored.formats) {
      this.formats.set([...stored.formats]);
    }
  }

  private async loadCapabilities(): Promise<void> {
    this.capabilitiesStatus.set('loading');

    try {
      const response = await this.wailsService.getTranscriptionCapabilities(
        this.preferences.whisperPath(),
        this.preferences.whisperModelPath(),
      );
      this.capabilities.set(response);
      this.capabilitiesStatus.set(response.success ? 'ok' : 'error');

      if (response.success) {
        const offered = this.languageOptions().map(
          (option) => option.value,
        );
        const defaultLanguage = response.defaultLanguage?.trim() ?? '';
        if (
          this.language() === 'auto' &&
          defaultLanguage &&
          offered.includes(defaultLanguage)
        ) {
          this.language.set(defaultLanguage);
        }
      }
    } catch {
      this.capabilities.set(null);
      this.capabilitiesStatus.set('error');
    }
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    return {
      toolId: TRANSCRIBE_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options: {
        language: this.language(),
        formats: [...this.formats()],
        whisperPath: this.preferences.whisperPath(),
        modelPath: this.preferences.whisperModelPath(),
      },
    };
  }

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.durationSeconds === 'number' && info.durationSeconds > 0) {
      parts.push(formatDuration(info.durationSeconds));
    }
    return parts.join(' · ');
  }
}
