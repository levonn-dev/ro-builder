// Class-load lock-in checks for the expanded classes (Taekwon path + Ninja /
// Gunslinger). See classes.shared.ts for the shared class lists, tier-sharding
// rationale, and check logic.
import { runClassLoadChecks, EXPANDED } from "./classes.shared.ts";

runClassLoadChecks(EXPANDED);
