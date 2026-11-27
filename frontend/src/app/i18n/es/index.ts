import type { TranslationKey } from '../en';
import type { DictionaryValue } from '../types';

import { activityEs } from './activity';
import { audioToolsEs } from './audio-tools';
import { automationsEs } from './automations';
import { categoriesEs } from './categories';
import { commonEs } from './common';
import { contextMenuEs } from './context-menu';
import { destinationPresetsEs } from './destination-presets';
import { docDocxToPdfEs } from './doc-docx-to-pdf';
import { docMdToPdfEs } from './doc-md-to-pdf';
import { duplicatesEs } from './duplicates';
import { fileDropEs } from './file-drop';
import { fileQueueEs } from './file-queue';
import { globalDropEs } from './global-drop';
import { homeEs } from './home';
import { imageAnnotateEs } from './image-annotate';
import { imageBackgroundEs } from './image-background';
import { imageUpscaleEs } from './image-upscale';
import { imageCompressEs } from './image-compress';
import { imageConverterEs } from './image-converter';
import { imageCropEs } from './image-crop';
import { imageOcrEs } from './image-ocr';
import { imageWatermarkEs } from './image-watermark';
import { jobCardEs } from './job-card';
import { outputPlanEs } from './output-plan-card';
import { paletteEs } from './palette';
import { pdfCropEs } from './pdf-crop';
import { pdfFromImagesEs } from './pdf-from-images';
import { pdfMergeEs } from './pdf-merge';
import { pdfOcrEs } from './pdf-ocr';
import { pdfOptimizeEs } from './pdf-optimize';
import { pdfPageOpsEs } from './pdf-page-ops';
import { pdfRendererEs } from './pdf-renderer';
import { pdfSplitEs } from './pdf-split';
import { pdfToDocxEs } from './pdf-to-docx';
import { recipeRunEs } from './recipe-run';
import { recipesEs } from './recipes';
import { resultCardEs } from './result-card';
import { settingsEs } from './settings';
import { shellEs } from './shell';
import { shortcutsEs } from './shortcuts';
import { toastEs } from './toast';
import { toolCatalogEs } from './tool-catalog';
import { toolErrorEs } from './tool-error';
import { toolchainEs } from './toolchain';
import { windowEs } from './window';
import { toolsEs } from './tools';
import { videoConvertEs } from './video-convert';
import { videoMergeEs } from './video-merge';
import { videoTargetEs } from './video-target';
import { videoAudioSyncEs } from './video-audio-sync';
import { videoTranscribeEs } from './video-transcribe';
import { videoTrimEs } from './video-trim';
import { webDownloadEs } from './web-download';

export const es: Record<TranslationKey, DictionaryValue> = {
  ...commonEs,
  ...contextMenuEs,
  ...destinationPresetsEs,
  ...audioToolsEs,
  ...automationsEs,
  ...duplicatesEs,
  ...docDocxToPdfEs,
  ...docMdToPdfEs,
  ...shellEs,
  ...shortcutsEs,
  ...settingsEs,
  ...categoriesEs,
  ...toolsEs,
  ...paletteEs,
  ...homeEs,
  ...imageConverterEs,
  ...imageCropEs,
  ...imageAnnotateEs,
  ...imageCompressEs,
  ...imageWatermarkEs,
  ...imageOcrEs,
  ...imageBackgroundEs,
  ...imageUpscaleEs,
  ...pdfMergeEs,
  ...pdfSplitEs,
  ...pdfCropEs,
  ...pdfPageOpsEs,
  ...pdfOptimizeEs,
  ...pdfOcrEs,
  ...pdfToDocxEs,
  ...pdfFromImagesEs,
  ...recipesEs,
  ...recipeRunEs,
  ...fileDropEs,
  ...fileQueueEs,
  ...outputPlanEs,
  ...jobCardEs,
  ...resultCardEs,
  ...toolErrorEs,
  ...activityEs,
  ...globalDropEs,
  ...toastEs,
  ...toolCatalogEs,
  ...toolchainEs,
  ...videoConvertEs,
  ...videoTrimEs,
  ...videoMergeEs,
  ...videoTargetEs,
  ...videoTranscribeEs,
  ...videoAudioSyncEs,
  ...webDownloadEs,
  ...pdfRendererEs,
  ...windowEs,
};
