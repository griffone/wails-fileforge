import { categoriesEn } from './categories';
import { homeEn } from './home';
import { paletteEn } from './palette';
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
} as const;

export type TranslationKey = keyof typeof en;
