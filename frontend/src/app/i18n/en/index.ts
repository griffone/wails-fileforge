import { categoriesEn } from './categories';
import { homeEn } from './home';
import { paletteEn } from './palette';
import { recipeRunEn } from './recipe-run';
import { recipesEn } from './recipes';
import { settingsEn } from './settings';
import { shellEn } from './shell';
import { toolsEn } from './tools';

export const en = {
  ...shellEn,
  ...settingsEn,
  ...categoriesEn,
  ...toolsEn,
  ...paletteEn,
  ...homeEn,
  ...recipesEn,
  ...recipeRunEn,
} as const;

export type TranslationKey = keyof typeof en;
