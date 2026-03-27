import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { Home } from './home';
import { Wails } from '../../services/wails';

describe('Home', () => {
  let component: Home;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', ['listToolsV1', 'isRuntimeAvailable']);

    await TestBed.configureTestingModule({
      imports: [Home],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(Home);
    component = fixture.componentInstance;
  });

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
        ],
      })
    );

    fixture.detectChanges();
    await fixture.whenStable();

    expect(component.catalogToolsCount).toBe(3);
    expect(component.catalogTools.length).toBe(3);
  });

  it('falls back to empty catalog when ListToolsV1 fails', async () => {
    wailsSpy.listToolsV1.and.returnValue(
      Promise.resolve({
        success: false,
        message: 'ipc error',
        tools: [],
      })
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
});
