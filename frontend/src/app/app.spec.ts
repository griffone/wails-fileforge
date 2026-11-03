import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { App } from './app';

describe('App', () => {
  beforeEach(async () => {
    localStorage.removeItem('ff-recent-tools');

    await TestBed.configureTestingModule({
      imports: [App],
      providers: [provideRouter([])],
    }).compileComponents();
  });

  it('creates the app shell', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    expect(fixture.componentInstance).toBeTruthy();
    expect(fixture.nativeElement.querySelector('.app-frame')).toBeTruthy();
  });

  it('renders the FileForge brand block', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('.rail-brand-wordmark')?.textContent).toContain('FILEFORGE');
    expect(compiled.querySelector('.rail-brand-mark')).toBeTruthy();
  });

  it('opens the command palette with Ctrl+K', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.palette')).toBeTruthy();
  });

  it('opens the command palette with Meta+K', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'K', metaKey: true, bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.palette')).toBeTruthy();
  });

  it('closes the command palette with Escape', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.palette')).toBeTruthy();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.palette')).toBeNull();
  });
});
