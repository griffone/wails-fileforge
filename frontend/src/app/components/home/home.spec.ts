import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';

import { Home } from './home';
import { I18nService } from '../../services/i18n.service';
import { RecentConversionsService } from '../../services/recent-conversions.service';
import { RecentToolsService } from '../../services/recent-tools.service';
import { JobResult, Wails } from '../../services/wails';

describe('Home', () => {
  let component: Home;
  let fixture: ComponentFixture<Home>;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    localStorage.removeItem('ff-recent-tools');
    localStorage.removeItem('ff-recent-conversions');
    localStorage.removeItem('ff-onboarding-dismissed');
    localStorage.setItem('ff-preferences', JSON.stringify({ language: 'en' }));

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'listTools',
      'isRuntimeAvailable',
      'openPath',
      'revealInFolder',
      'getAutomationStatus',
      'listRecipes',
    ]);
    wailsSpy.listTools.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', tools: [] }),
    );
    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [],
    });
    wailsSpy.isRuntimeAvailable.and.returnValue(false);
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });
    wailsSpy.getAutomationStatus.and.resolveTo({
      success: true,
      message: 'ok',
      totalRules: 0,
      activeRules: 0,
    });

    await TestBed.configureTestingModule({
      imports: [Home],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(Home);
    component = fixture.componentInstance;
  });

  afterEach(() => {
    localStorage.removeItem('ff-recent-tools');
    localStorage.removeItem('ff-recent-conversions');
    localStorage.removeItem('ff-onboarding-dismissed');
    localStorage.removeItem('ff-preferences');
  });

  function searchInput(): HTMLInputElement {
    return fixture.nativeElement.querySelector('#home-tool-search') as HTMLInputElement;
  }

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function conversionJob(overrides: Partial<JobResult> = {}): JobResult {
    return {
      jobId: 'job-1',
      success: true,
      message: '',
      toolId: 'tool.image.convert',
      status: 'success',
      progress: { current: 1, total: 1, stage: 'done', message: '' },
      items: [
        {
          inputPath: '/tmp/in/a.png',
          outputPath: '/tmp/out/a.webp',
          success: true,
          message: 'ok',
        },
      ],
      startedAt: Date.now() - 5000,
      endedAt: Date.now() - 1000,
      ...overrides,
    };
  }

  function recordConversion(job: JobResult = conversionJob()): void {
    TestBed.inject(RecentConversionsService).record(job);
  }

  function filterChip(label: string): HTMLButtonElement {
    const chips = Array.from(
      fixture.nativeElement.querySelectorAll('.home-filters .chip') as NodeListOf<HTMLButtonElement>,
    );
    const chip = chips.find((item) => item.textContent?.trim() === label);

    if (!chip) {
      throw new Error(`Filter chip not found: ${label}`);
    }

    return chip;
  }

  function typeQuery(value: string): void {
    const input = searchInput();
    input.value = value;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    fixture.detectChanges();
  }

  function cardNames(): string[] {
    const titles = fixture.nativeElement.querySelectorAll(
      '.tool-card-title',
    ) as NodeListOf<HTMLElement>;
    return Array.from(titles).map((title) => title.textContent?.trim() ?? '');
  }

  it('loads catalog count from ListTools on init', async () => {
    wailsSpy.listTools.and.returnValue(
      Promise.resolve({
        success: true,
        message: 'ok',
        tools: [
          {
            manifest: {
              toolId: 'tool.image.convert',
              name: 'Image Convert',
              description: 'convert images',
              domain: 'image',
              capability: 'convert',
              version: '1.0.0',
              supportsSingle: true,
              supportsBatch: true,
              inputExtensions: ['png', 'jpg'],
              outputExtensions: ['webp', 'jpeg'],
              runtimeDependencies: [],
              tags: ['image'],
            },
            state: { status: 'enabled', healthy: true },
          },
          {
            manifest: {
              toolId: 'tool.pdf.merge',
              name: 'PDF Merge',
              description: 'merge pdf files',
              domain: 'pdf',
              capability: 'merge',
              version: '1.0.0',
              supportsSingle: true,
              supportsBatch: false,
              inputExtensions: ['pdf'],
              outputExtensions: ['pdf'],
              runtimeDependencies: [],
              tags: ['pdf'],
            },
            state: { status: 'enabled', healthy: true },
          },
          {
            manifest: {
              toolId: 'tool.image.crop',
              name: 'Image Crop',
              description: 'crop images',
              domain: 'image',
              capability: 'crop',
              version: '1.0.0',
              supportsSingle: true,
              supportsBatch: true,
              inputExtensions: ['png', 'jpg'],
              outputExtensions: ['png', 'jpg'],
              runtimeDependencies: [],
              tags: ['image'],
            },
            state: { status: 'enabled', healthy: true },
          },
          {
            manifest: {
              toolId: 'tool.image.annotate',
              name: 'Image Annotate',
              description: 'annotate images',
              domain: 'image',
              capability: 'annotate',
              version: '1.0.0',
              supportsSingle: true,
              supportsBatch: true,
              inputExtensions: ['png', 'jpg'],
              outputExtensions: ['png', 'jpg'],
              runtimeDependencies: [],
              tags: ['image'],
            },
            state: { status: 'enabled', healthy: true },
          },
          {
            manifest: {
              toolId: 'tool.doc.md_to_pdf',
              name: 'Markdown to PDF',
              description: 'md to pdf',
              domain: 'doc',
              capability: 'tool.doc.md_to_pdf',
              version: 'v1',
              supportsSingle: true,
              supportsBatch: false,
              inputExtensions: ['md'],
              outputExtensions: ['pdf'],
              runtimeDependencies: [],
              tags: ['doc'],
            },
            state: { status: 'enabled', healthy: true },
          },
          {
            manifest: {
              toolId: 'tool.doc.docx_to_pdf',
              name: 'DOCX to PDF',
              description: 'docx to pdf',
              domain: 'doc',
              capability: 'tool.doc.docx_to_pdf',
              version: 'v1',
              supportsSingle: true,
              supportsBatch: true,
              inputExtensions: ['docx'],
              outputExtensions: ['pdf'],
              runtimeDependencies: [],
              tags: ['doc'],
            },
            state: { status: 'enabled', healthy: true },
          },
        ],
      }),
    );

    fixture.detectChanges();
    await fixture.whenStable();

    expect(component.catalogToolsCount).toBe(6);
    expect(component.catalogTools.length).toBe(6);
  });

  it('falls back to empty catalog when ListTools fails', async () => {
    wailsSpy.listTools.and.returnValue(
      Promise.resolve({
        success: false,
        message: 'ipc error',
        tools: [],
      }),
    );

    fixture.detectChanges();
    await fixture.whenStable();

    expect(component.catalogToolsCount).toBe(0);
    expect(component.catalogTools).toEqual([]);
  });

  it('delegates runtime availability to wails service', () => {
    wailsSpy.isRuntimeAvailable.and.returnValue(true);

    expect(component.isRuntimeAvailable()).toBeTrue();
  });

  it('renders every workbench tool by default', () => {
    fixture.detectChanges();

    expect(cardNames().length).toBe(28);
    expect(filterChip('All').getAttribute('aria-pressed')).toBe('true');
  });

  it('filters the grid by search query', () => {
    fixture.detectChanges();
    typeQuery('merge');

    expect(cardNames()[0]).toBe('PDF Merge');
    expect(cardNames()).not.toContain('Image Convert');
  });

  it('filters the grid by category', () => {
    fixture.detectChanges();

    filterChip('Image').click();
    fixture.detectChanges();

    expect(cardNames().length).toBe(8);
    expect(cardNames()).toEqual([
      'Image Convert',
      'Image Crop',
      'Image Annotate',
      'Image Compress',
      'Image Watermark',
      'Image OCR',
      'Remove Background',
      'Upscale Image',
    ]);
    expect(filterChip('Image').getAttribute('aria-pressed')).toBe('true');
  });

  it('shows the eleven PDF tools under the PDF category', () => {
    fixture.detectChanges();

    expect(component.categoryCounts()['pdf']).toBe(11);

    filterChip('PDF').click();
    fixture.detectChanges();

    expect(cardNames()).toEqual([
      'PDF Merge',
      'PDF Split',
      'PDF Crop',
      'PDF Optimize',
      'PDF OCR',
      'PDF Rotate',
      'PDF Remove Pages',
      'PDF Reorder Pages',
      'PDF Extract Images',
      'Images to PDF',
      'PDF to DOCX',
    ]);
  });

  it('shows the four video tools under the Video category', () => {
    fixture.detectChanges();

    expect(component.categoryCounts()['video']).toBe(4);

    filterChip('Video').click();
    fixture.detectChanges();

    expect(cardNames()).toEqual([
      'Video Convert',
      'Video Trim',
      'Video Merge',
      'Video Transcribe',
    ]);
  });

  it('shows the three audio tools under the Audio category', () => {
    fixture.detectChanges();

    expect(component.categoryCounts()['audio']).toBe(3);

    filterChip('Audio').click();
    fixture.detectChanges();

    expect(cardNames()).toEqual([
      'Extract Audio',
      'Audio Convert',
      'Normalize Audio',
    ]);
  });

  it('opens the first filtered tool when Enter is pressed', () => {
    const router = TestBed.inject(Router);
    const navigate = spyOn(router, 'navigateByUrl').and.resolveTo(true);

    fixture.detectChanges();
    typeQuery('merge');

    searchInput().dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
    );

    expect(navigate).toHaveBeenCalledWith('/pdf-merge');
  });

  it('restores every tool when filters are cleared', () => {
    fixture.detectChanges();
    typeQuery('zzzz');

    expect(fixture.nativeElement.querySelector('.empty-state')).not.toBeNull();

    (fixture.nativeElement.querySelector('.empty-state-action') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(cardNames().length).toBe(28);
    expect(searchInput().value).toBe('');
    expect(filterChip('All').getAttribute('aria-pressed')).toBe('true');
  });

  it('shows recently used tools when no filter is active', () => {
    const recentTools = TestBed.inject(RecentToolsService);
    recentTools.record('/pdf-merge');

    fixture.detectChanges();

    const section = fixture.nativeElement.querySelector('.home-recent') as HTMLElement | null;
    expect(section).not.toBeNull();
    expect(section?.textContent).toContain('PDF Merge');
  });

  it('renders the recipe cards outside the tool grid', () => {
    fixture.detectChanges();

    const cards = fixture.nativeElement.querySelectorAll(
      '.home-recipe-card',
    ) as NodeListOf<HTMLElement>;
    expect(cards.length).toBe(6);

    expect(cards[0].textContent).toContain('Email PDF');
    expect(cards[0].textContent).toContain('Target size');
    expect(cards[0].textContent).toContain('Use recipe');
    expect(cards[0].querySelector('a')?.getAttribute('href')).toBe('/recipe/email-pdf');

    expect(cards[1].textContent).toContain('Web image set');
    expect(cards[1].textContent).toContain('ZIP + srcset');
    expect(cards[1].querySelector('a')?.getAttribute('href')).toBe('/recipe/web-image-set');

    expect(cards[2].textContent).toContain('Social clip');
    expect(cards[2].textContent).toContain('MP4 + WebM');
    expect(cards[2].textContent).toContain('Use recipe');
    expect(cards[2].querySelector('a')?.getAttribute('href')).toBe('/recipe/social-clip');

    expect(cards[3].textContent).toContain('Clean scan');
    expect(cards[3].textContent).toContain('Local OCR');
    expect(cards[3].textContent).toContain('Searchable PDF');
    expect(cards[3].textContent).toContain('Text sidecar');
    expect(cards[3].textContent).toContain('PDF');
    expect(cards[3].textContent).toContain('TXT');
    expect(cards[3].textContent).toContain('Use recipe');
    expect(cards[3].querySelector('a')?.getAttribute('href')).toBe('/recipe/clean-scan');

    expect(cards[4].textContent).toContain('Burned-in subtitles');
    expect(cards[4].textContent).toContain('Local Whisper');
    expect(cards[4].textContent).toContain('Synchronized burn-in');
    expect(cards[4].textContent).toContain('SRT sidecar');
    expect(cards[4].textContent).toContain('MP4');
    expect(cards[4].textContent).toContain('SRT');
    expect(cards[4].textContent).toContain('Use recipe');
    expect(cards[4].querySelector('a')?.getAttribute('href')).toBe('/recipe/burned-subtitles');

    expect(cards[5].textContent).toContain('Archive document');
    expect(cards[5].textContent).toContain('PDF/A-2');
    expect(cards[5].textContent).toContain('SHA-256 checksum');
    expect(cards[5].textContent).toContain('Optional sidecar');
    expect(cards[5].textContent).toContain('Use recipe');
    expect(cards[5].querySelector('a')?.getAttribute('href')).toBe('/recipe/archive-document');

    expect(query<HTMLElement>('.home-recipes')?.textContent).toContain(
      'Outcomes, not one tool at a time.',
    );
    expect(cardNames().length).toBe(28);
  });

  it('hides recipes while a search query is active', () => {
    fixture.detectChanges();
    typeQuery('pdf');

    expect(query('.home-recipes')).toBeNull();
  });

  it('shows folder rules with active count, last run and the manage link', async () => {
    wailsSpy.getAutomationStatus.and.resolveTo({
      success: true,
      message: 'ok',
      totalRules: 2,
      activeRules: 1,
      lastRunAt: Date.now() - 5 * 60_000,
      lastStatus: 'failed',
      lastMessage: 'Ghostscript is missing.',
      lastFile: '/tmp/in/a.pdf',
    });

    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const section = query<HTMLElement>('.home-automation');
    expect(section).not.toBeNull();
    expect(section?.textContent).toContain('1 of 2 rules active');
    expect(section?.textContent).toContain('5 min ago');
    expect(section?.textContent).toContain('Failed');
    expect(section?.querySelector('a')?.getAttribute('href')).toBe(
      '/automations',
    );
    expect(cardNames().length).toBe(28);
  });

  it('hides the folder-rule card when there are no rules', async () => {
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(query('.home-automation')).toBeNull();
  });

  it('hides the recent conversions section when the history is empty', () => {
    fixture.detectChanges();

    expect(query('.home-conversions')).toBeNull();
  });

  it('renders recent conversions with tool, status, time and output summary', () => {
    recordConversion();

    fixture.detectChanges();

    const section = query<HTMLElement>('.home-conversions');
    expect(section).not.toBeNull();
    expect(section?.textContent).toContain('Recent conversions');
    expect(section?.textContent).toContain('Image Convert');
    expect(section?.textContent).toContain('Success');
    expect(section?.textContent).toContain('Just now');
    expect(query<HTMLElement>('.home-conversion-file')?.textContent).toContain('a.webp');
    expect(query<HTMLElement>('.home-conversion-dir')?.textContent).toContain('/tmp/out');
    expect(query<HTMLElement>('.home-conversions-count')?.textContent).toContain('1 finished');
  });

  it('summarizes multiple outputs as a count and the first file name', () => {
    recordConversion(
      conversionJob({
        toolId: 'tool.pdf.split',
        items: [
          {
            inputPath: '/tmp/in/a.pdf',
            outputPath: '/tmp/out/a_pages',
            outputs: ['/tmp/out/a_pages/one.pdf', '/tmp/out/a_pages/two.pdf'],
            success: true,
            message: 'ok',
          },
        ],
      }),
    );

    fixture.detectChanges();

    expect(query<HTMLElement>('.home-conversion-file')?.textContent).toContain(
      '2 files · one.pdf + more',
    );
  });

  it('shows failed conversions without open or reveal actions', () => {
    recordConversion(
      conversionJob({
        status: 'failed',
        items: [
          {
            inputPath: '/tmp/in/a.png',
            outputPath: '',
            success: false,
            message: 'failed',
          },
        ],
      }),
    );

    fixture.detectChanges();

    expect(query<HTMLElement>('.home-conversion')?.textContent).toContain('Failed');
    expect(query<HTMLElement>('.home-conversion')?.textContent).toContain(
      'No files created',
    );
    expect(query('.home-conversion-open')).toBeNull();
    expect(query('.home-conversion-reveal')).toBeNull();
  });

  it('opens the first output and reveals its folder', async () => {
    recordConversion();
    fixture.detectChanges();

    query<HTMLButtonElement>('.home-conversion-open')?.click();
    query<HTMLButtonElement>('.home-conversion-reveal')?.click();
    await fixture.whenStable();

    expect(wailsSpy.openPath).toHaveBeenCalledWith('/tmp/out/a.webp');
    expect(wailsSpy.revealInFolder).toHaveBeenCalledWith('/tmp/out');
  });

  it('shows a quiet message when opening fails', async () => {
    recordConversion();
    fixture.detectChanges();
    wailsSpy.openPath.and.resolveTo({
      success: false,
      message: '<!DOCTYPE html><html><body>Cannot POST</body></html>',
    });

    query<HTMLButtonElement>('.home-conversion-open')?.click();
    await fixture.whenStable();
    fixture.detectChanges();

    const message = query<HTMLElement>('.home-conversion-message')?.textContent ?? '';
    expect(message).toContain('The file could not be opened.');
    expect(message).not.toContain('DOCTYPE');
  });

  it('clears recent conversions', () => {
    recordConversion();
    fixture.detectChanges();

    query<HTMLButtonElement>('.home-conversions-clear')?.click();
    fixture.detectChanges();

    expect(query('.home-conversions')).toBeNull();
    expect(localStorage.getItem('ff-recent-conversions')).toBe('[]');
  });

  it('formats relative timestamps', () => {
    const now = Date.now();

    expect(component.relativeTime(now)).toBe('Just now');
    expect(component.relativeTime(now - 5 * 60_000 - 1000)).toBe('5 min ago');
    expect(component.relativeTime(now - 2 * 3_600_000 - 1000)).toBe('2 h ago');
    expect(component.relativeTime(0)).toBe('');
  });

  it('renders the onboarding strip and dismisses it persistently', () => {
    fixture.detectChanges();

    const strip = query<HTMLElement>('.home-onboarding');
    expect(strip).not.toBeNull();
    expect(strip?.textContent).toContain('Drop files');
    expect(strip?.textContent).toContain('Pick a tool');
    expect(strip?.textContent).toContain('Open the result');
    expect(strip?.textContent).toContain(
      'Everything runs on this machine. No uploads, no accounts.',
    );
    expect(
      query<HTMLButtonElement>('.home-onboarding-dismiss')?.getAttribute('aria-label'),
    ).toBe('Dismiss getting started');

    query<HTMLButtonElement>('.home-onboarding-dismiss')?.click();
    fixture.detectChanges();

    expect(query('.home-onboarding')).toBeNull();
    expect(localStorage.getItem('ff-onboarding-dismissed')).toBe('1');
  });

  it('keeps onboarding hidden when dismissal was saved earlier', () => {
    localStorage.setItem('ff-onboarding-dismissed', '1');
    const fresh = TestBed.createComponent(Home);

    fresh.detectChanges();

    expect(fresh.nativeElement.querySelector('.home-onboarding')).toBeNull();
    fresh.destroy();
  });

  it('renders the home copy in Spanish when the locale is Spanish', () => {
    const i18n = TestBed.inject(I18nService);
    i18n.setLocale('es');

    fixture.detectChanges();

    const title = query<HTMLElement>('.home-title')?.textContent
      ?.replace(/\s+/g, ' ')
      .trim();
    expect(title).toBe(
      'Todas las herramientas de archivo. Un solo espacio de trabajo.',
    );
    expect(query<HTMLElement>('.home-sub')?.textContent).toContain(
      'Convierte, recorta, une y anota sin salir de tu equipo.',
    );
    expect(searchInput().placeholder).toBe(
      'Encuentra una herramienta: prueba «unir» o «webp»',
    );
    expect(filterChip('Todas').getAttribute('aria-pressed')).toBe('true');
    expect(cardNames()).toContain('Unir PDF');

    const recipeCards = fixture.nativeElement.querySelectorAll(
      '.home-recipe-card',
    ) as NodeListOf<HTMLElement>;
    expect(recipeCards[0].textContent).toContain('Unir si hace falta');
    expect(recipeCards[0].textContent).toContain('Usar receta');
  });
});
