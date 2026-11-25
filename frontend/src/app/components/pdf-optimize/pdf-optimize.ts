import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
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
import { FileInfo, JobRequest, Wails } from '../../services/wails';
import {
  fileExtension,
  fileName,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { createToolOptions } from '../../utils/tool-options';
import { ToolRunController } from '../../utils/tool-run-controller';

const PDF_OPTIMIZE_TOOL_ID = 'tool.pdf.optimize';
const PLAN_DEBOUNCE_MS = 250;
const METADATA_MAX_LENGTH = 512;
const OPTIONS_SCHEMA_VERSION = 1;

type MetadataKey = 'title' | 'author' | 'subject' | 'keywords' | 'creator';

interface MetadataFieldDefinition {
  key: MetadataKey;
  label: string;
  id: string;
  placeholder: string;
}

const METADATA_FIELDS: readonly MetadataFieldDefinition[] = [
  {
    key: 'title',
    label: 'Title',
    id: 'pdf-optimize-title',
    placeholder: 'Document title',
  },
  {
    key: 'author',
    label: 'Author',
    id: 'pdf-optimize-author',
    placeholder: 'Document author',
  },
  {
    key: 'subject',
    label: 'Subject',
    id: 'pdf-optimize-subject',
    placeholder: 'Document subject',
  },
  {
    key: 'keywords',
    label: 'Keywords',
    id: 'pdf-optimize-keywords',
    placeholder: 'Comma-separated keywords',
  },
  {
    key: 'creator',
    label: 'Creator',
    id: 'pdf-optimize-creator',
    placeholder: 'Application or source',
  },
];

type MetadataValues = Record<MetadataKey, string>;

function emptyMetadata(): MetadataValues {
  return {
    title: '',
    author: '',
    subject: '',
    keywords: '',
    creator: '',
  };
}

type PdfOptimizeOptions = {
  compress: boolean;
  metadata: MetadataValues;
};

function defaultOptions(): PdfOptimizeOptions {
  return { compress: true, metadata: emptyMetadata() };
}

/** Only known fields with valid shapes survive a stale or corrupt payload. */
function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<PdfOptimizeOptions> {
  const result: Partial<PdfOptimizeOptions> = {};

  if (typeof stored['compress'] === 'boolean') {
    result.compress = stored['compress'];
  }

  const rawMetadata = stored['metadata'];
  if (typeof rawMetadata === 'object' && rawMetadata !== null) {
    const metadata = emptyMetadata();
    const record = rawMetadata as Record<string, unknown>;
    for (const field of METADATA_FIELDS) {
      const value = record[field.key];
      if (typeof value === 'string' && characterCount(value) <= METADATA_MAX_LENGTH) {
        metadata[field.key] = value;
      }
    }
    result.metadata = metadata;
  }

  return result;
}

/** Counts code points so the limit matches the backend's rune count. */
function characterCount(value: string): number {
  return [...value].length;
}

@Component({
  selector: 'app-pdf-optimize',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-optimize.html',
  styleUrl: './pdf-optimize.css',
})
export class PdfOptimize implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  private readonly lastRequests = inject(LastRequestService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: PDF_OPTIMIZE_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: PDF_OPTIMIZE_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: this.lastRequests,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) => fileExtension(path).toLowerCase() === 'pdf',
    canPlan: () => this.selectedPaths().length > 0 && !this.metadataInvalid(),
    canRun: () => this.canRun(),
    extraBlocked: () => this.plan()?.success === false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    messages: {
      start: 'pdfOptimize.error.start',
      planFailed: 'pdfOptimize.error.plan',
      waiting: 'pdfOptimize.status.waiting',
      inspectFailed: 'pdfOptimize.error.inspect',
      statusMissing: 'pdfOptimize.error.status',
      cancelFailed: 'pdfOptimize.error.cancel',
      openFileFailed: 'pdfOptimize.error.openFile',
      revealFailed: 'pdfOptimize.error.openFolder',
    },
    notice: {
      rejectedOne: 'pdfOptimize.notice.rejectedOne',
      rejectedMany: 'pdfOptimize.notice.rejectedMany',
      duplicateOne: 'pdfOptimize.notice.duplicateOne',
      duplicateMany: 'pdfOptimize.notice.duplicateMany',
      rejectedManyFormat: 'names',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly metadataFields = METADATA_FIELDS;

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

  readonly compress = signal(true);
  readonly metadata = signal<MetadataValues>(emptyMetadata());

  /** True when a metadata value exceeds the backend's 512-character limit. */
  readonly metadataInvalid = computed(() =>
    METADATA_FIELDS.some(
      (field) =>
        characterCount(this.metadata()[field.key].trim()) > METADATA_MAX_LENGTH,
    ),
  );

  readonly hasMetadata = computed(() => {
    const values = this.metadata();
    return METADATA_FIELDS.some(
      (field) => values[field.key].trim().length > 0,
    );
  });

  readonly compressionOnly = computed(
    () => !this.compress() && !this.hasMetadata(),
  );

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.metadataInvalid() &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('pdfOptimize.action.optimizing');
    }
    const count = this.selectedPaths().length;
    return count === 0
      ? this.i18n.t('pdfOptimize.action.optimize')
      : count === 1
        ? this.i18n.t('pdfOptimize.action.optimizeOne')
        : this.i18n.t('pdfOptimize.action.optimizeMany', { count });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('pdfOptimize.destination.each'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  /** A failed plan means the request cannot run as typed. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly actionHelper = computed(() => {
    if (this.selectedPaths().length === 0) {
      return this.i18n.t('pdfOptimize.helper.addOne');
    }
    if (this.metadataInvalid()) {
      return this.i18n.t('pdfOptimize.helper.metadataTooLong', {
        max: METADATA_MAX_LENGTH,
      });
    }
    return '';
  });

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/pdf-optimize');
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.job.dispose();
  }

  fileCountLabel(): string {
    return formatCount(
      this.selectedPaths().length,
      this.i18n.t('common.pdfOne'),
      this.i18n.t('common.pdfMany'),
    );
  }

  /** Metadata copy resolved at render time so the locale switch applies. */
  metadataLabel(field: MetadataFieldDefinition): string {
    return this.i18n.tOr(
      `pdfOptimize.metadata.${field.key}.label`,
      field.label,
    );
  }

  metadataPlaceholder(field: MetadataFieldDefinition): string {
    return this.i18n.tOr(
      `pdfOptimize.metadata.${field.key}.placeholder`,
      field.placeholder,
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

  onCompressToggle(event: Event): void {
    this.compress.set(!!(event.target as HTMLInputElement | null)?.checked);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  onMetadataInput(key: MetadataKey, event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.metadata.update((current) => ({ ...current, [key]: value }));
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  resetAdvanced(): void {
    const empty = emptyMetadata();
    const values = this.metadata();
    const alreadyEmpty = METADATA_FIELDS.every(
      (field) => values[field.key] === empty[field.key],
    );
    if (alreadyEmpty && this.compress()) {
      return;
    }

    this.metadata.set(empty);
    this.compress.set(true);
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    await this.job.refreshPlan();
  }

  useOriginalFolders(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.job.refreshPlan();
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

  /** Retries only the failed inputs with the same compression and metadata. */
  async retryFailed(): Promise<void> {
    const failed = (this.jobResult()?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => item.inputPath)
      .filter((path) => path.length > 0);

    if (failed.length === 0) {
      return;
    }

    this.job.replacePaths([...new Set(failed)], { notify: false });
    this.jobResult.set(null);
    this.jobError.set(null);
    this.outputInfos.set({});
    this.actionMessage.set('');

    await this.job.run();
  }

  reset(): void {
    this.job.reset();
    this.restoreOptions();
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.plan.set(event.plan);
    this.job.adoptRunAgain(event);
  }

  private currentOptions(): PdfOptimizeOptions {
    return { compress: this.compress(), metadata: { ...this.metadata() } };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.compress.set(options.compress);
    this.metadata.set({ ...options.metadata });
  }

  private buildRequest(): JobRequest {
    const paths = this.selectedPaths();
    const options: Record<string, unknown> = {};

    // Compression defaults to on in the backend, so only the opt-out is sent.
    if (!this.compress()) {
      options['compress'] = false;
    }

    const metadata = this.metadataPayload();
    if (Object.keys(metadata).length > 0) {
      options['metadata'] = metadata;
    }

    return {
      toolId: PDF_OPTIMIZE_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options,
    };
  }

  /** Trims and omits blank fields; the backend keeps the existing values. */
  private metadataPayload(): Record<string, string> {
    const values = this.metadata();
    const payload: Record<string, string> = {};
    for (const field of METADATA_FIELDS) {
      const value = values[field.key].trim();
      if (value.length > 0) {
        payload[field.key] = value;
      }
    }
    return payload;
  }

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    if (typeof info.pageCount === 'number' && info.pageCount > 0) {
      parts.push(
        formatCount(
          info.pageCount,
          this.i18n.t('common.pageOne'),
          this.i18n.t('common.pageMany'),
        ),
      );
    }
    return parts.join(' · ');
  }
}
