import type { TranslationKey } from '../en';
import type { DictionaryValue } from '../types';

import { categoriesEs } from './categories';
import { homeEs } from './home';
import { paletteEs } from './palette';
import { recipesEs } from './recipes';
import { settingsEs } from './settings';
import { shellEs } from './shell';
import { toolsEs } from './tools';

export const es: Record<TranslationKey, DictionaryValue> = {
  ...shellEs,
  ...settingsEs,
  ...categoriesEs,
  ...toolsEs,
  ...paletteEs,
  ...homeEs,
  ...recipesEs,
};
