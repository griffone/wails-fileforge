import {
  Component,
  OnDestroy,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import {
  LucideAngularModule,
  Plus,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { JobCard } from '../job-card/job-card';
import { OpenWindowAction } from '../open-window-action/open-window-action';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard, RunAgainStartedEvent } from '../tool-flow/result-card';
import { ActivityService } from '../../services/activity.service';
import { DropIntakeService } from '../../services/drop-intake.service';
import { I18nService } from '../../services/i18n.service';
import { LastRequestService } from '../../services/last-request.service';
import { PreferencesService } from '../../services/preferences.service';
import {
  DownloadAudioOption,
  DownloadLinkInfoResponse,
  DownloadVideoOption,
  JobRequest,
  Wails,
} from '../../services/wails';
import { formatBytes, formatCount, formatDuration } from '../../utils/format';
import { ToolRunController } from '../../utils/tool-run-controller';

const WEB_DOWNLOAD_TOOL_ID = 'tool.web.download';
const PLAN_DEBOUNCE_MS = 200;

/** The page probes at most two links at once so a long queue stays calm. */
const MAX_CONCURRENT_PROBES = 2;

/** MP3 targets offered for every audio download; 0 means best (VBR). */
const AUDIO_BITRATES = [320, 256, 192, 128] as const;

type DownloadKind = 'video' | 'audio';
type RowStatus = 'pending' | 'ready' | 'failed';

interface Notice {
  tone: 'info' | 'warning' | 'danger';
  text: string;
}

/**
 * One queued link. `url` is both the identity and the queue item the backend
 * receives; the probed formats belong to that exact URL, so a row is never
 * reused for another link.
 */
interface DownloadRow {
  url: string;
  title: string;
  id: string;
  durationSeconds: number;
  status: RowStatus;
  /** The probe failure message, or the warning of an empty playlist. */
  message: string;
  /** True when `message` is guidance, not a failure. */
  warning: boolean;
  video: DownloadVideoOption[];
  audio: DownloadAudioOption[];
  fromPlaylist: boolean;
}

function newPendingRow(
  url: string,
  title = '',
  durationSeconds = 0,
  fromPlaylist = false,
): DownloadRow {
  return {
    url,
    title,
    id: '',
    durationSeconds,
    status: 'pending',
    message: '',
    warning: false,
    video: [],
    audio: [],
    fromPlaylist,
  };
}

/**
 * URL downloader.
 *
 * Links are pasted, not dropped: the queue is text, so the page owns its rows
 * instead of the shared file drop. Each row is probed for the formats the link
 * really offers, and the quality control is built from that answer — never from
 * fixed presets. The request stays a policy (`maxHeight` / `audioBitrateKbps`),
 * so yt-dlp decides the exact stream.
 *
 * The shared ToolRunController still owns plan, run, progress, verification and
 * undo; the page keeps `selectedPaths` in sync with the row URLs so the backend
 * sees the same queue the user sees.
 */
@Component({
  selector: 'app-web-download',
  standalone: true,
  imports: [
    LucideAngularModule,
    OpenWindowAction,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './web-download.html',
  styleUrl: './web-download.css',
})
export class WebDownload implements OnInit, OnDestroy {
  private readonly preferences = inject(PreferencesService);
  private readonly dropIntake = inject(DropIntakeService);
  readonly i18n = inject(I18nService);
  private readonly wailsService = inject(Wails);

  private readonly job: ToolRunController = new ToolRunController({
    toolId: WEB_DOWNLOAD_TOOL_ID,
    wails: this.wailsService,
    i18n: this.i18n,
    activity: inject(ActivityService),
    dropIntake: this.dropIntake,
    lastRequests: inject(LastRequestService),
    initialOutputDir: this.preferences.defaultOutputDir(),
    // URLs are not files: inspection always fails for them and is swallowed.
    loadThumbnails: false,
    buildRequest: () => this.buildRequest(),
    acceptsPath: () => true,
    canPlan: () => this.canPlan(),
    canRun: () => this.canRun(),
    alwaysRefreshPlanBeforeRun: false,
    describeFileInfo: () => '',
    afterPathsChanged: () => this.schedulePlan(),
    onRunAgainAdopted: (request) => this.adoptRequest(request),
    messages: {
      start: 'webDownload.error.start',
      planFailed: 'webDownload.error.plan',
      waiting: 'webDownload.status.waiting',
      inspectFailed: 'webDownload.error.inspect',
      statusMissing: 'webDownload.error.status',
      cancelFailed: 'webDownload.error.cancel',
      openFileFailed: 'webDownload.error.openFile',
      revealFailed: 'webDownload.error.openFolder',
    },
  });

  readonly addIcon = Plus;
  readonly shieldIcon = ShieldCheck;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  readonly selectedPaths = this.job.selectedPaths;
  readonly outputDir = this.job.outputDir;
  readonly plan = this.job.plan;
  readonly planLoading = this.job.planLoading;
  readonly jobResult = this.job.jobResult;
  readonly outputInfos = this.job.outputInfos;
  readonly isSubmitting = this.job.isSubmitting;
  readonly isPolling = this.job.isPolling;
  readonly actionMessage = this.job.actionMessage;
  readonly terminalJob = this.job.terminalJob;
  readonly showProgress = this.job.showProgress;
  readonly jobErrorCopy = this.job.jobErrorCopy;
  readonly verification = this.job.verification;
  readonly verificationLoading = this.job.verificationLoading;
  readonly verificationUnavailable = this.job.verificationUnavailable;

  readonly audioBitrates = AUDIO_BITRATES;

  readonly draft = signal('');
  readonly rows = signal<DownloadRow[]>([]);
  readonly kind = signal<DownloadKind>('video');
  /**
   * Per-row requested quality. Video values are heights, audio values are
   * kbps; 0 always means "best available". The map is reset when the kind
   * changes because a height and a bitrate are not comparable.
   */
  readonly quality = signal<Record<string, number>>({});

  /** Intake feedback (ignored links, playlist expansion). */
  private readonly intakeNotice = signal<Notice | null>(null);

  private readonly pendingCount = computed(
    () => this.rows().filter((row) => row.status === 'pending').length,
  );
  private readonly failedRows = computed(() =>
    this.rows().filter((row) => row.status === 'failed'),
  );
  private readonly failedCount = computed(() => this.failedRows().length);
  readonly linkCount = computed(() => this.rows().length);

  readonly linkCountLabel = computed(() =>
    formatCount(
      this.linkCount(),
      this.i18n.t('webDownload.intake.countOne'),
      this.i18n.t('webDownload.intake.countMany', { count: this.linkCount() }),
    ),
  );

  /** Intake notice, with failed rows taking precedence and naming themselves. */
  readonly statusNotice = computed<Notice | null>(() => {
    const failed = this.failedRows();
    if (failed.length > 0) {
      const names = failed.map((row) => row.title || row.url).join(', ');
      return {
        tone: 'danger',
        text:
          failed.length === 1
            ? this.i18n.t('webDownload.notice.failedOne', { name: names })
            : this.i18n.t('webDownload.notice.failedMany', {
                count: failed.length,
                names,
              }),
      };
    }
    return this.intakeNotice();
  });

  /** Explicit caps selected across the queue; more than one cannot be sent. */
  private readonly explicitQualities = computed(() => {
    const values = new Set<number>();
    for (const row of this.rows()) {
      if (row.status !== 'ready') {
        continue;
      }
      const value = this.qualityFor(row.url);
      if (value > 0) {
        values.add(value);
      }
    }
    return [...values];
  });

  /** The single value the backend receives; 0 means best. */
  private readonly policyValue = computed(() => {
    const explicit = this.explicitQualities();
    return explicit.length === 1 ? explicit[0] : 0;
  });

  /**
   * True when the rows cannot agree on one policy. A row at 0 is compatible
   * when it does not offer the selected height: its best is already at or
   * below the cap, so it means the same thing.
   */
  readonly qualityConflict = computed(() => {
    const explicit = this.explicitQualities();
    if (explicit.length > 1) {
      return true;
    }

    const policy = this.policyValue();
    for (const row of this.rows()) {
      if (row.status !== 'ready') {
        continue;
      }
      const value = this.qualityFor(row.url);
      if (value === policy) {
        continue;
      }
      if (value === 0 && !this.supportsQuality(row, policy)) {
        continue;
      }
      return true;
    }

    return false;
  });

  readonly kindSummary = computed(() =>
    this.kind() === 'video'
      ? this.i18n.t('webDownload.kind.videoSummary')
      : this.i18n.t('webDownload.kind.audioSummary'),
  );

  readonly qualitySummary = computed(() => {
    const value = this.policyValue();
    if (value <= 0) {
      return this.i18n.t('webDownload.quality.summaryBest');
    }
    return this.kind() === 'video'
      ? this.i18n.t('webDownload.quality.summaryVideo', { height: value })
      : this.i18n.t('webDownload.quality.summaryAudio', { bitrate: value });
  });

  readonly destinationLabel = computed(
    () =>
      this.outputDir().trim() ||
      this.i18n.t('webDownload.output.noDestination'),
  );

  readonly canPlan = computed(
    () =>
      this.rows().length > 0 &&
      this.pendingCount() === 0 &&
      this.failedCount() === 0 &&
      !this.qualityConflict() &&
      this.outputDir().trim().length > 0,
  );

  readonly canRun = computed(
    () =>
      this.canPlan() &&
      this.plan()?.success === true &&
      !this.isSubmitting() &&
      !this.isPolling(),
  );

  readonly primaryLabel = computed(() => {
    if (this.isSubmitting() || this.isPolling()) {
      return this.i18n.t('webDownload.action.downloading');
    }
    const count = this.linkCount();
    if (count === 0) {
      return this.i18n.t('webDownload.action.download');
    }
    return count === 1
      ? this.i18n.t('webDownload.action.downloadOne')
      : this.i18n.t('webDownload.action.downloadMany', { count });
  });

  readonly actionHelper = computed(() => {
    if (this.linkCount() === 0) {
      return this.i18n.t('webDownload.helper.addLinks');
    }
    if (this.pendingCount() > 0) {
      return this.i18n.t('webDownload.helper.probing');
    }
    if (this.failedCount() > 0) {
      return this.i18n.t('webDownload.helper.failed');
    }
    if (this.qualityConflict()) {
      return this.i18n.t('webDownload.helper.qualityConflict');
    }
    if (this.outputDir().trim().length === 0) {
      return this.i18n.t('webDownload.helper.destination');
    }
    if (this.planLoading()) {
      return this.i18n.t('webDownload.helper.planning');
    }
    return '';
  });

  /** One queue of URLs waiting for a probe slot. */
  private readonly probeQueue: string[] = [];
  private activeProbes = 0;

  ngOnInit(): void {
    this.job.resume();
  }

  ngOnDestroy(): void {
    this.probeQueue.length = 0;
    this.job.dispose();
  }

  onDraftInput(event: Event): void {
    this.draft.set((event.target as HTMLTextAreaElement | null)?.value ?? '');
  }

  /** Adds every non-empty line that is not already queued. */
  addLinks(): void {
    const text = this.draft().trim();
    if (!text) {
      return;
    }

    const existing = new Set(this.rows().map((row) => row.url));
    const added: string[] = [];
    let empty = 0;
    let duplicate = 0;

    for (const line of text.split(/\r?\n/)) {
      const url = line.trim();
      if (!url) {
        empty += 1;
        continue;
      }
      if (existing.has(url)) {
        duplicate += 1;
        continue;
      }
      existing.add(url);
      added.push(url);
    }

    this.intakeNotice.set(this.ignoredLinksNotice(empty, duplicate));
    this.draft.set('');

    if (added.length === 0) {
      return;
    }

    this.rows.update((rows) => [
      ...rows,
      ...added.map((url) => newPendingRow(url)),
    ]);
    this.syncQueue();
    for (const url of added) {
      this.probeLink(url);
    }
  }

  /** Clears the queue and the paste box. */
  clear(): void {
    this.draft.set('');
    this.rows.set([]);
    this.quality.set({});
    this.intakeNotice.set(null);
    this.job.clear();
  }

  removeRow(url: string): void {
    if (!this.rows().some((row) => row.url === url)) {
      return;
    }

    this.rows.update((rows) => rows.filter((row) => row.url !== url));
    this.dropQuality(url);
    this.syncQueue();
  }

  retry(url: string): void {
    const row = this.rows().find((candidate) => candidate.url === url);
    if (!row) {
      return;
    }

    this.updateRow(url, {
      status: 'pending',
      message: '',
      warning: false,
      video: [],
      audio: [],
    });
    this.schedulePlan();
    this.probeLink(url);
  }

  setKind(kind: DownloadKind): void {
    if (kind === this.kind()) {
      return;
    }

    this.kind.set(kind);
    // A height and a bitrate are not comparable; every row restarts at best.
    this.quality.set({});
    this.schedulePlan();
  }

  qualityFor(url: string): number {
    return this.quality()[url] ?? 0;
  }

  onQualityChange(event: Event, url: string): void {
    const select = event.target as HTMLSelectElement | null;
    const value = Number(select?.value ?? '0');
    const next = Number.isFinite(value) && value > 0 ? Math.round(value) : 0;
    this.quality.update((current) => ({ ...current, [url]: next }));
    this.schedulePlan();
  }

  /**
   * Copies one row's quality to every row that can offer it. A video row
   * without that height keeps its own best: the cap still applies to it, so
   * setting it to 0 is the truthful value.
   */
  applyQualityToAll(url: string): void {
    const value = this.qualityFor(url);
    const next: Record<string, number> = {};
    for (const row of this.rows()) {
      if (row.status !== 'ready') {
        continue;
      }
      next[row.url] = this.supportsQuality(row, value) ? value : 0;
    }
    this.quality.set(next);
    this.schedulePlan();
  }

  /** `1080p · MP4 · 60 fps · 320 MB`, omitting what the probe does not know. */
  videoOptionLabel(option: DownloadVideoOption): string {
    const parts = [`${option.height}p`];
    if (option.ext) {
      parts.push(option.ext.toUpperCase());
    }
    if (option.fps > 0) {
      parts.push(
        this.i18n.t('webDownload.quality.fps', {
          fps: Math.round(option.fps),
        }),
      );
    }
    if (option.sizeBytes > 0) {
      parts.push(formatBytes(option.sizeBytes));
    }
    if (option.hdr) {
      parts.push('HDR');
    }
    return parts.join(' · ');
  }

  audioOptionLabel(bitrate: number): string {
    return this.i18n.t('webDownload.quality.audioOption', { bitrate });
  }

  rowName(row: DownloadRow): string {
    return row.title || row.url;
  }

  rowDuration(row: DownloadRow): string {
    return row.durationSeconds > 0 ? formatDuration(row.durationSeconds) : '';
  }

  async chooseOutputFolder(): Promise<void> {
    const directory = (await this.wailsService.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }
    this.outputDir.set(directory);
    this.actionMessage.set('');
    this.schedulePlan();
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

  onRunAgainStarted(event: RunAgainStartedEvent): void {
    this.job.adoptRunAgain(event);
  }

  reset(): void {
    this.job.reset();
    this.draft.set('');
    this.rows.set([]);
    this.quality.set({});
    this.intakeNotice.set(null);
    this.probeQueue.length = 0;
    this.activeProbes = 0;
  }

  private buildRequest(): JobRequest {
    const rows = this.rows();
    const kind = this.kind();
    const policy = this.policyValue();

    return {
      toolId: WEB_DOWNLOAD_TOOL_ID,
      mode: rows.length > 1 ? 'batch' : 'single',
      inputPaths: rows.map((row) => row.url),
      outputDir: this.outputDir().trim(),
      options: {
        kind,
        maxHeight: kind === 'video' ? policy : 0,
        audioBitrateKbps: kind === 'audio' ? policy : 0,
        items: rows.map((row) => ({
          url: row.url,
          title: row.title,
          id: row.id,
          ext: kind === 'video' ? 'mp4' : 'mp3',
        })),
      },
    };
  }

  private adoptRequest(request: JobRequest): void {
    const kind: DownloadKind =
      request.options['kind'] === 'audio' ? 'audio' : 'video';
    const rawItems = request.options['items'];
    const items = Array.isArray(rawItems)
      ? (rawItems as Array<Record<string, unknown>>)
      : [];

    this.kind.set(kind);
    this.quality.set({});
    this.rows.set(
      items.map((item) => ({
        ...newPendingRow(String(item['url'] ?? '')),
        title: String(item['title'] ?? ''),
        id: String(item['id'] ?? ''),
        status: 'ready' as RowStatus,
      })),
    );
    this.intakeNotice.set(null);
  }

  /** Mirrors the row order into the controller's queue, without re-planning. */
  private syncQueue(): void {
    this.job.replacePaths(
      this.rows().map((row) => row.url),
      { notify: false },
    );
    this.schedulePlan();
  }

  private schedulePlan(): void {
    if (!this.canPlan()) {
      this.job.clearPlan();
      return;
    }
    this.job.schedulePlanRefresh(PLAN_DEBOUNCE_MS);
  }

  private probeLink(url: string): void {
    this.probeQueue.push(url);
    this.pumpProbes();
  }

  /** Two probes at a time: enough to feel instant, calm on a long queue. */
  private pumpProbes(): void {
    while (
      this.activeProbes < MAX_CONCURRENT_PROBES &&
      this.probeQueue.length > 0
    ) {
      const url = this.probeQueue.shift();
      if (!url) {
        return;
      }

      this.activeProbes += 1;
      void this.wailsService
        .probeDownloadLink(url)
        .then((response) => this.applyProbe(url, response))
        .catch(() => this.applyProbeFailure(url))
        .finally(() => {
          this.activeProbes -= 1;
          this.pumpProbes();
        });
    }
  }

  private applyProbeFailure(url: string): void {
    if (!this.rows().some((row) => row.url === url)) {
      return;
    }
    this.updateRow(url, {
      status: 'failed',
      message: this.i18n.t('webDownload.error.probe'),
      warning: false,
    });
    this.schedulePlan();
  }

  private applyProbe(url: string, response: DownloadLinkInfoResponse): void {
    const row = this.rows().find((candidate) => candidate.url === url);
    if (!row) {
      return;
    }

    if (!response.success) {
      this.updateRow(url, {
        status: 'failed',
        message: response.message || this.i18n.t('webDownload.error.probe'),
        warning: false,
      });
      this.schedulePlan();
      return;
    }

    if (response.isPlaylist) {
      this.expandPlaylist(row, response.entries);
      return;
    }

    this.updateRow(url, {
      status: 'ready',
      title: response.title,
      id: response.id,
      durationSeconds: response.durationSeconds,
      message: '',
      warning: false,
      video: response.video,
      audio: response.audio,
    });
    // The URL set did not change, so the queue mirror is already correct.
    this.schedulePlan();
  }

  /**
   * A playlist link becomes its entries, in order and in place. Entries that
   * are already queued are skipped so one run never downloads a link twice.
   */
  private expandPlaylist(
    row: DownloadRow,
    entries: Array<{ url: string; title: string; durationSeconds: number }>,
  ): void {
    if (entries.length === 0) {
      this.updateRow(row.url, {
        status: 'ready',
        message: this.i18n.t('webDownload.row.warningPlaylistEmpty'),
        warning: true,
        video: [],
        audio: [],
      });
      this.syncQueue();
      return;
    }

    const known = new Set(this.rows().map((candidate) => candidate.url));
    const expansions: DownloadRow[] = [];
    for (const entry of entries) {
      const entryURL = entry.url.trim();
      if (!entryURL || entryURL === row.url || known.has(entryURL)) {
        continue;
      }
      known.add(entryURL);
      expansions.push(
        newPendingRow(entryURL, entry.title, entry.durationSeconds, true),
      );
    }

    if (expansions.length === 0) {
      // Every entry was already queued; the playlist row itself would
      // duplicate them, so it leaves the queue.
      this.rows.update((rows) =>
        rows.filter((candidate) => candidate.url !== row.url),
      );
      this.dropQuality(row.url);
      this.syncQueue();
      return;
    }

    this.rows.update((rows) => {
      const index = rows.findIndex((candidate) => candidate.url === row.url);
      if (index < 0) {
        return rows;
      }
      const next = [...rows];
      next.splice(index, 1, ...expansions);
      return next;
    });
    this.dropQuality(row.url);
    this.intakeNotice.set({
      tone: 'info',
      text: this.i18n.t('webDownload.notice.playlistExpanded', {
        count: expansions.length,
      }),
    });

    this.syncQueue();
    for (const expansion of expansions) {
      this.probeLink(expansion.url);
    }
  }

  private updateRow(url: string, patch: Partial<DownloadRow>): void {
    this.rows.update((rows) =>
      rows.map((row) => (row.url === url ? { ...row, ...patch } : row)),
    );
  }

  private dropQuality(url: string): void {
    if (!(url in this.quality())) {
      return;
    }
    this.quality.update((current) => {
      const next = { ...current };
      delete next[url];
      return next;
    });
  }

  private supportsQuality(row: DownloadRow, value: number): boolean {
    if (value <= 0) {
      return true;
    }
    if (this.kind() === 'audio') {
      // Every MP3 target is reachable from any source audio track.
      return true;
    }

    return row.video.some((option) => option.height === value);
  }

  private ignoredLinksNotice(empty: number, duplicate: number): Notice | null {
    const parts: string[] = [];
    if (empty === 1) {
      parts.push(this.i18n.t('webDownload.notice.emptyOne'));
    } else if (empty > 1) {
      parts.push(this.i18n.t('webDownload.notice.emptyMany', { count: empty }));
    }
    if (duplicate === 1) {
      parts.push(this.i18n.t('webDownload.notice.duplicateOne'));
    } else if (duplicate > 1) {
      parts.push(
        this.i18n.t('webDownload.notice.duplicateMany', { count: duplicate }),
      );
    }

    return parts.length > 0
      ? { tone: 'warning', text: parts.join(' ') }
      : null;
  }
}
