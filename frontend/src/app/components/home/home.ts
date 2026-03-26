import { Component, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { CommonModule } from '@angular/common';
import { Wails, SupportedFormat } from '../../services/wails';

@Component({
  selector: 'app-home',
  imports: [CommonModule],
  templateUrl: './home.html',
  styleUrl: './home.css',
})
export class Home implements OnInit {
  supportedFormats: SupportedFormat[] = [];
  catalogToolsCount = 0;

  constructor(
    private readonly router: Router,
    private readonly wailsService: Wails
  ) {}

  ngOnInit(): void {
    this.loadData();
  }

  private async loadData(): Promise<void> {
    console.log('Home component initializing...');
    console.log('Runtime available:', this.wailsService.isRuntimeAvailable());

    try {
      this.supportedFormats = await this.wailsService.getSupportedFormats();
      const catalogResponse = await this.wailsService.listToolsV1();
      this.catalogToolsCount = catalogResponse.success
        ? catalogResponse.tools.length
        : 0;
      console.log('Supported formats loaded:', this.supportedFormats);
    } catch (error) {
      console.error('Failed to load supported formats:', error);
      // Agregar formatos por defecto para prueba
      this.supportedFormats = [
        { category: 'img', formats: ['png', 'jpg', 'webp', 'gif'] },
      ];
    }
  }

  isRuntimeAvailable(): boolean {
    return this.wailsService.isRuntimeAvailable();
  }

  navigateToConverter(category: string): void {
    console.log('Navigating to converter:', category);
    if (category === 'img') {
      this.router.navigate(['/image-converter']);
    }
    // Add more converters as they're implemented
  }

  navigateToToolCatalog(): void {
    this.router.navigate(['/tool-catalog']);
  }

  navigateToPdfMerge(): void {
    this.router.navigate(['/pdf-merge']);
  }

  navigateToPdfSplit(): void {
    this.router.navigate(['/pdf-split']);
  }

  navigateToPdfCrop(): void {
    this.router.navigate(['/pdf-crop']);
  }

  navigateToVideoConvert(): void {
    this.router.navigate(['/video-convert']);
  }

  navigateToVideoTrim(): void {
    this.router.navigate(['/video-trim']);
  }
}
