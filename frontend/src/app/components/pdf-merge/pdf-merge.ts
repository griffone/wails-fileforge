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
import { ResultCard } from '../tool-flow/result-card';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { ActivityService } from '../../services/activity.service';
import { FileInfo, JobRequest, Wails } from '../../services/wails';
import {
  fileDir,
  fileExtension,
  fileName,
  fileStem,
  formatBytes,
  formatCount,
} from '../../utils/format';
import { ToolRunController } from '../../utils/tool-run-controller';

const PDF_MERGE_TOOL_ID = 'tool.pdf.merge';
const PLAN_DEBOUNCE_MS = 250;

@Component({
  selector: 'app-pdf-merge',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-merge.html',
  styleUrl: './pdf-merge.css',
})
export class PdfMerge implements OnDestroy, OnInit {
  private readonly dropIntake = inject(DropIntakeService);
  private readonly preferences = inject(PreferencesService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly job: ToolRunController = new ToolRunController({
    toolId: PDF_MERGE_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    initialOutputDir: this.preferences.defaultOutputDir(),
    buildRequest: () => this.buildRequest(),
    acceptsPath: (path) => fileExtension(path).toLowerCase() === 'pdf',
    canPlan: () => this.selectedPaths().length >= 2,
    canRun: () => this.canRun(),
    extraBlocked: () =>
      this.customNameInvalid() || this.plan()?.success === false,
    alwaysRefreshPlanBeforeRun: false,
    describeFileInfo: (info) => this.describeFileInfo(info),
    afterPathsChanged: () => {
      this.syncPageRanges();
      void this.job.refreshPlan();
    },
    messages: {
      start: 'pdfMerge.error.start',
      planFailed: 'pdfMerge.error.plan',
      waiting: 'pdfMerge.status.waiting',
      inspectFailed: 'pdfMerge.error.inspect',
      statusMissing: 'pdfMerge.error.status',
      cancelFailed: 'pdfMerge.error.cancel',
      openFileFailed: 'pdfMerge.error.openFile',
      revealFailed: 'pdfMerge.error.openFolder',
    },
    notice: {
      rejectedOne: 'pdfMerge.notice.rejectedOne',
      rejectedMany: 'pdfMerge.notice.rejectedMany',
      duplicateOne: 'pdfMerge.notice.duplicateOne',
      duplicateMany: 'pdfMerge.notice.duplicateMany',
      rejectedManyFormat: 'names',
    },
  });

  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

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

  /** Page ranges keyed by input path so reordering keeps them attached. */
  readonly pageRanges = signal<Record<string, string>>({});
  readonly customName = signal('');
  readonly advancedOpen = signal(false);

  readonly fileName = fileName;

  readonly canRun = computed(
    () =>
      this.selectedPaths().length >= 2 &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('pdfMerge.action.merging');
    }
    const count = this.selectedPaths().length;
    return count >= 2
      ? this.i18n.t('pdfMerge.action.mergeMany', { count })
      : this.i18n.t('pdfMerge.action.merge');
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('pdfMerge.destination.firstPdf'),
  );

  readonly isDefaultDestination = computed(
    () => this.outputDir().trim().length === 0,
  );

  readonly customNameInvalid = computed(() =>
    /[\\/]/.test(this.customName().trim()),
  );

  /**
   * A failed plan means the chosen name or folder cannot be used. The plan is
   * the source of truth, so the primary action stays blocked until it passes.
   */
  readonly planBlocked = computed(
    () => this.customNameInvalid() || this.plan()?.success === false,
  );

  readonly automaticName = computed(() => {
    const first = this.selectedPaths()[0];
    return first ? `${fileStem(first)}_merged.pdf` : '';
  });

  readonly fileNameHelp = computed(() => {
    const name = this.automaticName();
    return name
      ? this.i18n.t('pdfMerge.field.fileNameHelpNamed', { name })
      : this.i18n.t('pdfMerge.field.fileNameHelpEmpty');
  });

  readonly actionHelper = computed(() => {
    const count = this.selectedPaths().length;
    if (count === 0) {
      return this.i18n.t('pdfMerge.helper.addTwo');
    }
    if (count === 1) {
      return this.i18n.t('pdfMerge.helper.addOneMore');
    }
    return '';
  });

  ngOnInit(): void {
    this.job.takePendingDrop('/pdf-merge');
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

  moveUp(path: string): void {
    this.job.movePath(path, -1);
  }

  moveDown(path: string): void {
    this.job.movePath(path, 1);
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

  useFirstPdfFolder(): void {
    if (this.outputDir() === '') {
      return;
    }
    this.outputDir.set('');
    void this.job.refreshPlan();
  }

  onAdvancedToggle(event: Event): void {
    const target = event.target as HTMLDetailsElement | null;
    this.advancedOpen.set(!!target?.open);
  }

  onCustomNameInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.customName.set(value);
    this.actionMessage.set('');
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  onPageRangeInput(path: string, event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.pageRanges.update((ranges) => ({ ...ranges, [path]: value }));
    this.actionMessage.set('');
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

  /** A failed merge has no partial queue to retry: rerun the same inputs. */
  async retry(): Promise<void> {
    if (!this.canRun()) {
      return;
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
    this.pageRanges.set({});
    this.customName.set('');
  }

  private buildRequest(): JobRequest {
    const options: Record<string, unknown> = {};
    const name = this.sanitizedCustomName();
    if (name) {
      const dir =
        this.outputDir().trim() || fileDir(this.selectedPaths()[0] ?? '');
      options['outputPath'] = this.joinPath(dir, name);
    }

    const ranges = this.selectedPaths().map(
      (path) => (this.pageRanges()[path] ?? '').trim(),
    );
    if (ranges.some((range) => range.length > 0)) {
      options['inputPageRanges'] = ranges;
    }

    return {
      toolId: PDF_MERGE_TOOL_ID,
      mode: 'single',
      inputPaths: [...this.selectedPaths()],
      outputDir: this.outputDir(),
      options,
    };
  }

  private sanitizedCustomName(): string {
    const raw = this.customName().trim();
    if (!raw || this.customNameInvalid()) {
      return '';
    }
    const stem = raw.replace(/\.pdf$/i, '').trim();
    return stem ? `${stem}.pdf` : '';
  }

  /** Keeps Windows separators when the directory uses them. */
  private joinPath(dir: string, name: string): string {
    const trimmed = (dir ?? '').trim();
    if (!trimmed) {
      return name;
    }
    if (/[\\/]$/.test(trimmed)) {
      return `${trimmed}${name}`;
    }
    const separator =
      trimmed.includes('\\') && !trimmed.includes('/') ? '\\' : '/';
    return `${trimmed}${separator}${name}`;
  }

  /** Drops stale path keys and adds empty entries for new inputs. */
  private syncPageRanges(): void {
    const current = this.pageRanges();
    const next: Record<string, string> = {};
    for (const path of this.selectedPaths()) {
      next[path] = current[path] ?? '';
    }
    this.pageRanges.set(next);
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
