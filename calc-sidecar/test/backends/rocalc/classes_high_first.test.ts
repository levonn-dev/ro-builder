// Class-load lock-in checks for the transcendent first-class tier (High
// Swordman, etc.). See classes.shared.ts for the shared class lists,
// tier-sharding rationale, and check logic.
import { runClassLoadChecks, HIGH_FIRST } from "./classes.shared.ts";

runClassLoadChecks(HIGH_FIRST);
