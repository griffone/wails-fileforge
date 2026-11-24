import type { TranslationKey } from '../en';
import type { DictionaryValue } from '../types';

import { categoriesEs } from './categories';
import { commonEs } from './common';
import { fileDropEs } from './file-drop';
import { fileQueueEs } from './file-queue';
import { homeEs } from './home';
import { outputPlanEs } from './output-plan-card';
import { paletteEs } from './palette';
import { recipeRunEs } from './recipe-run';
import { recipesEs } from './recipes';
import { settingsEs } from './settings';
import { shellEs } from './shell';
import { toolsEs } from './tools';

export const es: Record<TranslationKey, DictionaryValue> = {
  ...commonEs,
  ...shellEs,
  ...settingsEs,
  ...categoriesEs,
  ...toolsEs,
  ...paletteEs,
  ...homeEs,
  ...recipesEs,
  ...recipeRunEs,
  ...fileDropEs,
  ...fileQueueEs,
  ...outputPlanEs,
};
