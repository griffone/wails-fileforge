import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { ToolCatalog } from './tool-catalog';
import { Wails } from '../../services/wails';

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

    fixture = TestBed.createComponent(ToolCatalog);
    component = fixture.componentInstance;
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
});
