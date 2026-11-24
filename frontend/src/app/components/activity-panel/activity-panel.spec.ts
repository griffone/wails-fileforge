import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { WritableSignal, computed, signal } from '@angular/core';

import { ActivityPanel } from './activity-panel';
import { ActivityService, type ActivityTask } from '../../services/activity.service';
import { I18nService } from '../../services/i18n.service';

function activityTask(overrides: Partial<ActivityTask> = {}): ActivityTask {
  return {
    id: 'job-1',
    kind: 'tool',
    title: 'PDF Merge',
    route: '/pdf-merge',
    toolId: 'tool.pdf.merge',
    status: 'running',
    stage: 'processing',
    message: '',
    current: 0,
    total: 0,
    percent: null,
    startedAt: Date.now() - 5000,
    endedAt: null,
    etaSeconds: null,
    inputCount: 0,
    outputCount: 0,
    errorMessage: '',
    ...overrides,
  };
}

describe('ActivityPanel', () => {
  let fixture: ComponentFixture<ActivityPanel>;
  let router: Router;
  let panelOpen: WritableSignal<boolean>;
  let activeTasks: WritableSignal<readonly ActivityTask[]>;
  let recentTasks: WritableSignal<readonly ActivityTask[]>;
  let closePanel: jasmine.Spy;
  let cancelTask: jasmine.Spy;
  let dismissTask: jasmine.Spy;
  let clearFinished: jasmine.Spy;

  beforeEach(async () => {
    localStorage.setItem('ff-preferences', JSON.stringify({ language: 'en' }));
    panelOpen = signal(false);
    activeTasks = signal<readonly ActivityTask[]>([]);
    recentTasks = signal<readonly ActivityTask[]>([]);

    closePanel = jasmine
      .createSpy('closePanel')
      .and.callFake(() => panelOpen.set(false));
    cancelTask = jasmine.createSpy('cancelTask').and.resolveTo('');
    dismissTask = jasmine.createSpy('dismissTask');
    clearFinished = jasmine.createSpy('clearFinished');

    const activity = {
      tasks: computed(() => [...activeTasks(), ...recentTasks()]),
      activeTasks: activeTasks.asReadonly(),
      recentTasks: recentTasks.asReadonly(),
      runningCount: computed(() => activeTasks().length),
      panelOpen: panelOpen.asReadonly(),
      openPanel: jasmine.createSpy('openPanel').and.callFake(() => panelOpen.set(true)),
      closePanel,
      togglePanel: jasmine.createSpy('togglePanel'),
      activeJobForTool: jasmine.createSpy('activeJobForTool').and.returnValue(null),
      activeRecipeFor: jasmine.createSpy('activeRecipeFor').and.returnValue(null),
      refresh: jasmine.createSpy('refresh').and.resolveTo(),
      cancelTask,
      dismissTask,
      clearFinished,
    };

    await TestBed.configureTestingModule({
      imports: [ActivityPanel],
      providers: [
        provideRouter([]),
        { provide: ActivityService, useValue: activity },
      ],
    }).compileComponents();

    router = TestBed.inject(Router);
    fixture = TestBed.createComponent(ActivityPanel);
    fixture.detectChanges();
  });

  afterEach(() => {
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    fixture.destroy();
  });

  function open(): void {
    panelOpen.set(true);
    fixture.detectChanges();
  }

  function text(selector: string): string {
    return (
      (fixture.nativeElement.querySelector(selector) as HTMLElement | null)
        ?.textContent?.trim() ?? ''
    );
  }

  function titles(): string[] {
    return Array.from(
      fixture.nativeElement.querySelectorAll(
        '.activity-task-title',
      ) as NodeListOf<HTMLElement>,
    ).map((element) => element.textContent?.trim() ?? '');
  }

  it('renders nothing while the panel is closed', () => {
    expect(fixture.nativeElement.querySelector('.activity-drawer')).toBeNull();
    expect(fixture.nativeElement.querySelector('.activity-backdrop')).toBeNull();
  });

  it('renders running and recent tasks with percent text', () => {
    activeTasks.set([activityTask({ id: 'job-active', percent: 42 })]);
    recentTasks.set([
      activityTask({
        id: 'job-done',
        title: 'Image Convert',
        toolId: 'tool.image.convert',
        route: '/image-converter',
        status: 'success',
        stage: 'done',
        percent: 100,
        endedAt: Date.now(),
      }),
    ]);

    open();

    expect(titles()).toEqual(['PDF Merge', 'Image Convert']);
    expect(text('.activity-percent')).toBe('42%');
  });

  it('shows both empty states when no tasks exist', () => {
    open();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('.activity-drawer')).toBeTruthy();
    expect(compiled.textContent).toContain('Nothing is running.');
    expect(compiled.textContent).toContain('Finished tasks will appear here.');
    expect(text('.activity-eyebrow')).toBe('Background work');
    expect(text('.activity-title')).toContain('Activity');
    expect(text('#activity-running-title')).toBe('Running');
    expect(text('#activity-recent-title')).toBe('Recent');
    expect(text('.activity-clear')).toBe('Clear finished');
    expect(
      (compiled.querySelector('.activity-clear') as HTMLButtonElement).disabled,
    ).toBeTrue();
  });

  it('renders the drawer copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    open();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(text('.activity-eyebrow')).toBe('Trabajo en segundo plano');
    expect(text('.activity-title')).toContain('Actividad');
    expect(text('.activity-title')).toContain('0 en curso');
    expect(text('#activity-running-title')).toBe('En curso');
    expect(text('#activity-recent-title')).toBe('Recientes');
    expect(text('.activity-clear')).toBe('Limpiar finalizadas');
    expect(compiled.textContent).toContain('No hay nada en curso.');
    expect(compiled.textContent).toContain(
      'Las tareas finalizadas aparecerán aquí.',
    );
  });

  it('translates task labels, progress and actions in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    activeTasks.set([
      activityTask({
        id: 'job-es',
        title: 'Unir PDF',
        status: 'processing',
        stage: 'processing',
        percent: 40,
        etaSeconds: 5,
        inputCount: 3,
      }),
    ]);
    open();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(text('.activity-task-kind')).toBe('Herramienta');
    expect(text('.activity-task-stage')).toBe('Procesando');
    expect(text('.activity-chip')).toBe('En curso');
    expect(text('.activity-meta')).toContain('3 archivos');
    expect(text('.activity-meta')).toContain('~5s restantes');
    expect(
      compiled.querySelector('.activity-progress')?.getAttribute('aria-label'),
    ).toBe('Progreso de Unir PDF');
    expect(text('.activity-cancel')).toBe('Cancelar');
    expect(text('.activity-open')).toBe('Abrir');
  });

  it('cancels an active task through the service', () => {
    const task = activityTask({ id: 'job-cancel' });
    activeTasks.set([task]);
    open();

    (fixture.nativeElement.querySelector('.activity-cancel') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(cancelTask).toHaveBeenCalledWith(task);
  });

  it('opens the owning route and closes the panel', () => {
    const navigate = spyOn(router, 'navigateByUrl').and.resolveTo(true);
    activeTasks.set([activityTask({ route: '/pdf-merge' })]);
    open();

    (fixture.nativeElement.querySelector('.activity-open') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(navigate).toHaveBeenCalledWith('/pdf-merge');
    expect(closePanel).toHaveBeenCalled();
    expect(fixture.nativeElement.querySelector('.activity-drawer')).toBeNull();
  });

  it('clears finished tasks through the service', () => {
    recentTasks.set([
      activityTask({ id: 'job-done', status: 'success', endedAt: Date.now() }),
    ]);
    open();

    (fixture.nativeElement.querySelector('.activity-clear') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(clearFinished).toHaveBeenCalled();
  });

  it('closes with Escape while open', () => {
    open();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(closePanel).toHaveBeenCalled();
    expect(panelOpen()).toBeFalse();
  });
});
