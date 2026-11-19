import { Injectable, Signal, signal } from '@angular/core';

export interface ToastItem {
  id: number;
  tone: 'success' | 'warning' | 'danger' | 'info';
  title: string;
  body: string;
  actionLabel?: string;
  action?: () => void;
}

const DEFAULT_TTL_MS = 6000;
const MAX_VISIBLE = 3;

/**
 * In-app notification queue.
 *
 * Toasts are intentionally DOM-free: the shell renders them, this service only
 * owns the recent queue, the auto-dismiss timers and the id sequence. Timers
 * are cleared on dismiss and on service teardown so tests stay deterministic.
 */
@Injectable({
  providedIn: 'root',
})
export class ToastService {
  private readonly toastState = signal<readonly ToastItem[]>([]);
  private readonly timers = new Map<number, ReturnType<typeof setTimeout>>();
  private nextId = 1;

  readonly toasts: Signal<readonly ToastItem[]> = this.toastState.asReadonly();

  /**
   * Adds a toast to the front of the queue. The oldest visible toast is dropped
   * when the queue exceeds three entries. A non-positive `ttlMs` keeps the
   * toast until it is dismissed.
   */
  push(toast: Omit<ToastItem, 'id'>, ttlMs = DEFAULT_TTL_MS): number {
    const id = this.nextId++;
    const next = [{ ...toast, id }, ...this.toastState()];
    const visible = next.slice(0, MAX_VISIBLE);

    for (const dropped of next.slice(MAX_VISIBLE)) {
      this.clearTimer(dropped.id);
    }

    this.toastState.set(visible);

    if (ttlMs > 0) {
      this.timers.set(
        id,
        setTimeout(() => this.dismiss(id), ttlMs),
      );
    }

    return id;
  }

  dismiss(id: number): void {
    this.clearTimer(id);
    this.toastState.update((toasts) => toasts.filter((toast) => toast.id !== id));
  }

  clear(): void {
    for (const timer of this.timers.values()) {
      clearTimeout(timer);
    }
    this.timers.clear();
    this.toastState.set([]);
  }

  private clearTimer(id: number): void {
    const timer = this.timers.get(id);
    if (timer !== undefined) {
      clearTimeout(timer);
      this.timers.delete(id);
    }
  }
}
