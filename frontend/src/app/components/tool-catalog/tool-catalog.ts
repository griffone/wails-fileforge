import { CommonModule } from '@angular/common';
import { Component, OnInit, inject } from '@angular/core';
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
import { I18nService } from '../../services/i18n.service';
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

  readonly i18n = inject(I18nService);

  tools: ToolCatalogEntry[] = [];
  loading = false;
  errorMessage = '';
  errorFallback = false;
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
    this.errorFallback = false;

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
      this.errorFallback = true;
    } finally {
      this.loading = false;
    }
  }

  /** Localized banner copy: a translated fallback or the backend detail. */
  errorText(): string {
    return this.errorFallback
      ? this.i18n.t('toolCatalog.backendUnavailable')
      : this.errorMessage;
  }

  private friendlyError(message: string): string {
    const text = (message ?? '').trim();
    if (!text || /<\/?[a-z][\s\S]*>/i.test(text) || /Cannot POST|DOCTYPE html/i.test(text)) {
      this.errorFallback = true;
      return '';
    }

    return text.length > 240 ? `${text.slice(0, 237)}…` : text;
  }

  runtimeTitle(): string {
    return this.i18n.t(
      this.runtimeConnected
        ? 'toolCatalog.runtime.connected'
        : 'toolCatalog.runtime.previewOnly',
    );
  }

  runtimeDetail(): string {
    return this.i18n.t(
      this.runtimeConnected
        ? 'toolCatalog.runtime.connectedDetail'
        : 'toolCatalog.runtime.unavailableDetail',
    );
  }

  statusLabel(status: string): string {
    return this.i18n.tOr(`toolCatalog.status.${status}`, status);
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

  /** Registry name when the tool is known, otherwise the backend manifest name. */
  toolName(tool: ToolCatalogEntry): string {
    const definition = this.toolDefinition(tool);
    return definition ? this.registry.toolName(definition) : tool.manifest.name;
  }

  toolDescription(tool: ToolCatalogEntry): string {
    const definition = this.toolDefinition(tool);
    return definition
      ? this.registry.toolDescription(definition)
      : tool.manifest.description;
  }

  toolCategory(tool: ToolCatalogEntry): string {
    const definition = this.toolDefinition(tool);
    return definition
      ? this.registry.categoryLabel(definition.category)
      : tool.manifest.domain;
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
      tool.manifest.supportsSingle ? this.i18n.t('toolCatalog.mode.single') : '',
      tool.manifest.supportsBatch ? this.i18n.t('toolCatalog.mode.batch') : '',
    ].filter(Boolean);
    return modes.length > 0
      ? modes.join(' + ')
      : this.i18n.t('toolCatalog.mode.none');
  }

  trackByToolId(_index: number, tool: ToolCatalogEntry): string {
    return tool.manifest.toolId;
  }
}
