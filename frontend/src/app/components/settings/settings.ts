import { DOCUMENT } from '@angular/common';
import { Component, OnInit, computed, inject, signal } from '@angular/core';
import {
  FolderOpen,
  History,
  LucideAngularModule,
  Moon,
  RefreshCw,
  Sun,
  TriangleAlert,
} from 'lucide-angular';

import { PreferencesService } from '../../services/preferences.service';
import { RecentConversionsService } from '../../services/recent-conversions.service';
import { Theme, ThemeService } from '../../services/theme.service';
import {
  LocalCapabilityV1,
  LocalCapabilitiesResponseV1,
  Wails,
} from '../../services/wails';

/** Shared with the Home onboarding strip. */
const ONBOARDING_STORAGE_KEY = 'ff-onboarding-dismissed';

const CAPABILITIES_ERROR_FALLBACK =
  'The local capability check did not run. Open FileForge in the desktop app, then retry.';

@Component({
  selector: 'app-settings',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './settings.html',
  styleUrl: './settings.css',
})
export class Settings implements OnInit {
  private readonly preferences = inject(PreferencesService);
  private readonly themeService = inject(ThemeService);
  private readonly recentConversions = inject(RecentConversionsService);
  private readonly wails = inject(Wails);
  private readonly document = inject(DOCUMENT);

  readonly folderIcon = FolderOpen;
  readonly sunIcon = Sun;
  readonly moonIcon = Moon;
  readonly historyIcon = History;
  readonly refreshIcon = RefreshCw;
  readonly alertIcon = TriangleAlert;

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

  readonly capabilities = signal<LocalCapabilitiesResponseV1 | null>(null);
  readonly capabilitiesLoading = signal(false);
  readonly capabilitiesError = signal('');

  ngOnInit(): void {
    void this.loadCapabilities();
  }

  async loadCapabilities(): Promise<void> {
    this.capabilitiesLoading.set(true);
    this.capabilitiesError.set('');

    try {
      const response = await this.wails.getLocalCapabilitiesV1();
      if (!response.success) {
        this.capabilities.set(null);
        this.capabilitiesError.set(
          this.friendlyCapabilitiesError(response.message),
        );
        return;
      }

      this.capabilities.set(response);
    } catch {
      this.capabilities.set(null);
      this.capabilitiesError.set(CAPABILITIES_ERROR_FALLBACK);
    } finally {
      this.capabilitiesLoading.set(false);
    }
  }

  refreshCapabilities(): void {
    void this.loadCapabilities();
  }

  capabilityBadgeClass(check: LocalCapabilityV1): string {
    if (check.available) {
      return 'badge badge-success';
    }
    return check.required ? 'badge badge-danger' : 'badge badge-neutral';
  }

  capabilityStateLabel(check: LocalCapabilityV1): string {
    if (check.available) {
      return 'Ready';
    }
    return check.required ? 'Missing' : 'Optional';
  }

  capabilityDotState(check: LocalCapabilityV1): string {
    if (check.available) {
      return 'success';
    }
    return check.required ? 'danger' : 'idle';
  }

  ocrLanguageLabel(): string {
    const capabilities = this.capabilities();
    const languages = capabilities?.ocrLanguages ?? [];
    if (languages.length === 0) {
      return 'No OCR language data installed';
    }

    const installed = languages
      .map((language) => language.toUpperCase())
      .join(' · ');
    const recommended = (capabilities?.recommendedOcrLanguage ?? '')
      .trim()
      .toUpperCase();

    return recommended
      ? `OCR languages: ${installed} · recommended ${recommended}`
      : `OCR languages: ${installed}`;
  }

  private friendlyCapabilitiesError(message: string): string {
    const text = (message ?? '').trim();
    if (
      !text ||
      /<\/?[a-z][\s\S]*>/i.test(text) ||
      /Cannot POST|DOCTYPE html/i.test(text)
    ) {
      return CAPABILITIES_ERROR_FALLBACK;
    }

    return text.length > 240 ? `${text.slice(0, 237)}…` : text;
  }

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
