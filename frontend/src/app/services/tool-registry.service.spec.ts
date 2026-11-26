import { TestBed } from '@angular/core/testing';

import { routes } from '../app.routes';
import { I18nService } from './i18n.service';
import { PREFERENCES_STORAGE_KEY } from './preferences.service';
import { ToolRegistryService } from './tool-registry.service';

describe('ToolRegistryService', () => {
  let service: ToolRegistryService;

  beforeEach(() => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    TestBed.configureTestingModule({});
    service = TestBed.inject(ToolRegistryService);
  });

  afterEach(() => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
  });

  it('exposes the twenty-seven conversion tools', () => {
    expect(service.tools.length).toBe(27);
  });

  it('exposes thirty-two navigation entries', () => {
    expect(service.navigation.length).toBe(32);
    expect(service.navigation[0].route).toBe('/');
    expect(service.navigation[service.navigation.length - 3].route).toBe(
      '/automations',
    );
    expect(service.navigation[service.navigation.length - 2].route).toBe(
      '/duplicates',
    );
    expect(service.navigation[service.navigation.length - 1].route).toBe(
      '/settings',
    );
  });

  it('keeps routes and ids unique', () => {
    const routesSeen = new Set(service.navigation.map((item) => item.route));
    const idsSeen = new Set(service.navigation.map((item) => item.id));

    expect(routesSeen.size).toBe(service.navigation.length);
    expect(idsSeen.size).toBe(service.navigation.length);
  });

  it('resolves entries by route, including Home and System Status', () => {
    expect(service.findByRoute('/')?.name).toBe('Home');
    expect(service.findByRoute('/image-converter')?.name).toBe('Image Convert');
    expect(service.findByRoute('/pdf-ocr')?.name).toBe('PDF OCR');
    expect(service.findByRoute('/pdf-ocr')?.navName).toBe('OCR');
    expect(service.findByRoute('/pdf-ocr')?.category).toBe('pdf');
    expect(service.findByRoute('/video-transcribe')?.name).toBe(
      'Video Transcribe',
    );
    expect(service.findByRoute('/video-transcribe')?.navName).toBe(
      'Transcribe',
    );
    expect(service.findByRoute('/video-transcribe')?.category).toBe('video');
    expect(service.findByRoute('/tool-catalog')?.name).toBe('System Status');
    expect(service.findByRoute('/automations')?.name).toBe('Automations');
    expect(service.findByRoute('/settings')?.name).toBe('Settings');
    expect(service.findByRoute('/unknown-route')).toBeNull();
    expect(service.findByRoute(undefined)).toBeNull();
  });

  it('normalises URLs with queries, hashes and trailing slashes', () => {
    expect(service.findByRoute('/pdf-merge?mode=batch#output')?.name).toBe('PDF Merge');
    expect(service.findByRoute('/pdf-merge/')?.name).toBe('PDF Merge');
    expect(service.findByRoute('pdf-merge')?.name).toBe('PDF Merge');
  });

  it('only treats conversion entries as tools', () => {
    const home = service.findByRoute('/');
    const system = service.findByRoute('/tool-catalog');
    const automations = service.findByRoute('/automations');
    const duplicates = service.findByRoute('/duplicates');
    const settings = service.findByRoute('/settings');
    const tool = service.findByRoute('/video-trim');
    const ocr = service.findByRoute('/pdf-ocr');
    const transcribe = service.findByRoute('/video-transcribe');

    expect(home && service.isTool(home)).toBeFalse();
    expect(system && service.isTool(system)).toBeFalse();
    expect(automations && service.isTool(automations)).toBeFalse();
    expect(duplicates && service.isTool(duplicates)).toBeFalse();
    expect(settings && service.isTool(settings)).toBeFalse();
    expect(tool && service.isTool(tool)).toBeTrue();
    expect(ocr && service.isTool(ocr)).toBeTrue();
    expect(transcribe && service.isTool(transcribe)).toBeTrue();
  });

  it('groups every navigation entry exactly once, with Recipes after Workspace', () => {
    const toolGroups = service.navGroups.filter((group) => group.id !== 'recipes');
    const grouped = toolGroups.reduce((total, group) => total + group.items.length, 0);

    expect(service.navGroups.length).toBe(8);
    expect(grouped).toBe(service.navigation.length);
    expect(service.navGroups.map((group) => group.label)).toEqual([
      'Workspace',
      'Recipes',
      'Image',
      'PDF',
      'Documents',
      'Video',
      'Audio',
      'System',
    ]);
    expect(
      service.navGroups.find((group) => group.id === 'recipes')?.items.length,
    ).toBe(5);
    expect(
      service.navGroups.find((group) => group.id === 'image')?.items.length,
    ).toBe(7);
    expect(
      service.navGroups.find((group) => group.id === 'pdf')?.items.length,
    ).toBe(11);
    expect(
      service.navGroups.find((group) => group.id === 'video')?.items.length,
    ).toBe(4);
    expect(
      service.navGroups.find((group) => group.id === 'audio')?.items.length,
    ).toBe(3);
  });

  it('registers the three audio tools with their manifest routes and formats', () => {
    const audioGroup = service.navGroups.find((group) => group.id === 'audio');
    expect(audioGroup?.label).toBe('Audio');
    expect(audioGroup?.items.map((item) => item.id)).toEqual([
      'tool.audio.extract',
      'tool.audio.convert',
      'tool.audio.normalize',
    ]);

    const extract = service.findByRoute('/audio-extract');
    expect(extract?.id).toBe('tool.audio.extract');
    expect(extract?.category).toBe('audio');
    expect(extract?.formats).toEqual(['MP3', 'M4A', 'OPUS', 'WAV', 'FLAC']);

    const convert = service.findByRoute('/audio-convert');
    expect(convert?.id).toBe('tool.audio.convert');
    expect(convert?.category).toBe('audio');
    expect(convert?.formats).toEqual(['MP3', 'M4A', 'OPUS', 'WAV', 'FLAC']);

    const normalize = service.findByRoute('/audio-normalize');
    expect(normalize?.id).toBe('tool.audio.normalize');
    expect(normalize?.category).toBe('audio');
    expect(normalize?.formats).toEqual(['MP3', 'M4A', 'OPUS', 'WAV', 'FLAC']);
  });

  it('registers the four PDF page tools in the PDF group', () => {
    const pdfGroup = service.navGroups.find((group) => group.id === 'pdf');
    expect(pdfGroup?.label).toBe('PDF');
    expect(pdfGroup?.items.map((item) => item.id)).toEqual([
      'tool.pdf.merge',
      'tool.pdf.split',
      'tool.pdf.crop',
      'tool.pdf.optimize',
      'tool.pdf.ocr',
      'tool.pdf.rotate',
      'tool.pdf.remove_pages',
      'tool.pdf.reorder',
      'tool.pdf.extract_images',
      'tool.pdf.images',
      'tool.pdf.to_docx',
    ]);

    const rotate = service.findByRoute('/pdf-rotate');
    expect(rotate?.id).toBe('tool.pdf.rotate');
    expect(rotate?.category).toBe('pdf');
    expect(rotate?.formats).toEqual(['PDF']);

    const removePages = service.findByRoute('/pdf-remove-pages');
    expect(removePages?.id).toBe('tool.pdf.remove_pages');
    expect(removePages?.category).toBe('pdf');
    expect(removePages?.formats).toEqual(['PDF']);

    const reorder = service.findByRoute('/pdf-reorder');
    expect(reorder?.id).toBe('tool.pdf.reorder');
    expect(reorder?.category).toBe('pdf');
    expect(reorder?.formats).toEqual(['PDF']);

    const extractImages = service.findByRoute('/pdf-extract-images');
    expect(extractImages?.id).toBe('tool.pdf.extract_images');
    expect(extractImages?.category).toBe('pdf');
    expect(extractImages?.formats).toEqual([
      'PNG',
      'JPG',
      'JPEG',
      'TIF',
      'TIFF',
      'JP2',
    ]);
  });

  it('registers the duplicates page as a non-tool system entry', () => {
    const systemGroup = service.navGroups.find((group) => group.id === 'system');
    expect(systemGroup?.items.map((item) => item.id)).toEqual([
      'system.status',
      'system.automations',
      'system.duplicates',
      'system.settings',
    ]);

    const duplicates = service.findByRoute('/duplicates');
    expect(duplicates?.id).toBe('system.duplicates');
    expect(duplicates?.category).toBe('system');
    expect(duplicates?.formats).toEqual([]);
    expect(service.findById('system.duplicates')?.route).toBe('/duplicates');
  });

  it('exposes the five recipes as navigation entries outside the tool counts', () => {
    expect(service.recipes.length).toBe(5);
    expect(service.recipeNav.length).toBe(5);
    expect(service.navigation.length).toBe(32);
    expect(service.tools.length).toBe(27);
  });

  it('resolves recipes by route', () => {
    const emailPdf = service.findRecipeByRoute('/recipe/email-pdf');

    expect(emailPdf?.id).toBe('recipe.email_pdf');
    expect(emailPdf?.fallback.name).toBe('Email PDF');
    expect(service.findRecipeByRoute('/recipe/email-pdf?x=1')?.id).toBe(
      'recipe.email_pdf',
    );
    expect(service.findRecipeByRoute('/unknown')).toBeNull();
    expect(service.findRecipeByRoute(undefined)).toBeNull();
  });

  it('labels categories', () => {
    expect(service.categoryLabel('docs')).toBe('Documents');
    expect(service.categoryLabel('pdf')).toBe('PDF');
  });

  it('keeps English overlays byte-identical to the canonical config', () => {
    for (const entry of service.navigation) {
      expect(service.toolName(entry))
        .withContext(`name for ${entry.id}`)
        .toBe(entry.name);
      expect(service.navName(entry))
        .withContext(`navName for ${entry.id}`)
        .toBe(entry.navName);
      expect(service.toolDescription(entry))
        .withContext(`description for ${entry.id}`)
        .toBe(entry.description);
    }

    for (const entry of service.recipeNav) {
      expect(service.toolName(entry))
        .withContext(`name for ${entry.id}`)
        .toBe(entry.name);
      expect(service.navName(entry))
        .withContext(`navName for ${entry.id}`)
        .toBe(entry.navName);
      expect(service.toolDescription(entry))
        .withContext(`description for ${entry.id}`)
        .toBe(entry.description);
    }

    for (const recipe of service.recipes) {
      expect(service.recipeName(recipe))
        .withContext(`recipe name for ${recipe.id}`)
        .toBe(recipe.fallback.name);
      expect(service.recipeDescription(recipe))
        .withContext(`recipe description for ${recipe.id}`)
        .toBe(recipe.fallback.description);
    }
  });

  it('resolves English overlays for tools, recipes and groups by default', () => {
    const merge = service.findByRoute('/pdf-merge');
    const emailPdf = service.findRecipeByRoute('/recipe/email-pdf');
    const recipesGroup = service.navGroups.find((group) => group.id === 'recipes');

    expect(merge && service.toolName(merge)).toBe('PDF Merge');
    expect(merge && service.navName(merge)).toBe('Merge');
    expect(merge && service.toolDescription(merge)).toBe(
      'Combine several PDF files into one document in the order you choose.',
    );
    expect(service.categoryLabel('pdf')).toBe('PDF');
    expect(recipesGroup && service.groupLabel(recipesGroup)).toBe('Recipes');
    expect(service.recipesLabel()).toBe('Recipes');
    expect(emailPdf && service.recipeName(emailPdf)).toBe('Email PDF');
    expect(emailPdf && service.recipeDescription(emailPdf)).toBe(
      'Merge one or more PDFs and compress the result to a size that is easy to send by email.',
    );
  });

  it('resolves Spanish overlays for tools, recipes and groups', () => {
    TestBed.inject(I18nService).setLocale('es');

    const merge = service.findByRoute('/pdf-merge');
    const emailPdf = service.findRecipeByRoute('/recipe/email-pdf');
    const recipesGroup = service.navGroups.find((group) => group.id === 'recipes');
    const pdfGroup = service.navGroups.find((group) => group.id === 'pdf');
    const rotate = service.findByRoute('/pdf-rotate');
    const removePages = service.findByRoute('/pdf-remove-pages');
    const reorder = service.findByRoute('/pdf-reorder');
    const extractImages = service.findByRoute('/pdf-extract-images');

    expect(merge && service.toolName(merge)).toBe('Unir PDF');
    expect(merge && service.navName(merge)).toBe('Unir');
    expect(merge && service.toolDescription(merge)).toBe(
      'Combina varios archivos PDF en un solo documento en el orden que elijas.',
    );
    expect(rotate && service.toolName(rotate)).toBe('Rotar PDF');
    expect(rotate && service.navName(rotate)).toBe('Rotar');
    expect(removePages && service.toolName(removePages)).toBe(
      'Eliminar páginas de PDF',
    );
    expect(removePages && service.navName(removePages)).toBe('Eliminar páginas');
    expect(reorder && service.toolName(reorder)).toBe(
      'Reordenar páginas de PDF',
    );
    expect(reorder && service.navName(reorder)).toBe('Reordenar');
    expect(extractImages && service.toolName(extractImages)).toBe(
      'Extraer imágenes de PDF',
    );
    expect(extractImages && service.navName(extractImages)).toBe(
      'Extraer imágenes',
    );
    expect(service.categoryLabel('docs')).toBe('Documentos');
    expect(recipesGroup && service.groupLabel(recipesGroup)).toBe('Recetas');
    expect(pdfGroup && service.groupLabel(pdfGroup)).toBe('PDF');
    expect(service.recipesLabel()).toBe('Recetas');
    expect(emailPdf && service.recipeName(emailPdf)).toBe('PDF por email');
    expect(emailPdf && service.recipeDescription(emailPdf)).toBe(
      'Une uno o varios PDF y comprime el resultado a un tamaño fácil de enviar por correo electrónico.',
    );
  });

  it('matches every tool route declared in app.routes.ts', () => {
    const declaredRoutes = new Set(
      routes
        .filter((route) => route.path !== '**')
        .map((route) => (route.path === '' ? '/' : `/${route.path}`)),
    );

    for (const tool of service.tools) {
      expect(declaredRoutes.has(tool.route))
        .withContext(`route ${tool.route} is declared in app.routes.ts`)
        .toBeTrue();
    }

    for (const entry of service.navigation) {
      expect(declaredRoutes.has(entry.route))
        .withContext(`route ${entry.route} is declared in app.routes.ts`)
        .toBeTrue();
    }

    for (const recipe of service.recipeNav) {
      expect(declaredRoutes.has(recipe.route))
        .withContext(`recipe route ${recipe.route} is declared in app.routes.ts`)
        .toBeTrue();
    }
  });
});
