import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';

import {
  Duplicates,
  DUPLICATES_DEFAULT_MAX_FILES,
  DUPLICATES_DEFAULT_MIN_SIZE_KIB,
} from './duplicates';
import { ClipboardService } from '../../services/clipboard.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import {
  DuplicateScanGroup,
  DuplicateScanProgressEvent,
  DuplicateScanState,
  DuplicateScanTotals,
  JobProgress,
  Wails,
} from '../../services/wails';

const EMPTY_TOTALS: DuplicateScanTotals = {
  filesScanned: 0,
  filesHashed: 0,
  groups: 0,
  wastedBytes: 0,
};

const DONE_PROGRESS: JobProgress = {
  current: 0,
  total: 0,
  stage: 'done',
  message: '',
};

function sampleGroup(overrides: Partial<DuplicateScanGroup> = {}): DuplicateScanGroup {
  return {
    hash: 'hash-a',
    sizeBytes: 1024,
    count: 2,
    wastedBytes: 1024,
    paths: ['/tmp/photos/a.bin', '/tmp/photos/b.bin'],
    ...overrides,
  };
}

function sampleScan(
  overrides: Partial<DuplicateScanState> = {},
): DuplicateScanState {
  return {
    scanId: 'scan_1',
    kind: 'duplicates',
    success: true,
    message: 'No duplicate files found',
    status: 'success',
    progress: DONE_PROGRESS,
    groups: [],
    totals: EMPTY_TOTALS,
    warnings: [],
    startedAt: Date.now() - 60_000,
    endedAt: Date.now() - 30_000,
    ...overrides,
  };
}

describe('Duplicates', () => {
  let fixture: ComponentFixture<Duplicates>;
  let component: Duplicates;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let clipboardSpy: jasmine.SpyObj<ClipboardService>;
  let progressSubject: Subject<DuplicateScanProgressEvent>;
  let stateSubject: Subject<DuplicateScanState>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function queryAll(selector: string): HTMLElement[] {
    return Array.from(
      fixture.nativeElement.querySelectorAll(selector),
    ) as HTMLElement[];
  }

  function text(selector: string): string {
    return (query(selector)?.textContent ?? '').replace(/\s+/g, ' ').trim();
  }

  function click(selector: string): void {
    const target = query<HTMLElement>(selector);
    if (!target) {
      throw new Error(`Missing element: ${selector}`);
    }
    target.click();
  }

  function setInput(selector: string, value: string): void {
    const input = query<HTMLInputElement>(selector);
    if (!input) {
      throw new Error(`Missing input: ${selector}`);
    }
    input.value = value;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    fixture.detectChanges();
  }

  function setCheckbox(selector: string, checked: boolean): void {
    const input = query<HTMLInputElement>(selector);
    if (!input) {
      throw new Error(`Missing checkbox: ${selector}`);
    }
    input.checked = checked;
    input.dispatchEvent(new Event('change', { bubbles: true }));
    fixture.detectChanges();
  }

  async function open(scans: DuplicateScanState[] = []): Promise<void> {
    wailsSpy.listDuplicateScans.and.resolveTo({
      success: true,
      message: 'scans listed',
      scans,
    });

    fixture = TestBed.createComponent(Duplicates);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  async function addFolder(path: string): Promise<void> {
    wailsSpy.openDirectoryDialog.and.resolveTo(path);
    click('.duplicates-add-folder');
    await fixture.whenStable();
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);

    progressSubject = new Subject<DuplicateScanProgressEvent>();
    stateSubject = new Subject<DuplicateScanState>();

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'listDuplicateScans',
      'startDuplicateScan',
      'getDuplicateScan',
      'cancelDuplicateScan',
      'openDirectoryDialog',
      'revealInFolder',
    ]);

    wailsSpy.listDuplicateScans.and.resolveTo({
      success: true,
      message: '',
      scans: [],
    });
    wailsSpy.getDuplicateScan.and.resolveTo({
      success: false,
      message: 'scan not found',
      found: false,
    });
    wailsSpy.startDuplicateScan.and.resolveTo({
      success: false,
      message: 'not started',
    });
    wailsSpy.cancelDuplicateScan.and.resolveTo({
      success: true,
      message: 'cancellation requested',
    });
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    clipboardSpy = jasmine.createSpyObj<ClipboardService>('ClipboardService', [
      'writeText',
    ]);
    clipboardSpy.writeText.and.resolveTo(true);

    (
      wailsSpy as unknown as { duplicateProgress$: unknown }
    ).duplicateProgress$ = progressSubject.asObservable();
    (wailsSpy as unknown as { duplicateState$: unknown }).duplicateState$ =
      stateSubject.asObservable();

    await TestBed.configureTestingModule({
      imports: [Duplicates],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ClipboardService, useValue: clipboardSpy },
      ],
    }).compileComponents();
  });

  afterEach(() => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
  });

  it('starts from the backend defaults', async () => {
    await open();

    expect(DUPLICATES_DEFAULT_MIN_SIZE_KIB * 1024).toBe(4096);
    expect(DUPLICATES_DEFAULT_MAX_FILES).toBe(20_000);
    expect(component.folders()).toEqual([]);
    expect(component.recursive()).toBeTrue();
    expect(component.minSizeKiB()).toBe('4');
    expect(component.extensionsText()).toBe('');
    expect(component.maxFilesText()).toBe('20000');
    expect(component.unlimitedFiles()).toBeFalse();

    const recursive = query<HTMLInputElement>('#duplicates-recursive');
    const minSize = query<HTMLInputElement>('#duplicates-min-size');
    const maxFiles = query<HTMLInputElement>('#duplicates-max-files');
    expect(recursive?.checked).toBeTrue();
    expect(minSize?.value).toBe('4');
    expect(maxFiles?.value).toBe('20000');
    expect(text('.duplicates-folders-empty')).toContain('No folders yet');
    expect(text('.duplicates-start')).toContain('Start scan');
  });

  it('adds and removes scan folders', async () => {
    await open();

    await addFolder('/tmp/photos');
    await addFolder('/tmp/archive');
    await addFolder('/tmp/photos');

    expect(component.folders()).toEqual(['/tmp/photos', '/tmp/archive']);
    expect(queryAll('.duplicates-folder').length).toBe(2);

    click('.duplicates-remove-folder');
    fixture.detectChanges();

    expect(component.folders()).toEqual(['/tmp/archive']);
    expect(queryAll('.duplicates-folder').length).toBe(1);
    expect(text('.duplicates-folder-path')).toBe('/tmp/archive');
  });

  it('sends the exact request built from the form', async () => {
    await open();
    await addFolder('/tmp/photos');

    setCheckbox('#duplicates-recursive', false);
    setInput('#duplicates-min-size', '8');
    setInput('#duplicates-extensions', 'JPG, .png, jpg');
    setInput('#duplicates-max-files', '100');

    wailsSpy.startDuplicateScan.and.resolveTo({
      success: true,
      message: 'scan started',
      scanId: 'scan_9',
    });
    wailsSpy.getDuplicateScan.and.resolveTo({
      success: true,
      message: 'scan found',
      found: true,
      scan: sampleScan({
        scanId: 'scan_9',
        status: 'running',
        success: false,
        progress: { current: 0, total: 0, stage: 'walking', message: 'Scanning folders' },
      }),
    });

    click('.duplicates-start');
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.startDuplicateScan).toHaveBeenCalledWith({
      directories: ['/tmp/photos'],
      recursive: false,
      minSizeBytes: 8 * 1024,
      extensions: ['jpg', 'png'],
      maxFiles: 100,
    });
    expect(component.activeScanId()).toBe('scan_9');
  });

  it('sends the default size and cap when the form is untouched', async () => {
    await open();
    await addFolder('/tmp/photos');

    wailsSpy.startDuplicateScan.and.resolveTo({
      success: true,
      message: 'scan started',
      scanId: 'scan_9',
    });

    click('.duplicates-start');
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.startDuplicateScan).toHaveBeenCalledWith({
      directories: ['/tmp/photos'],
      recursive: true,
      minSizeBytes: 4096,
      extensions: [],
      maxFiles: 20_000,
    });
  });

  it('sends no cap when the file cap is disabled', async () => {
    await open();
    await addFolder('/tmp/photos');
    setCheckbox('#duplicates-unlimited-files', true);

    wailsSpy.startDuplicateScan.and.resolveTo({
      success: true,
      message: 'scan started',
      scanId: 'scan_9',
    });

    click('.duplicates-start');
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.startDuplicateScan).toHaveBeenCalledWith(
      jasmine.objectContaining({ maxFiles: -1 }),
    );
  });

  it('refuses to start without a folder', async () => {
    await open();

    click('.duplicates-start');
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.startDuplicateScan).not.toHaveBeenCalled();
    expect(text('.banner-danger')).toContain('Add at least one folder');
  });

  it('renders hashing progress from the progress event', async () => {
    await open([
      sampleScan({
        scanId: 'scan_1',
        status: 'running',
        success: false,
        progress: { current: 0, total: 0, stage: 'walking', message: 'Scanning folders' },
      }),
    ]);

    progressSubject.next({
      scanId: 'scan_1',
      kind: 'duplicates',
      status: 'running',
      progress: {
        current: 5,
        total: 10,
        stage: 'hashing',
        message: 'Hashed 5 of 10 files',
      },
    });
    fixture.detectChanges();

    expect(text('.duplicates-progress-percent')).toBe('50%');
    expect(text('.duplicates-progress-detail')).toBe('5 / 10 files hashed');
    expect(text('.duplicates-progress-stage')).toContain('Hashing candidates');
    expect(
      query('.duplicates-progress-track')?.getAttribute('aria-valuenow'),
    ).toBe('50');
  });

  it('ignores progress events from another scan', async () => {
    await open([
      sampleScan({
        scanId: 'scan_1',
        status: 'running',
        success: false,
        progress: { current: 0, total: 0, stage: 'walking', message: '' },
      }),
    ]);

    progressSubject.next({
      scanId: 'scan_other',
      status: 'running',
      progress: { current: 9, total: 10, stage: 'hashing', message: '' },
    });
    fixture.detectChanges();

    expect(query('.duplicates-progress-percent')).toBeNull();
    expect(text('.duplicates-progress-stage')).toContain('Walking folders');
  });

  it('cancels the active scan through the backend', async () => {
    await open([
      sampleScan({
        scanId: 'scan_1',
        status: 'running',
        success: false,
        progress: { current: 1, total: 4, stage: 'hashing', message: '' },
      }),
    ]);

    click('.duplicates-cancel');
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.cancelDuplicateScan).toHaveBeenCalledWith('scan_1');
    expect(text('.banner-warning')).toContain('Cancellation requested');
  });

  it('shows totals, sorted groups and warnings for a finished scan', async () => {
    await open([
      sampleScan({
        scanId: 'scan_1',
        totals: {
          filesScanned: 10,
          filesHashed: 6,
          groups: 2,
          wastedBytes: 3 * 1024 * 1024,
        },
        groups: [
          sampleGroup({
            hash: 'small',
            sizeBytes: 1024,
            count: 2,
            wastedBytes: 1024,
            paths: ['/tmp/a/small-1.bin', '/tmp/a/small-2.bin'],
          }),
          sampleGroup({
            hash: 'large',
            sizeBytes: 2 * 1024 * 1024,
            count: 2,
            wastedBytes: 2 * 1024 * 1024,
            paths: ['/tmp/a/large-1.bin', '/tmp/a/large-2.bin'],
          }),
        ],
        warnings: [
          { path: '/tmp/a/locked.bin', message: 'could not read file: permission denied' },
          { message: 'stopped after the file cap of 20000 files; some files were not scanned' },
        ],
      }),
    ]);

    const stats = queryAll('.duplicates-stat dd').map((entry) =>
      entry.textContent?.trim(),
    );
    expect(stats).toEqual(['10', '6', '2', '3 MB']);

    const names = queryAll('.duplicates-group-name').map((entry) =>
      entry.textContent?.trim(),
    );
    expect(names).toEqual(['large-1.bin', 'small-1.bin']);

    expect(text('.duplicates-group-wasted')).toBe('Wasted: 2 MB');
    expect(queryAll('.duplicates-path').length).toBe(4);

    expect(query('.duplicates-warnings')).not.toBeNull();
    expect(queryAll('.duplicates-warning').length).toBe(2);
    expect(text('.duplicates-warnings')).toContain('/tmp/a/locked.bin');
    expect(text('.duplicates-warnings')).toContain('file cap of 20000');
    // Warnings are informational, never the scan error banner.
    expect(query('.duplicates-warnings .banner-danger')).toBeNull();
  });

  it('shows the empty result when the scan finds no duplicates', async () => {
    await open([sampleScan({ scanId: 'scan_1', groups: [] })]);

    expect(query('.duplicates-results')).not.toBeNull();
    expect(query('.duplicates-empty')).not.toBeNull();
    expect(text('.duplicates-empty')).toContain('No duplicates found');
    expect(queryAll('.duplicates-group').length).toBe(0);
    expect(query('.duplicates-failed')).toBeNull();
  });

  it('shows an error state and no result when the scan fails', async () => {
    await open([
      sampleScan({
        scanId: 'scan_1',
        success: false,
        status: 'failed',
        message: 'duplicates: folder "/nope": no such file or directory',
        groups: [],
      }),
    ]);

    expect(query('.duplicates-failed')).not.toBeNull();
    expect(query('.duplicates-failed')?.getAttribute('role')).toBe('alert');
    expect(text('.duplicates-failed')).toContain('The scan failed');
    expect(text('.duplicates-failed')).toContain('/nope');
    expect(query('.duplicates-results')).toBeNull();
    expect(query('.duplicates-empty')).toBeNull();
  });

  it('reveals and copies a duplicate path, and offers nothing else', async () => {
    await open([
      sampleScan({
        scanId: 'scan_1',
        totals: { filesScanned: 2, filesHashed: 2, groups: 1, wastedBytes: 1024 },
        groups: [sampleGroup()],
      }),
    ]);

    click('.duplicates-reveal');
    await fixture.whenStable();

    expect(wailsSpy.revealInFolder).toHaveBeenCalledWith('/tmp/photos/a.bin');

    click('.duplicates-copy');
    await fixture.whenStable();
    fixture.detectChanges();

    expect(clipboardSpy.writeText).toHaveBeenCalledWith('/tmp/photos/a.bin');
    expect(text('.banner-success')).toContain('Path copied');

    const results = query('.duplicates-results') as HTMLElement;
    const actions = Array.from(
      results.querySelectorAll('button'),
    ) as HTMLButtonElement[];

    expect(actions.length).toBe(4);
    for (const action of actions) {
      expect(action.getAttribute('aria-label') ?? '').toMatch(
        /^(Reveal|Copy path) /,
      );
    }

    const markup = `${results.textContent ?? ''} ${actions
      .map((action) => action.getAttribute('aria-label') ?? '')
      .join(' ')}`;
    expect(markup).not.toMatch(/delete|move|clean/i);

    // The whole page, not just the results, stays read-only: the only
    // non-result buttons add or remove scan folders and start/cancel a scan.
    const pageActions = (
      Array.from(fixture.nativeElement.querySelectorAll('button')) as HTMLElement[]
    )
      .map(
        (button) =>
          `${button.textContent ?? ''} ${button.getAttribute('aria-label') ?? ''}`,
      )
      .join(' ');
    expect(pageActions).not.toMatch(/delete|move|clean/i);
  });

  it('shows an error and claims no success when the clipboard is unavailable', async () => {
    clipboardSpy.writeText.and.resolveTo(false);

    await open([
      sampleScan({
        scanId: 'scan_1',
        totals: { filesScanned: 2, filesHashed: 2, groups: 1, wastedBytes: 1024 },
        groups: [sampleGroup()],
      }),
    ]);

    click('.duplicates-copy');
    await fixture.whenStable();
    fixture.detectChanges();

    expect(clipboardSpy.writeText).toHaveBeenCalledWith('/tmp/photos/a.bin');
    expect(text('.banner-danger')).toContain('The path could not be copied');
    expect(query('.banner-success')).toBeNull();
  });

  it('collapses long path lists behind a show-all control', async () => {
    const paths = [
      '/tmp/photos/1.bin',
      '/tmp/photos/2.bin',
      '/tmp/photos/3.bin',
      '/tmp/photos/4.bin',
      '/tmp/photos/5.bin',
    ];

    await open([
      sampleScan({
        scanId: 'scan_1',
        totals: { filesScanned: 5, filesHashed: 5, groups: 1, wastedBytes: 4096 },
        groups: [
          sampleGroup({ sizeBytes: 1024, count: 5, wastedBytes: 4096, paths }),
        ],
      }),
    ]);

    expect(queryAll('.duplicates-path').length).toBe(3);
    expect(text('.duplicates-toggle-paths')).toBe('Show all 5 paths');

    click('.duplicates-toggle-paths');
    fixture.detectChanges();

    expect(queryAll('.duplicates-path').length).toBe(5);
    expect(text('.duplicates-toggle-paths')).toBe('Show fewer');

    click('.duplicates-toggle-paths');
    fixture.detectChanges();

    expect(queryAll('.duplicates-path').length).toBe(3);
  });

  it('lists previous scans and shows the newest result on open', async () => {
    const now = Date.now();

    await open([
      sampleScan({
        scanId: 'scan_new',
        startedAt: now - 10_000,
        totals: { filesScanned: 4, filesHashed: 2, groups: 1, wastedBytes: 2048 },
        groups: [sampleGroup({ wastedBytes: 2048 })],
      }),
      sampleScan({
        scanId: 'scan_old',
        startedAt: now - 120_000,
        totals: EMPTY_TOTALS,
        groups: [],
      }),
    ]);

    expect(wailsSpy.listDuplicateScans).toHaveBeenCalled();
    expect(queryAll('.duplicates-history-row').length).toBe(2);
    expect(component.activeScanId()).toBe('scan_new');
    expect(queryAll('.duplicates-group').length).toBe(1);

    const rows = queryAll('.duplicates-history-row');
    const olderRow = rows.find((row) =>
      (row.textContent ?? '').includes('No duplicates'),
    );
    olderRow?.click();
    fixture.detectChanges();

    expect(component.activeScanId()).toBe('scan_old');
    expect(query('.duplicates-empty')).not.toBeNull();
    expect(queryAll('.duplicates-group').length).toBe(0);
  });

  it('reports a history failure without inventing a result', async () => {
    wailsSpy.listDuplicateScans.and.resolveTo({
      success: false,
      message: 'wails/runtime error: Cannot POST',
      scans: [],
    });

    fixture = TestBed.createComponent(Duplicates);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(text('.banner-danger')).toContain('Previous scans could not be listed');
    expect(query('.duplicates-results')).toBeNull();
    expect(query('.duplicates-history-list')).toBeNull();
  });
});
