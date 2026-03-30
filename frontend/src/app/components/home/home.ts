import { Component, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { CommonModule } from '@angular/common';
import { ToolCatalogEntryV1, Wails } from '../../services/wails';

@Component({
  selector: 'app-home',
  imports: [CommonModule],
  templateUrl: './home.html',
  styleUrl: './home.css',
})
export class Home implements OnInit {
  catalogTools: ToolCatalogEntryV1[] = [];
  catalogToolsCount = 0;

  constructor(
    private readonly router: Router,
    private readonly wailsService: Wails
  ) {}

  ngOnInit(): void {
    this.loadData();
  }

  private async loadData(): Promise<void> {
    try {
      const catalogResponse = await this.wailsService.listToolsV1();
      if (!catalogResponse.success) {
        this.catalogTools = [];
        this.catalogToolsCount = 0;
        return;
      }

      this.catalogTools = catalogResponse.tools;
      this.catalogToolsCount = catalogResponse.tools.length;
    } catch {
      this.catalogTools = [];
      this.catalogToolsCount = 0;
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

  navigateToImageCrop(): void {
    this.router.navigate(['/image-crop']);
  }

  navigateToImageAnnotate(): void {
    this.router.navigate(['/image-annotate']);
  }

  navigateToPdfSplit(): void {
    this.router.navigate(['/pdf-split']);
  }

  navigateToPdfCrop(): void {
    this.router.navigate(['/pdf-crop']);
  }

  navigateToDocMdToPdf(): void {
    this.router.navigate(['/doc-md-to-pdf']);
  }

  navigateToDocDocxToPdf(): void {
    this.router.navigate(['/doc-docx-to-pdf']);
  }

  navigateToVideoConvert(): void {
    this.router.navigate(['/video-convert']);
  }

  navigateToVideoTrim(): void {
    this.router.navigate(['/video-trim']);
  }

  navigateToVideoMerge(): void {
    this.router.navigate(['/video-merge']);
  }
}
