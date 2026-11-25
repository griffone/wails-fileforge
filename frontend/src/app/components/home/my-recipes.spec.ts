import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { Home } from './home';
import {
  Recipe,
  RecipeOptionKind,
  SaveUserRecipeRequest,
  Wails,
} from '../../services/wails';

const MEGABYTE = 1024 * 1024;
const USER_RECIPE_ID = 'recipe.user.1';

const emailRecipe: Recipe = {
  id: 'recipe.email_pdf',
  name: 'Email PDF',
  description: 'Merge PDFs and compress them to an email-friendly size.',
  category: 'documents',
  inputKind: 'pdf',
  acceptExtensions: ['pdf'],
  maxInputs: 0,
  highlights: ['Merge when needed', 'Target size'],
  steps: [
    {
      id: 'merge',
      name: 'Merge PDFs',
      toolId: 'tool.pdf.merge',
      detail: '',
    },
    {
      id: 'optimize',
      name: 'Optimize to target',
      toolId: 'tool.pdf.optimize',
      detail: '',
    },
  ],
  defaultTargetSizeBytes: 10 * MEGABYTE,
  duplicable: true,
  optionSchema: [
    {
      key: 'targetSizeBytes',
      label: 'Target size',
      kind: 'integer' as RecipeOptionKind,
      unit: 'MB',
      min: 1 * MEGABYTE,
      max: 100 * MEGABYTE,
      scale: MEGABYTE,
      default: 10 * MEGABYTE,
      help: 'Best effort: the output may stay above the target.',
    },
  ],
};

const webRecipe: Recipe = {
  id: 'recipe.web_image_set',
  name: 'Web image set',
  description: 'Export responsive WebP and JPEG variants.',
  category: 'image',
  inputKind: 'image',
  acceptExtensions: ['png', 'jpg'],
  maxInputs: 1,
  highlights: ['WebP + JPEG'],
  steps: [
    {
      id: 'build',
      name: 'Build variants',
      toolId: 'tool.image.convert',
      detail: '',
    },
  ],
  defaultTargetSizeBytes: 0,
  duplicable: true,
  optionSchema: [
    {
      key: 'variantPreset',
      label: 'Variant preset',
      kind: 'enum' as RecipeOptionKind,
      default: 'standard',
      values: [
        { value: 'standard', label: '1600 / 800 px' },
        { value: 'compact', label: '1200 / 600 px' },
        { value: 'large', label: '2400 / 1200 px' },
      ],
    },
  ],
};

const socialRecipe: Recipe = {
  id: 'recipe.social_clip',
  name: 'Social clip',
  description: 'Encode one video as MP4 and WebM.',
  category: 'video',
  inputKind: 'video',
  acceptExtensions: ['mp4', 'mov'],
  maxInputs: 1,
  highlights: ['MP4 + WebM'],
  steps: [
    { id: 'mp4', name: 'Encode MP4', toolId: 'tool.video.convert', detail: '' },
  ],
  defaultTargetSizeBytes: 0,
  duplicable: false,
};

const userRecipe: Recipe = {
  ...emailRecipe,
  id: USER_RECIPE_ID,
  name: 'Email under 2 MB',
  userDefined: true,
  baseRecipeId: 'recipe.email_pdf',
  optionOverrides: { targetSizeBytes: 2 * MEGABYTE },
};

function cloneRecipe(recipe: Recipe): Recipe {
  return { ...recipe, steps: [...recipe.steps] };
}

describe('Home (My recipes)', () => {
  let fixture: ComponentFixture<Home>;
  let component: Home;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let savedRecipes: Recipe[];

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function queryAll<T extends Element>(selector: string): T[] {
    return Array.from(
      fixture.nativeElement.querySelectorAll(selector) as NodeListOf<T>,
    );
  }

  function cardFor(label: string): HTMLElement | null {
    const cards = queryAll<HTMLElement>('.home-recipe-card');
    return (
      cards.find((card) => card.textContent?.includes(label)) ?? null
    );
  }

  function inputValue(element: HTMLInputElement, value: string): void {
    element.value = value;
    element.dispatchEvent(new Event('input', { bubbles: true }));
    fixture.detectChanges();
  }

  async function openRecipeDialog(): Promise<void> {
    const duplicate = query<HTMLButtonElement>('.recipe-duplicate');
    if (!duplicate) {
      throw new Error('Duplicate button not found');
    }
    duplicate.click();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.clear();
    localStorage.setItem('ff-preferences', JSON.stringify({ language: 'en' }));
    savedRecipes = [];

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'listTools',
      'isRuntimeAvailable',
      'openPath',
      'revealInFolder',
      'getAutomationStatus',
      'listRecipes',
      'saveUserRecipe',
      'renameUserRecipe',
      'deleteUserRecipe',
    ]);

    wailsSpy.listTools.and.resolveTo({
      success: true,
      message: 'ok',
      tools: [],
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
    wailsSpy.listRecipes.and.callFake(() =>
      Promise.resolve({
        success: true,
        message: 'ok',
        recipes: [
          cloneRecipe(emailRecipe),
          cloneRecipe(webRecipe),
          cloneRecipe(socialRecipe),
          ...savedRecipes,
        ],
      }),
    );
    wailsSpy.saveUserRecipe.and.resolveTo({
      success: true,
      message: 'user recipe saved',
      recipe: cloneRecipe(userRecipe),
    });
    wailsSpy.renameUserRecipe.and.resolveTo({
      success: true,
      message: 'user recipe renamed',
      recipe: { ...cloneRecipe(userRecipe), name: 'Renamed' },
    });
    wailsSpy.deleteUserRecipe.and.resolveTo({
      success: true,
      message: 'user recipe deleted',
    });

    await TestBed.configureTestingModule({
      imports: [Home],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(Home);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    localStorage.clear();
  });

  it('offers duplicate only for built-in recipes that declare a schema', async () => {
    const emailCard = cardFor('Email PDF');
    const socialCard = cardFor('Social clip');

    expect(emailCard?.querySelector('.recipe-duplicate')).not.toBeNull();
    expect(socialCard?.querySelector('.recipe-duplicate')).toBeNull();
  });

  it('renders the declared schema in the duplicate dialog with unit and bounds', async () => {
    await openRecipeDialog();

    const title = query<HTMLElement>('#recipe-dialog-title');
    expect(title?.textContent).toContain('Duplicate recipe');

    const target = query<HTMLInputElement>('#recipe-option-targetSizeBytes');
    expect(target).not.toBeNull();
    expect(target?.value).toBe('10');
    expect(target?.getAttribute('min')).toBe('1');
    expect(target?.getAttribute('max')).toBe('100');
    expect(fixture.nativeElement.textContent).toContain('MB');
  });

  it('renders an enum preset field for the Web image set duplicate', async () => {
    const webCard = cardFor('Web image set');
    const duplicate = webCard?.querySelector(
      '.recipe-duplicate',
    ) as HTMLButtonElement | null;
    duplicate?.click();
    fixture.detectChanges();

    const preset = query<HTMLSelectElement>('#recipe-option-variantPreset');
    expect(preset).not.toBeNull();
    expect(preset?.value).toBe('standard');
    expect(preset?.querySelectorAll('option').length).toBe(3);
    expect(fixture.nativeElement.textContent).toContain('1200 / 600 px');
  });

  it('blocks saving when an integer override is out of bounds', async () => {
    await openRecipeDialog();

    const target = query<HTMLInputElement>(
      '#recipe-option-targetSizeBytes',
    ) as HTMLInputElement;
    inputValue(target, '0');

    expect(fixture.nativeElement.textContent).toContain(
      'must be between 1 and 100 MB',
    );
    const save = query<HTMLButtonElement>('.recipe-dialog-save') as HTMLButtonElement;
    expect(save.disabled).toBeTrue();

    save.click();
    fixture.detectChanges();
    await fixture.whenStable();

    expect(wailsSpy.saveUserRecipe).not.toHaveBeenCalled();
  });

  it('saves a duplicate and lists it under My recipes', async () => {
    await openRecipeDialog();

    const name = query<HTMLInputElement>('#recipe-dialog-name') as HTMLInputElement;
    inputValue(name, 'Email under 5 MB');
    const target = query<HTMLInputElement>(
      '#recipe-option-targetSizeBytes',
    ) as HTMLInputElement;
    inputValue(target, '5');

    savedRecipes = [
      {
        ...cloneRecipe(userRecipe),
        name: 'Email under 5 MB',
        optionOverrides: { targetSizeBytes: 5 * MEGABYTE },
      },
    ];

    const save = query<HTMLButtonElement>('.recipe-dialog-save') as HTMLButtonElement;
    save.click();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.saveUserRecipe).toHaveBeenCalledTimes(1);
    const request = wailsSpy.saveUserRecipe.calls.mostRecent()
      .args[0] as SaveUserRecipeRequest;
    expect(request.name).toBe('Email under 5 MB');
    expect(request.id).toBe('');
    expect(request.baseRecipeId).toBe('recipe.email_pdf');
    expect(request.options['targetSizeBytes']).toBe(5 * MEGABYTE);

    const myRecipes = query<HTMLElement>('.home-user-recipes');
    expect(myRecipes).not.toBeNull();
    expect(myRecipes?.textContent).toContain('Email under 5 MB');
  });

  it('links a saved user recipe to its run page', async () => {
    savedRecipes = [cloneRecipe(userRecipe)];
    await component['loadRecipes']();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const runLink = query<HTMLAnchorElement>(
      '.home-user-recipe-card .recipe-run',
    );
    expect(runLink?.getAttribute('href')).toBe(`/recipe/user/${USER_RECIPE_ID}`);
  });

  it('renames a user recipe through the same dialog', async () => {
    savedRecipes = [cloneRecipe(userRecipe)];
    await component['loadRecipes']();
    fixture.detectChanges();
    await fixture.whenStable();

    const rename = query<HTMLButtonElement>('.recipe-rename') as HTMLButtonElement;
    rename.click();
    fixture.detectChanges();

    const name = query<HTMLInputElement>('#recipe-dialog-name') as HTMLInputElement;
    inputValue(name, 'Renamed');

    const save = query<HTMLButtonElement>('.recipe-dialog-save') as HTMLButtonElement;
    save.click();
    fixture.detectChanges();
    await fixture.whenStable();

    expect(wailsSpy.renameUserRecipe).toHaveBeenCalledWith(
      USER_RECIPE_ID,
      'Renamed',
    );
  });

  async function loadUserRecipe(): Promise<void> {
    savedRecipes = [cloneRecipe(userRecipe)];
    await component['loadRecipes']();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  async function openDeleteConfirm(): Promise<HTMLButtonElement> {
    const remove = query<HTMLButtonElement>(
      '.recipe-delete',
    ) as HTMLButtonElement;
    remove.focus();
    remove.click();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    return remove;
  }

  it('shows a blocking delete confirmation that names the recipe and focuses Cancel', async () => {
    await loadUserRecipe();
    await openDeleteConfirm();

    const dialog = query<HTMLElement>('#recipe-delete-title');
    expect(dialog?.textContent).toContain('Delete recipe?');

    const body = query<HTMLElement>('#recipe-delete-body')?.textContent ?? '';
    expect(body).toContain(userRecipe.name);
    expect(body).toContain('permanent');
    expect(body).toContain('run history stays available');

    expect(
      document.activeElement,
    ).toBe(query<HTMLButtonElement>('.recipe-delete-cancel'));
  });

  it('cancel leaves the list untouched and makes no backend call', async () => {
    await loadUserRecipe();
    const remove = await openDeleteConfirm();

    const cancel = query<HTMLButtonElement>(
      '.recipe-delete-cancel',
    ) as HTMLButtonElement;
    cancel.click();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.deleteUserRecipe).not.toHaveBeenCalled();
    expect(query('.recipe-delete-dialog')).toBeNull();
    expect(query('.home-user-recipe-card')).not.toBeNull();
    expect(document.activeElement).toBe(remove);
  });

  it('Escape cancels the delete confirmation without calling the backend', async () => {
    await loadUserRecipe();
    await openDeleteConfirm();

    document.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      }),
    );
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.deleteUserRecipe).not.toHaveBeenCalled();
    expect(query('.recipe-delete-dialog')).toBeNull();
    expect(query('.home-user-recipe-card')).not.toBeNull();
  });

  it('confirms the delete exactly once and refreshes the list', async () => {
    await loadUserRecipe();
    await openDeleteConfirm();

    savedRecipes = [];
    const confirm = query<HTMLButtonElement>(
      '.recipe-delete-confirm',
    ) as HTMLButtonElement;
    confirm.click();
    confirm.click();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.deleteUserRecipe).toHaveBeenCalledTimes(1);
    expect(wailsSpy.deleteUserRecipe).toHaveBeenCalledWith(USER_RECIPE_ID);
    expect(query('.recipe-delete-dialog')).toBeNull();
    expect(query('.home-user-recipe-card')).toBeNull();
  });

  it('shows the dialog copy in Spanish', async () => {
    component.i18n.setLocale('es');
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const duplicate = query<HTMLButtonElement>('.recipe-duplicate');
    duplicate?.click();
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Duplicar receta');
  });
});
