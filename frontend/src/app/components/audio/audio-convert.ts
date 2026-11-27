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
import { AUDIO_CONVERT_COPY } from './audio-copy';
import { audioToolPageDeps } from './audio-tool-page';
import {
  AUDIO_INPUT_ACCEPT,
  AUDIO_INPUT_EXTENSIONS,
} from './audio-tool-config';

const AUDIO_CONVERT_TOOL_ID = 'tool.audio.convert';

@Component({
  selector: 'app-audio-convert',
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
export class AudioConvert extends AudioConversionPage {
  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;

  constructor() {
    super(audioToolPageDeps(), {
      toolId: AUDIO_CONVERT_TOOL_ID,
      route: '/audio-convert',
      copy: AUDIO_CONVERT_COPY,
      verbPast: 'converted',
      acceptAttribute: AUDIO_INPUT_ACCEPT,
      acceptedExtensions: AUDIO_INPUT_EXTENSIONS,
      optionsSchemaVersion: 1,
    });
  }
}
