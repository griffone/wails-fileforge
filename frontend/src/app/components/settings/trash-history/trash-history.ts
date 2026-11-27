import {
  Component,
  ElementRef,
  HostListener,
  OnInit,
  ViewChild,
  computed,
  inject,
  signal,
} from '@angular/core';
import {
  FolderOpen,
  History,
  LucideAngularModule,
  Redo2,
  RefreshCw,
  Trash2,
  TriangleAlert,
  Undo2,
} from 'lucide-angular';

import type { TranslationKey } from '../../../i18n';
import { I18nService } from '../../../services/i18n.service';
import { ToolRegistryService } from '../../../services/tool-registry.service';
import {
  JobHistoryEntry,
  JobOutputRecord,
  Wails,
} from '../../../services/wails';
import { formatBytes, formatCount, formatRelativeTime } from '../../../utils/format';

type HistoryState = 'done' | 'undone' | 'trashed' | 'emptied';

interface HistoryRow {
  entry: JobHistoryEntry;
  toolName: string;
  relativeDate: string;
  outputLabel: string;
  totalSize: string;
  state: HistoryState;
  stateLabel: string;
  stateClass: string;
  canReveal: boolean;
  canUndo: boolean;
  canRestore: boolean;
  revealPath: string;
}

const STATE_KEYS: Readonly<Record<HistoryState, TranslationKey>> = {
  done: 'settings.jobHistory.state.done',
  undone: 'settings.jobHistory.state.undone',
  trashed: 'settings.jobHistory.state.trashed',
  emptied: 'settings.jobHistory.state.emptied',
};

const STATE_CLASSES: Readonly<Record<HistoryState, string>> = {
  done: 'badge badge-success',
  undone: 'badge badge-warning',
  trashed: 'badge badge-info',
  emptied: 'badge badge-neutral',
};

/**
 * Trash and job history panel.
 *
 * Undo lives on the result card, but once that card is dismissed the only way
 * back to an output is this list. The trash size is derived from the recorded
 * history outputs (undone records that still carry a trash path); the backend
 * exposes no separate trash summary endpoint.
 */
@Component({
  selector: 'app-trash-history',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './trash-history.html',
  styleUrl: './trash-history.css',
})
export class TrashHistory implements OnInit {
  private readonly wails = inject(Wails);
  private readonly registry = inject(ToolRegistryService);
  readonly i18n = inject(I18nService);

  readonly historyIcon = History;
  readonly refreshIcon = RefreshCw;
  readonly trashIcon = Trash2;
  readonly folderIcon = FolderOpen;
  readonly undoIcon = Undo2;
  readonly restoreIcon = Redo2;
  readonly alertIcon = TriangleAlert;

  readonly entries = signal<JobHistoryEntry[]>([]);
  readonly loading = signal(false);
  readonly loadError = signal('');
  readonly trashError = signal('');
  readonly trashFiles = signal(0);
  readonly trashBytes = signal(0);
  readonly busyJobId = signal<string | null>(null);
  readonly emptying = signal(false);
  readonly confirmOpen = signal(false);
  readonly message = signal('');

  @ViewChild('dialog') private dialog?: ElementRef<HTMLElement>;
  @ViewChild('cancelButton') private cancelButton?: ElementRef<HTMLButtonElement>;

  readonly trashFilesLabel = computed(() =>
    formatCount(
      this.trashFiles(),
      this.i18n.t('common.fileOne'),
      this.i18n.t('common.fileMany'),
    ),
  );

  readonly trashSummary = computed(() =>
    this.i18n.t('settings.trash.summary', {
      files: this.trashFilesLabel(),
      size: formatBytes(this.trashBytes()),
    }),
  );

  readonly confirmBody = computed(() =>
    this.i18n.t('settings.trash.confirmBody', {
      files: this.trashFilesLabel(),
      size: formatBytes(this.trashBytes()),
    }),
  );

  readonly rows = computed<HistoryRow[]>(() =>
    [...this.entries()]
      .sort((left, right) => (right.createdAt ?? 0) - (left.createdAt ?? 0))
      .map((entry) => this.toRow(entry)),
  );

  ngOnInit(): void {
    void this.load();
  }

  /** Refreshes both sources; either one can fail without hiding the other. */
  async load(): Promise<void> {
    this.loading.set(true);
    this.loadError.set('');
    this.trashError.set('');
    await Promise.all([this.loadHistory(), this.loadTrashInfo()]);
    this.loading.set(false);
  }

  private async loadHistory(): Promise<void> {
    try {
      const response = await this.wails.listJobHistory();
      if (!response.success) {
        this.entries.set([]);
        this.loadError.set(
          this.i18n.t('settings.trashHistory.historyUnavailable'),
        );
        return;
      }

      this.entries.set(response.entries);
      this.loadError.set('');
    } catch {
      this.entries.set([]);
      this.loadError.set(
        this.i18n.t('settings.trashHistory.historyUnavailable'),
      );
    }
  }

  private async loadTrashInfo(): Promise<void> {
    try {
      const response = await this.wails.trashInfo();
      if (!response.success) {
        this.trashFiles.set(0);
        this.trashBytes.set(0);
        this.trashError.set(
          this.i18n.t('settings.trashHistory.trashUnavailable'),
        );
        return;
      }

      this.trashFiles.set(response.files);
      this.trashBytes.set(response.bytes);
      this.trashError.set('');
    } catch {
      this.trashFiles.set(0);
      this.trashBytes.set(0);
      this.trashError.set(
        this.i18n.t('settings.trashHistory.trashUnavailable'),
      );
    }
  }

  refresh(): void {
    void this.load();
  }

  async reveal(row: HistoryRow): Promise<void> {
    if (!row.revealPath) {
      return;
    }

    await this.wails.revealInFolder(row.revealPath);
  }

  async undo(row: HistoryRow): Promise<void> {
    await this.runAction(row, 'undo');
  }

  async restore(row: HistoryRow): Promise<void> {
    await this.runAction(row, 'restore');
  }

  requestEmptyTrash(): void {
    if (this.trashFiles() === 0 || this.trashError() || this.emptying()) {
      return;
    }

    this.confirmOpen.set(true);
    void Promise.resolve().then(() => this.cancelButton?.nativeElement.focus());
  }

  cancelEmptyTrash(): void {
    if (this.emptying()) {
      return;
    }
    this.confirmOpen.set(false);
  }

  async confirmEmptyTrash(): Promise<void> {
    if (this.emptying()) {
      return;
    }

    this.emptying.set(true);
    try {
      const response = await this.wails.emptyTrash();
      this.message.set(
        response.success
          ? this.i18n.t('settings.trash.emptied')
          : this.i18n.t('settings.trash.emptyFailed'),
      );
    } catch {
      this.message.set(this.i18n.t('settings.trash.emptyFailed'));
    } finally {
      this.emptying.set(false);
      this.confirmOpen.set(false);
      await this.load();
    }
  }

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (!this.confirmOpen()) {
      return;
    }

    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      this.cancelEmptyTrash();
      return;
    }

    if (event.key === 'Tab') {
      this.trapFocus(event);
    }
  }

  private async runAction(
    row: HistoryRow,
    mode: 'undo' | 'restore',
  ): Promise<void> {
    if (this.busyJobId() !== null) {
      return;
    }

    this.busyJobId.set(row.entry.jobId);
    this.message.set('');

    try {
      const result =
        mode === 'undo'
          ? await this.wails.undoJob(row.entry.jobId)
          : await this.wails.restoreJob(row.entry.jobId);

      if (!result.success) {
        this.message.set(
          this.i18n.t(
            mode === 'undo'
              ? 'settings.jobHistory.undoFailed'
              : 'settings.jobHistory.restoreFailed',
          ),
        );
        return;
      }

      const skipped = result.skipped.length;
      const doneKey: TranslationKey =
        mode === 'undo'
          ? 'settings.jobHistory.undoDone'
          : 'settings.jobHistory.restoreDone';
      const skippedKey: TranslationKey =
        mode === 'undo'
          ? 'settings.jobHistory.undoSkipped'
          : 'settings.jobHistory.restoreSkipped';

      this.message.set(
        skipped > 0
          ? this.i18n.t(skippedKey, { count: skipped })
          : this.i18n.t(doneKey),
      );
    } catch {
      this.message.set(
        this.i18n.t(
          mode === 'undo'
            ? 'settings.jobHistory.undoFailed'
            : 'settings.jobHistory.restoreFailed',
        ),
      );
    } finally {
      this.busyJobId.set(null);
      await this.load();
    }
  }

  private toRow(entry: JobHistoryEntry): HistoryRow {
    const outputs = entry.outputs ?? [];
    const state = this.stateFor(entry, outputs);
    const revealOutput = outputs.find(
      (output) => !output.undone && output.path.trim() !== '',
    );

    return {
      entry,
      toolName: this.toolName(entry.toolId),
      relativeDate: formatRelativeTime(
        entry.createdAt,
        this.i18n.locale(),
        (key, params) => this.i18n.t(key, params),
      ),
      outputLabel: formatCount(
        outputs.length,
        this.i18n.t('common.fileOne'),
        this.i18n.t('common.fileMany'),
      ),
      totalSize: formatBytes(
        outputs.reduce((total, output) => total + (output.sizeBytes ?? 0), 0),
      ),
      state,
      stateLabel: this.i18n.t(STATE_KEYS[state]),
      stateClass: STATE_CLASSES[state],
      canReveal: !!revealOutput,
      canUndo: outputs.some((output) => !output.undone),
      canRestore: outputs.some(
        (output) => (output.trashedPath ?? '').trim() !== '',
      ),
      revealPath: revealOutput?.path ?? '',
    };
  }

  private stateFor(
    entry: JobHistoryEntry,
    outputs: JobOutputRecord[],
  ): HistoryState {
    if (entry.trashEmptied) {
      return 'emptied';
    }
    if (outputs.some((output) => (output.trashedPath ?? '').trim() !== '')) {
      return 'trashed';
    }
    if (
      entry.undone ||
      (outputs.length > 0 && outputs.every((output) => output.undone))
    ) {
      return 'undone';
    }
    return 'done';
  }

  /** Display name from the registry; recipes included, raw ids never shown. */
  private toolName(toolId: string): string {
    const tool = this.registry.findById(toolId);
    if (tool) {
      return this.registry.toolName(tool);
    }

    const recipe = this.registry.recipes.find(
      (candidate) => candidate.id === toolId,
    );
    if (recipe) {
      return this.registry.recipeName(recipe);
    }

    const synthetic = this.syntheticToolName(toolId);
    if (synthetic) {
      return synthetic;
    }

    return this.i18n.t('settings.jobHistory.unknownTool');
  }

  /**
   * History ids that are not registered tools or recipes, such as the moves an
   * organize automation records. They get an explicit label so the panel never
   * calls a real operation "Unknown tool".
   */
  private syntheticToolName(toolId: string): string | null {
    if (toolId === 'automation.organize') {
      return this.i18n.t('settings.jobHistory.organizeTool');
    }

    return null;
  }

  /** Keeps Tab inside the confirmation dialog, matching the activity drawer. */
  private trapFocus(event: KeyboardEvent): void {
    const dialog = this.dialog?.nativeElement;
    if (!dialog) {
      return;
    }

    const focusable = Array.from(
      dialog.querySelectorAll<HTMLElement>(
        'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    ).filter((element) => element.offsetParent !== null);
    if (focusable.length === 0) {
      return;
    }

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
      return;
    }
    if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }
}
