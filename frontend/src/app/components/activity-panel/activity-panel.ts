import { NgTemplateOutlet } from '@angular/common';
import {
  Component,
  DestroyRef,
  ElementRef,
  HostListener,
  Injector,
  ViewChild,
  afterNextRender,
  effect,
  inject,
  signal,
} from '@angular/core';
import { Router } from '@angular/router';
import {
  Activity,
  type LucideIconData,
  LucideAngularModule,
  X,
} from 'lucide-angular';

import { RECIPES, type RecipeDefinition } from '../../config/recipes';
import { type ToolCategory, type ToolDefinition } from '../../config/tools';
import { ActivityService, type ActivityTask } from '../../services/activity.service';
import { I18nService } from '../../services/i18n.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import { formatCount } from '../../utils/format';

const TERMINAL_STATUSES: ReadonlySet<string> = new Set([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

type ActivityChipTone = 'success' | 'warning' | 'danger' | 'info';

/**
 * Background task activity drawer.
 *
 * Reads the global ActivityService store and renders running and recently
 * finished tool jobs / recipe runs. The drawer stays presentational: cancel,
 * dismiss and clear go through the service, which already owns the failure
 * toasts, and "Open" closes the drawer before routing to the owning surface.
 */
@Component({
  selector: 'app-activity-panel',
  standalone: true,
  imports: [NgTemplateOutlet, LucideAngularModule],
  templateUrl: './activity-panel.html',
  styleUrl: './activity-panel.css',
})
export class ActivityPanel {
  protected readonly activity = inject(ActivityService);
  readonly i18n = inject(I18nService);
  private readonly registry = inject(ToolRegistryService);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);
  private readonly injector = inject(Injector);

  readonly closeIcon = X;
  readonly fallbackIcon = Activity;

  /** Drives the elapsed-time labels; ticks once per second while alive. */
  readonly now = signal(Date.now());

  private readonly cancellingState = signal<ReadonlySet<string>>(new Set());

  @ViewChild('closeButton') private closeButton?: ElementRef<HTMLButtonElement>;
  @ViewChild('drawer') private drawer?: ElementRef<HTMLElement>;

  constructor() {
    // Elapsed-time labels only: job and recipe status arrive from the shared
    // event streams, so this timer never talks to the backend.
    const ticker = setInterval(() => this.now.set(Date.now()), 1000);
    this.destroyRef.onDestroy(() => clearInterval(ticker));

    // The drawer is created on open, so focus has to wait for rendering.
    effect(() => {
      if (!this.activity.panelOpen()) {
        return;
      }

      afterNextRender(
        () => this.closeButton?.nativeElement.focus(),
        { injector: this.injector },
      );
    });
  }

  close(): void {
    this.activity.closePanel();
  }

  onBackdropClick(event: MouseEvent): void {
    if (event.target === event.currentTarget) {
      this.close();
    }
  }

  onCancel(task: ActivityTask): void {
    if (this.isCancelling(task.id)) {
      return;
    }

    void this.cancel(task);
  }

  openTask(task: ActivityTask): void {
    void this.router.navigateByUrl(task.route);
    this.activity.closePanel();
  }

  dismissTask(id: string): void {
    this.activity.dismissTask(id);
  }

  clearFinished(): void {
    this.activity.clearFinished();
  }

  isCancelling(id: string): boolean {
    return this.cancellingState().has(id);
  }

  taskKey(task: ActivityTask): string {
    return `${task.kind}:${task.id}`;
  }

  toolFor(task: ActivityTask): ToolDefinition | null {
    return task.toolId ? this.registry.findById(task.toolId) : null;
  }

  recipeFor(task: ActivityTask): RecipeDefinition | null {
    return task.recipeId
      ? RECIPES.find((recipe) => recipe.id === task.recipeId) ?? null
      : null;
  }

  iconFor(task: ActivityTask): LucideIconData {
    return this.toolFor(task)?.icon ?? this.recipeFor(task)?.icon ?? this.fallbackIcon;
  }

  iconTone(task: ActivityTask): ToolCategory | null {
    return this.toolFor(task)?.category ?? null;
  }

  kindLabel(task: ActivityTask): string {
    return task.kind === 'recipe'
      ? this.i18n.t('activity.kind.recipe')
      : this.i18n.t('activity.kind.tool');
  }

  stageLabel(task: ActivityTask): string {
    const stage = (task.stage || task.status || '').trim();
    switch (stage) {
      case 'queued':
        return this.i18n.t('activity.stage.queued');
      case 'running':
        return this.i18n.t('activity.stage.running');
      case 'processing':
        return this.i18n.t('activity.stage.processing');
      case 'finalizing':
        return this.i18n.t('activity.stage.finalizing');
      case 'ocr':
        return this.i18n.t('activity.stage.ocr');
      case 'extract':
        return this.i18n.t('activity.stage.extract');
      case 'transcribe':
        return this.i18n.t('activity.stage.transcribe');
      case 'publish':
        return this.i18n.t('activity.stage.publish');
      case 'done':
      case 'success':
        return this.i18n.t('activity.stage.done');
      case 'partial_success':
        return this.i18n.t('activity.stage.partial');
      case 'failed':
        return this.i18n.t('activity.stage.failed');
      case 'cancelled':
        return this.i18n.t('activity.stage.cancelled');
      case 'interrupted':
        return this.i18n.t('activity.stage.interrupted');
      default:
        return this.humanize(stage) || this.i18n.t('activity.stage.running');
    }
  }

  statusLabel(status: string): string {
    switch (status) {
      case 'success':
        return this.i18n.t('activity.status.success');
      case 'partial_success':
        return this.i18n.t('activity.status.partial');
      case 'failed':
        return this.i18n.t('activity.status.failed');
      case 'interrupted':
        return this.i18n.t('activity.status.interrupted');
      case 'cancelled':
        return this.i18n.t('activity.status.cancelled');
      case 'queued':
        return this.i18n.t('activity.status.queued');
      default:
        return this.i18n.t('activity.status.running');
    }
  }

  statusTone(status: string): ActivityChipTone {
    switch (status) {
      case 'success':
      case 'partial_success':
        return 'success';
      case 'failed':
      case 'interrupted':
        return 'danger';
      case 'cancelled':
        return 'warning';
      default:
        return 'info';
    }
  }

  isTerminal(task: ActivityTask): boolean {
    return TERMINAL_STATUSES.has(task.status);
  }

  isIndeterminate(task: ActivityTask): boolean {
    return !this.isTerminal(task) && task.percent === null;
  }

  progressNow(task: ActivityTask): number | null {
    return task.percent;
  }

  progressWidth(task: ActivityTask): number | null {
    if (this.isIndeterminate(task)) {
      return null;
    }

    if (task.percent !== null) {
      return task.percent;
    }

    if (
      this.isTerminal(task) &&
      (task.status === 'success' || task.status === 'partial_success')
    ) {
      return 100;
    }

    return 0;
  }

  progressLabel(task: ActivityTask): string {
    return this.i18n.t('activity.progressAria', { title: task.title });
  }

  metaLine(task: ActivityTask): string {
    const parts: string[] = [];

    if (task.total > 0) {
      parts.push(`${task.current} / ${task.total}`);
    } else if (task.inputCount > 0) {
      parts.push(
        formatCount(
          task.inputCount,
          this.i18n.t('common.fileOne'),
          this.i18n.t('common.fileMany'),
        ),
      );
    }

    parts.push(this.formatElapsed(this.elapsedMs(task)));

    const eta = task.etaSeconds;
    if (!this.isTerminal(task) && eta !== null && eta > 0) {
      parts.push(
        this.i18n.t('activity.meta.eta', {
          seconds: Math.max(1, Math.round(eta)),
        }),
      );
    }

    return parts.join(' · ');
  }

  showMessage(task: ActivityTask): boolean {
    const message = task.message.trim();
    return message.length > 0 && message !== task.title.trim();
  }

  private async cancel(task: ActivityTask): Promise<void> {
    this.setCancelling(task.id, true);
    try {
      // The service reports cancel failures through a danger toast.
      await this.activity.cancelTask(task);
    } catch {
      // Never surface a duplicate error surface from the drawer.
    } finally {
      this.setCancelling(task.id, false);
    }
  }

  private setCancelling(id: string, cancelling: boolean): void {
    this.cancellingState.update((current) => {
      const next = new Set(current);
      if (cancelling) {
        next.add(id);
      } else {
        next.delete(id);
      }
      return next;
    });
  }

  private elapsedMs(task: ActivityTask): number {
    const end = task.endedAt ?? this.now();
    return Math.max(0, end - task.startedAt);
  }

  private formatElapsed(ms: number): string {
    const totalSeconds = Math.floor(ms / 1000);
    if (totalSeconds < 60) {
      return `${totalSeconds}s`;
    }

    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    if (minutes < 60) {
      return `${minutes}m ${seconds.toString().padStart(2, '0')}s`;
    }

    const hours = Math.floor(minutes / 60);
    return `${hours}h ${(minutes % 60).toString().padStart(2, '0')}m`;
  }

  private humanize(value: string): string {
    const text = value
      .replace(/[_.]/g, ' ')
      .replace(/\s+/g, ' ')
      .trim();
    if (!text) {
      return '';
    }

    return text.charAt(0).toUpperCase() + text.slice(1);
  }

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (!this.activity.panelOpen()) {
      return;
    }

    if (event.key === 'Escape') {
      // Keep the shell from acting on the same Escape press.
      event.preventDefault();
      event.stopPropagation();
      this.close();
      return;
    }

    if (event.key === 'Tab') {
      this.trapFocus(event);
    }
  }

  /** Keeps Tab inside the modal drawer, matching the command palette. */
  private trapFocus(event: KeyboardEvent): void {
    const drawer = this.drawer?.nativeElement;
    if (!drawer) {
      return;
    }

    const focusable = Array.from(
      drawer.querySelectorAll<HTMLElement>(
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
