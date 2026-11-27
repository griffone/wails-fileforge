import { Component, inject } from '@angular/core';
import { LucideAngularModule, ShieldCheck } from 'lucide-angular';

import { I18nService } from '../../services/i18n.service';

/**
 * Audio-in-video sync page shell. The timeline editor lands in the next task;
 * this shell keeps the route, the registry and the navigation consistent and
 * documents the sign convention the editor will use.
 */
@Component({
  selector: 'app-video-audio-sync',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './video-audio-sync.html',
  styleUrl: './video-audio-sync.css',
})
export class VideoAudioSync {
  readonly i18n = inject(I18nService);
  readonly shieldIcon = ShieldCheck;
}
