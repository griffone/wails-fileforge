import {
  Component,
  DestroyRef,
  ElementRef,
  HostListener,
  Injector,
  NgZone,
  ViewChild,
  afterNextRender,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { Events } from '@wailsio/runtime';
import { filter } from 'rxjs';
import { Activity, LucideAngularModule, Menu, Moon, Search, Sun, X } from 'lucide-angular';

import { RecipeDefinition } from './config/recipes';
import {
  findShortcut,
  findShortcutById,
  formatShortcutLabel,
  shouldIgnoreShortcutEvent,
} from './config/shortcuts';
import { BRAND_ICON, ToolDefinition } from './config/tools';
import { ActivityPanel } from './components/activity-panel/activity-panel';
import { CommandPalette } from './components/command-palette/command-palette';
import { GlobalDrop } from './components/global-drop/global-drop';
import { ShortcutSheet } from './components/shortcut-sheet/shortcut-sheet';
import { ToastHost } from './components/toast-host/toast-host';
import { ToolchainSetup } from './components/toolchain-setup/toolchain-setup';
import { ActivityService } from './services/activity.service';
import { GlobalDropService } from './services/global-drop.service';
import { I18nService } from './services/i18n.service';
import { RecentToolsService } from './services/recent-tools.service';
import { ShortcutService } from './services/shortcut.service';
import { ThemeService } from './services/theme.service';
import { ToolRegistryService } from './services/tool-registry.service';
import { UpdateStateService } from './services/update-state.service';
import { Wails } from './services/wails';
import { WindowLauncher } from './services/window-launcher.service';

interface NativeDropPayload {
  droppedFiles?: unknown;
  files?: unknown;
  dropTargetDetails?: { id?: unknown } | null;
}

const LOCAL_DROP_TARGET_PREFIX = 'ff-drop-';

@Component({
  selector: 'app-root',
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    LucideAngularModule,
    CommandPalette,
    GlobalDrop,
    ToolchainSetup,
    ActivityPanel,
    ShortcutSheet,
    ToastHost,
  ],
  templateUrl: './app.html',
  styleUrl: './app.css',
})
export class App {
  protected readonly registry = inject(ToolRegistryService);
  readonly activity = inject(ActivityService);
  readonly i18n = inject(I18nService);
  private readonly router = inject(Router);
  private readonly themeService = inject(ThemeService);
  private readonly wails = inject(Wails);
  private readonly destroyRef = inject(DestroyRef);
  private readonly injector = inject(Injector);
  private readonly zone = inject(NgZone);
  private readonly globalDrop = inject(GlobalDropService);
  private readonly updates = inject(UpdateStateService);
  private readonly shortcuts = inject(ShortcutService);
  private readonly windows = inject(WindowLauncher);

  readonly brandIcon = BRAND_ICON;
  readonly searchIcon = Search;
  readonly activityIcon = Activity;
  readonly menuIcon = Menu;
  readonly closeIcon = X;

  readonly isDark = this.themeService.isDark;
  readonly themeIcon = computed(() => (this.isDark() ? Moon : Sun));
  readonly themeActionLabel = computed(() =>
    this.i18n.t(this.isDark() ? 'shell.theme.toLight' : 'shell.theme.toDark'),
  );
  readonly localeToggleLabel = computed(() => {
    const locale = this.i18n.locale();
    const label =
      this.i18n.locales.find((option) => option.id === locale)?.label ?? locale;
    return this.i18n.t('shell.locale.toggle', { language: label });
  });

  readonly runtimeAvailable = this.wails.isRuntimeAvailable();
  readonly shortcutLabel = formatShortcutLabel(
    findShortcutById('palette.open'),
    this.shortcuts.platform,
  );

  readonly exactMatch = { exact: true } as const;
  readonly prefixMatch = { exact: false } as const;

  readonly currentTool = signal<ToolDefinition | null>(null);
  readonly currentRecipe = signal<RecipeDefinition | null>(null);
  /** Router URL of this window, fed to the palette's window action. */
  readonly currentRoute = signal('');
  readonly paletteOpen = signal(false);
  readonly mobileNavOpen = signal(false);
  readonly shortcutSheetOpen = signal(false);

  @ViewChild('menuButton') private menuButton?: ElementRef<HTMLButtonElement>;
  @ViewChild('paletteTrigger') private paletteTrigger?: ElementRef<HTMLButtonElement>;
  @ViewChild('activityTrigger') private activityTrigger?: ElementRef<HTMLButtonElement>;
  @ViewChild('railNav') private railNav?: ElementRef<HTMLElement>;

  private paletteNavigated = false;
  private activityWasOpen = false;
  private nativeDropUnsubscribe: (() => void) | null = null;

  constructor() {
    // Keep the recents history alive from app start; it records on its own.
    inject(RecentToolsService);
    void this.activity.refresh();
    // Production builds check once per launch, at most once per 24 hours, and
    // stay silent when the check fails. Development builds never check here.
    void this.updates.ensureLaunchCheck();
    this.syncCurrentTool(this.router.url);
    this.router.events
      .pipe(
        filter((event): event is NavigationEnd => event instanceof NavigationEnd),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe((event) => {
        this.syncCurrentTool(event.urlAfterRedirects);
        this.mobileNavOpen.set(false);
      });

    // Return focus to the trigger when the drawer closes on its own; opening
    // the palette closes the drawer first and must keep focus in the palette.
    effect(() => {
      const open = this.activity.panelOpen();
      const paletteOpen = this.paletteOpen();
      if (this.activityWasOpen && !open && !paletteOpen) {
        this.activityTrigger?.nativeElement.focus();
      }
      this.activityWasOpen = open;
    });

    this.listenForNativeDrops();
    this.destroyRef.onDestroy(() => this.stopNativeDropListener());
  }

  toggleTheme(): void {
    this.themeService.toggle();
  }

  toggleLocale(): void {
    this.i18n.toggle();
  }

  openPalette(): void {
    this.paletteNavigated = false;
    this.mobileNavOpen.set(false);
    this.activity.closePanel();
    this.paletteOpen.set(true);
  }

  openSettings(): void {
    this.mobileNavOpen.set(false);
    void this.router.navigateByUrl('/settings');
  }

  openShortcutSheet(): void {
    this.mobileNavOpen.set(false);
    this.activity.closePanel();
    this.shortcutSheetOpen.set(true);
  }

  onShortcutSheetClosed(): void {
    this.shortcutSheetOpen.set(false);
  }

  toggleActivity(): void {
    this.mobileNavOpen.set(false);
    this.activity.togglePanel();
    void this.activity.refresh();
  }

  onPaletteClosed(): void {
    const restoreFocus = !this.paletteNavigated;
    this.paletteNavigated = false;
    this.paletteOpen.set(false);

    if (restoreFocus) {
      this.paletteTrigger?.nativeElement.focus();
    }
  }

  onPaletteNavigate(route: string): void {
    this.paletteNavigated = true;
    this.paletteOpen.set(false);
    void this.router.navigateByUrl(route);
  }

  /** Palette action: open the tool of this window in a window of its own. */
  onPaletteOpenWindow(route: string): void {
    void this.windows.open(route);
  }

  toggleMobileNav(): void {
    const next = !this.mobileNavOpen();
    this.mobileNavOpen.set(next);

    if (!next) {
      return;
    }

    // The rail is only focusable once the open state has rendered.
    afterNextRender(
      () => {
        const nav = this.railNav?.nativeElement;
        const target =
          nav?.querySelector<HTMLElement>('.nav-item.is-active') ??
          nav?.querySelector<HTMLElement>('.nav-item');
        target?.focus();
      },
      { injector: this.injector },
    );
  }

  closeMobileNav(restoreFocus = false): void {
    if (!this.mobileNavOpen()) {
      return;
    }

    this.mobileNavOpen.set(false);

    if (restoreFocus) {
      this.menuButton?.nativeElement.focus();
    }
  }

  onRailNavigate(): void {
    this.mobileNavOpen.set(false);
  }

  /**
   * Routes a native window drop to the global overlay. Drops aimed at a local
   * `ff-drop-*` zone stay with that zone; anything else is shell intake.
   */
  handleNativeDrop(event: unknown): void {
    const eventData = (event as { data?: unknown } | null)?.data ?? event;
    if (!eventData || typeof eventData !== 'object') {
      return;
    }

    const data = eventData as NativeDropPayload;
    const rawFiles = data.droppedFiles ?? data.files;
    if (!Array.isArray(rawFiles)) {
      return;
    }

    const paths = rawFiles
      .filter((path): path is string => typeof path === 'string')
      .map((path) => path.trim())
      .filter((path) => path.length > 0);
    if (paths.length === 0) {
      return;
    }

    const target = data.dropTargetDetails;
    const targetId = typeof target?.id === 'string' ? target.id : '';
    if (targetId.startsWith(LOCAL_DROP_TARGET_PREFIX)) {
      return;
    }

    this.globalDrop.show(paths);
  }

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    // The mobile drawer is shell chrome, not a dialog: it keeps its own Escape.
    if (event.key === 'Escape' && this.mobileNavOpen()) {
      event.preventDefault();
      this.closeMobileNav(true);
      return;
    }

    // Page shortcuts belong to the active ToolRunController. Escape is not one
    // of them: dialogs close themselves, so nothing here can close a dialog
    // twice.
    if (this.shortcuts.handlePageShortcut(event)) {
      return;
    }

    const shortcut = findShortcut(event, this.shortcuts.platform, 'global');
    if (!shortcut || shouldIgnoreShortcutEvent(event, shortcut)) {
      return;
    }

    switch (shortcut.id) {
      case 'palette.open':
        event.preventDefault();
        if (!this.paletteOpen()) {
          this.openPalette();
        }
        return;
      case 'settings.open':
        event.preventDefault();
        this.openSettings();
        return;
      case 'shortcutSheet.open':
        event.preventDefault();
        if (!this.shortcutSheetOpen()) {
          this.openShortcutSheet();
        }
        return;
      default:
        return;
    }
  }

  private syncCurrentTool(route: string): void {
    const recipe = this.registry.findRecipeByRoute(route);
    this.currentRoute.set(route);
    this.currentRecipe.set(recipe);
    this.currentTool.set(recipe ? null : this.registry.findByRoute(route));
  }

  private listenForNativeDrops(): void {
    try {
      this.nativeDropUnsubscribe = Events.On(
        'common:WindowFilesDropped',
        (event: unknown) => this.zone.run(() => this.handleNativeDrop(event)),
      );
    } catch {
      this.nativeDropUnsubscribe = null;
    }
  }

  private stopNativeDropListener(): void {
    try {
      this.nativeDropUnsubscribe?.();
    } catch {
      // The runtime may already be gone during teardown.
    }
    this.nativeDropUnsubscribe = null;
  }
}
