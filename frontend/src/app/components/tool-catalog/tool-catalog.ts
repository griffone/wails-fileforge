import { CommonModule } from '@angular/common';
import { Component, OnInit } from '@angular/core';
import { RouterLink } from '@angular/router';
import {
  Activity,
  ArrowUpRight,
  Inbox,
  LucideAngularModule,
  RefreshCw,
  type LucideIconData,
} from 'lucide-angular';

import type { ToolDefinition } from '../../config/tools';
import { ToolRegistryService } from '../../services/tool-registry.service';
import { ToolCatalogEntryV1, Wails } from '../../services/wails';

@Component({
  selector: 'app-tool-catalog',
  standalone: true,
  imports: [CommonModule, RouterLink, LucideAngularModule],
  templateUrl: './tool-catalog.html',
  styleUrl: './tool-catalog.css',
})
export class ToolCatalog implements OnInit {
  readonly activityIcon = Activity;
  readonly refreshIcon = RefreshCw;
  readonly emptyIcon = Inbox;
  readonly arrowIcon = ArrowUpRight;
  readonly skeletonRows = [0, 1, 2, 3];

  tools: ToolCatalogEntryV1[] = [];
  loading = false;
  errorMessage = '';

  constructor(
    public readonly wailsService: Wails,
    public readonly registry: ToolRegistryService,
  ) {}

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
        return 'badge badge-success';
      case 'degraded':
        return 'badge badge-warning';
      case 'disabled':
        return 'badge badge-danger';
      default:
        return 'badge badge-neutral';
    }
  }

  /** Registry definition for a backend tool id, when one exists. */
  toolDefinition(tool: ToolCatalogEntryV1): ToolDefinition | null {
    return this.registry.findById(tool.manifest.toolId);
  }

  toolIcon(tool: ToolCatalogEntryV1): LucideIconData {
    return this.toolDefinition(tool)?.icon ?? this.activityIcon;
  }

  categoryTone(tool: ToolCatalogEntryV1): string {
    return this.toolDefinition(tool)?.category ?? 'system';
  }

  routeForTool(toolId: string): string | null {
    return this.registry.findById(toolId)?.route ?? null;
  }

  ioLabel(tool: ToolCatalogEntryV1): string {
    const input = tool.manifest.inputExtensions.join(', ') || '—';
    const output = tool.manifest.outputExtensions.join(', ') || '—';
    return `${input} → ${output}`;
  }

  modeLabel(tool: ToolCatalogEntryV1): string {
    const modes = [
      tool.manifest.supportsSingle ? 'single' : '',
      tool.manifest.supportsBatch ? 'batch' : '',
    ].filter(Boolean);
    return modes.length > 0 ? modes.join(' + ') : 'n/a';
  }

  trackByToolId(_index: number, tool: ToolCatalogEntryV1): string {
    return tool.manifest.toolId;
  }
}
