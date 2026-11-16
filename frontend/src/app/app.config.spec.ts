import { Component, OnInit } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { RouteReuseStrategy, provideRouter, withRouterConfig } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';

import { ReloadRouteReuseStrategy } from './app-route-reuse.strategy';
import { DropIntakeService } from './services/drop-intake.service';

@Component({
  selector: 'app-reload-stub',
  standalone: true,
  template: '{{ label }}',
})
class ReloadStub implements OnInit {
  label = 'empty';

  constructor(private readonly intake: DropIntakeService) {}

  ngOnInit(): void {
    const pending = this.intake.take('/stub');
    if (pending.length > 0) {
      this.label = pending.join(',');
    }
  }
}

describe('same URL intake reload', () => {
  it('recreates the page so queued paths are consumed', async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter(
          [{ path: 'stub', component: ReloadStub }],
          withRouterConfig({ onSameUrlNavigation: 'reload' }),
        ),
        { provide: RouteReuseStrategy, useClass: ReloadRouteReuseStrategy },
      ],
    });

    const harness = await RouterTestingHarness.create('/stub');
    expect(harness.routeNativeElement?.textContent).toContain('empty');

    TestBed.inject(DropIntakeService).queue('/stub', ['/tmp/queued.png']);
    await harness.navigateByUrl('/stub');

    expect(harness.routeNativeElement?.textContent).toContain('/tmp/queued.png');
  });
});
