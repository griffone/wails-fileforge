import { Component, inject } from '@angular/core';
import {
  AlertTriangle,
  CheckCircle2,
  Info,
  type LucideIconData,
  LucideAngularModule,
  X,
  XCircle,
} from 'lucide-angular';

import { I18nService } from '../../services/i18n.service';
import { ToastService, type ToastItem } from '../../services/toast.service';

/**
 * Shell-level toast stack.
 *
 * Renders the ToastService queue bottom-right. Actions run their handler and
 * then dismiss the toast so the queue never keeps a stale call to action.
 */
@Component({
  selector: 'app-toast-host',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './toast-host.html',
  styleUrl: './toast-host.css',
})
export class ToastHost {
  protected readonly toastService = inject(ToastService);
  readonly i18n = inject(I18nService);

  readonly closeIcon = X;

  readonly toneIcons: Record<ToastItem['tone'], LucideIconData> = {
    success: CheckCircle2,
    warning: AlertTriangle,
    danger: XCircle,
    info: Info,
  };

  iconFor(tone: ToastItem['tone']): LucideIconData {
    return this.toneIcons[tone] ?? Info;
  }

  runAction(toast: ToastItem): void {
    toast.action?.();
    this.toastService.dismiss(toast.id);
  }

  dismiss(id: number): void {
    this.toastService.dismiss(id);
  }
}
