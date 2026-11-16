import { Mail, type LucideIconData } from 'lucide-angular';

/**
 * A curated outcome that chains several tools behind one action.
 *
 * Recipes stay out of the tool registry on purpose: they are outcomes, not
 * tools, and the Home tool count keeps describing the underlying bench.
 */
export interface RecipeDefinition {
  /** Stable identifier; mirrors the backend recipe id. */
  id: string;
  name: string;
  /** One sentence describing only what the recipe actually does. */
  description: string;
  /** Absolute route path, always starting with a slash. */
  route: string;
  /** Accepted inputs, e.g. `1+ PDFs`. */
  inputLabel: string;
  /** Final output formats. */
  formats: string[];
  icon: LucideIconData;
  /** Short capability notes shown on the card. */
  highlights: string[];
}

export const RECIPES: readonly RecipeDefinition[] = [
  {
    id: 'recipe.email_pdf',
    name: 'Email PDF',
    description:
      'Merge your PDFs and compress the result into one file that fits an email limit.',
    route: '/recipe/email-pdf',
    inputLabel: '1+ PDFs',
    formats: ['PDF'],
    icon: Mail,
    highlights: ['Merge when needed', 'Target size', 'Verified + undo'],
  },
];
