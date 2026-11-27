import { Component } from '@angular/core';
import {
  FolderOpen,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { OpenWindowAction } from '../open-window-action/open-window-action';
import { AudioConversionPage } from './audio-conversion-page';
import { AUDIO_EXTRACT_COPY } from './audio-copy';
import { audioToolPageDeps } from './audio-tool-page';
import {
  AUDIO_EXTRACT_INPUT_ACCEPT,
  AUDIO_EXTRACT_INPUT_EXTENSIONS,
} from './audio-tool-config';

const AUDIO_EXTRACT_TOOL_ID = 'tool.audio.extract';

@Component({
  selector: 'app-audio-extract',
  standalone: true,
  imports: [
    LucideAngularModule,
    OpenWindowAction,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './audio-conversion.html',
  styleUrl: './audio-tools.css',
})
export class AudioExtract extends AudioConversionPage {
  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  constructor() {
    super(audioToolPageDeps(), {
      toolId: AUDIO_EXTRACT_TOOL_ID,
      route: '/audio-extract',
      copy: AUDIO_EXTRACT_COPY,
      verbPast: 'extracted',
      acceptAttribute: AUDIO_EXTRACT_INPUT_ACCEPT,
      acceptedExtensions: AUDIO_EXTRACT_INPUT_EXTENSIONS,
      optionsSchemaVersion: 1,
    });
  }
}
