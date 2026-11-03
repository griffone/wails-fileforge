import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';

import { Home } from './home';
import { RecentToolsService } from '../../services/recent-tools.service';
import { Wails } from '../../services/wails';

describe('Home', () => {
  let component: Home;
  let fixture: ComponentFixture<Home>;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    localStorage.removeItem('ff-recent-tools');

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', ['listToolsV1', 'isRuntimeAvailable']);
    wailsSpy.listToolsV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', tools: [] }),
    );
    wailsSpy.isRuntimeAvailable.and.returnValue(false);

    await TestBed.configureTestingModule({
      imports: [Home],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(Home);
    component = fixture.componentInstance;
  });

  function searchInput(): HTMLInputElement {
    return fixture.nativeElement.querySelector('#home-tool-search') as HTMLInputElement;
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

  it('loads catalog count from ListToolsV1 on init', async () => {
    wailsSpy.listToolsV1.and.returnValue(
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
              runtimeDependencies: ['libreoffice', 'pandoc'],
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

  it('falls back to empty catalog when ListToolsV1 fails', async () => {
    wailsSpy.listToolsV1.and.returnValue(
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

    expect(cardNames().length).toBe(11);
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

    expect(cardNames().length).toBe(3);
    expect(filterChip('Image').getAttribute('aria-pressed')).toBe('true');
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

    expect(cardNames().length).toBe(11);
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
});
