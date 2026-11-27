import {
  Component,
  NgZone,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import {
  Copy,
  FolderSearch,
  LucideAngularModule,
  Plus,
  ScanSearch,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';
import { Subject, takeUntil } from 'rxjs';

import {
  DuplicateScanGroup,
  DuplicateScanProgressEvent,
  DuplicateScanRequest,
  DuplicateScanState,
  JobProgress,
  Wails,
} from '../../services/wails';
import { I18nService } from '../../services/i18n.service';
import { ClipboardService } from '../../services/clipboard.service';
import { fileName, formatBytes, formatRelativeTime } from '../../utils/format';
import { isTerminalJobStatus } from '../../utils/job-status';

type ActionTone = 'success' | 'warning' | 'danger';

interface ActionMessage {
  tone: ActionTone;
  text: string;
}

/** Paths visible per group before the "show all" control. */
const VISIBLE_PATHS = 3;

/** Interface defaults, mirrored from the duplicates service. */
export const DUPLICATES_DEFAULT_MIN_SIZE_KIB = 4;
export const DUPLICATES_DEFAULT_MAX_FILES = 20_000;

/**
 * Read-only duplicate scanner page.
 *
 * Inputs are folders, not files, so this page deliberately has no file drop,
 * job queue or plan card. It starts a scan, renders the events it emits, and
 * never offers a delete, move or cleanup action.
 */
@Component({
  selector: 'app-duplicates',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './duplicates.html',
  styleUrl: './duplicates.css',
})
export class Duplicates implements OnInit, OnDestroy {
  private readonly wailsService = inject(Wails);
  private readonly clipboard = inject(ClipboardService);
  private readonly zone = inject(NgZone);

  readonly i18n = inject(I18nService);

  readonly shieldIcon = ShieldCheck;
  readonly plusIcon = Plus;
  readonly removeIcon = X;
  readonly scanIcon = ScanSearch;
  readonly alertIcon = TriangleAlert;
  readonly copyIcon = Copy;
  readonly revealIcon = FolderSearch;

  /** Exposed to the template; the helper is pure. */
  readonly formatBytes = formatBytes;

  readonly folders = signal<string[]>([]);
  readonly recursive = signal(true);
  readonly minSizeKiB = signal(String(DUPLICATES_DEFAULT_MIN_SIZE_KIB));
  readonly extensionsText = signal('');
  readonly maxFilesText = signal(String(DUPLICATES_DEFAULT_MAX_FILES));
  readonly unlimitedFiles = signal(false);
  readonly formError = signal('');
  readonly formAttempted = signal(false);

  readonly scans = signal<DuplicateScanState[]>([]);
  readonly historyLoading = signal(true);
  readonly historyError = signal('');

  readonly activeScanId = signal('');
  readonly activeScan = signal<DuplicateScanState | null>(null);
  readonly liveProgress = signal<JobProgress | null>(null);
  readonly starting = signal(false);
  readonly cancelling = signal(false);
  readonly actionMessage = signal<ActionMessage | null>(null);

  readonly expandedGroups = signal<ReadonlySet<string>>(new Set());

  /** A state event can arrive before the start RPC resolves; keep it aside. */
  private pendingState: DuplicateScanState | null = null;

  private readonly destroy$ = new Subject<void>();

  readonly scanning = computed(() => {
    const scan = this.activeScan();
    return (
      !!scan &&
      !!scan.scanId &&
      !isTerminalJobStatus(scan.status)
    );
  });

  readonly scanFailed = computed(
    () => this.activeScan()?.status === 'failed',
  );

  readonly scanCancelled = computed(
    () => this.activeScan()?.status === 'cancelled',
  );

  readonly showResults = computed(() => {
    const status = this.activeScan()?.status;
    return status === 'success' || status === 'partial_success';
  });

  /** Groups are re-sorted defensively: largest waste first, always. */
  readonly groups = computed<DuplicateScanGroup[]>(() =>
    [...(this.activeScan()?.groups ?? [])].sort(
      (left, right) =>
        right.wastedBytes - left.wastedBytes ||
        right.sizeBytes - left.sizeBytes ||
        left.hash.localeCompare(right.hash),
    ),
  );

  readonly warnings = computed(() => this.activeScan()?.warnings ?? []);

  readonly totals = computed(() => this.activeScan()?.totals ?? null);

  readonly currentProgress = computed<JobProgress | null>(
    () => this.liveProgress() ?? this.activeScan()?.progress ?? null,
  );

  readonly progressPercent = computed<number | null>(() => {
    const scan = this.activeScan();
    if (!scan) {
      return null;
    }
    if (isTerminalJobStatus(scan.status)) {
      return scan.status === 'success' || scan.status === 'partial_success'
        ? 100
        : null;
    }

    const progress = this.currentProgress();
    if (!progress || progress.stage !== 'hashing' || progress.total <= 0) {
      return null;
    }

    return Math.min(
      100,
      Math.max(0, Math.round((progress.current / progress.total) * 100)),
    );
  });

  readonly stageLabel = computed(() => {
    const progress = this.currentProgress();
    const stage = (progress?.stage ?? '').trim();

    switch (stage) {
      case 'walking':
        return this.i18n.t('duplicates.scan.stage.walking');
      case 'hashing':
        return this.i18n.t('duplicates.scan.stage.hashing');
      case 'done':
        return this.i18n.t('duplicates.scan.stage.done');
      default:
        return progress?.message?.trim() || this.i18n.t('duplicates.scan.title');
    }
  });

  readonly progressDetail = computed(() => {
    const progress = this.currentProgress();
    if (!progress) {
      return '';
    }

    if (progress.stage === 'hashing' && progress.total > 0) {
      return this.i18n.t('duplicates.scan.hashed', {
        current: progress.current,
        total: progress.total,
      });
    }
    if (progress.stage === 'walking') {
      return progress.current > 0
        ? this.i18n.t('duplicates.scan.walked', { count: progress.current })
        : '';
    }

    return progress.message?.trim() ?? '';
  });

  constructor() {
    this.wailsService.duplicateProgress$
      ?.pipe(takeUntil(this.destroy$))
      .subscribe((event) => {
        this.zone.run(() => this.applyProgressEvent(event));
      });

    this.wailsService.duplicateState$
      ?.pipe(takeUntil(this.destroy$))
      .subscribe((state) => {
        this.zone.run(() => this.applyStateEvent(state));
      });
  }

  async ngOnInit(): Promise<void> {
    await this.loadHistory(true);
  }

  ngOnDestroy(): void {
    this.destroy$.next();
    this.destroy$.complete();
  }

  // ---- Form ---------------------------------------------------------------

  async addFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }

    this.folders.update((folders) =>
      folders.includes(directory) ? folders : [...folders, directory],
    );
    this.formError.set('');
  }

  removeFolder(path: string): void {
    this.folders.update((folders) => folders.filter((folder) => folder !== path));
    this.formError.set('');
  }

  onRecursiveToggle(event: Event): void {
    this.recursive.set(
      (event.target as HTMLInputElement | null)?.checked ?? false,
    );
  }

  onMinSizeInput(event: Event): void {
    this.minSizeKiB.set((event.target as HTMLInputElement | null)?.value ?? '');
  }

  onExtensionsInput(event: Event): void {
    this.extensionsText.set(
      (event.target as HTMLInputElement | null)?.value ?? '',
    );
  }

  onMaxFilesInput(event: Event): void {
    this.maxFilesText.set(
      (event.target as HTMLInputElement | null)?.value ?? '',
    );
  }

  onUnlimitedToggle(event: Event): void {
    this.unlimitedFiles.set(
      (event.target as HTMLInputElement | null)?.checked ?? false,
    );
  }

  async startScan(): Promise<void> {
    if (this.starting() || this.scanning()) {
      return;
    }

    this.formAttempted.set(true);
    this.formError.set('');
    this.actionMessage.set(null);

    const request = this.buildRequest();
    if (!request) {
      return;
    }

    this.starting.set(true);
    const response = await this.wailsService.startDuplicateScan(request);
    this.starting.set(false);

    if (!response.success) {
      this.formError.set(
        this.friendlyMessage(
          response.message,
          this.i18n.t('duplicates.error.start'),
        ),
      );
      return;
    }

    const scanId = (response.scanId ?? '').trim();
    if (!scanId) {
      this.formError.set(this.i18n.t('duplicates.error.start'));
      return;
    }

    this.activeScanId.set(scanId);
    const pending = this.pendingState;
    if (pending && pending.scanId === scanId) {
      this.applyState(pending, false);
    }

    const snapshot = await this.wailsService.getDuplicateScan(scanId);
    if (snapshot.success && snapshot.scan) {
      this.applyState(snapshot.scan, false);
    }

    void this.loadHistory(false);
  }

  async cancelScan(): Promise<void> {
    const scanId = this.activeScanId();
    if (!scanId || this.cancelling() || !this.scanning()) {
      return;
    }

    this.cancelling.set(true);
    const response = await this.wailsService.cancelDuplicateScan(scanId);
    this.cancelling.set(false);

    if (!response.success) {
      this.actionMessage.set({
        tone: 'danger',
        text: this.friendlyMessage(
          response.message,
          this.i18n.t('duplicates.error.cancel'),
        ),
      });
      return;
    }

    this.actionMessage.set({
      tone: 'warning',
      text: this.i18n.t('duplicates.scan.requested'),
    });
  }

  // ---- Results ------------------------------------------------------------

  selectScan(scan: DuplicateScanState): void {
    if (!scan.scanId) {
      return;
    }

    this.actionMessage.set(null);
    this.applyState(scan, false);

    if (!isTerminalJobStatus(scan.status)) {
      void this.reloadScan(scan.scanId);
    }
  }

  groupTitle(group: DuplicateScanGroup): string {
    const first = group.paths[0] ?? '';
    return fileName(first) || group.hash.slice(0, 12);
  }

  groupCountLabel(group: DuplicateScanGroup): string {
    return group.count === 1
      ? this.i18n.t('duplicates.group.filesOne', { count: group.count })
      : this.i18n.t('duplicates.group.filesMany', { count: group.count });
  }

  groupSizeLabel(group: DuplicateScanGroup): string {
    return this.i18n.t('duplicates.group.size', {
      size: formatBytes(group.sizeBytes),
    });
  }

  groupWastedLabel(group: DuplicateScanGroup): string {
    return this.i18n.t('duplicates.group.wasted', {
      size: formatBytes(group.wastedBytes),
    });
  }

  isGroupExpanded(hash: string): boolean {
    return this.expandedGroups().has(hash);
  }

  visiblePaths(group: DuplicateScanGroup): string[] {
    return this.isGroupExpanded(group.hash)
      ? group.paths
      : group.paths.slice(0, VISIBLE_PATHS);
  }

  hasMorePaths(group: DuplicateScanGroup): boolean {
    return group.paths.length > VISIBLE_PATHS;
  }

  toggleGroup(hash: string): void {
    this.expandedGroups.update((current) => {
      const next = new Set(current);
      if (next.has(hash)) {
        next.delete(hash);
      } else {
        next.add(hash);
      }
      return next;
    });
  }

  async revealPath(path: string): Promise<void> {
    const response = await this.wailsService.revealInFolder(path);
    if (!response.success) {
      this.actionMessage.set({
        tone: 'danger',
        text: this.friendlyMessage(
          response.message,
          this.i18n.t('duplicates.group.revealFailed'),
        ),
      });
    }
  }

  async copyPath(path: string): Promise<void> {
    const copied = await this.clipboard.writeText(path);
    if (!copied) {
      this.actionMessage.set({
        tone: 'danger',
        text: this.i18n.t('duplicates.group.copyFailed'),
      });
      return;
    }

    this.actionMessage.set({
      tone: 'success',
      text: this.i18n.t('duplicates.group.copied'),
    });
  }

  // ---- History ------------------------------------------------------------

  isActiveScan(scan: DuplicateScanState): boolean {
    return scan.scanId === this.activeScanId();
  }

  historySummary(scan: DuplicateScanState): string {
    if (scan.totals.groups === 0) {
      return this.i18n.t('duplicates.history.noGroups');
    }

    return this.i18n.t('duplicates.history.summary', {
      groups: scan.totals.groups,
      size: formatBytes(scan.totals.wastedBytes),
    });
  }

  scanStartedLabel(scan: DuplicateScanState | null): string {
    const time = scan ? this.relativeTime(scan.startedAt) : '';
    return time ? this.i18n.t('duplicates.history.started', { time }) : '';
  }

  relativeTime(timestamp: number | undefined): string {
    return formatRelativeTime(
      timestamp ?? 0,
      this.i18n.locale(),
      (key, params) => this.i18n.t(key, params),
    );
  }

  scanStatusLabel(status: string | undefined): string {
    switch (status) {
      case 'queued':
        return this.i18n.t('duplicates.status.queued');
      case 'running':
        return this.i18n.t('duplicates.status.running');
      case 'success':
        return this.i18n.t('duplicates.status.success');
      case 'partial_success':
        return this.i18n.t('duplicates.status.partial');
      case 'failed':
        return this.i18n.t('duplicates.status.failed');
      case 'cancelled':
        return this.i18n.t('duplicates.status.cancelled');
      case 'interrupted':
        return this.i18n.t('duplicates.status.interrupted');
      default:
        return status?.trim() || this.i18n.t('duplicates.status.running');
    }
  }

  scanBadgeClass(status: string | undefined): string {
    switch (status) {
      case 'success':
        return 'badge badge-success';
      case 'partial_success':
      case 'interrupted':
        return 'badge badge-warning';
      case 'failed':
        return 'badge badge-danger';
      case 'queued':
      case 'running':
        return 'badge badge-info';
      default:
        return 'badge badge-neutral';
    }
  }

  // ---- Internals ----------------------------------------------------------

  private buildRequest(): DuplicateScanRequest | null {
    if (this.folders().length === 0) {
      this.formError.set(this.i18n.t('duplicates.error.folders'));
      return null;
    }

    const minRaw = this.minSizeKiB().trim();
    let minKiB = DUPLICATES_DEFAULT_MIN_SIZE_KIB;
    if (minRaw.length > 0) {
      if (!/^\d+$/.test(minRaw) || Number(minRaw) <= 0) {
        this.formError.set(this.i18n.t('duplicates.error.minSize'));
        return null;
      }
      minKiB = Number(minRaw);
    }

    let maxFiles = DUPLICATES_DEFAULT_MAX_FILES;
    if (!this.unlimitedFiles()) {
      const maxRaw = this.maxFilesText().trim();
      if (maxRaw.length > 0) {
        if (!/^\d+$/.test(maxRaw) || Number(maxRaw) <= 0) {
          this.formError.set(this.i18n.t('duplicates.error.maxFiles'));
          return null;
        }
        maxFiles = Number(maxRaw);
      }
    } else {
      // The backend reads a negative cap as "no cap".
      maxFiles = -1;
    }

    return {
      directories: [...this.folders()],
      recursive: this.recursive(),
      minSizeBytes: minKiB * 1024,
      extensions: this.parseExtensions(this.extensionsText()),
      maxFiles,
    };
  }

  private parseExtensions(value: string): string[] {
    const parsed = value
      .split(/[\s,]+/)
      .map((part) => part.trim().toLowerCase().replace(/^\.+/, ''))
      .filter((part) => part.length > 0);

    return Array.from(new Set(parsed));
  }

  private async loadHistory(selectLatest: boolean): Promise<void> {
    this.historyLoading.set(true);
    const response = await this.wailsService.listDuplicateScans();
    this.historyLoading.set(false);

    if (!response.success) {
      this.historyError.set(
        this.friendlyMessage(
          response.message,
          this.i18n.t('duplicates.error.history'),
        ),
      );
      return;
    }

    this.historyError.set('');
    this.scans.set(response.scans ?? []);

    if (!selectLatest) {
      return;
    }

    const latest = this.latestScan();
    if (!latest) {
      return;
    }

    this.applyState(latest, false);
    if (!isTerminalJobStatus(latest.status)) {
      void this.reloadScan(latest.scanId);
    }
  }

  private latestScan(): DuplicateScanState | null {
    return [...this.scans()].sort(
      (left, right) =>
        right.startedAt - left.startedAt ||
        right.scanId.localeCompare(left.scanId),
    )[0] ?? null;
  }

  private async reloadScan(scanId: string): Promise<void> {
    const response = await this.wailsService.getDuplicateScan(scanId);
    if (response.success && response.scan) {
      this.applyState(response.scan, false);
    }
  }

  private applyProgressEvent(event: DuplicateScanProgressEvent): void {
    if (!event || typeof event.scanId !== 'string') {
      return;
    }

    const activeId = this.activeScanId();
    if (!activeId || event.scanId !== activeId) {
      return;
    }

    this.liveProgress.set(event.progress);
    this.activeScan.update((scan) =>
      scan ? { ...scan, status: event.status, progress: event.progress } : scan,
    );
  }

  private applyStateEvent(state: DuplicateScanState): void {
    if (!state || typeof state.scanId !== 'string' || !state.scanId) {
      return;
    }

    const activeId = this.activeScanId();
    if (!activeId) {
      if (!isTerminalJobStatus(state.status)) {
        this.pendingState = state;
      }
      return;
    }
    if (state.scanId !== activeId) {
      return;
    }

    this.applyState(state, true);
  }

  private applyState(state: DuplicateScanState, notifyHistory: boolean): void {
    if (!state?.scanId) {
      return;
    }

    this.activeScanId.set(state.scanId);
    this.activeScan.set(state);
    this.liveProgress.set(state.progress);
    this.pendingState = null;
    this.cancelling.set(false);

    if (notifyHistory && isTerminalJobStatus(state.status)) {
      void this.loadHistory(false);
    }
  }

  private friendlyMessage(message: string, fallback: string): string {
    const trimmed = message?.trim() ?? '';
    const technical = /wails\/runtime|Cannot POST|<!DOCTYPE/i.test(trimmed);

    return trimmed.length > 0 &&
      trimmed.length <= 200 &&
      !technical &&
      !/[<>]/.test(trimmed)
      ? trimmed
      : fallback;
  }
}
