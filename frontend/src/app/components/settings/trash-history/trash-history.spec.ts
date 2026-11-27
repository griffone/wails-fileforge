import {
  ComponentFixture,
  TestBed,
  fakeAsync,
  flushMicrotasks,
} from '@angular/core/testing';

import { I18nService } from '../../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../../services/preferences.service';
import {
  JobHistoryEntry,
  JobOutputRecord,
  ListJobHistoryResponse,
  TrashInfoResponse,
  Wails,
} from '../../../services/wails';
import { TrashHistory } from './trash-history';

function output(overrides: Partial<JobOutputRecord> = {}): JobOutputRecord {
  return {
    path: '/tmp/out/a.webp',
    sizeBytes: 1024,
    modifiedAt: 1,
    ...overrides,
  };
}

function historyEntry(
  overrides: Partial<JobHistoryEntry> = {},
): JobHistoryEntry {
  return {
    jobId: 'job-1',
    toolId: 'tool.image.convert',
    createdAt: Date.now() - 1000,
    outputs: [output()],
    ...overrides,
  };
}

function historyResponse(entries: JobHistoryEntry[]): ListJobHistoryResponse {
  return { success: true, message: '', entries };
}

function trashResponse(files = 0, bytes = 0): TrashInfoResponse {
  return { success: true, message: '', files, bytes };
}

describe('TrashHistory', () => {
  let fixture: ComponentFixture<TrashHistory>;
  let component: TrashHistory;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let i18n: I18nService;

  function element(): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function button(selector: string): HTMLButtonElement {
    const found = element().querySelector<HTMLButtonElement>(selector);
    if (!found) {
      throw new Error(`Missing button: ${selector}`);
    }
    return found;
  }

  async function render(
    entries: JobHistoryEntry[],
    trashFiles = 0,
    trashBytes = 0,
  ): Promise<void> {
    wailsSpy.listJobHistory.and.resolveTo(historyResponse(entries));
    wailsSpy.trashInfo.and.resolveTo(trashResponse(trashFiles, trashBytes));
    fixture = TestBed.createComponent(TrashHistory);
    component = fixture.componentInstance;
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ language: 'en' }),
    );

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'listJobHistory',
      'trashInfo',
      'undoJob',
      'restoreJob',
      'emptyTrash',
      'revealInFolder',
    ]);
    wailsSpy.listJobHistory.and.resolveTo(historyResponse([]));
    wailsSpy.trashInfo.and.resolveTo(trashResponse());
    wailsSpy.undoJob.and.resolveTo({
      success: true,
      message: 'moved',
      moved: ['/tmp/out/a.webp'],
      skipped: [],
    });
    wailsSpy.restoreJob.and.resolveTo({
      success: true,
      message: 'restored',
      moved: ['/tmp/out/a.webp'],
      skipped: [],
    });
    wailsSpy.emptyTrash.and.resolveTo({
      success: true,
      message: 'trash emptied',
    });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: 'opened' });

    await TestBed.configureTestingModule({
      imports: [TrashHistory],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    i18n = TestBed.inject(I18nService);
  });

  afterEach(() => {
    fixture?.destroy();
    i18n.setLocale('en');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
  });

  it('shows empty states for an empty history and an empty trash', fakeAsync(() => {
    void render([]);

    expect(element().querySelector('.job-history-empty')?.textContent).toContain(
      'No finished jobs yet.',
    );
    expect(element().querySelector('.trash-empty')?.textContent).toContain(
      'The trash is empty.',
    );
    expect(element().querySelector('.trash-empty-action')).toBeNull();
  }));

  it('lists jobs newest first with a resolved tool name, date, count, size and state', fakeAsync(() => {
    const older = historyEntry({
      jobId: 'job-older',
      toolId: 'tool.pdf.merge',
      createdAt: Date.now() - 3_600_000,
      outputs: [
        output({
          path: '/tmp/out/merged.pdf',
          sizeBytes: 2048,
          trashedPath: '/tmp/trash/job-older/0-merged.pdf',
          undone: true,
        }),
      ],
      undone: true,
    });
    const newer = historyEntry({
      jobId: 'job-newer',
      toolId: 'tool.image.convert',
      createdAt: Date.now() - 1000,
      outputs: [
        output({ path: '/tmp/out/a.webp', sizeBytes: 1024 }),
        output({ path: '/tmp/out/b.webp', sizeBytes: 2048 }),
      ],
    });
    void render([newer, older], 1, 2048);

    const rows = element().querySelectorAll('.job-history-row');
    expect(rows.length).toBe(2);

    const first = rows[0].textContent ?? '';
    expect(first).toContain('Image Convert');
    expect(first).toContain('2 files');
    expect(first).toContain('3 KB');
    expect(first).toContain('Done');
    expect(first).not.toContain('tool.image.convert');

    const second = rows[1].textContent ?? '';
    expect(second).toContain('PDF Merge');
    expect(second).toContain('In trash');
    expect(second).toContain('Restore');

    expect(element().querySelector('.trash-summary')?.textContent).toContain(
      '1 file · 2 KB',
    );
  }));

  it('labels an organize automation move instead of calling it unknown', fakeAsync(() => {
    void render([
      historyEntry({
        jobId: 'auto-1',
        toolId: 'automation.organize',
        outputs: [output({ path: '/tmp/sorted/a.pdf' })],
      }),
    ]);

    const row = element().querySelector('.job-history-row');
    expect(row?.textContent).toContain('Organize files');
    expect(row?.textContent).not.toContain('Unknown tool');
    expect(row?.textContent).not.toContain('automation.organize');
  }));

  it('restores an undone job and refreshes the list', fakeAsync(() => {
    const undone = historyEntry({
      jobId: 'job-restore',
      undone: true,
      outputs: [
        output({
          trashedPath: '/tmp/trash/job-restore/0-a.webp',
          undone: true,
        }),
      ],
    });
    void render([undone], 1, 1024);

    wailsSpy.listJobHistory.and.resolveTo(
      historyResponse([historyEntry({ jobId: 'job-restore' })]),
    );
    wailsSpy.trashInfo.and.resolveTo(trashResponse());

    button('.job-history-restore').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.restoreJob).toHaveBeenCalledOnceWith('job-restore');
    expect(wailsSpy.listJobHistory).toHaveBeenCalledTimes(2);
    expect(wailsSpy.trashInfo).toHaveBeenCalledTimes(2);
    expect(element().querySelector('.trash-history-message')?.textContent).toContain(
      'Outputs restored.',
    );
    expect(element().querySelector('.job-history-restore')).toBeNull();
    expect(element().querySelector('.trash-empty')).not.toBeNull();
  }));

  it('surfaces a failed action in the aria-live region', fakeAsync(() => {
    void render([
      historyEntry({
        jobId: 'job-undo',
        outputs: [output()],
      }),
    ]);

    wailsSpy.undoJob.and.resolveTo({
      success: false,
      message: 'job is not in history',
      moved: [],
      skipped: [],
    });

    button('.job-history-undo').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(element().querySelector('.trash-history-message')?.textContent).toContain(
      'The undo could not be completed.',
    );
  }));

  it('states the backend trash count and size before emptying and supports cancelling', fakeAsync(() => {
    // History reports one 1 KB output, but the trash folder really holds five
    // files: the confirmation must show the backend numbers, not the history.
    void render(
      [
        historyEntry({
          jobId: 'job-trash',
          undone: true,
          outputs: [
            output({
              trashedPath: '/tmp/trash/job-trash/0-a.webp',
              undone: true,
            }),
          ],
        }),
      ],
      5,
      5120,
    );

    button('.trash-empty-action').click();
    fixture.detectChanges();

    const dialog = element().querySelector('.trash-confirm-dialog');
    expect(dialog).not.toBeNull();
    expect(dialog?.getAttribute('aria-modal')).toBe('true');
    expect(dialog?.textContent).toContain('5 files');
    expect(dialog?.textContent).toContain('5 KB');

    button('.trash-confirm-cancel').click();
    fixture.detectChanges();

    expect(wailsSpy.emptyTrash).not.toHaveBeenCalled();
    expect(element().querySelector('.trash-confirm-dialog')).toBeNull();
  }));

  it('closes the confirmation with Escape without emptying', fakeAsync(() => {
    void render(
      [
        historyEntry({
          jobId: 'job-escape',
          undone: true,
          outputs: [
            output({
              trashedPath: '/tmp/trash/job-escape/0-a.webp',
              undone: true,
            }),
          ],
        }),
      ],
      1,
      1024,
    );

    button('.trash-empty-action').click();
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      }),
    );
    fixture.detectChanges();

    expect(element().querySelector('.trash-confirm-dialog')).toBeNull();
    expect(wailsSpy.emptyTrash).not.toHaveBeenCalled();
  }));

  it('empties the trash from the confirmation and refreshes', fakeAsync(() => {
    void render(
      [
        historyEntry({
          jobId: 'job-empty',
          undone: true,
          outputs: [
            output({
              trashedPath: '/tmp/trash/job-empty/0-a.webp',
              undone: true,
            }),
          ],
        }),
      ],
      1,
      1024,
    );

    wailsSpy.listJobHistory.and.resolveTo(
      historyResponse([
        historyEntry({
          jobId: 'job-empty',
          undone: true,
          trashEmptied: true,
          outputs: [output({ undone: true })],
        }),
      ]),
    );
    wailsSpy.trashInfo.and.resolveTo(trashResponse());

    button('.trash-empty-action').click();
    fixture.detectChanges();
    button('.trash-confirm-submit').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.emptyTrash).toHaveBeenCalledTimes(1);
    expect(wailsSpy.listJobHistory).toHaveBeenCalledTimes(2);
    expect(element().querySelector('.trash-history-message')?.textContent).toContain(
      'Trash emptied.',
    );
    expect(element().querySelector('.job-history-row')?.textContent).toContain(
      'Trash emptied',
    );
  }));

  it('renders a distinct error state for a rejected history load and retries', fakeAsync(() => {
    wailsSpy.listJobHistory.and.rejectWith(new Error('ipc down'));
    wailsSpy.trashInfo.and.resolveTo(trashResponse());

    fixture = TestBed.createComponent(TrashHistory);
    component = fixture.componentInstance;
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(element().querySelector('.job-history-error')?.textContent).toContain(
      'Job history could not be loaded.',
    );
    expect(element().querySelector('.job-history-empty')).toBeNull();
    expect(element().querySelector('.job-history-loading')).toBeNull();

    const calls = wailsSpy.listJobHistory.calls.count();
    wailsSpy.listJobHistory.and.resolveTo(historyResponse([]));

    button('.job-history-retry').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.listJobHistory.calls.count()).toBe(calls + 1);
    expect(element().querySelector('.job-history-error')).toBeNull();
    expect(element().querySelector('.job-history-empty')).not.toBeNull();
  }));

  it('shows a trash read error instead of a false empty trash', fakeAsync(() => {
    wailsSpy.listJobHistory.and.resolveTo(historyResponse([]));
    wailsSpy.trashInfo.and.rejectWith(new Error('ipc down'));

    fixture = TestBed.createComponent(TrashHistory);
    component = fixture.componentInstance;
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(element().querySelector('.trash-error')?.textContent).toContain(
      'Trash contents could not be read.',
    );
    expect(element().querySelector('.trash-empty')).toBeNull();
    expect(element().querySelector('.trash-empty-action')).toBeNull();
  }));
});
