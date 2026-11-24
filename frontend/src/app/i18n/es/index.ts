import type { TranslationKey } from '../en';
import type { DictionaryValue } from '../types';

import { activityEs } from './activity';
import { categoriesEs } from './categories';
import { commonEs } from './common';
import { fileDropEs } from './file-drop';
import { fileQueueEs } from './file-queue';
import { globalDropEs } from './global-drop';
import { homeEs } from './home';
import { imageAnnotateEs } from './image-annotate';
import { imageConverterEs } from './image-converter';
import { imageCropEs } from './image-crop';
import { outputPlanEs } from './output-plan-card';
import { paletteEs } from './palette';
import { recipeRunEs } from './recipe-run';
import { recipesEs } from './recipes';
import { resultCardEs } from './result-card';
import { settingsEs } from './settings';
import { shellEs } from './shell';
import { toastEs } from './toast';
import { toolErrorEs } from './tool-error';
import { toolchainEs } from './toolchain';
import { toolsEs } from './tools';

export const es: Record<TranslationKey, DictionaryValue> = {
  ...commonEs,
  ...shellEs,
  ...settingsEs,
  ...categoriesEs,
  ...toolsEs,
  ...paletteEs,
  ...homeEs,
  ...imageConverterEs,
  ...imageCropEs,
  ...imageAnnotateEs,
  ...recipesEs,
  ...recipeRunEs,
  ...fileDropEs,
  ...fileQueueEs,
  ...outputPlanEs,
  ...resultCardEs,
  ...toolErrorEs,
  ...activityEs,
  ...globalDropEs,
  ...toastEs,
  ...toolchainEs,
};
