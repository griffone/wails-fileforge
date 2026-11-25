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
});
