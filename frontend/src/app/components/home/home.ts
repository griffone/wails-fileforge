import { Component, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { CommonModule } from '@angular/common';
import {
  LucideAngularModule,
  Image,
  FileText,
  Video,
  Music,
} from 'lucide-angular';
import { Wails, SupportedFormat } from '../../services/wails';

@Component({
  selector: 'app-home',
  imports: [CommonModule, LucideAngularModule],
  templateUrl: './home.html',
  styleUrl: './home.css',
})
export class Home implements OnInit {
  readonly ImageIcon = Image;
  readonly FileTextIcon = FileText;
  readonly VideoIcon = Video;
  readonly MusicIcon = Music;
  
  supportedFormats: SupportedFormat[] = [];

  constructor(private router: Router, private wailsService: Wails) {}

  async ngOnInit() {
    try {
      this.supportedFormats = await this.wailsService.getSupportedFormats();
    } catch (error) {
      console.error('Failed to load supported formats:', error);
    }
  }

  navigateToConverter(category: string) {
    if (category === 'img') {
      this.router.navigate(['/image-converter']);
    }
    // Add more converters as they're implemented
  }

  getIconForCategory(category: string): string {
    switch (category) {
      case 'img':
        return 'image';
      case 'doc':
        return 'file-text';
      case 'video':
        return 'video';
      case 'audio':
        return 'music';
      default:
        return 'file';
    }
  }

  getCategoryDisplayName(category: string): string {
    switch (category) {
      case 'img':
        return 'Images';
      case 'doc':
        return 'Documents';
      case 'video':
        return 'Video';
      case 'audio':
        return 'Audio';
      default:
        return category;
    }
  }

  getCategoryDescription(category: string): string {
    switch (category) {
      case 'img':
        return 'Convert between PNG, JPG, WebP, GIF formats';
      case 'doc':
        return 'Convert PDF, DOCX, Markdown files';
      case 'video':
        return 'Convert MP4, WEBM, GIF videos';
      case 'audio':
        return 'Convert MP3, FLAC, OGG audio files';
      default:
        return 'File conversion';
    }
  }
}
