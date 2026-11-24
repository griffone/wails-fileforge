import { categoriesEn } from './categories';
import { paletteEn } from './palette';
import { settingsEn } from './settings';
import { shellEn } from './shell';
import { toolsEn } from './tools';

export const en = {
  ...shellEn,
  ...settingsEn,
  ...categoriesEn,
  ...toolsEn,
  ...paletteEn,
} as const;

export type TranslationKey = keyof typeof en;
