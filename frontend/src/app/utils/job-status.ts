import type { TranslationKey } from '../i18n';

/**
 * One status vocabulary for the whole workbench.
 *
 * The job API has a single set of statuses, but four surfaces used to carry
 * their own status → label/tone switch: the job card, the activity drawer, the
 * result card and Home. They drifted (a partial run was "success" in one place
 * and "warning" in another), so the mapping lives here once.
 *
 * Labels are per surface on purpose: the copy was already different by
 * product design ("Partial" on Home, "Partial success" elsewhere) and T12 is
 * not allowed to change what the user reads. Tones are per surface for the same
 * reason: the activity drawer treats a partial run as a completed task while
 * the result card still asks for attention.
 */

/** Terminal states: no further progress or state change is expected. */
export const TERMINAL_JOB_STATUSES: ReadonlySet<string> = new Set([
  'success',
  'failed',
  'partial_success',
  'cancelled',
  'interrupted',
]);

export function isTerminalJobStatus(status: string): boolean {
  return TERMINAL_JOB_STATUSES.has(status);
}

export type JobStatusSurface = 'jobCard' | 'activity' | 'resultCard' | 'home';

export type JobStatusActivityTone = 'success' | 'warning' | 'danger' | 'info';
export type JobStatusResultTone = 'success' | 'warning' | 'danger' | 'neutral';
export type JobStatusBadgeClass =
  | 'badge-success'
  | 'badge-warning'
  | 'badge-danger'
  | 'badge-neutral';

export interface JobStatusDefinition {
  /**
   * Copy per surface. A surface left out keeps its previous fallback (the raw
   * status for Home and the result card, a stage label for the job card).
   */
  labels: Partial<Record<JobStatusSurface, TranslationKey>>;
  tones: {
    activity: JobStatusActivityTone;
    resultCard: JobStatusResultTone;
    home: JobStatusBadgeClass;
  };
}

export const JOB_STATUS_VOCABULARY: Readonly<
  Record<string, JobStatusDefinition>
> = {
  queued: {
    labels: {
      jobCard: 'jobCard.stage.queued',
      activity: 'activity.status.queued',
    },
    tones: {
      activity: 'info',
      resultCard: 'neutral',
      home: 'badge-neutral',
    },
  },
  running: {
    labels: {
      jobCard: 'jobCard.stage.running',
      activity: 'activity.status.running',
    },
    tones: {
      activity: 'info',
      resultCard: 'neutral',
      home: 'badge-neutral',
    },
  },
  success: {
    labels: {
      jobCard: 'jobCard.stage.done',
      activity: 'activity.status.success',
      resultCard: 'home.status.success',
      home: 'home.status.success',
    },
    tones: {
      activity: 'success',
      resultCard: 'success',
      home: 'badge-success',
    },
  },
  partial_success: {
    labels: {
      jobCard: 'jobCard.stage.partial',
      activity: 'activity.status.partial',
      resultCard: 'home.status.partial',
      home: 'home.status.partial',
    },
    tones: {
      activity: 'success',
      resultCard: 'warning',
      home: 'badge-warning',
    },
  },
  failed: {
    labels: {
      jobCard: 'jobCard.stage.failed',
      activity: 'activity.status.failed',
      resultCard: 'home.status.failed',
      home: 'home.status.failed',
    },
    tones: {
      activity: 'danger',
      resultCard: 'danger',
      home: 'badge-danger',
    },
  },
  cancelled: {
    labels: {
      jobCard: 'jobCard.stage.cancelled',
      activity: 'activity.status.cancelled',
      resultCard: 'home.status.cancelled',
      home: 'home.status.cancelled',
    },
    tones: {
      activity: 'warning',
      resultCard: 'neutral',
      home: 'badge-neutral',
    },
  },
  interrupted: {
    labels: {
      jobCard: 'jobCard.stage.interrupted',
      activity: 'activity.status.interrupted',
      resultCard: 'home.status.interrupted',
      home: 'home.status.interrupted',
    },
    tones: {
      activity: 'danger',
      resultCard: 'warning',
      home: 'badge-warning',
    },
  },
};

/** The shared entry for a status, or null for an unknown value. */
export function jobStatusDefinition(
  status: string,
): JobStatusDefinition | null {
  return JOB_STATUS_VOCABULARY[status] ?? null;
}
