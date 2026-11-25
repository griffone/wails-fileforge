import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { ToolCatalog } from './tool-catalog';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { ToolCatalogEntry, Wails } from '../../services/wails';

describe('ToolCatalog', () => {
  let fixture: ComponentFixture<ToolCatalog>;
  let component: ToolCatalog;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', ['listTools', 'isRuntimeAvailable']);
    wailsSpy.listTools.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', tools: [] }),
    );
    wailsSpy.isRuntimeAvailable.and.returnValue(true);

    await TestBed.configureTestingModule({
      imports: [ToolCatalog],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(ToolCatalog);
    component = fixture.componentInstance;
  });

  afterEach(() => {
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
  });

  it('maps runtime states to global badge classes', () => {
    expect(component.getStatusClass('enabled')).toBe('badge badge-success');
    expect(component.getStatusClass('degraded')).toBe('badge badge-warning');
    expect(component.getStatusClass('disabled')).toBe('badge badge-danger');
    expect(component.getStatusClass('unknown')).toBe('badge badge-neutral');
  });

  it('resolves open-tool routes from the registry instead of hardcoded paths', () => {
    expect(component.routeForTool('tool.pdf.merge')).toBe('/pdf-merge');
    expect(component.routeForTool('tool.video.convert')).toBe('/video-convert');
    expect(component.routeForTool('tool.unknown')).toBeNull();
  });

  it('uses the registry icon for a registered tool and a fallback otherwise', () => {
    const registered = component.toolIcon({ manifest: { toolId: 'tool.pdf.split' } } as never);
    expect(registered).toBe(component.registry.findById('tool.pdf.split')!.icon);

    const fallback = component.toolIcon({ manifest: { toolId: 'tool.unknown' } } as never);
    expect(fallback).toBe(component.activityIcon);
  });

  it('renders the page title, headings and tool copy in Spanish', async () => {
    const tool: ToolCatalogEntry = {
      manifest: {
        toolId: 'tool.pdf.merge',
        name: 'PDF Merge',
        description: 'Combine several PDF files into one document in the order you choose.',
        domain: 'pdf',
        capability: 'merge',
        version: '1.2.3',
        supportsSingle: true,
        supportsBatch: true,
        inputExtensions: ['pdf'],
        outputExtensions: ['pdf'],
        runtimeDependencies: null,
        tags: null,
      },
      state: { status: 'enabled', healthy: true },
    };
    wailsSpy.listTools.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', tools: [tool] }),
    );

    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const title = fixture.nativeElement.querySelector('.page-title') as HTMLElement;
    expect(title.textContent).toContain('Estado del sistema');

    const countLabel = fixture.nativeElement.querySelector(
      '.catalog-count-label',
    ) as HTMLElement;
    expect(countLabel.textContent).toContain('herramientas registradas');

    const list = fixture.nativeElement.querySelector('.catalog-list') as HTMLElement;
    expect(list.getAttribute('aria-label')).toBe('Herramientas registradas');

    const name = fixture.nativeElement.querySelector('.catalog-tool-name') as HTMLElement;
    expect(name.textContent).toContain('Unir PDF');

    const status = fixture.nativeElement.querySelector('.badge') as HTMLElement;
    expect(status.textContent).toContain('habilitada');

    const open = fixture.nativeElement.querySelector('.catalog-open') as HTMLElement;
    expect(open.getAttribute('aria-label')).toBe('Abrir herramienta: Unir PDF');
    expect(open.textContent).toContain('Abrir herramienta');

    expect(component.modeLabel(tool)).toBe('individual + por lotes');
    expect(component.toolCategory(tool)).toBe('PDF');
  });
});
