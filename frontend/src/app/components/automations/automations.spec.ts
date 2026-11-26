import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { Automations } from './automations';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import {
  AutomationRule,
  AutomationRunEvent,
  AutomationRunRecord,
  AutomationStatusResponse,
  AutomationOrganizePreviewResponse,
  OrganizePlannedMove,
  Recipe,
  Wails,
} from '../../services/wails';

const LAST_RUN_AT = Date.now() - 2 * 60_000;
const HISTORY_RUN_AT = Date.now() - 5 * 60_000;

const EMAIL_RECIPE: Recipe = {
  id: 'recipe.email_pdf',
  name: 'Email PDF',
  description: 'Merge and compress PDFs for email.',
  category: 'documents',
  inputKind: 'pdf',
  acceptExtensions: ['pdf'],
  maxInputs: 0,
  steps: [],
  defaultTargetSizeBytes: 0,
  duplicable: false,
};

function sampleRule(
  overrides: Partial<AutomationRule> = {},
): AutomationRule {
  return {
    id: 'rule_1',
    name: 'Inbox PDFs',
    enabled: true,
    watchDir: '/tmp/inbox',
    recipeId: EMAIL_RECIPE.id,
    extensions: ['pdf'],
    outputDir: '/tmp/out',
    options: { targetSizeBytes: 10_000_000 },
    createdAt: 1,
    lastRunAt: LAST_RUN_AT,
    lastStatus: 'success',
    lastMessage: 'Email PDF ready.',
    lastFile: '/tmp/inbox/a.pdf',
    lastOutputs: ['/tmp/out/a.pdf'],
    runtimeStatus: 'watching',
    ...overrides,
  };
}

function sampleStatus(
  overrides: Partial<AutomationStatusResponse> = {},
): AutomationStatusResponse {
  return {
    success: true,
    message: 'ok',
    totalRules: 1,
    activeRules: 1,
    lastRunAt: LAST_RUN_AT,
    lastStatus: 'success',
    lastMessage: 'Email PDF ready.',
    lastFile: '/tmp/inbox/a.pdf',
    ...overrides,
  };
}

function sampleRun(
  overrides: Partial<AutomationRunRecord> = {},
): AutomationRunRecord {
  return {
    id: 'run_1',
    ruleId: 'rule_1',
    ruleName: 'Inbox PDFs',
    filePath: '/tmp/inbox/history.pdf',
    status: 'success',
    message: 'Compressed for email.',
    outputs: ['/tmp/out/history.pdf'],
    at: HISTORY_RUN_AT,
    ...overrides,
  };
}

/** An organize rule whose exact current settings were previewed. */
function sampleOrganizeRule(
  overrides: Partial<AutomationRule> = {},
): AutomationRule {
  return sampleRule({
    id: 'rule_organize',
    name: 'Sort downloads',
    action: 'organize',
    recipeId: '',
    extensions: ['pdf'],
    outputDir: '',
    targetDir: '/tmp/library',
    folderTemplate: '{year}/{month}',
    nameTemplate: '{name}-{date}{ext}',
    collisionPolicy: 'suffix',
    previewHash: 'preview-hash-1',
    previewedAt: HISTORY_RUN_AT,
    lastMessage: 'Moved to /tmp/library/2026/11/a.pdf.',
    lastOutputs: ['/tmp/library/2026/11/a.pdf'],
    ...overrides,
  });
}

function sampleMoves(): OrganizePlannedMove[] {
  return [
    {
      source: '/tmp/inbox/a.pdf',
      destination: '/tmp/library/2026/11/a.pdf',
      action: 'move',
      sizeBytes: 2048,
    },
    {
      source: '/tmp/inbox/b.pdf',
      destination: '/tmp/library/2026/11/b-2.pdf',
      action: 'move',
      sizeBytes: 1024,
    },
    {
      source: '/tmp/inbox/c.pdf',
      destination: '',
      action: 'skip',
      reason: 'The destination already exists, so the file was left in place.',
      sizeBytes: 10,
    },
  ];
}

function samplePreview(
  overrides: Partial<AutomationOrganizePreviewResponse> = {},
): AutomationOrganizePreviewResponse {
  return {
    success: true,
    message: 'Preview: 2 moves planned, 1 skip.',
    ruleId: 'rule_organize',
    targetDir: '/tmp/library',
    moves: sampleMoves(),
    previewHash: 'preview-hash-1',
    previewStored: true,
    generatedAt: HISTORY_RUN_AT,
    ...overrides,
  };
}

describe('Automations', () => {
  let fixture: ComponentFixture<Automations>;
  let component: Automations;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let automationRunSubject: Subject<AutomationRunEvent>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
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

  function setSelect(selector: string, value: string): void {
    const select = query<HTMLSelectElement>(selector);
    if (!select) {
      throw new Error(`Missing select: ${selector}`);
    }
    select.value = value;
    select.dispatchEvent(new Event('change', { bubbles: true }));
    fixture.detectChanges();
  }

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'listAutomationRules',
      'listRecipes',
      'getAutomationStatus',
      'listAutomationRuns',
      'clearAutomationRuns',
      'saveAutomationRule',
      'deleteAutomationRule',
      'setAutomationRuleEnabled',
      'previewAutomationRule',
      'openDirectoryDialog',
    ]);

    wailsSpy.listRecipes.and.resolveTo({
      success: true,
      message: 'ok',
      recipes: [EMAIL_RECIPE],
    });
    wailsSpy.listAutomationRules.and.resolveTo({
      success: true,
      message: 'ok',
      rules: [sampleRule()],
    });
    wailsSpy.getAutomationStatus.and.resolveTo(sampleStatus());
    wailsSpy.listAutomationRuns.and.resolveTo({
      success: true,
      message: 'ok',
      runs: [sampleRun()],
    });
    wailsSpy.clearAutomationRuns.and.resolveTo({
      success: true,
      message: 'Automation run history cleared.',
    });
    wailsSpy.saveAutomationRule.and.resolveTo({
      success: true,
      message: 'Automation rule saved.',
      rule: sampleRule(),
    });
    wailsSpy.deleteAutomationRule.and.resolveTo({
      success: true,
      message: 'Automation rule deleted.',
    });
    wailsSpy.setAutomationRuleEnabled.and.resolveTo({
      success: true,
      message: 'Automation rule disabled.',
      rule: sampleRule({ enabled: false, runtimeStatus: 'stopped' }),
    });
    wailsSpy.previewAutomationRule.and.resolveTo(samplePreview());
    wailsSpy.openDirectoryDialog.and.resolveTo('');

    automationRunSubject = new Subject<AutomationRunEvent>();
    (
      wailsSpy as unknown as { automationRun$: unknown }
    ).automationRun$ = automationRunSubject.asObservable();

    await TestBed.configureTestingModule({
      imports: [Automations],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(Automations);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
  });

  it('loads recipes, rules and status and renders a rule', () => {
    expect(component.recipes().length).toBe(1);
    expect(component.rules().length).toBe(1);
    expect(component.status()?.totalRules).toBe(1);

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Inbox PDFs');
    expect(text).toContain('/tmp/inbox');
    expect(text).toContain('Email PDF');
    expect(text).toContain('Watching');
    expect(text).toContain('2 min ago');
    expect(text).toContain('Success');
    expect(text).toContain('PDF');
    expect(text).toContain('/tmp/out');
    expect(text).toContain('a.pdf');
  });

  it('shows the empty state and opens the create form', async () => {
    wailsSpy.listAutomationRules.and.resolveTo({
      success: true,
      message: 'No automation rules yet.',
      rules: [],
    });
    wailsSpy.getAutomationStatus.and.resolveTo(
      sampleStatus({ totalRules: 0, activeRules: 0, lastRunAt: undefined }),
    );

    const emptyFixture = TestBed.createComponent(Automations);
    emptyFixture.detectChanges();
    await emptyFixture.whenStable();
    emptyFixture.detectChanges();

    const empty = emptyFixture.nativeElement.querySelector(
      '.empty-state',
    ) as HTMLElement | null;
    expect(empty).not.toBeNull();
    expect(empty?.textContent).toContain('No folder rules yet');

    const action = empty?.querySelector(
      '.empty-state-action',
    ) as HTMLButtonElement | null;
    action?.click();
    emptyFixture.detectChanges();

    expect(
      emptyFixture.nativeElement.querySelector('.automation-form'),
    ).not.toBeNull();
    expect(emptyFixture.nativeElement.textContent).toContain('New rule');

    emptyFixture.destroy();
  });

  it('blocks save when the name or watch folder is missing', fakeAsync(() => {
    click('.automation-new-rule');
    fixture.detectChanges();

    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.saveAutomationRule).not.toHaveBeenCalled();
    expect(query('.automation-form-error')?.textContent).toContain('name');

    setInput('#automation-name', 'Inbox PDFs');
    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.saveAutomationRule).not.toHaveBeenCalled();
    expect(query('.automation-form-error')?.textContent).toContain(
      'folder to watch',
    );
  }));

  it('saves the parsed payload and refreshes the list', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.returnValues(
      Promise.resolve(' /tmp/watch '),
      Promise.resolve('/tmp/out'),
    );
    wailsSpy.saveAutomationRule.and.resolveTo({
      success: true,
      message: 'Automation rule saved.',
      rule: sampleRule({
        id: 'rule_2',
        name: 'Inbox PDFs',
        watchDir: '/tmp/watch',
      }),
    });
    wailsSpy.listAutomationRules.and.resolveTo({
      success: true,
      message: 'ok',
      rules: [sampleRule({ id: 'rule_2', name: 'Inbox PDFs' })],
    });

    click('.automation-new-rule');
    fixture.detectChanges();

    setInput('#automation-name', 'Inbox PDFs');

    click('.automation-watch-folder');
    flushMicrotasks();
    fixture.detectChanges();

    click('.automation-output-folder');
    flushMicrotasks();
    fixture.detectChanges();

    setInput('#automation-extensions', ' PDF, .jpg  png ');

    click('#automation-delete-source');
    flushMicrotasks();
    fixture.detectChanges();

    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.saveAutomationRule).toHaveBeenCalledTimes(1);
    const payload = wailsSpy.saveAutomationRule.calls.mostRecent()
      .args[0] as AutomationRule;
    expect(payload.name).toBe('Inbox PDFs');
    expect(payload.watchDir).toBe('/tmp/watch');
    expect(payload.recipeId).toBe(EMAIL_RECIPE.id);
    expect(payload.extensions).toEqual(['pdf', 'jpg', 'png']);
    expect(payload.outputDir).toBe('/tmp/out');
    expect(payload.deleteSource).toBeTrue();
    expect(payload.enabled).toBeTrue();

    expect(wailsSpy.listAutomationRules).toHaveBeenCalledTimes(2);
    expect(query('.automation-form')).toBeNull();
    expect(fixture.nativeElement.textContent).toContain(
      'Automation rule saved.',
    );
  }));

  it('saves recursion and patterns and renders them when set', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/watch');
    wailsSpy.listAutomationRules.and.resolveTo({
      success: true,
      message: 'ok',
      rules: [
        sampleRule({
          id: 'rule_2',
          recursive: true,
          patterns: ['invoice-*.pdf'],
        }),
      ],
    });

    click('.automation-new-rule');
    fixture.detectChanges();

    setInput('#automation-name', 'Inbox PDFs');
    click('.automation-watch-folder');
    flushMicrotasks();
    fixture.detectChanges();

    click('#automation-recursive');
    flushMicrotasks();
    fixture.detectChanges();

    setInput('#automation-patterns', ' invoice-*.pdf, report-*.pdf ');
    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();

    const payload = wailsSpy.saveAutomationRule.calls.mostRecent()
      .args[0] as AutomationRule;
    expect(payload.recursive).toBeTrue();
    expect(payload.patterns).toEqual(['invoice-*.pdf', 'report-*.pdf']);

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Include subfolders');
    expect(text).toContain('Included');
    expect(text).toContain('File name patterns');
    expect(text).toContain('invoice-*.pdf');
  }));

  it('shows inline feedback and blocks folder-scoped patterns', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/watch');

    click('.automation-new-rule');
    fixture.detectChanges();

    setInput('#automation-name', 'Inbox PDFs');
    click('.automation-watch-folder');
    flushMicrotasks();
    fixture.detectChanges();

    setInput('#automation-patterns', 'nested/*.pdf');
    fixture.detectChanges();

    expect(query('.automation-patterns-error')?.textContent).toContain(
      'file name only',
    );

    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.saveAutomationRule).not.toHaveBeenCalled();

    setInput('#automation-patterns', 'invoice-[12.pdf');
    fixture.detectChanges();

    expect(query('.automation-patterns-error')?.textContent).toContain(
      'is not valid',
    );
    expect(query('#automation-patterns')?.getAttribute('aria-invalid')).toBe(
      'true',
    );
  }));

  it('renders existing rules without recursion or pattern rows', () => {
    const text = fixture.nativeElement.textContent as string;

    expect(text).not.toContain('Include subfolders');
    expect(text).not.toContain('File name patterns');
    expect(fixture.nativeElement.querySelectorAll('.automation-meta-row').length)
      .toBe(4);
  });

  it('preserves id, createdAt, options and last-run fields when editing', fakeAsync(() => {
    click('.automation-edit');
    fixture.detectChanges();

    setInput('#automation-name', 'Inbox renamed');
    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.saveAutomationRule).toHaveBeenCalledTimes(1);
    const payload = wailsSpy.saveAutomationRule.calls.mostRecent()
      .args[0] as AutomationRule;
    expect(payload.id).toBe('rule_1');
    expect(payload.createdAt).toBe(1);
    expect(payload.options).toEqual({ targetSizeBytes: 10_000_000 });
    expect(payload.lastRunAt).toBe(LAST_RUN_AT);
    expect(payload.lastStatus).toBe('success');
    expect(payload.lastOutputs).toEqual(['/tmp/out/a.pdf']);
    expect(payload.name).toBe('Inbox renamed');
  }));

  it('toggles a rule and deletes only after confirmation', fakeAsync(() => {
    click('.automation-toggle');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.setAutomationRuleEnabled).toHaveBeenCalledWith(
      'rule_1',
      false,
    );
    expect(fixture.nativeElement.textContent).toContain(
      'Automation rule disabled.',
    );

    click('.automation-delete');
    fixture.detectChanges();

    expect(wailsSpy.deleteAutomationRule).not.toHaveBeenCalled();
    expect(query('.automation-confirm')).not.toBeNull();

    click('.automation-confirm-cancel');
    fixture.detectChanges();

    expect(query('.automation-confirm')).toBeNull();
    expect(wailsSpy.deleteAutomationRule).not.toHaveBeenCalled();

    click('.automation-delete');
    fixture.detectChanges();
    click('.automation-confirm-delete');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.deleteAutomationRule).toHaveBeenCalledWith('rule_1');
    expect(fixture.nativeElement.textContent).toContain(
      'Automation rule deleted.',
    );
  }));

  it('applies a live run event to the matching rule only', fakeAsync(() => {
    const other = sampleRule({
      id: 'rule_2',
      name: 'Drop images',
      watchDir: '/tmp/drop',
      enabled: false,
      runtimeStatus: 'stopped',
      lastMessage: 'Still the old message.',
    });
    component.rules.set([sampleRule(), other]);
    fixture.detectChanges();

    automationRunSubject.next({
      ruleId: 'rule_1',
      ruleName: 'Inbox PDFs',
      filePath: '/tmp/inbox/late.pdf',
      status: 'failed',
      message: 'Ghostscript is missing.',
      outputs: [],
      at: LAST_RUN_AT + 5_000,
    });
    fixture.detectChanges();

    expect(component.rules()[0].lastStatus).toBe('failed');
    expect(component.rules()[0].lastMessage).toBe('Ghostscript is missing.');
    expect(component.rules()[0].lastFile).toBe('/tmp/inbox/late.pdf');
    expect(component.rules()[0].lastRunAt).toBe(LAST_RUN_AT + 5_000);
    expect(component.rules()[1].lastStatus).toBe('success');
    expect(component.rules()[1].lastMessage).toBe('Still the old message.');
    expect(component.status()?.lastStatus).toBe('failed');

    automationRunSubject.next({
      ruleId: 'unknown_rule',
      ruleName: '',
      filePath: '',
      status: 'success',
      message: 'Not ours.',
      outputs: [],
      at: 0,
    });
    fixture.detectChanges();

    expect(component.rules()[0].lastStatus).toBe('failed');
    expect(fixture.nativeElement.textContent).not.toContain('Not ours.');
  }));

  it('renders persisted recent runs', () => {
    const text = fixture.nativeElement.textContent as string;

    expect(text).toContain('Recent runs');
    expect(text).toContain('Inbox PDFs');
    expect(text).toContain('history.pdf');
    expect(text).toContain('Success');
    expect(text).toContain('Compressed for email.');
    expect(text).toContain('5 min ago');
    expect(text).toContain('1 file');
    expect(wailsSpy.listAutomationRuns).toHaveBeenCalledWith(20);
  });

  it('prepends terminal live runs and ignores running events', fakeAsync(() => {
    automationRunSubject.next({
      ruleId: 'rule_1',
      ruleName: 'Inbox PDFs',
      filePath: '/tmp/inbox/live.pdf',
      status: 'failed',
      message: 'Ghostscript is missing.',
      outputs: [],
      at: LAST_RUN_AT + 5_000,
    });
    fixture.detectChanges();

    expect(component.runs().length).toBe(2);
    expect(component.runs()[0].id).toBe(`live-${LAST_RUN_AT + 5_000}-rule_1`);
    expect(component.runs()[0].filePath).toBe('/tmp/inbox/live.pdf');
    expect(component.runs()[0].status).toBe('failed');
    expect(fixture.nativeElement.textContent).toContain('live.pdf');

    automationRunSubject.next({
      ruleId: 'rule_1',
      ruleName: 'Inbox PDFs',
      filePath: '/tmp/inbox/running.pdf',
      status: 'running',
      message: 'Working…',
      outputs: [],
      at: LAST_RUN_AT + 6_000,
    });
    fixture.detectChanges();

    expect(component.runs().length).toBe(2);
    expect(component.runs()[0].status).toBe('failed');
    expect(component.runs().some((run) => run.status === 'running')).toBeFalse();
  }));

  it('clears the run history only after confirmation', fakeAsync(() => {
    click('.automation-clear-runs');
    fixture.detectChanges();

    expect(wailsSpy.clearAutomationRuns).not.toHaveBeenCalled();
    expect(query('.automation-confirm-clear-runs')).not.toBeNull();

    click('.automation-cancel-clear-runs');
    fixture.detectChanges();

    expect(query('.automation-confirm-clear-runs')).toBeNull();
    expect(wailsSpy.clearAutomationRuns).not.toHaveBeenCalled();

    click('.automation-clear-runs');
    fixture.detectChanges();
    click('.automation-confirm-clear-runs');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.clearAutomationRuns).toHaveBeenCalledTimes(1);
    expect(component.runs().length).toBe(0);
    expect(fixture.nativeElement.textContent).toContain(
      'No automation runs yet.',
    );
    expect(fixture.nativeElement.textContent).toContain(
      'Automation run history cleared.',
    );
  }));

  it('renders the page, rule status and validation copy in Spanish', fakeAsync(() => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    expect(query('.page-title')?.textContent).toContain('Automatizaciones');
    expect(fixture.nativeElement.textContent).toContain('Vigilando');

    click('.automation-new-rule');
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Incluir subcarpetas');
    expect(fixture.nativeElement.textContent).toContain(
      'Patrones de nombre de archivo',
    );
    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.saveAutomationRule).not.toHaveBeenCalled();
    expect(query('.automation-form-error')?.textContent).toContain(
      'Dale un nombre a la regla.',
    );
  }));

  it('switches the editor between the recipe and organize actions', () => {
    click('.automation-new-rule');
    fixture.detectChanges();

    expect(query('#automation-recipe')).not.toBeNull();
    expect(query('#automation-folder-template')).toBeNull();
    expect(query('.automation-target-path')).toBeNull();

    setSelect('#automation-action', 'organize');

    expect(component.formAction()).toBe('organize');
    expect(query('#automation-recipe')).toBeNull();
    expect(query('.automation-target-folder')).not.toBeNull();
    expect(query('#automation-folder-template')).not.toBeNull();
    expect(query('#automation-name-template')).not.toBeNull();
    expect(query('.automation-collision')).not.toBeNull();
    expect(query('#automation-delete-source')).toBeNull();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Available tokens');
    expect(text).toContain('{year}');
    expect(text).toContain('{title}');
    expect(text).toContain('Four-digit year of the file date.');
    expect(text).toContain('Document title when the format has one');

    setSelect('#automation-action', 'recipe');

    expect(query('#automation-recipe')).not.toBeNull();
    expect(query('#automation-folder-template')).toBeNull();
  });

  it('loads an existing rule as process with its fields untouched', () => {
    click('.automation-edit');
    fixture.detectChanges();

    expect(component.formAction()).toBe('recipe');
    expect(component.formRecipeId()).toBe(EMAIL_RECIPE.id);
    expect(component.formOutputDir()).toBe('/tmp/out');
    expect(component.formDeleteSource()).toBeFalse();
    expect(component.formTargetDir()).toBe('');
    expect(component.formFolderTemplate()).toBe('');
    expect(component.formNameTemplate()).toBe('');
    expect(component.formCollisionPolicy()).toBe('skip');
  });

  it('names the offending token and blocks save on an invalid template', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/library');

    click('.automation-new-rule');
    fixture.detectChanges();
    setInput('#automation-name', 'Sort downloads');
    click('.automation-watch-folder');
    flushMicrotasks();
    fixture.detectChanges();

    setSelect('#automation-action', 'organize');
    click('.automation-target-folder');
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.formTargetDir()).toBe('/tmp/library');

    setInput('#automation-folder-template', '{year}/{mont}');

    const folderError = query('.automation-folder-template-error');
    expect(folderError?.textContent).toContain('mont');
    expect(folderError?.textContent).toContain('Available tokens');
    expect(
      query('#automation-folder-template')?.getAttribute('aria-invalid'),
    ).toBe('true');

    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();
    expect(wailsSpy.saveAutomationRule).not.toHaveBeenCalled();

    setInput('#automation-folder-template', '{year}/{month}');
    setInput('#automation-name-template', '{name}/x');

    expect(query('.automation-name-template-error')?.textContent).toContain(
      'folder separator',
    );

    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();
    expect(wailsSpy.saveAutomationRule).not.toHaveBeenCalled();

    setInput('#automation-name-template', '{naem}');
    expect(query('.automation-name-template-error')?.textContent).toContain(
      'naem',
    );

    setInput('#automation-name-template', '');
    setInput('#automation-folder-template', '..');
    expect(query('.automation-folder-template-error')?.textContent).toContain(
      'cannot contain',
    );
  }));

  it('blocks saving an organize rule without a target folder', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/inbox');

    click('.automation-new-rule');
    fixture.detectChanges();
    setInput('#automation-name', 'Sort downloads');

    setSelect('#automation-action', 'organize');
    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();
    expect(wailsSpy.saveAutomationRule).not.toHaveBeenCalled();
    expect(query('.automation-form-error')?.textContent).toContain(
      'folder to watch',
    );

    click('.automation-watch-folder');
    flushMicrotasks();
    fixture.detectChanges();

    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();
    expect(wailsSpy.saveAutomationRule).not.toHaveBeenCalled();
    expect(query('.automation-form-error')?.textContent).toContain(
      'target folder',
    );
    expect(query('.automation-form-error')?.textContent).not.toContain(
      'recipe',
    );
  }));

  it('renders the planned moves, skip reasons and totals', fakeAsync(() => {
    component.rules.set([sampleOrganizeRule({ enabled: false })]);
    fixture.detectChanges();

    click('.automation-edit');
    fixture.detectChanges();

    // The saved rule carries a preview of exactly these settings, so the gate
    // is already open without previewing again.
    expect(
      (query<HTMLInputElement>('#automation-enabled') as HTMLInputElement)
        .disabled,
    ).toBeFalse();
    expect(query('.automation-enable-gate')).toBeNull();

    click('.automation-preview');
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.previewAutomationRule).toHaveBeenCalledTimes(1);
    const payload = wailsSpy.previewAutomationRule.calls.mostRecent()
      .args[0] as AutomationRule;
    expect(payload.action).toBe('organize');
    expect(payload.targetDir).toBe('/tmp/library');
    expect(payload.folderTemplate).toBe('{year}/{month}');
    expect(payload.nameTemplate).toBe('{name}-{date}{ext}');
    expect(payload.collisionPolicy).toBe('suffix');

    const rows = fixture.nativeElement.querySelectorAll('.organize-preview-row');
    expect(rows.length).toBe(3);

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('/tmp/inbox/a.pdf');
    expect(text).toContain('/tmp/library/2026/11/a.pdf');
    expect(text).toContain(
      'The destination already exists, so the file was left in place.',
    );
    expect(text).toContain('2 moves planned');
    expect(text).toContain('1 skipped');
    expect(query('.automation-preview-size')?.textContent).toContain('3 KB');
    expect(query('.automation-preview-stored')).not.toBeNull();

    const badges = Array.from(
      fixture.nativeElement.querySelectorAll(
        '.organize-preview-row .badge',
      ) as NodeListOf<HTMLElement>,
    ).map((badge) => (badge.textContent ?? '').trim());
    expect(badges).toEqual(['Move', 'Suffix', 'Skip']);
  }));

  it('caps the preview list and shows every move on request', fakeAsync(() => {
    const moves: OrganizePlannedMove[] = Array.from({ length: 25 }, (_, index) => ({
      source: `/tmp/inbox/file-${index}.pdf`,
      destination: `/tmp/library/file-${index}.pdf`,
      action: 'move',
      sizeBytes: 100,
    }));
    wailsSpy.previewAutomationRule.and.resolveTo(samplePreview({ moves }));

    component.rules.set([sampleOrganizeRule({ enabled: false })]);
    fixture.detectChanges();
    click('.automation-edit');
    fixture.detectChanges();
    click('.automation-preview');
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      fixture.nativeElement.querySelectorAll('.organize-preview-row').length,
    ).toBe(20);
    expect(query('.automation-preview-toggle')?.textContent).toContain(
      'Show all 25',
    );

    click('.automation-preview-toggle');
    fixture.detectChanges();

    expect(
      fixture.nativeElement.querySelectorAll('.organize-preview-row').length,
    ).toBe(25);
    expect(query('.automation-preview-toggle')?.textContent).toContain(
      'Show fewer',
    );
    expect(query('.automation-preview-planned')?.textContent).toContain(
      '25 moves planned',
    );
  }));

  it('invalidates the preview when a relevant field changes', fakeAsync(() => {
    component.rules.set([sampleOrganizeRule({ enabled: false })]);
    fixture.detectChanges();
    click('.automation-edit');
    fixture.detectChanges();
    click('.automation-preview');
    flushMicrotasks();
    fixture.detectChanges();

    const enabled = (): HTMLInputElement =>
      query<HTMLInputElement>('#automation-enabled') as HTMLInputElement;

    expect(enabled().disabled).toBeFalse();
    expect(query('.organize-preview-row')).not.toBeNull();

    setInput('#automation-folder-template', '{year}');

    expect(query('.automation-preview-stale')).not.toBeNull();
    expect(query('.organize-preview-row')).toBeNull();
    expect(enabled().disabled).toBeTrue();
    expect(query('.automation-enable-gate')?.textContent).toContain(
      'changed after the last preview',
    );

    click('.automation-preview');
    flushMicrotasks();
    fixture.detectChanges();

    expect(enabled().disabled).toBeFalse();
    expect(query('.automation-preview-stale')).toBeNull();
  }));

  it('keeps the editor open when the name changes without a re-preview', fakeAsync(() => {
    component.rules.set([sampleOrganizeRule({ enabled: false })]);
    fixture.detectChanges();
    click('.automation-edit');
    fixture.detectChanges();
    click('.automation-preview');
    flushMicrotasks();
    fixture.detectChanges();

    // The name is not part of the preview fingerprint, so it never invalidates.
    setInput('#automation-name', 'Sort everything');

    expect(query('.automation-preview-stale')).toBeNull();
    expect(
      (query<HTMLInputElement>('#automation-enabled') as HTMLInputElement)
        .disabled,
    ).toBeFalse();
  }));

  it('gates enabling on a preview of the exact settings', fakeAsync(() => {
    component.rules.set([
      sampleOrganizeRule({
        enabled: false,
        previewHash: '',
        previewedAt: 0,
        runtimeStatus: 'stopped',
      }),
    ]);
    fixture.detectChanges();

    const toggle = (): HTMLButtonElement =>
      query<HTMLButtonElement>('.automation-toggle') as HTMLButtonElement;

    expect(toggle().disabled).toBeTrue();
    expect(query('.automation-rule-preview-needed')).not.toBeNull();
    expect(query('.automation-rule-gate')?.textContent).toContain(
      'preview of the current settings',
    );

    click('.automation-edit');
    fixture.detectChanges();

    expect(
      (query<HTMLInputElement>('#automation-enabled') as HTMLInputElement)
        .disabled,
    ).toBeTrue();
    expect(query('.automation-enable-gate')).not.toBeNull();

    click('.automation-preview');
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.previewStored()).toBeTrue();
    expect(
      (query<HTMLInputElement>('#automation-enabled') as HTMLInputElement)
        .disabled,
    ).toBeFalse();
    expect(query('.automation-preview-stored')?.textContent).toContain(
      'can be enabled',
    );

    click('#automation-enabled');
    fixture.detectChanges();
    expect(component.formEnabled()).toBeTrue();

    setInput('#automation-name-template', '{name}{ext}');

    expect(
      (query<HTMLInputElement>('#automation-enabled') as HTMLInputElement)
        .disabled,
    ).toBeTrue();

    click('.automation-save');
    flushMicrotasks();
    fixture.detectChanges();

    const payload = wailsSpy.saveAutomationRule.calls.mostRecent()
      .args[0] as AutomationRule;
    expect(payload.enabled).toBeFalse();
    expect(payload.nameTemplate).toBe('{name}{ext}');
  }));

  it('does not open the gate for a preview of unsaved settings', fakeAsync(() => {
    wailsSpy.previewAutomationRule.and.resolveTo(
      samplePreview({ ruleId: '', previewStored: false }),
    );
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/library');

    click('.automation-new-rule');
    fixture.detectChanges();
    setInput('#automation-name', 'Fresh organize');
    click('.automation-watch-folder');
    flushMicrotasks();
    fixture.detectChanges();

    setSelect('#automation-action', 'organize');
    click('.automation-target-folder');
    flushMicrotasks();
    fixture.detectChanges();

    click('.automation-preview');
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.previewStored()).toBeFalse();
    expect(query('.automation-preview-not-stored')).not.toBeNull();
    expect(
      (query<HTMLInputElement>('#automation-enabled') as HTMLInputElement)
        .disabled,
    ).toBeTrue();
    expect(query('.automation-enable-gate')?.textContent).toContain(
      'Save the rule',
    );
    // A preview the server did not store never refreshes the saved list.
    expect(wailsSpy.listAutomationRules).toHaveBeenCalledTimes(1);
  }));

  it('renders an organize rule card with its target and templates', () => {
    component.rules.set([sampleOrganizeRule()]);
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Organize');
    expect(text).toContain('/tmp/library');
    expect(text).toContain('{year}/{month}');
    expect(text).toContain('{name}-{date}{ext}');
    expect(text).toContain('Suffix on collision');
    expect(text).toContain('PDF');

    expect(query('.automation-rule-summary')).not.toBeNull();
    expect(query('.automation-rule-preview-needed')).toBeNull();
    expect(
      fixture.nativeElement.querySelectorAll('.automation-meta-row').length,
    ).toBe(6);
  });

  it('renders history rows that carry the synthetic organize tool id', () => {
    component.runs.set([
      sampleRun({
        id: 'run_organize',
        ruleId: 'automation.organize',
        ruleName: 'automation.organize',
        filePath: '/tmp/library/2026/11/a.pdf',
        message: 'Moved to /tmp/library/2026/11/a.pdf.',
        outputs: undefined,
        at: HISTORY_RUN_AT,
      }),
    ]);
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Organize files');
    expect(text).not.toContain('automation.organize');
    expect(text).toContain('a.pdf');
    expect(text).toContain('No outputs');
  });

  it('renders the organize copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    click('.automation-new-rule');
    fixture.detectChanges();
    setSelect('#automation-action', 'organize');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Acción');
    expect(text).toContain('Organizar archivos');
    expect(text).toContain('Carpeta destino');
    expect(text).toContain('Plantilla de carpetas');
    expect(text).toContain('Plantilla de nombre');
    expect(text).toContain('Tokens disponibles');
    expect(text).toContain('Vista previa');
    expect(text).toContain('Ver movimientos');
    expect(text).toContain('Guarda esta regla una vez');
  });
});
