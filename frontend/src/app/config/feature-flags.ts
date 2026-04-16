// Feature flags for UI/UX overhaul — gated behind a single flag for safe rollout
export const FEATURE_FLAGS = {
  // Toggle the new UI/UX overhaul (FileDrop + JobCard stage-aware enhancements)
  uiux_overhaul_v1: false,
};

// Note: This is intentionally a simple boolean flag for Phase1. For Phase2 we can
// wire a remote-config or persisted preferences provider.
