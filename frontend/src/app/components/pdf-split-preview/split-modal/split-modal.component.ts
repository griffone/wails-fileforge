import { Component, Input, Output, EventEmitter } from '@angular/core';

@Component({
  selector: 'app-split-modal',
  template: `
    <div class="modal">
      <h3>Vista previa</h3>
      <ng-content></ng-content>
      <div class="actions">
        <button (click)="confirm()">Split</button>
        <button (click)="cancel()">Cancelar</button>
      </div>
    </div>
  `,
  styles: [
    `.modal { background: white; padding: 16px; border-radius: 8px; } .actions { margin-top: 12px }`,
  ],
  standalone: true,
})
export class SplitModalComponent {
  @Input() open = false;
  @Output() close = new EventEmitter<boolean>();

  confirm() {
    this.close.emit(true);
  }

  cancel() {
    this.close.emit(false);
  }
}
