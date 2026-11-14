import { RulesPanel } from './RulesPanel';

/** Every policy rule of the host; a rule created here applies to the whole host (req_001 D-15, D-25). */
export default function PolicyRulesPage() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Rules</h1>
      <RulesPanel
        title="All rules"
        intro="Rules decide what the host does with calls by their request types and models: refuse them, allow them only in background hours, give them a priority or narrow their limits. This list holds the rules for the whole host and each grant's own rules; a rule created here is for the whole host. A grant's own rules are made on its consumer's grant page."
      />
    </div>
  );
}
