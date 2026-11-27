import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule } from '@angular/forms';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';
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
import { OpenWindowAction } from '../open-window-action/open-window-action';
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

const MD_TO_PDF_TOOL_ID = 'tool.doc.md_to_pdf';
const BAND_PLAN_DEBOUNCE_MS = 250;
const PAGE_SETUP_DEBOUNCE_MS = 200;
const OPTIONS_SCHEMA_VERSION = 1;

/** Mirrors the backend defaults: A4 portrait with 20/15/18/15 mm margins. */
const PAGE_SETUP_DEFAULTS = {
  pageSize: 'A4',
  orientation: 'P',
  marginTop: 20,
  marginRight: 15,
  marginBottom: 18,
  marginLeft: 15,
} as const;

const PAGE_SETUP_MARGIN_CONTROLS = [
  'marginTop',
  'marginRight',
  'marginBottom',
  'marginLeft',
] as const;

const PAGE_SIZES = ['A4', 'Letter'] as const;
const ORIENTATIONS = ['P', 'L'] as const;
const ALIGN_VALUES = ['left', 'center', 'right'] as const;
const FONT_VALUES = ['helvetica', 'times', 'courier'] as const;
const COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/;

type BandRole = 'header' | 'footer';

type BandOptions = {
  enabled: boolean;
  text: string;
  align: string;
  font: string;
  marginTop: number;
  marginBottom: number;
  color: string;
};

type PageSetupOptions = {
  pageSize: string;
  orientation: string;
  marginTop: number;
  marginRight: number;
  marginBottom: number;
  marginLeft: number;
};

type MdToPdfOptions = {
  header: BandOptions;
  footer: BandOptions;
  pageSetup: PageSetupOptions;
};

const BAND_DEFAULTS: BandOptions = {
  enabled: false,
  text: '',
  align: 'left',
  font: 'helvetica',
  marginTop: 0,
  marginBottom: 0,
  color: '#000000',
};

function defaultOptions(): MdToPdfOptions {
  return {
    header: { ...BAND_DEFAULTS },
    footer: { ...BAND_DEFAULTS },
    pageSetup: { ...PAGE_SETUP_DEFAULTS },
  };
}

function finiteNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

function normalizeBand(stored: unknown): BandOptions | undefined {
  if (typeof stored !== 'object' || stored === null) {
    return undefined;
  }

  const record = stored as Record<string, unknown>;
  const band: BandOptions = { ...BAND_DEFAULTS };
  if (typeof record['enabled'] === 'boolean') {
    band.enabled = record['enabled'];
  }
  if (typeof record['text'] === 'string') {
    band.text = record['text'];
  }
  const align = record['align'];
  if (typeof align === 'string' && ALIGN_VALUES.some((value) => value === align)) {
    band.align = align;
  }
  const font = record['font'];
  if (typeof font === 'string' && FONT_VALUES.some((value) => value === font)) {
    band.font = font;
  }
  if (
    typeof record['color'] === 'string' &&
    COLOR_PATTERN.test(record['color'])
  ) {
    band.color = record['color'];
  }
  band.marginTop = finiteNumber(record['marginTop'], BAND_DEFAULTS.marginTop);
  band.marginBottom = finiteNumber(
    record['marginBottom'],
    BAND_DEFAULTS.marginBottom,
  );
  return band;
}

function normalizeOptions(
  stored: Record<string, unknown>,
): Partial<MdToPdfOptions> {
  const result: Partial<MdToPdfOptions> = {};

  const header = normalizeBand(stored['header']);
  if (header) {
    result.header = header;
  }
  const footer = normalizeBand(stored['footer']);
  if (footer) {
    result.footer = footer;
  }

  const rawSetup = stored['pageSetup'];
  if (typeof rawSetup === 'object' && rawSetup !== null) {
    const record = rawSetup as Record<string, unknown>;
    const setup: PageSetupOptions = { ...PAGE_SETUP_DEFAULTS };
    const pageSize = record['pageSize'];
    if (
      typeof pageSize === 'string' &&
      PAGE_SIZES.some((value) => value === pageSize)
    ) {
      setup.pageSize = pageSize;
    }
    const orientation = record['orientation'];
    if (
      typeof orientation === 'string' &&
      ORIENTATIONS.some((value) => value === orientation)
    ) {
      setup.orientation = orientation;
    }
    setup.marginTop = finiteNumber(record['marginTop'], setup.marginTop);
    setup.marginRight = finiteNumber(record['marginRight'], setup.marginRight);
    setup.marginBottom = finiteNumber(
      record['marginBottom'],
      setup.marginBottom,
    );
    setup.marginLeft = finiteNumber(record['marginLeft'], setup.marginLeft);
    result.pageSetup = setup;
  }

  return result;
}

@Component({
  selector: 'app-doc-md-to-pdf',
  standalone: true,
  imports: [
    ReactiveFormsModule,
    LucideAngularModule,
    OpenWindowAction,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './doc-md-to-pdf.html',
  styleUrl: './doc-md-to-pdf.css',
})
export class DocMdToPdf implements OnDestroy, OnInit {
  private readonly destroy$ = new Subject<void>();
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly settings = createToolOptions(this.preferences, {
    toolId: MD_TO_PDF_TOOL_ID,
    schemaVersion: OPTIONS_SCHEMA_VERSION,
    defaults: defaultOptions,
    normalize: normalizeOptions,
  });

  private readonly job: ToolRunController = new ToolRunController({
    toolId: MD_TO_PDF_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) => this.isMarkdown(path),
    canPlan: () => this.selectedPaths().length > 0,
    canRun: () => this.canRun(),
    extraBlocked: () => this.planBlocked(),
    alwaysRefreshPlanBeforeRun: false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    messages: {
      start: 'docMdToPdf.error.start',
      planFailed: 'docMdToPdf.error.plan',
      waiting: 'docMdToPdf.status.waiting',
      inspectFailed: 'docMdToPdf.error.inspect',
      statusMissing: 'docMdToPdf.error.status',
      cancelFailed: 'docMdToPdf.error.cancel',
      openFileFailed: 'docMdToPdf.error.openFile',
      revealFailed: 'docMdToPdf.error.openFolder',
    },
    notice: {
      rejectedOne: 'docMdToPdf.notice.rejectedOne',
      rejectedMany: 'docMdToPdf.notice.rejectedMany',
      duplicateOne: 'docMdToPdf.notice.duplicateOne',
      duplicateMany: 'docMdToPdf.notice.duplicateMany',
      rejectedManyFormat: 'count',
    },
    onRunAccepted: () => {
      this.settings.save(this.currentOptions());
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  private readonly fb = inject(FormBuilder);

  /**
   * Header and footer are a fixed object with seven keys and the backend
   * rejects a partial object.
   */
  readonly form = this.fb.nonNullable.group({
    header: this.fb.nonNullable.group({
      enabled: false,
      text: '',
      align: 'left',
      font: 'helvetica',
      marginTop: 0,
      marginBottom: 0,
      color: '#000000',
    }),
    footer: this.fb.nonNullable.group({
      enabled: false,
      text: '',
      align: 'left',
      font: 'helvetica',
      marginTop: 0,
      marginBottom: 0,
      color: '#000000',
    }),
  });

  /**
   * Page setup stays a separate form so selects can refresh the plan at once
   * while margin edits debounce like every other numeric field.
   */
  readonly pageSetupForm = this.fb.nonNullable.group({
    pageSize: 'A4',
    orientation: 'P',
    marginTop: 20,
    marginRight: 15,
    marginBottom: 18,
    marginLeft: 15,
  });

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

  readonly canRun = computed(
    () =>
      this.selectedPaths().length > 0 &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  /** A failed plan (for example invalid margins) blocks the action. */
  readonly planBlocked = computed(() => this.plan()?.success === false);

  readonly primaryLabel = computed(() =>
    this.isSubmitting() || this.isPolling()
      ? this.i18n.t('docMdToPdf.action.converting')
      : this.i18n.t('docMdToPdf.action.convert'),
  );

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      (this.selectedPaths().length > 1
        ? this.i18n.t('docMdToPdf.destination.many')
        : this.i18n.t('docMdToPdf.destination.one')),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  constructor() {
    this.form.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => this.job.schedulePlanRefresh(BAND_PLAN_DEBOUNCE_MS));

    this.pageSetupForm.controls.pageSize.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => void this.job.refreshPlan());
    this.pageSetupForm.controls.orientation.valueChanges
      .pipe(takeUntil(this.destroy$))
      .subscribe(() => void this.job.refreshPlan());
    for (const name of PAGE_SETUP_MARGIN_CONTROLS) {
      this.pageSetupForm.controls[name].valueChanges
        .pipe(takeUntil(this.destroy$))
        .subscribe(() =>
          this.job.schedulePlanRefresh(PAGE_SETUP_DEBOUNCE_MS),
        );
    }
  }

  ngOnDestroy(): void {
    this.job.dispose();
    this.destroy$.next();
    this.destroy$.complete();
  }

  ngOnInit(): void {
    this.restoreOptions();
    this.job.takePendingDrop('/doc-md-to-pdf');
    this.job.resume();
  }

  fileCountLabel(): string {
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('docMdToPdf.count.none');
    }
    return formatCount(count, 'Markdown', 'Markdown');
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

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    await this.job.refreshPlan();
  }

  useMarkdownFolder(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.job.refreshPlan();
  }

  async run(): Promise<void> {
    await this.job.run();
  }

  /** Adopts a job the result card planned and started with the same request. */
  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.job.adoptRunAgain(event);
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

  /**
   * Reruns only the inputs that failed, as a fresh job. A single-file failure
   * keeps the current selection, so the request shape does not change.
   */
  async retry(): Promise<void> {
    if (!this.canRun()) {
      return;
    }

    const failed = (this.jobResult()?.items ?? [])
      .filter((item) => !item.success)
      .map((item) => item.inputPath)
      .filter((path) => path.length > 0);

    if (failed.length > 0 && failed.length < this.selectedPaths().length) {
      this.job.replacePaths([...new Set(failed)], { notify: false });
    }

    this.job.clearRunOutcome();

    const planned = await this.job.refreshPlan();
    if (!planned) {
      return;
    }

    await this.job.run();
  }

  reset(): void {
    this.job.reset();
  }

  /** Quiet summary shown in the page setup disclosure header. */
  pageSetupSummary(): string {
    const setup = this.pageSetupForm.getRawValue();
    const margins = [
      setup.marginTop,
      setup.marginRight,
      setup.marginBottom,
      setup.marginLeft,
    ].map((value) => this.marginText(value));
    const marginSummary = this.i18n.t(
      'docMdToPdf.pageSetup.marginSummary',
      {
        margins: margins.every((margin) => margin === margins[0])
          ? margins[0]
          : margins.join('/'),
      },
    );
    return `${setup.pageSize} · ${this.orientationLabel()} · ${marginSummary}`;
  }

  pageSetupLabel(): string {
    return `${this.pageSetupForm.controls.pageSize.value} ${this.orientationLabel().toLowerCase()}`;
  }

  resetPageSetup(): void {
    this.pageSetupForm.setValue({ ...PAGE_SETUP_DEFAULTS }, { emitEvent: false });
    this.actionMessage.set('');
    void this.job.refreshPlan();
  }

  previewEnabled(role: BandRole): boolean {
    return this.bandGroup(role).enabled.value;
  }

  previewText(role: BandRole): string {
    const text = this.bandGroup(role).text.value;
    const now = new Date();
    const date = `${now.getFullYear()}-${`${now.getMonth() + 1}`.padStart(
      2,
      '0',
    )}-${`${now.getDate()}`.padStart(2, '0')}`;

    return text
      .replaceAll('{page}', '1')
      .replaceAll('{totalPages}', '?')
      .replaceAll('{date}', date)
      .replaceAll('{fileName}', this.previewFileName() || 'document.md');
  }

  previewStyle(role: BandRole): Record<string, string> {
    const group = this.bandGroup(role);
    return {
      color: group.color.value,
      'font-family': this.previewFont(group.font.value),
      'text-align': group.align.value,
      'margin-top': `${group.marginTop.value}px`,
      'margin-bottom': `${group.marginBottom.value}px`,
    };
  }

  private bandGroup(role: BandRole) {
    return role === 'header'
      ? this.form.controls.header.controls
      : this.form.controls.footer.controls;
  }

  private previewFileName(): string {
    return fileName(this.selectedPaths()[0] ?? '');
  }

  private previewFont(font: string): string {
    switch (font) {
      case 'times':
        return 'Times New Roman, serif';
      case 'courier':
        return 'Courier New, monospace';
      default:
        return 'Helvetica, Arial, sans-serif';
    }
  }

  private orientationLabel(): string {
    return this.pageSetupForm.controls.orientation.value === 'L'
      ? this.i18n.t('docMdToPdf.pageSetup.landscape')
      : this.i18n.t('docMdToPdf.pageSetup.portrait');
  }

  private marginText(value: number): string {
    const numeric = Number(value);
    return Number.isFinite(numeric)
      ? String(Math.round(numeric * 10) / 10)
      : '0';
  }

  private currentOptions(): MdToPdfOptions {
    const bands = this.form.getRawValue();
    return {
      header: { ...bands.header },
      footer: { ...bands.footer },
      pageSetup: { ...this.pageSetupForm.getRawValue() },
    };
  }

  private restoreOptions(): void {
    const options = this.settings.restore();
    this.form.setValue(
      { header: options.header, footer: options.footer },
      { emitEvent: false },
    );
    this.pageSetupForm.setValue(options.pageSetup, { emitEvent: false });
  }

  private buildRequest(): JobRequest {
    const header = this.bandGroup('header');
    const footer = this.bandGroup('footer');
    const setup = this.pageSetupForm.getRawValue();
    const paths = this.selectedPaths();

    return {
      toolId: MD_TO_PDF_TOOL_ID,
      mode: paths.length > 1 ? 'batch' : 'single',
      inputPaths: [...paths],
      outputDir: this.outputDir(),
      options: {
        header: {
          enabled: header.enabled.value,
          text: header.text.value,
          align: header.align.value,
          font: header.font.value,
          marginTop: Number(header.marginTop.value),
          marginBottom: Number(header.marginBottom.value),
          color: header.color.value,
        },
        footer: {
          enabled: footer.enabled.value,
          text: footer.text.value,
          align: footer.align.value,
          font: footer.font.value,
          marginTop: Number(footer.marginTop.value),
          marginBottom: Number(footer.marginBottom.value),
          color: footer.color.value,
        },
        // Always send the resolved setup: the backend validates it and applies
        // the documented defaults when a value is missing.
        pageSize: setup.pageSize,
        orientation: setup.orientation,
        margins: {
          top: Number(setup.marginTop) || 0,
          right: Number(setup.marginRight) || 0,
          bottom: Number(setup.marginBottom) || 0,
          left: Number(setup.marginLeft) || 0,
        },
      },
    };
  }

  private describeFileInfo(info: FileInfo): string {
    const parts: string[] = [];
    if (typeof info.sizeBytes === 'number' && info.sizeBytes >= 0) {
      parts.push(formatBytes(info.sizeBytes));
    }
    return parts.join(' · ');
  }

  private isMarkdown(path: string): boolean {
    const extension = fileExtension(path).toLowerCase();
    return extension === 'md' || extension === 'markdown';
  }
}
