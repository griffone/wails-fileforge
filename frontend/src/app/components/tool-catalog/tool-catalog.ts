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
import { ToolCatalogEntry, Wails } from '../../services/wails';

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

  tools: ToolCatalogEntry[] = [];
  loading = false;
  errorMessage = '';
  runtimeConnected = false;

  constructor(
    public readonly wailsService: Wails,
    public readonly registry: ToolRegistryService,
  ) {
    this.runtimeConnected = wailsService.isRuntimeAvailable();
  }

  async ngOnInit(): Promise<void> {
    await this.loadTools();
  }

  async loadTools(): Promise<void> {
    this.loading = true;
    this.errorMessage = '';

    try {
      const response = await this.wailsService.listTools();
      if (!response.success) {
        this.errorMessage = this.friendlyError(response.message);
        this.tools = [];
        this.runtimeConnected = false;
        return;
      }

      this.tools = response.tools;
      this.runtimeConnected = true;
    } catch {
      this.tools = [];
      this.runtimeConnected = false;
      this.errorMessage =
        'The desktop backend did not respond. Open FileForge in the desktop app, then retry.';
    } finally {
      this.loading = false;
    }
  }

  private friendlyError(message: string): string {
    const text = (message ?? '').trim();
    if (!text || /<\/?[a-z][\s\S]*>/i.test(text) || /Cannot POST|DOCTYPE html/i.test(text)) {
      return 'The desktop backend did not respond. Open FileForge in the desktop app, then retry.';
    }

    return text.length > 240 ? `${text.slice(0, 237)}…` : text;
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
  toolDefinition(tool: ToolCatalogEntry): ToolDefinition | null {
    return this.registry.findById(tool.manifest.toolId);
  }

  toolIcon(tool: ToolCatalogEntry): LucideIconData {
    return this.toolDefinition(tool)?.icon ?? this.activityIcon;
  }

  categoryTone(tool: ToolCatalogEntry): string {
    return this.toolDefinition(tool)?.category ?? 'system';
  }

  routeForTool(toolId: string): string | null {
    return this.registry.findById(toolId)?.route ?? null;
  }

  ioLabel(tool: ToolCatalogEntry): string {
    const input = (tool.manifest.inputExtensions ?? []).join(', ') || '—';
    const output = (tool.manifest.outputExtensions ?? []).join(', ') || '—';
    return `${input} → ${output}`;
  }

  modeLabel(tool: ToolCatalogEntry): string {
    const modes = [
      tool.manifest.supportsSingle ? 'single' : '',
      tool.manifest.supportsBatch ? 'batch' : '',
    ].filter(Boolean);
    return modes.length > 0 ? modes.join(' + ') : 'n/a';
  }

  trackByToolId(_index: number, tool: ToolCatalogEntry): string {
    return tool.manifest.toolId;
  }
}
