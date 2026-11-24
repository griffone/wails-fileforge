import { categoriesEn } from './categories';
import { commonEn } from './common';
import { fileDropEn } from './file-drop';
import { fileQueueEn } from './file-queue';
import { homeEn } from './home';
import { outputPlanEn } from './output-plan-card';
import { paletteEn } from './palette';
import { recipeRunEn } from './recipe-run';
import { recipesEn } from './recipes';
import { resultCardEn } from './result-card';
import { settingsEn } from './settings';
import { shellEn } from './shell';
import { toolErrorEn } from './tool-error';
import { toolsEn } from './tools';

export const en = {
  ...commonEn,
  ...shellEn,
  ...settingsEn,
  ...categoriesEn,
  ...toolsEn,
  ...paletteEn,
  ...homeEn,
  ...recipesEn,
  ...recipeRunEn,
  ...fileDropEn,
  ...fileQueueEn,
  ...outputPlanEn,
  ...resultCardEn,
  ...toolErrorEn,
} as const;

export type TranslationKey = keyof typeof en;
