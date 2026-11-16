import { DOCUMENT } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import {
  FolderOpen,
  History,
  LucideAngularModule,
  Moon,
  Sun,
} from 'lucide-angular';

import { PreferencesService } from '../../services/preferences.service';
import { RecentConversionsService } from '../../services/recent-conversions.service';
import { Theme, ThemeService } from '../../services/theme.service';
import { Wails } from '../../services/wails';

/** Shared with the Home onboarding strip. */
const ONBOARDING_STORAGE_KEY = 'ff-onboarding-dismissed';

@Component({
  selector: 'app-settings',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './settings.html',
  styleUrl: './settings.css',
})
export class Settings {
  private readonly preferences = inject(PreferencesService);
  private readonly themeService = inject(ThemeService);
  private readonly recentConversions = inject(RecentConversionsService);
  private readonly wails = inject(Wails);
  private readonly document = inject(DOCUMENT);

  readonly folderIcon = FolderOpen;
  readonly sunIcon = Sun;
  readonly moonIcon = Moon;
  readonly historyIcon = History;

  readonly defaultOutputDir = this.preferences.defaultOutputDir;
  readonly isDark = this.themeService.isDark;
  readonly isLight = computed(() => !this.isDark());

  readonly recentCount = computed(() => this.recentConversions.recent().length);
  readonly historySummary = computed(() => {
    const count = this.recentCount();
    if (count === 0) {
      return 'No finished conversions yet.';
    }
    return count === 1 ? '1 finished conversion' : `${count} finished conversions`;
  });

  readonly hasDefaultDestination = computed(
    () => this.defaultOutputDir().length > 0,
  );
  readonly isNextToOriginal = computed(() => !this.hasDefaultDestination());

  readonly actionMessage = signal('');

  async chooseFolder(): Promise<void> {
    const directory = (await this.wails.openDirectoryDialog()).trim();
    if (!directory) {
      return;
    }

    this.preferences.setDefaultOutputDir(directory);
    this.actionMessage.set('');
  }

  useOriginalFolders(): void {
    if (!this.hasDefaultDestination()) {
      return;
    }

    this.preferences.clearDefaultOutputDir();
    this.actionMessage.set('');
  }

  setTheme(theme: Theme): void {
    this.themeService.set(theme);
  }

  clearHistory(): void {
    if (this.recentCount() === 0) {
      return;
    }

    this.recentConversions.clear();
    this.actionMessage.set('Recent conversions cleared.');
  }

  resetOnboarding(): void {
    try {
      this.document?.defaultView?.localStorage?.removeItem(
        ONBOARDING_STORAGE_KEY,
      );
    } catch {
      // Storage can be unavailable; there is nothing to remove.
    }

    this.actionMessage.set('Onboarding will show again on Home.');
  }
}
