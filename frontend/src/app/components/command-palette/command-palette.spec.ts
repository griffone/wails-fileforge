import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { RecentToolsService } from '../../services/recent-tools.service';
import { CommandPalette } from './command-palette';

describe('CommandPalette', () => {
  let fixture: ComponentFixture<CommandPalette>;
  let component: CommandPalette;

  beforeEach(async () => {
    localStorage.removeItem('ff-recent-tools');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);

    await TestBed.configureTestingModule({
      imports: [CommandPalette],
      providers: [provideRouter([])],
    }).compileComponents();

    fixture = TestBed.createComponent(CommandPalette);
    component = fixture.componentInstance;
  });

  afterEach(() => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    document.documentElement.lang = 'en';
  });

  function openPalette(): void {
    fixture.componentRef.setInput('open', true);
    fixture.detectChanges();
  }

  function optionNames(): string[] {
    const options = fixture.nativeElement.querySelectorAll('.palette-option-name') as NodeListOf<HTMLElement>;
    return Array.from(options).map((option) => option.textContent?.trim() ?? '');
  }

  function groupLabels(): string[] {
    const labels = fixture.nativeElement.querySelectorAll('.palette-group-label') as NodeListOf<HTMLElement>;
    return Array.from(labels).map((label) => label.textContent?.trim() ?? '');
  }

  function typeQuery(value: string): void {
    const input = fixture.nativeElement.querySelector('#palette-search') as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    fixture.detectChanges();
  }

  function pressKey(key: string): void {
    const input = fixture.nativeElement.querySelector('#palette-search') as HTMLInputElement;
    input.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));
    fixture.detectChanges();
  }

  it('filters by tool name', () => {
    openPalette();
    typeQuery('merge');

    expect(optionNames()).toEqual(['PDF Merge', 'Video Merge', 'Email PDF']);
  });

  it('finds a recipe by name', () => {
    openPalette();
    typeQuery('clean scan');

    expect(optionNames()).toEqual(['Clean scan']);
  });

  it('filters by keyword', () => {
    openPalette();
    typeQuery('redact');

    expect(optionNames()).toEqual(['Image Annotate']);
  });

  it('opens the active result with ArrowDown and Enter', () => {
    const routes: string[] = [];
    component.navigate.subscribe((route) => routes.push(route));
    openPalette();

    pressKey('ArrowDown');
    pressKey('Enter');

    // Recipes render right after Workspace, so the first step is a recipe.
    expect(routes).toEqual(['/recipe/email-pdf']);
  });

  it('emits closed on Escape', () => {
    const closed = jasmine.createSpy('closed');
    component.closed.subscribe(closed);
    openPalette();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );

    expect(closed).toHaveBeenCalled();
  });

  it('renders an empty state that echoes the query', () => {
    openPalette();
    typeQuery('zzzz');

    const compiled = fixture.nativeElement as HTMLElement;
    expect(optionNames().length).toBe(0);
    expect(compiled.querySelector('.palette-empty')?.textContent).toContain('zzzz');
  });

  it('shows recent tools before the category groups', () => {
    const recents = TestBed.inject(RecentToolsService);
    recents.record('/pdf-merge');

    openPalette();

    expect(groupLabels()[0]).toBe('Recent');
    expect(optionNames()[0]).toBe('PDF Merge');
  });

  it('searches translated labels in Spanish mode', () => {
    TestBed.inject(I18nService).setLocale('es');

    openPalette();
    typeQuery('unir');

    expect(optionNames()).toEqual(['Unir PDF', 'Unir video']);
  });
});
