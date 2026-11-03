import {
  Component,
  DestroyRef,
  ElementRef,
  HostListener,
  Injector,
  ViewChild,
  afterNextRender,
  computed,
  inject,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter } from 'rxjs';
import { LucideAngularModule, Menu, Moon, Search, Sun, X } from 'lucide-angular';

import { BRAND_ICON, ToolDefinition } from './config/tools';
import { CommandPalette } from './components/command-palette/command-palette';
import { RecentToolsService } from './services/recent-tools.service';
import { ThemeService } from './services/theme.service';
import { ToolRegistryService } from './services/tool-registry.service';
import { Wails } from './services/wails';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive, LucideAngularModule, CommandPalette],
  templateUrl: './app.html',
  styleUrl: './app.css',
})
export class App {
  protected readonly registry = inject(ToolRegistryService);
  private readonly router = inject(Router);
  private readonly themeService = inject(ThemeService);
  private readonly wails = inject(Wails);
  private readonly destroyRef = inject(DestroyRef);
  private readonly injector = inject(Injector);

  readonly brandIcon = BRAND_ICON;
  readonly searchIcon = Search;
  readonly menuIcon = Menu;
  readonly closeIcon = X;

  readonly isDark = this.themeService.isDark;
  readonly themeIcon = computed(() => (this.isDark() ? Moon : Sun));
  readonly themeActionLabel = computed(() =>
    this.isDark() ? 'Switch to light theme' : 'Switch to dark theme',
  );

  readonly runtimeAvailable = this.wails.isRuntimeAvailable();
  readonly shortcutLabel = this.resolveShortcutLabel();

  readonly exactMatch = { exact: true } as const;
  readonly prefixMatch = { exact: false } as const;

  readonly currentTool = signal<ToolDefinition | null>(null);
  readonly paletteOpen = signal(false);
  readonly mobileNavOpen = signal(false);

  @ViewChild('menuButton') private menuButton?: ElementRef<HTMLButtonElement>;
  @ViewChild('paletteTrigger') private paletteTrigger?: ElementRef<HTMLButtonElement>;
  @ViewChild('railNav') private railNav?: ElementRef<HTMLElement>;

  private paletteNavigated = false;

  constructor() {
    // Keep the recents history alive from app start; it records on its own.
    inject(RecentToolsService);
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
  }

  toggleTheme(): void {
    this.themeService.toggle();
  }

  openPalette(): void {
    this.paletteNavigated = false;
    this.mobileNavOpen.set(false);
    this.paletteOpen.set(true);
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

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape' && this.mobileNavOpen()) {
      event.preventDefault();
      this.closeMobileNav(true);
      return;
    }

    if (!this.isPaletteShortcut(event) || this.isEditableTarget(event.target)) {
      return;
    }

    event.preventDefault();
    if (!this.paletteOpen()) {
      this.openPalette();
    }
  }

  private syncCurrentTool(route: string): void {
    this.currentTool.set(this.registry.findByRoute(route));
  }

  private isPaletteShortcut(event: KeyboardEvent): boolean {
    return (
      event.key.toLowerCase() === 'k' &&
      (event.metaKey || event.ctrlKey) &&
      !event.altKey &&
      !event.shiftKey
    );
  }

  private isEditableTarget(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) {
      return false;
    }

    const tag = target.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') {
      return true;
    }

    return (
      target.isContentEditable ||
      target.closest('[contenteditable=""], [contenteditable="true"], [contenteditable="plaintext-only"]') !==
        null
    );
  }

  private resolveShortcutLabel(): string {
    if (typeof navigator === 'undefined') {
      return 'Ctrl K';
    }

    const platform = navigator.platform ?? '';
    const isMac = /mac/i.test(platform) || /mac os x/i.test(navigator.userAgent);
    return isMac ? '⌘K' : 'Ctrl K';
  }
}
