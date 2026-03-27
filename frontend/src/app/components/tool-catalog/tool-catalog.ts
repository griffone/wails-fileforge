import { Component, OnInit } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterLink } from '@angular/router';
import { ToolCatalogEntryV1, Wails } from '../../services/wails';

@Component({
  selector: 'app-tool-catalog',
  standalone: true,
  imports: [CommonModule, RouterLink],
  templateUrl: './tool-catalog.html',
  styleUrl: './tool-catalog.css',
})
export class ToolCatalog implements OnInit {
  tools: ToolCatalogEntryV1[] = [];
  loading = false;
  errorMessage = '';

  constructor(public readonly wailsService: Wails) {}

  async ngOnInit(): Promise<void> {
    await this.loadTools();
  }

  async loadTools(): Promise<void> {
    this.loading = true;
    this.errorMessage = '';

    try {
      const response = await this.wailsService.listToolsV1();
      if (!response.success) {
        this.errorMessage = response.message;
        this.tools = [];
        return;
      }

      this.tools = response.tools;
    } finally {
      this.loading = false;
    }
  }

  getStatusClass(status: string): string {
    switch (status) {
      case 'enabled':
        return 'bg-green-100 text-green-800';
      case 'degraded':
        return 'bg-amber-100 text-amber-800';
      case 'disabled':
        return 'bg-red-100 text-red-800';
      default:
        return 'bg-gray-100 text-gray-700';
    }
  }

  routeForTool(toolId: string): string | null {
    if (toolId === 'tool.image.crop') {
      return '/image-crop';
    }

    if (toolId === 'tool.pdf.merge') {
      return '/pdf-merge';
    }

    if (toolId === 'tool.pdf.split') {
      return '/pdf-split';
    }

    if (toolId === 'tool.pdf.crop') {
      return '/pdf-crop';
    }

    if (toolId === 'tool.video.convert') {
      return '/video-convert';
    }

    if (toolId === 'tool.video.trim') {
      return '/video-trim';
    }

    if (toolId === 'tool.video.merge') {
      return '/video-merge';
    }

    return null;
  }
}
